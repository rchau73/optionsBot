package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"optionsbot/internal/config"
	gw "optionsbot/internal/gateway"
)

// ── Mock Deribit server ──────────────────────────────────────────────────────

// mockReply scripts how the mock answers one request.
type mockReply struct {
	result    string // raw JSON result; "" means {}
	err       *gw.RPCError
	delay     time.Duration
	noReply   bool // swallow the request
	closeConn bool // drop the connection instead of replying
}

// mockDeribit is a scriptable JSON-RPC WebSocket server. Unscripted methods
// reply with {} and public/auth replies with a valid token.
type mockDeribit struct {
	srv *httptest.Server

	mu       sync.Mutex
	handlers map[string]func(req gw.JSONRPCRequest) mockReply
	received []gw.JSONRPCRequest
	replied  map[string]int // replies written, per method
	conns    []*mockConn
}

// mockConn pairs a server-side connection with its write lock: gorilla
// connections allow only one concurrent writer.
type mockConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *mockConn) write(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.WriteMessage(websocket.TextMessage, data)
}

func newMockDeribit(t *testing.T) *mockDeribit {
	t.Helper()
	m := &mockDeribit{handlers: map[string]func(gw.JSONRPCRequest) mockReply{}, replied: map[string]int{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.close)
	return m
}

func (m *mockDeribit) url() string { return "ws" + strings.TrimPrefix(m.srv.URL, "http") }

// on scripts the reply for a method.
func (m *mockDeribit) on(method string, fn func(req gw.JSONRPCRequest) mockReply) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method] = fn
}

func (m *mockDeribit) count(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.received {
		if r.Method == method {
			n++
		}
	}
	return n
}

func (m *mockDeribit) repliedTo(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replied[method]
}

func (m *mockDeribit) requests(method string) []gw.JSONRPCRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []gw.JSONRPCRequest
	for _, r := range m.received {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

// dropConnections closes every open client connection, as a network blip would.
func (m *mockDeribit) dropConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		c.ws.Close()
	}
	m.conns = nil
}

// push sends a server-initiated message to every connected client.
func (m *mockDeribit) push(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		c.write([]byte(msg))
	}
}

func (m *mockDeribit) close() {
	m.dropConnections()
	m.srv.Close()
}

func (m *mockDeribit) serve(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := &mockConn{ws: ws}
	m.mu.Lock()
	m.conns = append(m.conns, conn)
	m.mu.Unlock()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var req gw.JSONRPCRequest
		if json.Unmarshal(msg, &req) != nil {
			continue
		}
		m.mu.Lock()
		m.received = append(m.received, req)
		handler := m.handlers[req.Method]
		m.mu.Unlock()

		reply := mockReply{}
		switch {
		case handler != nil:
			reply = handler(req)
		case req.Method == "public/auth":
			reply.result = `{"access_token":"test-token","scope":"trade:read_write"}`
		}

		go func(req gw.JSONRPCRequest, reply mockReply) {
			time.Sleep(reply.delay)
			if reply.closeConn {
				ws.Close()
				return
			}
			if reply.noReply {
				return
			}
			result := reply.result
			if result == "" {
				result = "{}"
			}
			resp := gw.JSONRPCResponse{JsonRPC: "2.0", ID: req.ID, Error: reply.err}
			if reply.err == nil {
				resp.Result = json.RawMessage(result)
			}
			data, _ := json.Marshal(resp)
			conn.write(data)
			m.mu.Lock()
			m.replied[req.Method]++
			m.mu.Unlock()
		}(req, reply)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func testGatewayConfig() *config.Config {
	return &config.Config{
		RateLimit: config.RateLimitConfig{WsNonMatchRPS: 1000, WsMatchRPS: 1000, SafetyFactor: 1, MaxSubscriptions: 1000},
		Retry:     config.RetryConfig{InitialMS: 1, Multiplier: 2, MaxMS: 5, MaxRetries: 3},
		Circuit:   config.CircuitConfig{Threshold: 3, OpenSec: 60},
		Heartbeat: config.HeartbeatConfig{IntervalSec: 60, ReconnectMaxAttempts: 3, ReconnectBackoffBaseMS: 5},
	}
}

func connectGateway(t *testing.T, m *mockDeribit, cfg *config.Config) *gw.Gateway {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	g := gw.New(cfg, gw.WithEndpoint(m.url()))
	if err := g.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	// Let the startup set_heartbeat round-trip finish so its reply cannot
	// interleave with what a test is measuring (e.g. circuit-breaker counts).
	eventually(t, time.Second, "set_heartbeat answered", func() bool { return m.repliedTo("public/set_heartbeat") == 1 })
	if _, err := g.Call(ctx, "public/test", map[string]any{}, gw.PriorityLow); err != nil {
		t.Fatalf("warm-up call: %v", err)
	}
	return g
}

// eventually polls cond until it holds or the timeout elapses.
func eventually(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v: %s", timeout, msg)
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestGateway_ConnectAuthenticates(t *testing.T) {
	m := newMockDeribit(t)
	connectGateway(t, m, testGatewayConfig())
	if n := m.count("public/auth"); n != 1 {
		t.Errorf("auth calls = %d, want 1", n)
	}
}

func TestGateway_ConnectFailsWithoutAccessToken(t *testing.T) {
	m := newMockDeribit(t)
	m.on("public/auth", func(gw.JSONRPCRequest) mockReply { return mockReply{result: `{"scope":"public"}`} })

	g := gw.New(testGatewayConfig(), gw.WithEndpoint(m.url()))
	defer g.Close()
	err := g.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no access_token") {
		t.Fatalf("want access_token error, got %v", err)
	}
}

func TestGateway_CallReturnsResult(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/get_positions", func(gw.JSONRPCRequest) mockReply { return mockReply{result: `[{"size":-0.1}]`} })
	g := connectGateway(t, m, testGatewayConfig())

	resp, err := g.Call(context.Background(), "private/get_positions", map[string]any{}, gw.PriorityLow)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(resp.Result) != `[{"size":-0.1}]` {
		t.Errorf("result = %s", resp.Result)
	}
}

// A slow reply must not delay the requests queued behind it (no head-of-line blocking).
func TestGateway_SlowReplyDoesNotBlockOtherCalls(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/get_account_summary", func(gw.JSONRPCRequest) mockReply { return mockReply{delay: 2 * time.Second} })
	g := connectGateway(t, m, testGatewayConfig())

	ctx := context.Background()
	go g.Call(ctx, "private/get_account_summary", map[string]any{}, gw.PriorityLow)
	eventually(t, time.Second, "slow request sent", func() bool { return m.count("private/get_account_summary") == 1 })

	start := time.Now()
	if _, err := g.Call(ctx, "private/buy", map[string]any{}, gw.PriorityHigh); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("stop-loss waited %v behind a slow reply", elapsed)
	}
}

