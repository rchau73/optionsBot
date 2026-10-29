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
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
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
	// One registration per domain, each in routes_<domain>.go with its handlers.
	s.routesStatus(s.mux)
	s.routesBook(s.mux)
	s.routesTrades(s.mux)
	s.routesSummary(s.mux)
	s.routesJournal(s.mux)
	s.routesActions(s.mux)
	return s
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
