package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"optionsbot/internal/config"
)

// Gateway manages the WebSocket connection to Deribit including auth, heartbeat,
// reconnection, rate limiting, and the priority dispatch loop.
type Gateway struct {
	cfg          *config.Config
	conn         *websocket.Conn
	mu           sync.Mutex
	idCounter    atomic.Int64
	pending      sync.Map // int64 -> chan JSONRPCResponse
	notifyCh      chan JSONRPCResponse
	droppedNotifs atomic.Int64
	rl            *RateLimiter
	cb           *CircuitBreaker
	pq           *PriorityQueue
	subs         *SubscriptionRegistry
	reconnecting atomic.Bool

	// connCancel cancels the goroutines (readLoop, dispatchLoop, heartbeatLoop,
	// metricsLoop) for the current connection. Called before starting fresh goroutines
	// on reconnect to prevent goroutine proliferation.
	connCancel context.CancelFunc
	connMu     sync.Mutex

	// metrics
	reqCount    atomic.Int64
	retryCount  atomic.Int64
	metricsReset time.Time
}

func New(cfg *config.Config) *Gateway {
	return &Gateway{
		cfg:         cfg,
		notifyCh:    make(chan JSONRPCResponse, 32768),
		rl:          NewRateLimiter(cfg.RateLimit),
		cb:          NewCircuitBreaker(cfg.Circuit),
		pq:          NewPriorityQueue(64, 256),
		subs:        NewSubscriptionRegistry(cfg.RateLimit.MaxSubscriptions),
		metricsReset: time.Now(),
	}
}

func (g *Gateway) Connect(ctx context.Context) error {
	return g.connectOnce(ctx)
}

func (g *Gateway) connectOnce(ctx context.Context) error {
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, g.cfg.WSEndpoint(), nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	// Cancel goroutines from the previous connection before starting new ones.
	// Without this, each reconnect spawns additional goroutines that all run
	// concurrently, causing metrics to log multiple times per interval.
	g.connMu.Lock()
	if g.connCancel != nil {
		g.connCancel()
	}
	connCtx, cancel := context.WithCancel(ctx)
	g.connCancel = cancel
	g.connMu.Unlock()

	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	// Pass conn directly to readLoop so it holds a stable reference to this
	// connection and doesn't race with g.conn being replaced on the next reconnect.
	go g.readLoop(connCtx, conn)
	go g.dispatchLoop(connCtx)
	go g.heartbeatLoop(connCtx)
	go g.metricsLoop(connCtx)

	return g.authenticate(ctx)
}

