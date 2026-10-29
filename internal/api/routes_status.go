package api

import (
	"net/http"
	"time"

	"optionsbot/internal/account"
	"optionsbot/internal/gateway"
	"optionsbot/internal/history"
)

// routesStatus registers the bot's health, its header and KPI status, and the
// cached account summary.
func (s *Server) routesStatus(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/account", s.accountSummary)
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
		"median_flip":      v.MedianFlip,
		"gex_as_of":        v.GEXAsOf,
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
