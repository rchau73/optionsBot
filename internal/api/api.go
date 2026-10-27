// Package api serves a read-only JSON view of a running bot for the monitor
// UI. Every GET response is built from in-memory state the bot already holds,
// so it adds no load to the trading path or to Deribit's rate limits.
//
// Two routes change trading state: POST /api/positions/close (WithManualClose)
// — bearer token, testnet only, and the strategy re-checks every position
// against its rule (strategy.ManualCloseBlockReason) on the decision loop; the
// close is journaled like any other — and POST /api/rebalance. Both take the
// person who asked as "actor" (ActorOf) and journal it.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/account"
	"optionsbot/internal/gateway"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// StrategySource provides the strategy's read-only view.
type StrategySource interface {
	View() strategy.View
}

// EventSource provides recent journal events and counts by event type.
type EventSource interface {
	Recent(after uint64, limit int) []orders.RecentEvent
	EventCounts() map[string]int
}

// TradeSource provides the journal's opens and closes (orders.Logger).
type TradeSource interface {
	Trades(after uint64, limit int) []orders.Trade
	HistorySince() time.Time
}

const (
	defaultEventLimit = 200
	maxEventLimit     = 500
	defaultTradeLimit = 1000
	maxTradeLimit     = 5000
)

// PnLHistory provides bucketed P&L history (see history.Store).
type PnLHistory interface {
	Range(from, to time.Time, buckets int) []history.Point
}

// UtilisationSource reports margin utilisation over a window (history.Store);
// a PnLHistory that implements it fills /api/status utilisation_7d/_30d.
type UtilisationSource interface {
	Utilisation(from, to time.Time) history.UtilisationStats
}

// ManualCloser closes positions by hand on the decision loop (strategy.Strategy).
type ManualCloser interface {
	ManualClose(ctx context.Context, ids []string, by orders.Actor) ([]strategy.ManualCloseResult, error)
}

// ManualRebalancer runs a rebalance pass, or a dry run of one, on request.
type ManualRebalancer interface {
	RequestRebalance(ctx context.Context, dryRun bool, by orders.Actor) (strategy.RebalanceReport, error)
}

// AccountSource provides the cached account/collateral summary.
type AccountSource interface {
	Status() account.Status
}

// ConnectionSource provides a gateway's connection stability (gateway.Gateway).
type ConnectionSource interface {
	ConnStats() gateway.ConnStats
}

// Server is the monitor API for one bot process.
type Server struct {
	strategy    StrategySource
	events      EventSource
	history     PnLHistory                  // nil → empty history
	account     AccountSource               // nil → no account data
	closer      ManualCloser                // nil → no manual close route
	rebal       ManualRebalancer            // nil → no manual rebalance route
	connections map[string]ConnectionSource // by gateway name; empty → {}
	token       string
	started     time.Time
	deployment  string        // local environment: stable | mvp | "" (run by hand)
	version     string        // release tag or commit
	config      *ConfigStatus // nil → "config": null
	mux         *http.ServeMux
}

// Option configures a Server.
type Option func(*Server)

// WithPnLHistory serves /api/pnl/history from h (and /api/performance when h
// is a PointSource).
func WithPnLHistory(h PnLHistory) Option { return func(s *Server) { s.history = h } }

// WithManualClose serves POST /api/positions/close, authorised by token
// (Authorization: Bearer <token>). An empty token leaves the route off.
func WithManualClose(c ManualCloser, token string) Option {
	return func(s *Server) { s.closer, s.token = c, token }
}

// WithManualRebalance serves POST /api/rebalance, authorised by token
// (Authorization: Bearer <token>; the same token as manual close). An empty
// token leaves the route off.
func WithManualRebalance(r ManualRebalancer, token string) Option {
	return func(s *Server) { s.rebal, s.token = r, token }
}

// WithAccount serves /api/account from a.
func WithAccount(a AccountSource) Option { return func(s *Server) { s.account = a } }

// WithDeployment names the local environment (stable | mvp) and the release
// the bot runs, reported in /api/status as "deployment" and "version".
func WithDeployment(name, version string) Option {
	return func(s *Server) { s.deployment, s.version = name, version }
}

