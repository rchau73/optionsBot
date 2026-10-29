package api

import (
	"net/http"
	"time"
)

// routesBook registers the open book: strangles, working orders, P&L by
// strategy and each strangle's payoff chart.
func (s *Server) routesBook(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/positions", s.positions)
	mux.HandleFunc("GET /api/orders", s.pendingOrders)
	mux.HandleFunc("GET /api/pnl", s.pnl)
	mux.HandleFunc("GET /api/strangles/payoff", s.payoff)
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

// payoff serves GET /api/strangles/payoff.
func (s *Server) payoff(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, PayoffOf(s.strategy.View(), time.Now()))
}
