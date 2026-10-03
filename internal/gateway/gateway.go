package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"optionsbot/internal/config"
)

// Errors a Call can return besides a Deribit *RPCError.
var (
	ErrRequestTimeout = errors.New("request timed out waiting for Deribit")
	ErrConnectionLost = errors.New("connection to Deribit lost")
)

const (
	// requestTimeout bounds how long a request may wait for a reply, counted
	// from when Call is made (time in the queue counts too).
	requestTimeout = 30 * time.Second
	writeTimeout   = 10 * time.Second
	// subscribeChunkSize is the max channels per public/subscribe call.
	// Deribit enforces a 32 KB message limit; BTC options chains can have
	// hundreds of instruments × 2 channels each.
	subscribeChunkSize = 50
)

// Gateway owns the single WebSocket connection to Deribit. Every API call goes
// through Call, which applies, in order: the priority queue, the rate limiter,
// the circuit breaker and a reply timeout.
//
// Concurrency model: per connection there is one reader goroutine (readLoop)
// and one writer goroutine (dispatchLoop). The writer never waits for replies;
// readLoop routes each reply to its caller by request ID through g.pending.
// A slow reply therefore never holds up the requests queued behind it.
type Gateway struct {
	cfg      *config.Config
	endpoint string
	// publicOnly gateways never authenticate and refuse private/* methods,
	// so no credentials or account request ever travel over them.
	publicOnly bool
	log        *slog.Logger // tagged with the gateway's name

	writeMu sync.Mutex // serialises writes and guards conn
	conn    *websocket.Conn

	idCounter atomic.Int64
	pendingMu sync.Mutex
	pending   map[int64]*pendingCall

	notifyCh      chan JSONRPCResponse
	droppedNotifs atomic.Int64

	rl   *RateLimiter
	cb   *CircuitBreaker
	pq   *PriorityQueue
	subs *SubscriptionRegistry

	reconnecting atomic.Bool
	connMu       sync.Mutex
	connCancel   context.CancelFunc // stops the goroutines of the current connection

	fatal chan error // receives once when the gateway gives up reconnecting

	reqCount   atomic.Int64
	retryCount atomic.Int64
}

// pendingCall is a request that has been written and is waiting for its reply.
type pendingCall struct {
	method string
	reply  chan callResult
	timer  *time.Timer
}

// Option customises a Gateway at construction.
type Option func(*Gateway)

// WithEndpoint overrides the WebSocket URL (tests point it at a mock; the
// bot points its market-data gateway at mainnet).
func WithEndpoint(url string) Option {
	return func(g *Gateway) { g.endpoint = url }
}

// PublicOnly makes a gateway for public market data: it skips
// authentication and refuses private/* methods with ErrPrivateOnPublic.
func PublicOnly() Option {
	return func(g *Gateway) { g.publicOnly = true }
}

// WithName tags the gateway's log lines (e.g. "trading", "mainnet-public").
func WithName(name string) Option {
	return func(g *Gateway) { g.log = slog.Default().With("gateway", name) }
}

// ErrPrivateOnPublic is returned when a private method is called on a
// PublicOnly gateway.
var ErrPrivateOnPublic = errors.New("private method on a public-only gateway")