// ConfigStatus is /api/status "config": the fingerprint of the effective
// settings and when they last changed (#256, the journal's config_loaded).
type ConfigStatus struct {
	Fingerprint string    `json:"fingerprint"`
	Since       time.Time `json:"since"`
}

// WithConfig reports the settings fingerprint and the time of their last
// change in /api/status as "config".
func WithConfig(fingerprint string, since time.Time) Option {
	return func(s *Server) { s.config = &ConfigStatus{Fingerprint: fingerprint, Since: since} }
}

// WithConnection adds a gateway's drops, reconnects and timeouts to
// /api/status under connections.<name> (e.g. "trading", "mainnet-public").
// Drops at the same moment on both gateways point at the host or network.
func WithConnection(name string, c ConnectionSource) Option {
	return func(s *Server) { s.connections[name] = c }
}

// New builds the API. Use Handler for tests, ListenAndServe to run it.
func New(src StrategySource, events EventSource, opts ...Option) *Server {
	s := &Server{strategy: src, events: events, started: time.Now(), mux: http.NewServeMux(),
		connections: map[string]ConnectionSource{}}
	for _, opt := range opts {
		opt(s)
	}
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("GET /api/positions", s.positions)
	s.mux.HandleFunc("GET /api/orders", s.pendingOrders)
	s.mux.HandleFunc("GET /api/pnl", s.pnl)
	s.mux.HandleFunc("GET /api/pnl/history", s.pnlHistory)
	s.mux.HandleFunc("GET /api/events", s.recentEvents)
	s.mux.HandleFunc("GET /api/account", s.accountSummary)
	s.mux.HandleFunc("GET /api/trades", s.trades)
	s.mux.HandleFunc("GET /api/summary", s.summary)
	s.mux.HandleFunc("GET /api/performance", s.performance)
	s.mux.HandleFunc("GET /api/income", s.income)
	s.mux.HandleFunc("GET /api/strangles/payoff", s.payoff)
	if s.closer != nil && s.token != "" {
		s.mux.HandleFunc("POST /api/positions/close", s.manualClose)
	}
	if s.rebal != nil && s.token != "" {
		s.mux.HandleFunc("POST /api/rebalance", s.manualRebalance)
	}
	return s
}

const (
	maxCloseIDs     = 20
	manualCloseWait = 60 * time.Second // a market buy-back is seconds; the loop may be mid-cycle
	// A pass runs a few margin simulations per strangle (Deribit allows one a
	// second) plus its orders; the loop may be mid-cycle.
	manualRebalanceWait = 120 * time.Second
)

