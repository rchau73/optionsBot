package api

import (
	"strings"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/perf"
)

// The performance summary (/api/performance, #232) serves perf.Summarize over
// the P&L history: net return in coin and USD, the return of just holding the
// coin, the max drawdown, the worst week and the days with no data. It reads
// the history held in memory — never a Deribit call — so the evidence report
// and the investor view read one source of performance figures.

// PerformanceRanges maps the range names to how far back they reach; "all"
// starts at the first recorded point (the store keeps a year).
var PerformanceRanges = map[string]time.Duration{
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"all": 366 * 24 * time.Hour,
}

// PointSource provides the raw P&L history points (history.Store); a
// PnLHistory that implements it serves /api/performance.
type PointSource interface {
	Points(from, to time.Time) []history.Point
}

// Performance is the /api/performance payload. Every percentage of equity is
// null while equity is unknown (EquityReason says why) — never 0. Label is
// "live" or "testnet", so testnet figures cannot be shown as live.
type Performance struct {
	AsOf        time.Time `json:"as_of"`
	Range       string    `json:"range"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Label       string    `json:"label"` // live | testnet
	Points      int       `json:"points"`
	Source      string    `json:"source"`
	NetPnL      float64   `json:"net_pnl"`       // coin
	NetPnLUSD   *float64  `json:"net_pnl_usd"`   // at the window's last spot; null without a spot
	NetPnLKnown bool      `json:"net_pnl_known"` // at least one point in the window

	Equity       *float64 `json:"equity"` // coin, at the window start
	EquityKnown  bool     `json:"equity_known"`
	EquityReason string   `json:"equity_reason,omitempty"` // why it is unknown, or how it was converted
	ReturnPct    *float64 `json:"return_pct"`              // net P&L ÷ equity, coin

	HoldKnown         bool     `json:"hold_known"`
	SpotStart         *float64 `json:"spot_start"`
	SpotEnd           *float64 `json:"spot_end"`
	HoldReturnUSDPct  *float64 `json:"hold_return_usd_pct"`
	HoldReturnCoinPct *float64 `json:"hold_return_coin_pct"` // 0: a held coin is still one coin
	ReturnUSDPct      *float64 `json:"return_usd_pct"`       // needs equity and spot
	ExcessReturnPct   *float64 `json:"excess_return_pct"`    // return_usd_pct − hold_return_usd_pct

	MaxDrawdown PerfAmount `json:"max_drawdown"`
	WorstWeek   WorstWeek  `json:"worst_week"`

	Days        int      `json:"days"`
	DaysCovered int      `json:"days_covered"`
	MissingDays []string `json:"missing_days"` // UTC dates (YYYY-MM-DD) with no point, never filled in
}

// PerfAmount is a coin amount and its % of equity (null while equity is unknown).
type PerfAmount struct {
	Coin float64  `json:"coin"`
	Pct  *float64 `json:"pct"`
}

// WorstWeek is the lowest change of the total over 7 days; unknown until
// the window holds 7 days of points.
type WorstWeek struct {
	Known bool       `json:"known"`
	Start *time.Time `json:"start"`
	End   *time.Time `json:"end"`
	Coin  float64    `json:"coin"`
	Pct   *float64   `json:"pct"`
}

// PerformanceLabel is "live" only for a live bot; anything else is testnet.
func PerformanceLabel(environment string) string {
	if environment == "live" {
		return "live"
	}
	return "testnet"
}

// EquityAtStart is the equity in coin at the window start: the margin balance
// of the first point that carries one (Deribit's figure, #277), converted at
// that point's spot when it is in USD (cross collateral). ok is false when no
// point carries a usable balance; reason then says why. Never computed from P&L.
func EquityAtStart(points []history.Point) (equity float64, reason string, ok bool) {
	for _, p := range points {
		if p.MarginBalance == nil || *p.MarginBalance <= 0 || p.Unit == "" {
			continue
		}
		if strings.EqualFold(p.Unit, "USD") {
			if p.Spot <= 0 {
				continue
			}
			return *p.MarginBalance / p.Spot, "margin balance in USD (cross collateral) at " +
				p.T.UTC().Format(time.RFC3339) + ", converted at that line's spot", true
		}
		return *p.MarginBalance, "margin balance in " + p.Unit + " at " + p.T.UTC().Format(time.RFC3339), true
	}
	return 0, "no P&L history line in the window carries Deribit's margin balance", false
}

// BuildPerformance is the pure payload for points within [from, to].
func BuildPerformance(points []history.Point, from, to time.Time, rangeName, environment string) Performance {
	var in []history.Point
	for _, p := range points {
		if !p.T.Before(from) && !p.T.After(to) {
			in = append(in, p)
		}
	}
	equity, reason, known := EquityAtStart(in)
	s := perf.Summarize(in, equity, from, to)

	out := Performance{
		AsOf: to, Range: rangeName, From: from, To: to, Label: PerformanceLabel(environment),
		Points: s.Points, Source: "data/pnl_history.jsonl (the bot's P&L history)",
		NetPnL: s.NetPnL, NetPnLKnown: s.Points > 0,
		EquityKnown: known && s.EquityKnown, EquityReason: reason,
		HoldKnown:   s.HoldKnown,
		MaxDrawdown: PerfAmount{Coin: s.MaxDrawdown},
		WorstWeek:   WorstWeek{Known: s.WorstWeekKnown, Coin: s.WorstWeek},
		Days:        s.Days, DaysCovered: s.DaysCovered, MissingDays: []string{},
	}
	if s.SpotEnd > 0 {
		out.NetPnLUSD = ptr(s.NetPnLUSD)
	}
	if out.EquityKnown {
		out.Equity = ptr(s.Equity)
		out.ReturnPct = ptr(s.ReturnPct)
		out.MaxDrawdown.Pct = ptr(s.MaxDrawdownPct)
		if s.WorstWeekKnown {
			out.WorstWeek.Pct = ptr(s.WorstWeekPct)
		}
	}
	if s.HoldKnown {
		out.SpotStart, out.SpotEnd = ptr(s.SpotStart), ptr(s.SpotEnd)
		out.HoldReturnUSDPct, out.HoldReturnCoinPct = ptr(s.HoldReturnUSDPct), ptr(s.HoldReturnCoinPct)
		if out.EquityKnown {
			out.ReturnUSDPct, out.ExcessReturnPct = ptr(s.ReturnUSDPct), ptr(s.ExcessReturnPct)
		}
	}
	if s.WorstWeekKnown {
		start, end := s.WorstWeekStart, s.WorstWeekEnd
		out.WorstWeek.Start, out.WorstWeek.End = &start, &end
	}
	for _, d := range s.MissingDays {
		out.MissingDays = append(out.MissingDays, d.Format(time.DateOnly))
	}
	return out
}

func ptr(v float64) *float64 { return &v }
