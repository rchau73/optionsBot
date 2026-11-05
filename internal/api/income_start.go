package api

import (
	"fmt"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
)

// The income page's "% of start" view (#629): each result as a % of the
// start balance — Deribit's margin balance (coin) the P&L history first
// recorded at or after the start of the period — next to what holding the
// coin alone did from that same time, so two environments of different size
// (stable, mvp) compare. All three figures use one start time:
//
//	result %   = closes and futures hedge after the start (journal, net of fees) ÷ B
//	hold %     = spot at the end ÷ spot at the start − 1
//	combined % = (B + result) × spot at the end ÷ (B × spot at the start) − 1
//
// Realised only: open positions count when they close (the worst week alone
// marks them, as perf does). A month is measured from its own first balance;
// one recorded after the month began is marked partial.

// IncomeStart is the "% of start" payload; Known is false (Reason says why)
// when no balance was recorded at or after the start, and every % is then null.
type IncomeStart struct {
	Known       bool       `json:"known"`
	Reason      string     `json:"reason,omitempty"`
	Requested   *time.Time `json:"requested"` // ?from=, null for the default (the window's first balance)
	At          *time.Time `json:"at"`        // when the start balance was recorded
	Source      string     `json:"source"`    // where the balance comes from
	BalanceCoin *float64   `json:"balance_coin"`
	BalanceUSD  *float64   `json:"balance_usd"` // at the start's spot
	To          time.Time  `json:"to"`
	StartResult
	WorstWeek StartWorstWeek `json:"worst_week"`
	Months    []MonthStart   `json:"months"` // oldest first, as the income months
}

// StartResult is the three figures measured from one start balance.
type StartResult struct {
	SpotStart   *float64 `json:"spot_start"`
	SpotEnd     *float64 `json:"spot_end"`
	ResultCoin  float64  `json:"result_coin"` // closes after the start, net of fees
	Closes      int      `json:"closes"`
	ResultPct   *float64 `json:"result_pct"`   // result ÷ balance
	HoldPct     *float64 `json:"hold_pct"`     // USD; 0 in coin. Null without both spots
	CombinedPct *float64 `json:"combined_pct"` // USD, bot + holding. Null without both spots
}

// StartWorstWeek is the worst 7 days of the total (open positions at their
// marks) since the start balance, as a % of it; KnownAt says when it will be
// known while the history holds less than 7 days since the start.
type StartWorstWeek struct {
	Known   bool       `json:"known"`
	Start   *time.Time `json:"start"`
	End     *time.Time `json:"end"`
	Coin    float64    `json:"coin"`
	Pct     *float64   `json:"pct"`
	KnownAt *time.Time `json:"known_at"`
}

// MonthStart is one month measured from its own first balance (at or after
// the start of the period). Partial: that balance was recorded after the
// month began, From says when. Reason says why a month has no figures.
type MonthStart struct {
	Month       string     `json:"month"` // YYYY-MM
	Partial     bool       `json:"partial"`
	From        *time.Time `json:"from"`
	BalanceCoin *float64   `json:"balance_coin"`
	BalanceUSD  *float64   `json:"balance_usd"`
	Reason      string     `json:"reason,omitempty"`
	StartResult
}

// IncomeStartOf is the "% of start" view of the months (YYYY-MM, oldest
// first) up to now. from is the requested start (zero: the first month's
// start, so the default is the window's first recorded balance).
func IncomeStartOf(months []string, trades []orders.Trade, points []history.Point, from, now time.Time) IncomeStart {
	now = now.UTC()
	st := IncomeStart{To: now, Months: []MonthStart{}}
	if !from.IsZero() {
		from = from.UTC()
		st.Requested = &from
	} else if len(months) > 0 {
		from, _ = time.Parse("2006-01", months[0])
	}

	in := windowOf(points, from, now)
	i, balance, source, ok := firstBalance(in)
	st.Source = source
	if !ok {
		st.Reason = source
	} else {
		at := in[i].T.UTC()
		st.Known, st.At = true, &at
		st.BalanceCoin = ptr(balance)
		since := in[i:]
		st.StartResult = measure(balance, since, trades, at, now)
		if st.SpotStart != nil {
			st.BalanceUSD = ptr(balance * *st.SpotStart)
		}
		st.WorstWeek = worstWeekSince(since, balance, at, now)
	}

	for _, m := range months {
		st.Months = append(st.Months, monthFromStart(m, trades, points, from, now))
	}
	return st
}

