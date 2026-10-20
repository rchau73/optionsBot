// Package perf turns the P&L history into the figures a sceptical investor
// asks for first: net return in coin and USD, the return of simply holding
// the coin over the same window, the max drawdown and the worst 7 days.
//
// Everything here is pure: it takes the points and the window and never
// reads the clock, so the same history always gives the same summary.
// Equity is a parameter (the account summary's equity at the window start,
// in coin) — it is never recomputed from P&L.
package perf

import (
	"sort"
	"time"

	"optionsbot/internal/history"
)

// Week is the length of the worst-week window.
const Week = 7 * 24 * time.Hour

// Summary is the performance of one bot over [From, To].
//
// Coin figures are in the underlying (BTC/ETH). Percentages are of Equity
// (coin) and are only meaningful when EquityKnown. Every figure comes from
// recorded points: a field whose input is missing stays zero and its Known
// flag says so — nothing is interpolated.
type Summary struct {
	From, To time.Time
	Points   int // points inside the window

	// Net P&L: the change of the cumulative total (realised + unrealised,
	// net of fees) from the window's first point to its last.
	NetPnL    float64 // coin
	NetPnLUSD float64 // coin earned, valued at the window's last spot

	Equity      float64 // coin, at the window start (the parameter)
	EquityKnown bool    // Equity > 0: the percentages below are defined
	ReturnPct   float64 // NetPnL ÷ Equity, in coin

	// Hold benchmark: what the same equity did by just holding the coin,
	// spot at the first point to spot at the last (points with a spot only).
	SpotStart, SpotEnd float64
	HoldKnown          bool
	HoldReturnUSDPct   float64 // spot end ÷ spot start − 1
	HoldReturnCoinPct  float64 // always 0: a held coin is still one coin
	// The strategy in USD: (equity + net P&L) at the end spot against equity
	// at the start spot. Needs both EquityKnown and HoldKnown.
	ReturnUSDPct    float64
	ExcessReturnPct float64 // ReturnUSDPct − HoldReturnUSDPct

	// Max drawdown: the largest fall of the total from a running peak.
	MaxDrawdown    float64 // coin, ≤ 0
	MaxDrawdownPct float64 // of Equity

	// Worst 7 days: the lowest change of the total from any point to the
	// last point at most 7 days later. Only windows that end inside the data
	// count (the history must span 7 days).
	WorstWeekKnown bool
	WorstWeekStart time.Time
	WorstWeekEnd   time.Time
	WorstWeek      float64 // coin
	WorstWeekPct   float64 // of Equity

	// Coverage: UTC days of the window with at least one point, and the days
	// without any (reported, never filled in).
	Days        int
	DaysCovered int
	MissingDays []time.Time // UTC midnight of each day with no point
}

// Summarize computes the Summary of points within [from, to] (inclusive).
// equity is the account's equity in coin at the window start; ≤ 0 means
// unknown and leaves every percentage of equity undefined.
func Summarize(points []history.Point, equity float64, from, to time.Time) Summary {
	s := Summary{From: from, To: to, Equity: equity, EquityKnown: equity > 0}
	if to.Before(from) {
		return s
	}
	in := window(points, from, to)
	s.Points = len(in)
	s.Days, s.DaysCovered, s.MissingDays = coverage(in, from, to)
	if len(in) == 0 {
		return s
	}

	first, last := in[0], in[len(in)-1]
	s.NetPnL = last.Total - first.Total
	s.NetPnLUSD = s.NetPnL * lastSpot(in)
	if s.EquityKnown {
		s.ReturnPct = pct(s.NetPnL, equity)
	}

	s.SpotStart, s.SpotEnd = firstSpot(in), lastSpot(in)
	if s.SpotStart > 0 && s.SpotEnd > 0 {
		s.HoldKnown = true
		s.HoldReturnUSDPct = (s.SpotEnd/s.SpotStart - 1) * 100
		if s.EquityKnown {
			endUSD := (equity + s.NetPnL) * s.SpotEnd
			startUSD := equity * s.SpotStart
			s.ReturnUSDPct = (endUSD/startUSD - 1) * 100
			s.ExcessReturnPct = s.ReturnUSDPct - s.HoldReturnUSDPct
		}
	}

	s.MaxDrawdown = maxDrawdown(in)
	if s.EquityKnown {
		s.MaxDrawdownPct = pct(s.MaxDrawdown, equity)
	}

	if start, end, change, ok := worstWeek(in); ok {
		s.WorstWeekKnown = true
		s.WorstWeekStart, s.WorstWeekEnd, s.WorstWeek = start, end, change
		if s.EquityKnown {
			s.WorstWeekPct = pct(change, equity)
		}
	}
	return s
}

// window returns the points in [from, to], sorted by time.
func window(points []history.Point, from, to time.Time) []history.Point {
	var in []history.Point
	for _, p := range points {
		if !p.T.Before(from) && !p.T.After(to) {
			in = append(in, p)
		}
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].T.Before(in[j].T) })
	return in
}

// coverage counts the UTC days of [from, to] and lists those with no point.
func coverage(in []history.Point, from, to time.Time) (days, covered int, missing []time.Time) {
	seen := make(map[time.Time]bool, len(in))
	for _, p := range in {
		seen[utcDay(p.T)] = true
	}
	for d := utcDay(from); !d.After(to); d = d.AddDate(0, 0, 1) {
		days++
		if seen[d] {
			covered++
		} else {
			missing = append(missing, d)
		}
	}
	return days, covered, missing
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// firstSpot and lastSpot skip points recorded without a spot (zero).
func firstSpot(in []history.Point) float64 {
	for _, p := range in {
		if p.Spot > 0 {
			return p.Spot
		}
	}
	return 0
}

func lastSpot(in []history.Point) float64 {
	for i := len(in) - 1; i >= 0; i-- {
		if in[i].Spot > 0 {
			return in[i].Spot
		}
	}
	return 0
}

// maxDrawdown is the lowest total − running peak (≤ 0).
func maxDrawdown(in []history.Point) float64 {
	peak, worst := in[0].Total, 0.0
	for _, p := range in {
		if p.Total > peak {
			peak = p.Total
		}
		if dd := p.Total - peak; dd < worst {
			worst = dd
		}
	}
	return worst
}

// worstWeek finds, for each start point, the last point at most a Week later
// and returns the window with the lowest change of the total. A start whose
// week runs past the last point is skipped: its change is not known yet.
func worstWeek(in []history.Point) (start, end time.Time, change float64, ok bool) {
	lastT := in[len(in)-1].T
	j := 0
	for i := range in {
		weekEnd := in[i].T.Add(Week)
		if weekEnd.After(lastT) {
			break
		}
		if j < i {
			j = i
		}
		for j+1 < len(in) && !in[j+1].T.After(weekEnd) {
			j++
		}
		c := in[j].Total - in[i].Total
		if !ok || c < change {
			start, end, change, ok = in[i].T, in[j].T, c, true
		}
	}
	return start, end, change, ok
}

func pct(v, base float64) float64 { return v / base * 100 }
