// Package api serves a read-only JSON view of a running bot for the monitor
// UI. It never places orders and never calls the exchange: every response is
// built from in-memory state the bot already holds, so it adds no load to the
// trading path or to Deribit's rate limits.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

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

const (
	defaultEventLimit = 200
	maxEventLimit     = 500
)

// PnLHistory provides bucketed P&L history (see history.Store).
type PnLHistory interface {
	Range(from, to time.Time, buckets int) []history.Point
}

// Server is the monitor API for one bot process.
type Server struct {
	strategy StrategySource
	events   EventSource
	history  PnLHistory // nil → empty history
	started  time.Time
	mux      *http.ServeMux
}

// Option configures a Server.
type Option func(*Server)

// WithPnLHistory serves /api/pnl/history from h.
func WithPnLHistory(h PnLHistory) Option { return func(s *Server) { s.history = h } }

// New builds the API. Use Handler for tests, ListenAndServe to run it.
func New(src StrategySource, events EventSource, opts ...Option) *Server {
	s := &Server{strategy: src, events: events, started: time.Now(), mux: http.NewServeMux()}
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
	return s
}

// Handler returns the HTTP handler (read-only GET routes only).
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
	writeJSON(w, map[string]any{
		"as_of":          v.AsOf,
		"strategy_id":    v.StrategyID,
		"underlying":     v.Underlying,
		"environment":    v.Environment,
		"halted":         v.Halted,
		"loop_at":        v.LoopAt,
		"uptime_sec":     int(time.Since(s.started).Seconds()),
		"market":         v.Market,
		"trend":          v.Trend,
		"account":        v.Account,
		"greeks":         v.Greeks,
		"open_strangles": len(v.Strangles),
		"open_legs":      openLegs,
		"pending":        len(v.Pending),
		"event_counts":   s.events.EventCounts(),
		"pnl":            v.PnL,
	})
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
	events := s.events.Recent(since, limit)
	if events == nil {
		events = []orders.RecentEvent{}
	}
	writeJSON(w, map[string]any{"as_of": time.Now(), "events": events})
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