func (g *Gateway) reconnect(ctx context.Context) {
	if !g.reconnecting.CompareAndSwap(false, true) {
		return
	}
	defer g.reconnecting.Store(false)

	// Save channel list before clearing — connectOnce starts fresh loops and
	// the old registry state would prevent re-subscribing after auth.
	channels := g.subs.All()
	g.subs.Clear()

	base := time.Duration(g.cfg.Heartbeat.ReconnectBackoffBaseMS) * time.Millisecond
	for attempt := 1; attempt <= g.cfg.Heartbeat.ReconnectMaxAttempts; attempt++ {
		backoff := time.Duration(math.Min(
			float64(base)*math.Pow(2, float64(attempt-1)),
			float64(30*time.Second),
		))
		slog.Info("reconnecting", "attempt", attempt, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if err := g.connectOnce(ctx); err == nil {
			slog.Info("reconnected successfully")
			if len(channels) > 0 {
				slog.Info("restoring subscriptions after reconnect", "channels", len(channels))
				if err := g.Subscribe(ctx, channels); err != nil {
					slog.Error("re-subscribe failed after reconnect", "err", err)
				}
			}
			return
		}
	}
	slog.Error("reconnect failed after max attempts — exiting so supervisor can restart")
	os.Exit(1)
}

func (g *Gateway) readLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return // connection cancelled intentionally — no reconnect
			}
			slog.Error("websocket read error", "err", err)
			go g.reconnect(ctx)
			return
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			slog.Warn("unmarshal error", "err", err)
			continue
		}

		if resp.Method == "heartbeat" {
			// Deribit sends test_request heartbeats and closes the connection
			// (code 4000) if the bot doesn't respond with public/test within the
			// heartbeat interval. Write the response directly to the socket,
			// bypassing the dispatch queue, so a stalled sendRequest (up to 30s)
			// cannot block the reply and trigger a spurious 4000 close.
			var hb struct {
				Params struct {
					Type string `json:"type"`
				} `json:"params"`
			}
			if json.Unmarshal(msg, &hb) == nil && hb.Params.Type == "test_request" {
				go func(c *websocket.Conn) {
					id := g.idCounter.Add(1)
					payload := JSONRPCRequest{
						JsonRPC: "2.0",
						ID:      id,
						Method:  "public/test",
						Params:  map[string]string{},
					}
					data, err := json.Marshal(payload)
					if err != nil {
						return
					}
					g.mu.Lock()
					c.SetWriteDeadline(time.Now().Add(5 * time.Second))
					err = c.WriteMessage(websocket.TextMessage, data)
					c.SetWriteDeadline(time.Time{})
					g.mu.Unlock()
					if err != nil {
						slog.Debug("heartbeat test_request response failed", "err", err)
					}
				}(conn)
			}
			continue
		}

		if resp.Method != "" {
			// Other push notifications (tickers, index price, etc.)
			select {
			case g.notifyCh <- resp:
			default:
				g.droppedNotifs.Add(1)
			}
			continue
		}

		// Response to a pending request
		if ch, ok := g.pending.LoadAndDelete(resp.ID); ok {
			ch.(chan JSONRPCResponse) <- resp
		}
	}
}

func (g *Gateway) dispatchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-g.pq.HighCh():
			g.sendRequest(ctx, req)
		default:
			select {
			case <-ctx.Done():
				return
			case req := <-g.pq.HighCh():
				g.sendRequest(ctx, req)
			case req := <-g.pq.LowCh():
				g.sendRequest(ctx, req)
			}
		}
	}
}

func (g *Gateway) sendRequest(ctx context.Context, req Request) {
	// Choose rate limiter scope
	if req.Priority == PriorityHigh {
		if err := g.rl.WaitMatch(ctx); err != nil {
			return
		}
	} else {
		if err := g.rl.WaitNonMatch(ctx); err != nil {
			return
		}
	}

	if err := g.cb.Allow(); err != nil {
		slog.Warn("circuit breaker blocked request", "method", req.Payload.Method)
		if req.RespCh != nil {
			req.RespCh <- JSONRPCResponse{
				Error: &RPCError{Code: -1, Message: "circuit breaker open"},
			}
		}
		return
	}

	data, err := json.Marshal(req.Payload)
	if err != nil {
		return
	}

	respCh := make(chan JSONRPCResponse, 1)
	g.pending.Store(req.Payload.ID, respCh)

	g.mu.Lock()
	g.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = g.conn.WriteMessage(websocket.TextMessage, data)
	g.conn.SetWriteDeadline(time.Time{})
	g.mu.Unlock()

	if err != nil {
		g.pending.Delete(req.Payload.ID)
		g.cb.Failure()
		slog.Error("write error", "err", err)
		return
	}

	g.reqCount.Add(1)

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			g.cb.Failure()
			g.retryCount.Add(1)
		} else {
			g.cb.Success()
		}
		if req.RespCh != nil {
			req.RespCh <- resp
		}
	case <-ctx.Done():
		g.pending.Delete(req.Payload.ID)
	case <-time.After(30 * time.Second):
		g.pending.Delete(req.Payload.ID)
		g.cb.Failure()
		if req.RespCh != nil {
			req.RespCh <- JSONRPCResponse{
				Error: &RPCError{Code: -1, Message: "request timeout"},
			}
		}
	}
}

