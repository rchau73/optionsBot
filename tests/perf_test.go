package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/perf"
)

// perfDay0 is the first day of the hand-made series (points at 12:00 UTC).
var perfDay0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// perfDaily builds one point per day from cumulative totals, at a fixed spot.
func perfDaily(totals []float64, spot float64) []history.Point {
	pts := make([]history.Point, len(totals))
	for i, total := range totals {
		pts[i] = history.Point{T: perfDay0.AddDate(0, 0, i), Total: total, Spot: spot}
	}
	return pts
}

// cumulative turns daily changes into cumulative totals starting at 0.
func cumulative(daily []float64) []float64 {
	out := []float64{0}
	for _, d := range daily {
		out = append(out, out[len(out)-1]+d)
	}
	return out
}

func perfNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func wholeWindow(pts []history.Point) (time.Time, time.Time) {
	return pts[0].T.Add(-time.Hour), pts[len(pts)-1].T.Add(time.Hour)
}

// Scenario: worst week — daily P&L with one week losing 3 % and the rest
// gaining → WorstWeek is that week with −3 %.
func TestPerf_WorstWeek(t *testing.T) {
	var daily []float64
	for i := 0; i < 10; i++ {
		daily = append(daily, 0.002) // days 0→10 gain
	}
	for i := 0; i < 7; i++ {
		daily = append(daily, -0.03/7) // days 10→17 lose 3 % of 1 coin
	}
	for i := 0; i < 10; i++ {
		daily = append(daily, 0.002)
	}
	pts := perfDaily(cumulative(daily), 60000)
	from, to := wholeWindow(pts)

	s := perf.Summarize(pts, 1, from, to)
	if !s.WorstWeekKnown {
		t.Fatal("worst week unknown on 27 days of data")
	}
	if !s.WorstWeekStart.Equal(perfDay0.AddDate(0, 0, 10)) || !s.WorstWeekEnd.Equal(perfDay0.AddDate(0, 0, 17)) {
		t.Errorf("worst week %s → %s, want day 10 → day 17", s.WorstWeekStart, s.WorstWeekEnd)
	}
	if !perfNear(s.WorstWeek, -0.03) || !perfNear(s.WorstWeekPct, -3) {
		t.Errorf("worst week %.6f coin %.4f %%, want -0.03 coin -3 %%", s.WorstWeek, s.WorstWeekPct)
	}
	if !perfNear(s.MaxDrawdown, -0.03) || !perfNear(s.MaxDrawdownPct, -3) {
		t.Errorf("max drawdown %.6f coin %.4f %%, want -0.03 / -3 %%", s.MaxDrawdown, s.MaxDrawdownPct)
	}
	if !perfNear(s.NetPnL, 0.01) || !perfNear(s.ReturnPct, 1) {
		t.Errorf("net %.6f coin %.4f %%, want 0.01 coin 1 %%", s.NetPnL, s.ReturnPct)
	}
}

// Scenario: hold benchmark — spot 60,000 at the start and 66,000 at the end →
// the hold return is +10 % in USD and 0 % in coin.
func TestPerf_HoldBenchmark(t *testing.T) {
	pts := []history.Point{
		{T: perfDay0, Total: 0, Spot: 60000},
		{T: perfDay0.AddDate(0, 0, 1), Total: 0.005, Spot: 0}, // no spot recorded: skipped for the benchmark
		{T: perfDay0.AddDate(0, 0, 2), Total: 0.01, Spot: 66000},
	}
	from, to := wholeWindow(pts)

	s := perf.Summarize(pts, 1, from, to)
	if !s.HoldKnown {
		t.Fatal("hold benchmark unknown with spot at both ends")
	}
	if s.SpotStart != 60000 || s.SpotEnd != 66000 {
		t.Errorf("spot %v → %v, want 60000 → 66000", s.SpotStart, s.SpotEnd)
	}
	if !perfNear(s.HoldReturnUSDPct, 10) || s.HoldReturnCoinPct != 0 {
		t.Errorf("hold %.4f %% USD %.4f %% coin, want +10 / 0", s.HoldReturnUSDPct, s.HoldReturnCoinPct)
	}
	// 1.01 coin at 66,000 against 1 coin at 60,000 = +11.1 %, 1.1 points over holding.
	if !perfNear(s.ReturnUSDPct, 11.1) || !perfNear(s.ExcessReturnPct, 1.1) {
		t.Errorf("strategy %.4f %% USD, excess %.4f, want 11.1 / 1.1", s.ReturnUSDPct, s.ExcessReturnPct)
	}
	if !perfNear(s.NetPnLUSD, 0.01*66000) {
		t.Errorf("net USD %.2f, want 660", s.NetPnLUSD)
	}
}

// Scenario: gaps — a day with no points → the summary reports the gap and
// does not invent values.
func TestPerf_GapsReportedNotInvented(t *testing.T) {
	pts := []history.Point{
		{T: perfDay0, Total: 0, Spot: 60000},
		{T: perfDay0.AddDate(0, 0, 1), Total: 0.01, Spot: 60000},
		// day 2: nothing recorded
		{T: perfDay0.AddDate(0, 0, 3), Total: 0.02, Spot: 60000},
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 4, 23, 59, 0, 0, time.UTC)

	s := perf.Summarize(pts, 1, from, to)
	if s.Points != 3 {
		t.Errorf("points %d, want 3 (nothing invented)", s.Points)
	}
	if s.Days != 4 || s.DaysCovered != 3 {
		t.Errorf("days %d covered %d, want 4 / 3", s.Days, s.DaysCovered)
	}
	want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if len(s.MissingDays) != 1 || !s.MissingDays[0].Equal(want) {
		t.Errorf("missing days %v, want [%s]", s.MissingDays, want)
	}
	if !perfNear(s.NetPnL, 0.02) {
		t.Errorf("net %.6f, want 0.02", s.NetPnL)
	}
	if s.WorstWeekKnown {
		t.Error("worst week reported on 3 days of data")
	}
}