// authorised checks the admin bearer token in constant time.
func (s *Server) authorised(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// actorName is a monitor username: 3–64 of a-z 0-9 . _ - (frontend lib/auth/store.js).
var actorName = regexp.MustCompile(`^[a-z0-9._-]{3,64}$`)

// actorRoles are the monitor's roles (PRD §3).
var actorRoles = map[string]bool{"admin": true, "manager": true, "user": true}

// ActorOf checks the person a state-changing request names (#171): nil →
// "monitor" (a request that names nobody, as before); otherwise a valid
// username and role, else an error. The monitor enforces who may act; the
// bot only records who did — its own check stays the bearer token.
func ActorOf(a *orders.Actor) (orders.Actor, error) {
	if a == nil {
		return orders.Actor{User: "monitor"}, nil
	}
	if !actorName.MatchString(a.User) || !actorRoles[a.Role] {
		return orders.Actor{}, errors.New(`actor must be {"user": 3–64 of a-z 0-9 . _ -, "role": admin|manager|user}`)
	}
	return *a, nil
}

// manualClose: {"position_ids": [...], "actor": {"user", "role"}} → the
// strategy's results. Audited in the bot log with the actor; each close is
// journaled by the strategy with it.
func (s *Server) manualClose(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		slog.Warn("manual close refused: bad token", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	var body struct {
		PositionIDs []string      `json:"position_ids"`
		Actor       *orders.Actor `json:"actor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"position_ids\": [...]}")
		return
	}
	if len(body.PositionIDs) == 0 || len(body.PositionIDs) > maxCloseIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("between 1 and %d position ids", maxCloseIDs))
		return
	}
	actor, err := ActorOf(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Warn("manual close requested", "positions", body.PositionIDs, "by", actor, "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), manualCloseWait)
	defer cancel()
	res, err := s.closer.ManualClose(ctx, body.PositionIDs, actor)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, strategy.ErrManualCloseBusy) {
			code = http.StatusGatewayTimeout
		}
		writeError(w, code, err.Error())
		return
	}
	slog.Warn("manual close done", "results", res)
	writeJSON(w, map[string]any{"results": res})
}

// manualRebalance: POST /api/rebalance[?dry_run=true] → the pass's report.
// 409 when the gates refuse it (the reason says which), 429 inside the
// cooldown, 504 when the decision loop did not take it. Audited in the bot
// log; a real pass is journaled (risk_limit rebalance_manual).
func (s *Server) manualRebalance(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		slog.Warn("manual rebalance refused: bad token", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	dryRun := false
	if v := r.URL.Query().Get("dry_run"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dry_run must be true or false")
			return
		}
		dryRun = b
	}
	// The body is optional ({"actor": {...}}); without one the actor is "monitor".
	var body struct {
		Actor *orders.Actor `json:"actor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "body must be empty or {\"actor\": {\"user\": …, \"role\": …}}")
		return
	}
	actor, err := ActorOf(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Warn("manual rebalance requested via API", "dry_run", dryRun, "by", actor, "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), manualRebalanceWait)
	defer cancel()
	rep, err := s.rebal.RequestRebalance(ctx, dryRun, actor)
	var refused strategy.RebalanceRefused
	var cooldown strategy.RebalanceCooldown
	switch {
	case err == nil:
		writeJSON(w, rep)
	case errors.As(err, &cooldown):
		w.Header().Set("Retry-After", strconv.Itoa(int(cooldown.RetryAfter.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.As(err, &refused):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, strategy.ErrRebalanceBusy):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeError(w, http.StatusConflict, err.Error())
	}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe serves on addr until ctx is cancelled. Bind to localhost or
// a private container network only: the API shows positions and equity.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	slog.Info("monitor API listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "uptime_sec": int(time.Since(s.started).Seconds())})
}

// status is everything the header and KPI strip need in one call.
func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	v := s.strategy.View()
	openLegs := 0
	for _, st := range v.Strangles {
		openLegs += len(st.Legs)
	}
	conns := make(map[string]gateway.ConnStats, len(s.connections))
	for name, c := range s.connections {
		conns[name] = c.ConnStats()
	}
	writeJSON(w, map[string]any{
		"as_of":            v.AsOf,
		"strategy_id":      v.StrategyID,
		"underlying":       v.Underlying,
		"environment":      v.Environment,
		"deployment":       s.deployment,
		"version":          s.version,
		"config":           s.config,
		"halted":           v.Halted,
		"loop_at":          v.LoopAt,
		"eval_interval_ms": v.EvalEveryMS,
		"uptime_sec":       int(time.Since(s.started).Seconds()),
		"market":           v.Market,
		"trend":            v.Trend,
		"direction":        v.Direction,
		"sma_trend":        v.SMATrend,
		"sma":              v.SMA,
		"gex_bias":         v.Bias,
		"flip_buffer_pct":  v.FlipBufferPct,
		"account":          v.Account,
		"risk":             v.Risk,
		"greeks":           v.Greeks,
		"open_strangles":   len(v.Strangles),
		"open_legs":        openLegs,
		"pending":          len(v.Pending),
		"held_legs":        v.HeldLegs,
		"event_counts":     s.events.EventCounts(),
		"history_since":    s.historySince(),
		"pnl":              v.PnL,
		"connections":      conns,
		"utilisation_7d":   s.utilisation(7 * 24 * time.Hour),
		"utilisation_30d":  s.utilisation(30 * 24 * time.Hour),
	})
}

// utilisation is the margin utilisation over the last span (#174); null
// without a P&L history that can report it.
func (s *Server) utilisation(span time.Duration) *history.UtilisationStats {
	u, ok := s.history.(UtilisationSource)
	if !ok {
		return nil
	}
	now := time.Now()
	stats := u.Utilisation(now.Add(-span), now)
	return &stats
}

// historySince is when the journal's history begins (null when unknown).
func (s *Server) historySince() any {
	if ts, ok := s.events.(TradeSource); ok && !ts.HistorySince().IsZero() {
		return ts.HistorySince()
	}
	return nil
}

// trades serves the journal's opens and closes with seq > after, oldest
// first (a client pages forward with the last seq it has). Read from memory,
// restored from orders.log at startup.
func (s *Server) trades(w http.ResponseWriter, r *http.Request) {
	ts, ok := s.events.(TradeSource)
	if !ok {
		writeJSON(w, map[string]any{"trades": []orders.Trade{}, "history_since": nil})
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = defaultTradeLimit
	}
	limit = min(limit, maxTradeLimit)
	writeJSON(w, map[string]any{"trades": plainTrades(ts.Trades(after, limit)), "history_since": s.historySince()})
}

func (s *Server) positions(w http.ResponseWriter, _ *http.Request) {
	v := s.strategy.View()
	writeJSON(w, map[string]any{"as_of": v.AsOf, "strategy_id": v.StrategyID, "underlying": v.Underlying, "strangles": v.Strangles})
}

func (s *Server) pendingOrders(w http.ResponseWriter, _ *http.Request) {
	v := s.strategy.View()
	writeJSON(w, map[string]any{"as_of": v.AsOf, "strategy_id": v.StrategyID, "pending": v.Pending})
}

func (s *Server) pnl(w http.ResponseWriter, _ *http.Request) {
	v := s.strategy.View()
	writeJSON(w, map[string]any{"as_of": v.AsOf, "strategy_id": v.StrategyID, "spot": v.Market.Spot, "pnl": v.PnL})
}

// accountSummary serves the cached account summary (collateral per asset,
// margin model, IM/MM). It never calls the exchange.
func (s *Server) accountSummary(w http.ResponseWriter, _ *http.Request) {
	st := account.Status{}
	if s.account != nil {
		st = s.account.Status()
	}
	writeJSON(w, map[string]any{
		"as_of":    time.Now(),
		"snapshot": st.Snapshot,
		"error":    st.Error,
		"error_at": st.ErrorAt,
	})
}

// HistoryRanges maps the chart's range names to how far back they reach.
var HistoryRanges = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"1d":  24 * time.Hour,
	"1w":  7 * 24 * time.Hour,
	"1m":  30 * 24 * time.Hour,
	"all": 366 * 24 * time.Hour,
}

// historyBuckets caps how many points one history response carries.
const historyBuckets = 300

// pnlHistory serves the P&L history for ?range=<name>, reduced to at most
// historyBuckets points so the browser never downloads raw history.
func (s *Server) pnlHistory(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "1d"
	}
	span, ok := HistoryRanges[name]
	if !ok {
		http.Error(w, "range must be one of 15m, 1h, 6h, 1d, 1w, 1m, all", http.StatusBadRequest)
		return
	}
	to := time.Now()
	from := to.Add(-span)
	points := []history.Point{}
	if s.history != nil {
		points = s.history.Range(from, to, historyBuckets)
	}
	writeJSON(w, map[string]any{
		"as_of": to, "range": name, "from": from, "to": to,
		"bucket_sec": int(history.BucketWidth(span, historyBuckets).Seconds()),
		"points":     points,
	})
}

// recentEvents serves journal events after ?since=<seq>, oldest first; the
// client passes the last seq it saw to receive only what is new.
func (s *Server) recentEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseUint(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, "since must be a non-negative integer", http.StatusBadRequest)
		return
	}
	limit := defaultEventLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = min(n, maxEventLimit)
	}
	writeJSON(w, map[string]any{"as_of": time.Now(), "events": plainEvents(s.events.Recent(since, limit))})
}

func parseUint(q string) (uint64, error) {
	if q == "" {
		return 0, nil
	}
	return strconv.ParseUint(q, 10, 64)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("monitor API: encode response failed", "err", err)
	}
}

// writeError answers {"error": msg} with code, so the monitor can show why.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