// monthFromStart measures one month from its first balance at or after from.
func monthFromStart(month string, trades []orders.Trade, points []history.Point, from, now time.Time) MonthStart {
	ms := MonthStart{Month: month}
	start, _ := time.Parse("2006-01", month)
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	if end.After(now) {
		end = now
	}
	if end.Before(from) {
		ms.Reason = "before the start of the period (" + from.Format("2 Jan 2006 15:04") + " UTC)"
		return ms
	}
	begin := start
	if from.After(begin) {
		begin = from
	}
	in := windowOf(points, begin, end)
	i, balance, _, ok := firstBalance(in)
	if !ok {
		ms.Reason = "no margin balance recorded in the P&L history this month"
		return ms
	}
	at := in[i].T.UTC()
	// A point stands for MaxSampleGap: a first line minutes into the month is the month's start.
	ms.Partial = at.Sub(start) > history.MaxSampleGap
	ms.From, ms.BalanceCoin = &at, ptr(balance)
	ms.StartResult = measure(balance, in[i:], trades, at, end)
	if ms.SpotStart != nil {
		ms.BalanceUSD = ptr(balance * *ms.SpotStart)
	}
	return ms
}

// measure is the three figures for balance (coin) recorded at at: the closes
// in (at, to], and the recorded spot first → last of the points from at.
func measure(balance float64, since []history.Point, trades []orders.Trade, at, to time.Time) StartResult {
	var r StartResult
	for _, t := range trades {
		if t.Kind == "close" && t.At.After(at) && !t.At.After(to) {
			r.ResultCoin += t.PnL
			if t.Reason != HedgeTradeReason {
				r.Closes++
			}
		}
	}
	r.ResultPct = ptr(r.ResultCoin / balance * 100)
	var first, last float64
	for _, p := range since {
		if p.Spot <= 0 {
			continue
		}
		if first == 0 {
			first = p.Spot
		}
		last = p.Spot
	}
	if first > 0 {
		r.SpotStart, r.SpotEnd = ptr(first), ptr(last)
		r.HoldPct = ptr((last/first - 1) * 100)
		r.CombinedPct = ptr(((balance+r.ResultCoin)*last/(balance*first) - 1) * 100)
	}
	return r
}

// worstWeekSince is perf's worst week over the points since the start
// balance, as a % of it; before 7 days it is not known, KnownAt says when.
func worstWeekSince(since []history.Point, balance float64, at, now time.Time) StartWorstWeek {
	s := perf.Summarize(since, balance, at, now)
	if !s.WorstWeekKnown {
		knownAt := at.Add(perf.Week)
		return StartWorstWeek{KnownAt: &knownAt}
	}
	start, end := s.WorstWeekStart, s.WorstWeekEnd
	return StartWorstWeek{Known: true, Start: &start, End: &end, Coin: s.WorstWeek, Pct: ptr(s.WorstWeekPct)}
}

// incomeFromLayouts are the forms ?from= accepts: RFC 3339 with or without
// seconds (2026-10-28T11:45Z), or a UTC date.
var incomeFromLayouts = []string{time.RFC3339, "2006-01-02T15:04Z07:00", time.DateOnly}

// ParseIncomeFrom reads the income page's ?from= ("" → zero time).
func ParseIncomeFrom(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range incomeFromLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("from must be a time such as 2026-10-28T11:45Z or a date such as 2026-10-28, got %q", s)
}