func New(cfg *config.Config, opts ...Option) *Gateway {
	g := &Gateway{
		cfg:      cfg,
		endpoint: cfg.WSEndpoint(),
		pending:  make(map[int64]*pendingCall),
		notifyCh: make(chan JSONRPCResponse, 32768),
		rl:       NewRateLimiter(cfg.RateLimit),
		cb:       NewCircuitBreaker(cfg.Circuit),
		pq:       NewPriorityQueue(64, 256),
		subs:     NewSubscriptionRegistry(cfg.RateLimit.MaxSubscriptions),
		fatal:    make(chan error, 1),
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Connect dials Deribit, starts the connection goroutines and authenticates.
// ctx governs the gateway's whole lifetime, including future reconnects.
func (g *Gateway) Connect(ctx context.Context) error {
	return g.connectOnce(ctx)
}

// Fatal delivers an error when the gateway has given up reconnecting. The
// caller should shut down so a supervisor (Docker restart policy, bot.sh)
// can start a fresh process that reconciles positions with the exchange.
func (g *Gateway) Fatal() <-chan error { return g.fatal }

func (g *Gateway) connectOnce(rootCtx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(rootCtx, g.endpoint, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	// Stop the previous connection's goroutines before starting new ones.
	g.connMu.Lock()
	if g.connCancel != nil {
		g.connCancel()
	}
	// The connection context derives from rootCtx, never from a previous
	// connection's context: otherwise cancelling the old connection would
	// cancel the new one too.
	connCtx, cancel := context.WithCancel(rootCtx)
	g.connCancel = cancel
	g.connMu.Unlock()

	g.writeMu.Lock()
	old := g.conn
	g.conn = conn
	g.writeMu.Unlock()
	if old != nil {
		old.Close() // unblocks the old readLoop, which then exits quietly
	}

	go g.readLoop(rootCtx, connCtx, cancel, conn)
	go g.dispatchLoop(connCtx)
	go g.heartbeatLoop(connCtx)
	go g.metricsLoop(connCtx)

	if g.publicOnly {
		g.log.Info("connected (public data only, not authenticated)", "endpoint", g.endpoint)
		return nil
	}
	if err := g.authenticate(connCtx); err != nil {
		cancel()
		return err
	}
	return nil
}

func (g *Gateway) reconnect(rootCtx context.Context) {
	if !g.reconnecting.CompareAndSwap(false, true) {
		return
	}
	defer g.reconnecting.Store(false)

	// Save the channel list: the new connection starts with no subscriptions.
	channels := g.subs.All()
	g.subs.Clear()

	base := time.Duration(g.cfg.Heartbeat.ReconnectBackoffBaseMS) * time.Millisecond
	for attempt := 1; attempt <= g.cfg.Heartbeat.ReconnectMaxAttempts; attempt++ {
		backoff := time.Duration(math.Min(
			float64(base)*math.Pow(2, float64(attempt-1)),
			float64(30*time.Second),
		))
		g.log.Info("reconnecting", "attempt", attempt, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-rootCtx.Done():
			return
		}
		if err := g.connectOnce(rootCtx); err != nil {
			g.log.Warn("reconnect attempt failed", "attempt", attempt, "err", err)
			continue
		}
		g.log.Info("reconnected successfully", "attempt", attempt)
		if len(channels) > 0 {
			g.log.Info("restoring subscriptions after reconnect", "channels", len(channels))
			if err := g.Subscribe(rootCtx, channels); err != nil {
				g.log.Error("re-subscribe failed after reconnect", "err", err)
			}
		}
		return
	}

	err := fmt.Errorf("reconnect failed after %d attempts", g.cfg.Heartbeat.ReconnectMaxAttempts)
	g.log.Error("gateway giving up", "err", err)
	select {
	case g.fatal <- err:
	default:
	}
}

// readLoop reads every message on conn and routes it: heartbeats are answered
// inline, subscription pushes go to notifyCh, replies go to their caller.
func (g *Gateway) readLoop(rootCtx, connCtx context.Context, cancelConn context.CancelFunc, conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if connCtx.Err() != nil {
				return // connection closed on purpose — no reconnect
			}
			g.log.Error("websocket read error", "err", err)
			cancelConn() // stop writing to a dead socket
			g.failAllPending(ErrConnectionLost)
			go g.reconnect(rootCtx)
			return
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			g.log.Warn("unmarshal error", "err", err)
			continue
		}

		switch {
		case resp.Method == "heartbeat":
			if isTestRequest(msg) {
				go g.answerTestRequest(conn)
			}
		case resp.Method != "":
			select {
			case g.notifyCh <- resp:
			default:
				g.droppedNotifs.Add(1)
			}
		default:
			g.deliver(resp)
		}
	}
}

// isTestRequest reports whether a heartbeat message is a test_request, which
// Deribit expects us to answer or it closes the connection (code 4000).
func isTestRequest(msg []byte) bool {
	var hb struct {
		Params struct {
			Type string `json:"type"`
		} `json:"params"`
	}
	return json.Unmarshal(msg, &hb) == nil && hb.Params.Type == "test_request"
}

// answerTestRequest replies to a Deribit test_request directly on the socket,
// bypassing the queue so a backlog cannot delay it past Deribit's deadline.
func (g *Gateway) answerTestRequest(conn *websocket.Conn) {
	payload := JSONRPCRequest{JsonRPC: "2.0", ID: g.idCounter.Add(1), Method: "public/test", Params: map[string]string{}}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		g.log.Debug("heartbeat test_request response failed", "err", err)
	}
}

// dispatchLoop is the single writer for a connection. It takes requests in
// priority order and sends them; it never waits for a reply.
func (g *Gateway) dispatchLoop(ctx context.Context) {
	for {
		req, ok := g.pq.Next(ctx)
		if !ok {
			return
		}
		g.send(ctx, req)
	}
}