func TestGateway_RPCErrorIsTyped(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/sell", func(gw.JSONRPCRequest) mockReply {
		return mockReply{err: &gw.RPCError{Code: 10009, Message: "not_enough_funds"}}
	})
	g := connectGateway(t, m, testGatewayConfig())

	_, err := g.Call(context.Background(), "private/sell", map[string]any{}, gw.PriorityLow)
	var rpcErr *gw.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != 10009 {
		t.Fatalf("want RPCError 10009, got %v", err)
	}
}

func TestGateway_BusinessRejectionsDoNotOpenCircuit(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/sell", func(gw.JSONRPCRequest) mockReply {
		return mockReply{err: &gw.RPCError{Code: 10005, Message: "price_too_low 0.005"}}
	})
	g := connectGateway(t, m, testGatewayConfig()) // threshold 3
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		g.Call(ctx, "private/sell", map[string]any{}, gw.PriorityLow)
	}
	if _, err := g.Call(ctx, "private/get_positions", map[string]any{}, gw.PriorityLow); err != nil {
		t.Errorf("circuit should stay closed after business rejections, got %v", err)
	}
}

func TestGateway_ExchangeOverloadOpensCircuitButNotForStopLoss(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/sell", func(gw.JSONRPCRequest) mockReply {
		return mockReply{err: &gw.RPCError{Code: 11051, Message: "system_maintenance"}}
	})
	g := connectGateway(t, m, testGatewayConfig()) // threshold 3
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		g.Call(ctx, "private/sell", map[string]any{}, gw.PriorityLow)
	}

	_, err := g.Call(ctx, "private/get_positions", map[string]any{}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrCircuitOpen) {
		t.Errorf("low-priority call should be blocked by the open circuit, got %v", err)
	}
	if _, err := g.Call(ctx, "private/buy", map[string]any{}, gw.PriorityHigh); err != nil {
		t.Errorf("high-priority (stop-loss) call must bypass the open circuit, got %v", err)
	}
}

func TestGateway_RetriesReadsButNeverOrders(t *testing.T) {
	m := newMockDeribit(t)
	var mu sync.Mutex
	reads := 0
	m.on("private/get_positions", func(gw.JSONRPCRequest) mockReply {
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads < 3 {
			return mockReply{err: &gw.RPCError{Code: 10028, Message: "too_many_requests"}}
		}
		return mockReply{result: `[]`}
	})
	m.on("private/sell", func(gw.JSONRPCRequest) mockReply {
		return mockReply{err: &gw.RPCError{Code: 10028, Message: "too_many_requests"}}
	})
	cfg := testGatewayConfig()
	cfg.Circuit.Threshold = 100 // keep the breaker out of this test
	g := connectGateway(t, m, cfg)
	ctx := context.Background()

	if _, err := g.Call(ctx, "private/get_positions", map[string]any{}, gw.PriorityLow); err != nil {
		t.Fatalf("read should succeed after retries, got %v", err)
	}
	if n := m.count("private/get_positions"); n != 3 {
		t.Errorf("read attempts = %d, want 3", n)
	}

	if _, err := g.Call(ctx, "private/sell", map[string]any{}, gw.PriorityLow); err == nil {
		t.Fatal("rate-limited order should fail")
	}
	if n := m.count("private/sell"); n != 1 {
		t.Errorf("order attempts = %d, want exactly 1 (orders are never retried)", n)
	}
}