func (g *Gateway) Call(ctx context.Context, method string, params interface{}, priority int) (JSONRPCResponse, error) {
	id := g.idCounter.Add(1)
	req := JSONRPCRequest{
		JsonRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	respCh := make(chan JSONRPCResponse, 1)
	g.pq.Enqueue(Request{
		Priority: priority,
		Payload:  req,
		RespCh:   respCh,
	})
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return JSONRPCResponse{}, ctx.Err()
	}
}

// subscribeChunkSize is the max channels per public/subscribe call.
// Deribit enforces a 32 KB message limit; BTC options chains can have hundreds
// of instruments × 2 channels each, which overflows that limit in one shot.
const subscribeChunkSize = 50

func (g *Gateway) Subscribe(ctx context.Context, channels []string) error {
	toSub := make([]string, 0, len(channels))
	for _, ch := range channels {
		if g.subs.Add(ch) {
			toSub = append(toSub, ch)
		}
	}
	if len(toSub) == 0 {
		return nil
	}
	for i := 0; i < len(toSub); i += subscribeChunkSize {
		end := i + subscribeChunkSize
		if end > len(toSub) {
			end = len(toSub)
		}
		chunk := toSub[i:end]
		slog.Debug("subscribing channel batch", "offset", i, "count", len(chunk), "total", len(toSub))
		if _, err := g.Call(ctx, "public/subscribe", map[string]interface{}{
			"channels": chunk,
		}, PriorityLow); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gateway) Notifications() <-chan JSONRPCResponse {
	return g.notifyCh
}

func (g *Gateway) authenticate(ctx context.Context) error {
	resp, err := g.Call(ctx, "public/auth", AuthParams{
		GrantType:    "client_credentials",
		ClientID:     g.cfg.ClientID,
		ClientSecret: g.cfg.ClientSecret,
	}, PriorityHigh)
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}

	// Validate the response contains a real access token.
	// Deribit can return success with an empty/public session for bad credentials.
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("auth failed: unexpected response format")
	}
	token, _ := result["access_token"].(string)
	if token == "" {
		return fmt.Errorf("auth failed: no access_token in response — check DERIBIT_CLIENT_ID and DERIBIT_CLIENT_SECRET in .env, and verify the API key has account:read and trade:read_write scopes")
	}

	scope, _ := result["scope"].(string)
	slog.Info("authenticated with Deribit", "scope", scope)
	return nil
}

func (g *Gateway) heartbeatLoop(ctx context.Context) {
	interval := time.Duration(g.cfg.Heartbeat.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Enable server-side heartbeats
	_, _ = g.Call(ctx, "public/set_heartbeat", HeartbeatParams{
		Interval: g.cfg.Heartbeat.IntervalSec,
	}, PriorityLow)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Drain any stacked ticks that accumulated while the previous call was slow.
			for {
				select {
				case <-ticker.C:
				default:
					goto sendTest
				}
			}
		sendTest:
			callCtx, cancel := context.WithTimeout(ctx, interval-time.Second)
			_, err := g.Call(callCtx, "public/test", map[string]string{}, PriorityLow)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Warn("heartbeat failed", "err", err)
			}
		}
	}
}

func (g *Gateway) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nonMatch, match := g.rl.Tokens()
			dropped := g.droppedNotifs.Swap(0)
			slog.Info("rate_limit_metrics",
				"event", "rate_limit_metrics",
				"ws_nonmatch_tokens_available", int(nonMatch),
				"ws_match_tokens_available", int(match),
				"circuit_breaker_state", g.cb.State(),
				"circuit_breaker_failures", g.cb.Failures(),
				"retries_last_60s", g.retryCount.Swap(0),
				"subscriptions_active", g.subs.Count(),
				"requests_last_60s", g.reqCount.Swap(0),
				"notifications_dropped_last_60s", dropped,
			)
			if dropped > 0 {
				slog.Warn("notifications dropped — consumer may be too slow",
					"dropped", dropped,
					"notifych_capacity", cap(g.notifyCh),
				)
			}
		}
	}
}

// Close gracefully closes the WebSocket connection.
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}