// send writes one request. Every exit path answers req.reply exactly once,
// either here or later from deliver / the timeout timer.
func (g *Gateway) send(ctx context.Context, req Request) {
	method := req.Payload.Method

	if !req.Deadline.IsZero() && time.Now().After(req.Deadline) {
		req.reply <- callResult{err: fmt.Errorf("%s: %w (expired in queue)", method, ErrRequestTimeout)}
		return
	}

	if err := g.rl.Wait(ctx, method); err != nil {
		req.reply <- callResult{err: fmt.Errorf("%s: rate limiter: %w", method, err)}
		return
	}

	// Risk-reducing orders (stop-loss, kill switch) skip the breaker: a failed
	// attempt costs one request, a stop-loss that is never sent can cost the account.
	if req.Priority != PriorityHigh {
		if err := g.cb.Allow(); err != nil {
			g.log.Warn("circuit breaker blocked request", "method", method)
			req.reply <- callResult{err: fmt.Errorf("%s: %w", method, err)}
			return
		}
	}

	data, err := json.Marshal(req.Payload)
	if err != nil {
		req.reply <- callResult{err: fmt.Errorf("%s: encode request: %w", method, err)}
		return
	}

	id := req.Payload.ID
	g.addPending(id, method, req.reply, req.Deadline)

	g.writeMu.Lock()
	g.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	err = g.conn.WriteMessage(websocket.TextMessage, data)
	g.conn.SetWriteDeadline(time.Time{})
	g.writeMu.Unlock()

	if err != nil {
		g.cb.Failure()
		g.log.Error("write error", "method", method, "err", err)
		if p := g.takePending(id); p != nil {
			p.reply <- callResult{err: fmt.Errorf("%s: write: %w", method, err)}
		}
		return
	}
	g.reqCount.Add(1)
}

// addPending registers a written request and arms its reply timeout.
func (g *Gateway) addPending(id int64, method string, reply chan callResult, deadline time.Time) {
	timeout := requestTimeout
	if !deadline.IsZero() {
		timeout = time.Until(deadline)
	}
	p := &pendingCall{method: method, reply: reply}

	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	g.pending[id] = p
	// Armed under the lock so takePending always sees a non-nil timer.
	p.timer = time.AfterFunc(timeout, func() {
		if p := g.takePending(id); p != nil {
			g.cb.Failure()
			p.reply <- callResult{err: fmt.Errorf("%s: %w", p.method, ErrRequestTimeout)}
		}
	})
}

// takePending removes and returns the pending call for id, or nil if it was
// already answered. Whoever takes it is the only one allowed to reply.
func (g *Gateway) takePending(id int64) *pendingCall {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	p, ok := g.pending[id]
	if !ok {
		return nil
	}
	delete(g.pending, id)
	p.timer.Stop()
	return p
}

// deliver hands a reply to its waiting caller and updates the circuit breaker.
func (g *Gateway) deliver(resp JSONRPCResponse) {
	p := g.takePending(resp.ID)
	if p == nil {
		return // caller already timed out or gave up
	}
	if resp.Error == nil {
		g.cb.Success()
		p.reply <- callResult{resp: resp}
		return
	}
	if isExchangeUnhealthy(resp.Error) {
		g.cb.Failure()
	} else {
		// A business rejection (bad price, no funds) proves the exchange is up.
		g.cb.Success()
	}
	p.reply <- callResult{resp: resp, err: resp.Error}
}

// failAllPending answers every in-flight request with err. Used when the
// connection drops: those replies will never arrive.
func (g *Gateway) failAllPending(err error) {
	g.pendingMu.Lock()
	calls := g.pending
	g.pending = make(map[int64]*pendingCall)
	g.pendingMu.Unlock()

	for _, p := range calls {
		p.timer.Stop()
		p.reply <- callResult{err: fmt.Errorf("%s: %w", p.method, err)}
	}
}

// Call sends a JSON-RPC request and waits for its reply. Read-only methods are
// retried with backoff when Deribit rate-limits them; order methods never are,
// because a "failed" order may still have reached the book.
func (g *Gateway) Call(ctx context.Context, method string, params any, priority int) (JSONRPCResponse, error) {
	if g.publicOnly && strings.HasPrefix(method, "private/") {
		return JSONRPCResponse{}, fmt.Errorf("%s: %w", method, ErrPrivateOnPublic)
	}
	if !isIdempotent(method) {
		return g.callOnce(ctx, method, params, priority)
	}
	var resp JSONRPCResponse
	err := WithRetry(ctx, g.cfg.Retry, func() error {
		var err error
		resp, err = g.callOnce(ctx, method, params, priority)
		if isRateLimitError(err) {
			g.retryCount.Add(1)
		}
		return err
	})
	return resp, err
}