func TestGateway_InFlightCallFailsFastWhenConnectionDrops(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/get_order_state", func(gw.JSONRPCRequest) mockReply {
		return mockReply{closeConn: true, delay: 20 * time.Millisecond}
	})
	g := connectGateway(t, m, testGatewayConfig())

	start := time.Now()
	_, err := g.Call(context.Background(), "private/get_order_state", map[string]any{}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrConnectionLost) {
		t.Fatalf("want ErrConnectionLost, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("caller waited %v; should fail as soon as the socket drops", elapsed)
	}
}

// Regression test: the reconnect used to derive its context from the dead
// connection's context, so the new connection was cancelled immediately.
func TestGateway_ReconnectsAndRestoresSubscriptions(t *testing.T) {
	m := newMockDeribit(t)
	g := connectGateway(t, m, testGatewayConfig())
	ctx := context.Background()

	channels := []string{"ticker.BTC-PERPETUAL.100ms", "deribit_price_index.btc_usd"}
	if err := g.Subscribe(ctx, channels); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	m.dropConnections()

	eventually(t, 3*time.Second, "re-authenticated", func() bool { return m.count("public/auth") == 2 })
	eventually(t, 3*time.Second, "re-subscribed", func() bool { return m.count("public/subscribe") == 2 })

	// The new connection must be usable.
	if _, err := g.Call(ctx, "public/test", map[string]any{}, gw.PriorityLow); err != nil {
		t.Fatalf("call after reconnect: %v", err)
	}

	resub := m.requests("public/subscribe")[1]
	params, _ := json.Marshal(resub.Params)
	for _, ch := range channels {
		if !strings.Contains(string(params), ch) {
			t.Errorf("re-subscribe missing %s: %s", ch, params)
		}
	}
}

func TestGateway_ReportsFatalWhenReconnectGivesUp(t *testing.T) {
	m := newMockDeribit(t)
	cfg := testGatewayConfig()
	cfg.Heartbeat.ReconnectMaxAttempts = 2
	g := connectGateway(t, m, cfg)

	m.close() // server gone for good

	select {
	case err := <-g.Fatal():
		if err == nil || !strings.Contains(err.Error(), "2 attempts") {
			t.Errorf("unexpected fatal error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway never reported a fatal error")
	}
}

func TestGateway_AnswersHeartbeatTestRequest(t *testing.T) {
	m := newMockDeribit(t)
	connectGateway(t, m, testGatewayConfig())
	before := m.count("public/test")

	m.push(`{"jsonrpc":"2.0","method":"heartbeat","params":{"type":"test_request"}}`)
	eventually(t, time.Second, "public/test reply to test_request", func() bool { return m.count("public/test") == before+1 })
}

func TestGateway_DeliversNotifications(t *testing.T) {
	m := newMockDeribit(t)
	g := connectGateway(t, m, testGatewayConfig())

	m.push(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"deribit_price_index.btc_usd","data":{"price":65000}}}`)
	select {
	case n := <-g.Notifications():
		if n.Params == nil || n.Params.Channel != "deribit_price_index.btc_usd" || string(n.Params.Data) != `{"price":65000}` {
			t.Errorf("unexpected notification: %+v", n.Params)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestGateway_SubscribeChunksAndDeduplicates(t *testing.T) {
	m := newMockDeribit(t)
	g := connectGateway(t, m, testGatewayConfig())
	ctx := context.Background()

	channels := make([]string, 120)
	for i := range channels {
		channels[i] = fmt.Sprintf("ticker.BTC-X-%d-C.100ms", i)
	}
	if err := g.Subscribe(ctx, channels); err != nil {
		t.Fatal(err)
	}
	if n := m.count("public/subscribe"); n != 3 {
		t.Errorf("subscribe calls = %d, want 3 chunks of ≤50", n)
	}
	if err := g.Subscribe(ctx, channels[:10]); err != nil {
		t.Fatal(err)
	}
	if n := m.count("public/subscribe"); n != 3 {
		t.Errorf("already-subscribed channels were sent again (%d calls)", n)
	}
}

func TestGateway_CallHonoursContextCancel(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/get_positions", func(gw.JSONRPCRequest) mockReply { return mockReply{noReply: true} })
	g := connectGateway(t, m, testGatewayConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := g.Call(ctx, "private/get_positions", map[string]any{}, gw.PriorityLow)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want DeadlineExceeded, got %v", err)
	}
}

func TestIsMatchingEngine(t *testing.T) {
	match := []string{"private/buy", "private/sell", "private/edit", "private/cancel", "private/cancel_all", "private/cancel_all_by_instrument"}
	for _, m := range match {
		if !gw.IsMatchingEngine(m) {
			t.Errorf("%s should use the matching-engine pool", m)
		}
	}
	for _, m := range []string{"public/auth", "private/get_positions", "private/get_margins", "public/subscribe"} {
		if gw.IsMatchingEngine(m) {
			t.Errorf("%s should use the non-matching pool", m)
		}
	}
}
