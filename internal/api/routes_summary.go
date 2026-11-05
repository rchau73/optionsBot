package api

import (
	"net/http"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// routesSummary registers the investor's answers and the history behind them:
// the summary, performance, income month by month and the P&L chart.
func (s *Server) routesSummary(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/summary", s.summary)
	mux.HandleFunc("GET /api/performance", s.performance)
	mux.HandleFunc("GET /api/income", s.income)
	mux.HandleFunc("GET /api/pnl/history", s.pnlHistory)
}

// summary serves GET /api/summary.
func (s *Server) summary(w http.ResponseWriter, _ *http.Request) {
	var trades []orders.Trade
	if ts, ok := s.events.(TradeSource); ok {
		trades = ts.Trades(0, 0)
	}
	writeJSON(w, BuildHedgedSummary(s.strategy.View(), trades, s.futuresHedge(), time.Now()))
}

// futuresHedge is the journal's hedge fills and the perpetual open now; empty
// on a bot without the experiment.
func (s *Server) futuresHedge() FuturesHedge {
	var h FuturesHedge
	if src, ok := s.events.(PerpFillSource); ok {
		h.Fills = src.PerpFills()
	}
	if s.hedge != nil {
		v := s.hedge.HedgeState()
		h.SizeUSD, h.AvgPrice, h.FundingCoin = v.PerpUSD, v.PerpAvg, v.FundingCoin
	}
	return h
}

// performance serves /api/performance?range=7d|30d|all (default 30d).
func (s *Server) performance(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "30d"
	}
	span, ok := PerformanceRanges[name]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of 7d, 30d, all")
		return
	}
	to := time.Now().UTC()
	from := to.Add(-span)
	points := []history.Point{}
	if src, ok := s.history.(PointSource); ok {
		points = src.Points(from, to)
	}
	if name == "all" {
		from = to
		if len(points) > 0 {
			from = points[0].T.UTC()
		}
	}
	writeJSON(w, BuildPerformance(points, from, to, name, s.strategy.View().Environment))
}

// income serves GET /api/income.
// ?from= (#629) starts the "% of start" view at the first balance at or after
// that time, so two environments line up on one start.
func (s *Server) income(w http.ResponseWriter, r *http.Request) {
	requested, err := ParseIncomeFrom(r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	var trades []orders.Trade
	if ts, ok := s.events.(TradeSource); ok {
		trades = ts.Trades(0, 0)
	}
	v := s.strategy.View()
	flows := s.futuresHedge().Flows(v.Market.Spot, now)
	inc := IncomeWithHedge(incomeOfView(v, trades, now), flows)
	var points []history.Point
	if src, ok := s.history.(PointSource); ok && len(inc.Months) > 0 {
		from, _ := time.Parse("2006-01", inc.Months[0].Month)
		if !requested.IsZero() && requested.Before(from) {
			from = requested
		}
		points = src.Points(from, now)
	}
	months := make([]string, len(inc.Months))
	for i, m := range inc.Months {
		months[i] = m.Month
	}
	start := IncomeStartOf(months, append(trades, HedgeTrades(flows)...), points, requested, now)
	inc = IncomeWithHistory(inc, points, now, v.Environment)
	inc.Start = &start
	writeJSON(w, inc)
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