func (g *Gateway) callOnce(ctx context.Context, method string, params any, priority int) (JSONRPCResponse, error) {
	id := g.idCounter.Add(1)
	reply := make(chan callResult, 1) // buffered: the replier never blocks
	req := Request{
		Priority: priority,
		Payload:  JSONRPCRequest{JsonRPC: "2.0", ID: id, Method: method, Params: params},
		Deadline: time.Now().Add(requestTimeout),
		reply:    reply,
	}
	if err := g.pq.Enqueue(ctx, req); err != nil {
		return JSONRPCResponse{}, fmt.Errorf("%s: enqueue: %w", method, err)
	}
	select {
	case r := <-reply:
		return r.resp, r.err
	case <-ctx.Done():
		g.takePending(id) // stop the timer; a late reply is dropped
		return JSONRPCResponse{}, ctx.Err()
	}
}

// Subscribe subscribes to channels not already subscribed, in chunks.
func (g *Gateway) Subscribe(ctx context.Context, channels []string) error {
	toSub := make([]string, 0, len(channels))
	for _, ch := range channels {
		switch {
		case g.subs.Has(ch):
		case g.subs.Add(ch):
			toSub = append(toSub, ch)
		default:
			g.log.Warn("subscription limit reached, channel skipped",
				"channel", ch, "limit", g.cfg.RateLimit.MaxSubscriptions)
		}
	}
	for i := 0; i < len(toSub); i += subscribeChunkSize {
		chunk := toSub[i:min(i+subscribeChunkSize, len(toSub))]
		g.log.Debug("subscribing channel batch", "offset", i, "count", len(chunk), "total", len(toSub))
		if _, err := g.Call(ctx, "public/subscribe", map[string]any{"channels": chunk}, PriorityLow); err != nil {
			for _, ch := range toSub[i:] {
				g.subs.Remove(ch) // not subscribed: allow a later retry
			}
			return err
		}
	}
	return nil
}

// Notifications returns the stream of subscription push messages.
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

	// Deribit can return success with an empty/public session for bad
	// credentials, so insist on a real access token.
	var result struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("auth failed: unexpected response format: %w", err)
	}
	if result.AccessToken == "" {
		return errors.New("auth failed: no access_token in response — check DERIBIT_CLIENT_ID and DERIBIT_CLIENT_SECRET in .env, and verify the API key has account:read and trade:read_write scopes")
	}

	g.log.Info("authenticated with Deribit", "scope", result.Scope)
	return nil
}

func (g *Gateway) heartbeatLoop(ctx context.Context) {
	interval := time.Duration(g.cfg.Heartbeat.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Ask Deribit to send heartbeats; it then closes the socket if we stop answering.
	if _, err := g.Call(ctx, "public/set_heartbeat", HeartbeatParams{
		Interval: g.cfg.Heartbeat.IntervalSec,
	}, PriorityLow); err != nil && ctx.Err() == nil {
		g.log.Warn("enable server heartbeat failed", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// time.Ticker drops ticks for slow receivers, so no backlog builds up.
			callCtx, cancel := context.WithTimeout(ctx, interval-time.Second)
			_, err := g.Call(callCtx, "public/test", map[string]string{}, PriorityLow)
			cancel()
			if err != nil && ctx.Err() == nil {
				g.log.Warn("heartbeat failed", "err", err)
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
			g.pendingMu.Lock()
			inFlight := len(g.pending)
			g.pendingMu.Unlock()
			g.log.Info("rate_limit_metrics",
				"event", "rate_limit_metrics",
				"ws_nonmatch_tokens_available", int(nonMatch),
				"ws_match_tokens_available", int(match),
				"circuit_breaker_state", g.cb.State(),
				"circuit_breaker_failures", g.cb.Failures(),
				"retries_last_60s", g.retryCount.Swap(0),
				"subscriptions_active", g.subs.Count(),
				"requests_last_60s", g.reqCount.Swap(0),
				"requests_in_flight", inFlight,
				"notifications_dropped_last_60s", dropped,
			)
			if dropped > 0 {
				g.log.Warn("notifications dropped — consumer may be too slow",
					"dropped", dropped,
					"notifych_capacity", cap(g.notifyCh),
				)
			}
		}
	}
}

// Close stops the connection goroutines and closes the WebSocket.
func (g *Gateway) Close() error {
	g.connMu.Lock()
	if g.connCancel != nil {
		g.connCancel()
	}
	g.connMu.Unlock()

	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}