func TestPerf_HandMadeSeries(t *testing.T) {
	flat := perfDaily(make([]float64, 10), 60000)
	steady := perfDaily(cumulative([]float64{0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001}), 60000)
	crash := perfDaily([]float64{0, 0.01, 0.02, -0.03, -0.05, -0.04, -0.02, -0.01, -0.005}, 60000)

	cases := []struct {
		name       string
		pts        []history.Point
		net, dd    float64
		worstWeek  float64
		worstStart int // day index
	}{
		{"flat", flat, 0, 0, 0, 0},
		{"steady gain", steady, 0.009, 0, 0.007, 0},
		// peak 0.02 on day 2, trough −0.05 on day 4; worst 7 days day 1 → 8.
		{"crash week", crash, -0.005, -0.07, -0.015, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, to := wholeWindow(c.pts)
			s := perf.Summarize(c.pts, 2, from, to)
			if !perfNear(s.NetPnL, c.net) || !perfNear(s.MaxDrawdown, c.dd) {
				t.Errorf("net %.6f dd %.6f, want %.6f / %.6f", s.NetPnL, s.MaxDrawdown, c.net, c.dd)
			}
			if !perfNear(s.MaxDrawdownPct, c.dd/2*100) {
				t.Errorf("dd %.4f %%, want %.4f", s.MaxDrawdownPct, c.dd/2*100)
			}
			if !s.WorstWeekKnown || !perfNear(s.WorstWeek, c.worstWeek) || !s.WorstWeekStart.Equal(perfDay0.AddDate(0, 0, c.worstStart)) {
				t.Errorf("worst week known=%v %.6f from %s, want %.6f from day %d",
					s.WorstWeekKnown, s.WorstWeek, s.WorstWeekStart, c.worstWeek, c.worstStart)
			}
			if !perfNear(s.HoldReturnUSDPct, 0) || len(s.MissingDays) != 0 {
				t.Errorf("hold %.4f missing %v, want 0 / none", s.HoldReturnUSDPct, s.MissingDays)
			}
		})
	}
}

func TestPerf_EdgeCases(t *testing.T) {
	pts := perfDaily(cumulative([]float64{0.01, -0.02}), 60000)
	from, to := wholeWindow(pts)

	t.Run("unknown equity leaves percentages undefined", func(t *testing.T) {
		s := perf.Summarize(pts, 0, from, to)
		if s.EquityKnown || s.ReturnPct != 0 || s.MaxDrawdownPct != 0 || s.ReturnUSDPct != 0 {
			t.Errorf("equity unknown but got %+v", s)
		}
		if !perfNear(s.NetPnL, -0.01) || !perfNear(s.MaxDrawdown, -0.02) {
			t.Errorf("coin figures still expected: net %.6f dd %.6f", s.NetPnL, s.MaxDrawdown)
		}
	})
	t.Run("no points", func(t *testing.T) {
		s := perf.Summarize(nil, 1, from, to)
		if s.Points != 0 || s.HoldKnown || s.WorstWeekKnown || s.NetPnL != 0 {
			t.Errorf("empty history gave %+v", s)
		}
		if s.DaysCovered != 0 || len(s.MissingDays) != s.Days || s.Days != 3 {
			t.Errorf("days %d covered %d missing %d, want 3 / 0 / 3", s.Days, s.DaysCovered, len(s.MissingDays))
		}
	})
	t.Run("points outside the window are ignored, unsorted input sorted", func(t *testing.T) {
		in := []history.Point{pts[2], pts[0], pts[1], {T: perfDay0.AddDate(0, 0, 30), Total: 5, Spot: 1}}
		s := perf.Summarize(in, 1, from, to)
		if s.Points != 3 || !perfNear(s.NetPnL, -0.01) || s.SpotEnd != 60000 {
			t.Errorf("points %d net %.6f spot end %v, want 3 / -0.01 / 60000", s.Points, s.NetPnL, s.SpotEnd)
		}
	})
	t.Run("reversed window", func(t *testing.T) {
		s := perf.Summarize(pts, 1, to, from)
		if s.Points != 0 || s.Days != 0 {
			t.Errorf("reversed window gave %+v", s)
		}
	})
	t.Run("no spot at all", func(t *testing.T) {
		in := []history.Point{{T: perfDay0, Total: 0}, {T: perfDay0.AddDate(0, 0, 1), Total: 0.01}}
		s := perf.Summarize(in, 1, from, to)
		if s.HoldKnown || s.ReturnUSDPct != 0 || s.NetPnLUSD != 0 {
			t.Errorf("no spot but got hold=%v usd=%.4f netUSD=%.2f", s.HoldKnown, s.ReturnUSDPct, s.NetPnLUSD)
		}
	})
}
