package api

import (
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
)

// The income page (/api/income, #319): the summary's income table month by
// month — where each month's result came from (paid to you, paid to buy back,
// fees) and what holding the coin alone did — plus a few figures over the
// months: the total, the average month, months up and down, the worst month
// and the worst week. Realised figures come from the journal's closes (their
// one source); the spot, the equity and the worst week from the P&L history
// (perf.Summarize; missing days listed, never filled). It is its own route,
// not part of /api/summary, because it reads months of raw history points and
// the summary is polled every second.

// IncomeSource is the history's own description in the payload.
const IncomeSource = "realised: journal closes, net of fees; spot, equity and worst week: data/pnl_history.jsonl (the bot's P&L history)"

// IncomeStats is the months in a few figures. Coin figures rank; USD ones are
// the same closes at their own spot.
type IncomeStats struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Label  string    `json:"label"` // live | testnet
	Source string    `json:"source"`

	Total      Amount   `json:"total_6m"`    // realised over every month shown
	Equity     *float64 `json:"equity"`      // coin, at the window start; null when unknown
	EquityNote string   `json:"equity_note"` // where it comes from, or why unknown
	ReturnPct  *float64 `json:"return_pct"`  // total coin ÷ equity: % more coin than holding

	// The average of the months with at least one close (a month the bot
	// did not trade is not a zero month).
	AverageMonth *Amount    `json:"average_month"` // null before the first close
	ActiveMonths int        `json:"active_months"`
	UpMonths     int        `json:"up_months"`
	DownMonths   int        `json:"down_months"`
	WorstMonth   *MonthRank `json:"worst_month"` // lowest realised coin; null before the first close

	WorstWeek   WorstWeek `json:"worst_week"` // perf's worst 7 days of the total, % of equity
	Days        int       `json:"days"`
	DaysCovered int       `json:"days_covered"`
	MissingDays []string  `json:"missing_days"` // UTC dates with no history point
}

// Amount is one figure in coin and in USD.
type Amount struct {
	Coin float64 `json:"coin"`
	USD  float64 `json:"usd"`
}

// MonthRank names a month with its result and the coin's recorded move.
type MonthRank struct {
	Month       string   `json:"month"` // YYYY-MM
	Coin        float64  `json:"coin"`
	USD         float64  `json:"usd"`
	SpotMovePct *float64 `json:"spot_move_pct"` // recorded spot first → last; null when not recorded
	SpotDays    *float64 `json:"spot_days"`
}

// CloseParts splits a close's net P&L into what it is made of, in coin:
// the premium received for the closed part, the buy-back cost and the fees
// (opening share + closing fee). The buy-back is what remains of the net
// (premium − fees − P&L), which is how the journal computed the P&L
// (orders.ClosedNetPnL), so the three always add up to the P&L.
func CloseParts(t orders.Trade) (premium, buyback, fees float64) {
	return t.Premium, t.Premium - t.Fees - t.PnL, t.Fees
}

// CloseSpot is the spot a close's P&L was valued at in USD: the journal's
// own ratio when it has one, else the market spot of the record.
func CloseSpot(t orders.Trade) float64 {
	if t.PnL != 0 && t.PnLUSD != 0 {
		return t.PnLUSD / t.PnL
	}
	return t.Spot
}

// IncomeWithHistory completes inc (from IncomeOf) with the P&L history's
// points: each month's spot and hold-alone figure, and the IncomeStats over
// the months shown. now ends the window; environment labels it.
func IncomeWithHistory(inc IncomeSummary, points []history.Point, now time.Time, environment string) IncomeSummary {
	now = now.UTC()
	months := make([]MonthIncome, len(inc.Months))
	copy(months, inc.Months)
	inc.Months = months

	var from time.Time
	if len(months) > 0 {
		from, _ = time.Parse("2006-01", months[0].Month)
	}
	for i := range months {
		start, _ := time.Parse("2006-01", months[i].Month)
		end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
		if end.After(now) {
			end = now
		}
		holdOfMonth(&months[i], windowOf(points, start, end))
	}
	if n := len(months); n > 0 {
		inc.ThisMonth = months[n-1]
	}

	in := windowOf(points, from, now)
	equity, note, known := EquityAtStart(in)
	s := perf.Summarize(in, equity, from, now)
	st := &IncomeStats{
		From: from, To: now, Label: PerformanceLabel(environment), Source: IncomeSource,
		EquityNote: note,
		WorstWeek:  WorstWeek{Known: s.WorstWeekKnown, Coin: s.WorstWeek},
		Days:       s.Days, DaysCovered: s.DaysCovered, MissingDays: []string{},
	}
	if known {
		st.Equity = ptr(equity)
	}
	if s.WorstWeekKnown {
		start, end := s.WorstWeekStart, s.WorstWeekEnd
		st.WorstWeek.Start, st.WorstWeek.End = &start, &end
		if known {
			st.WorstWeek.Pct = ptr(s.WorstWeekPct)
		}
	}
	for _, d := range s.MissingDays {
		st.MissingDays = append(st.MissingDays, d.Format(time.DateOnly))
	}

	for _, m := range months {
		st.Total.Coin += m.RealisedCoin
		st.Total.USD += m.RealisedUSD
		if m.Closes == 0 {
			continue
		}
		st.ActiveMonths++
		switch {
		case m.RealisedCoin > 0:
			st.UpMonths++
		case m.RealisedCoin < 0:
			st.DownMonths++
		}
		if st.WorstMonth == nil || m.RealisedCoin < st.WorstMonth.Coin {
			st.WorstMonth = &MonthRank{Month: m.Month, Coin: m.RealisedCoin, USD: m.RealisedUSD, SpotDays: m.SpotDays}
			if m.SpotStart != nil && m.SpotEnd != nil {
				st.WorstMonth.SpotMovePct = ptr((*m.SpotEnd / *m.SpotStart - 1) * 100)
			}
		}
	}
	if st.ActiveMonths > 0 {
		n := float64(st.ActiveMonths)
		st.AverageMonth = &Amount{Coin: st.Total.Coin / n, USD: st.Total.USD / n}
	}
	if known {
		st.ReturnPct = ptr(st.Total.Coin / equity * 100)
	}
	inc.Summary = st
	return inc
}

// holdOfMonth sets a month's recorded spot first → last and, with the
// equity at the month's start, the USD change of holding that equity.
func holdOfMonth(m *MonthIncome, in []history.Point) {
	var first, last *history.Point
	for i := range in {
		if in[i].Spot <= 0 {
			continue
		}
		if first == nil {
			first = &in[i]
		}
		last = &in[i]
	}
	if first == nil {
		m.HoldReason = "no spot recorded in the P&L history this month"
		return
	}
	m.SpotStart, m.SpotEnd = ptr(first.Spot), ptr(last.Spot)
	m.SpotDays = ptr(last.T.Sub(first.T).Hours() / 24)
	equity, reason, ok := EquityAtStart(in)
	if !ok {
		m.HoldReason = reason
		return
	}
	m.HoldUSD = ptr(equity * (last.Spot - first.Spot))
}

// windowOf is the points in [from, to].
func windowOf(points []history.Point, from, to time.Time) []history.Point {
	var in []history.Point
	for _, p := range points {
		if !p.T.Before(from) && !p.T.After(to) {
			in = append(in, p)
		}
	}
	return in
}
