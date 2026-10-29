package api

import (
	"net/http"
	"strconv"
	"time"

	"optionsbot/internal/orders"
)

// routesTrades registers the journal's recent events and its opens and closes.
func (s *Server) routesTrades(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/events", s.recentEvents)
	mux.HandleFunc("GET /api/trades", s.trades)
}

const (
	defaultEventLimit = 200
	maxEventLimit     = 500
	defaultTradeLimit = 1000
	maxTradeLimit     = 5000
)

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
