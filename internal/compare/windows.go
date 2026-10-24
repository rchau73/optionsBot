package compare

import (
	"fmt"
	"sort"
	"time"
)

// The comparison window by window (#301): the same backtest and replay once
// per UTC week (or day) of the span, so a proposal that wins only in one
// lucky week shows as fragile. Stress is not windowed: it is one run on the
// captured book, in the main report.

// Unit is a window's length.
type Unit string

const (
	Week Unit = "week" // ISO weeks, Monday to Sunday UTC
	Day  Unit = "day"
)

// ParseUnit reads -windows: "" (no windows), "week" or "day".
func ParseUnit(s string) (Unit, error) {
	switch u := Unit(s); u {
	case "", Week, Day:
		return u, nil
	}
	return "", fmt.Errorf("windows %q: want week or day", s)
}

// Window is one slice of the span: its first and last UTC day, both
// included. The first and last week are clipped to the span.
type Window struct {
	Label string    `json:"label"` // "2026-W42" or "2026-10-12"
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

// Windows cuts [from, to] (UTC days, both included) into weeks or days.
func Windows(from, to time.Time, unit Unit) []Window {
	from, to = from.UTC().Truncate(24*time.Hour), to.UTC().Truncate(24*time.Hour)
	var out []Window
	for start := from; !start.After(to); {
		end := start
		label := start.Format(time.DateOnly)
		if unit == Week {
			// Days to Sunday: Monday is 6 away, Sunday 0.
			end = start.AddDate(0, 0, (7-int(start.Weekday()))%7)
			y, w := start.ISOWeek()
			label = fmt.Sprintf("%d-W%02d", y, w)
		}
		if end.After(to) {
			end = to
		}
		out = append(out, Window{Label: label, From: start, To: end})
		start = end.AddDate(0, 0, 1)
	}
	return out
}

// WindowReport is the comparison per window and its summary.
type WindowReport struct {
	Unit Unit `json:"unit"`
	// StressNote says the stress is not windowed.
	StressNote string        `json:"stress_note"`
	Rows       []WindowRow   `json:"rows"`
	Summary    WindowSummary `json:"summary"`
}

// StressNotWindowed is WindowReport.StressNote.
const StressNotWindowed = "stress is one run on the captured book (section Stress shocks), not windowed"

// WindowRow is one window: each source's figures for both configs and the
// difference, or why the source did not run there.
type WindowRow struct {
	Window
	Backtest WindowSide `json:"backtest"` // USD
	Replay   WindowSide `json:"replay"`   // coin
}

// WindowSide is one source in one window. Ran is false when either config
// did not run (no book in the window, an error): Note says why, and the
// window is left out of that source's summary.
type WindowSide struct {
	Ran      bool    `json:"ran"`
	Note     string  `json:"note,omitempty"`
	Base     Figures `json:"base"`
	Proposed Figures `json:"proposed"`
	Diff     Figures `json:"diff"` // proposed − base
}

// Figures are the figures compared per window, in the source's unit.
type Figures struct {
	Net          float64 `json:"net"`
	ReturnPct    float64 `json:"return_pct"`
	WorstWeek    float64 `json:"worst_week"` // ≤ 0
	WorstWeekPct float64 `json:"worst_week_pct"`
	MaxDrawdown  float64 `json:"max_drawdown"`
}

func (f Figures) minus(o Figures) Figures {
	return Figures{Net: f.Net - o.Net, ReturnPct: f.ReturnPct - o.ReturnPct, WorstWeek: f.WorstWeek - o.WorstWeek,
		WorstWeekPct: f.WorstWeekPct - o.WorstWeekPct, MaxDrawdown: f.MaxDrawdown - o.MaxDrawdown}
}

// WindowSummary sums the windows each source ran in.
type WindowSummary struct {
	Backtest SourceSummary `json:"backtest"`
	Replay   SourceSummary `json:"replay"`
	// OrderFlips are the windows where one source has the proposal ahead
	// and the other has it behind (both ran; a tie is no flip).
	OrderFlips []string `json:"order_flips"`
}

// SourceSummary is one source over the windows it ran in.
type SourceSummary struct {
	Run          int      `json:"run"`            // windows with both configs' figures
	ProposedWins int      `json:"proposed_wins"`  // proposed net higher
	BaseWins     int      `json:"base_wins"`      // base net higher
	MedianDiff   float64  `json:"median_diff"`    // median of proposed − base net
	MedianDiffPP float64  `json:"median_diff_pp"` // median of proposed − base return, percentage points
	BestDiff     float64  `json:"best_diff"`      // the proposal's best window, net difference
	WorstDiff    float64  `json:"worst_diff"`     // and its worst: the spread is best − worst
	WorstBase    *WorstOf `json:"worst_base,omitempty"`
	WorstProp    *WorstOf `json:"worst_proposed,omitempty"`
	Missing      []string `json:"missing"` // windows not run, never filled
}

// WorstOf is a config's worst window: the lowest net.
type WorstOf struct {
	Label     string  `json:"label"`
	Net       float64 `json:"net"`
	ReturnPct float64 `json:"return_pct"`
}

// tie is the net difference under which two runs count as equal.
const tie = 1e-9

// RunWindows runs Run once per window of in's span — the same data, each
// replay from a flat book at the window's first day — and keeps the
// backtest and replay figures. Stress is not re-run (in's outcomes are not
// read).
func RunWindows(in Inputs, unit Unit) WindowReport {
	wr := WindowReport{Unit: unit, StressNote: StressNotWindowed}
	in.BaseStress, in.ProposedStress = nil, nil
	for _, w := range Windows(in.From, in.To, unit) {
		win := in
		win.From, win.To = w.From, w.To
		r := Run(win)
		wr.Rows = append(wr.Rows, WindowRow{Window: w, Backtest: backtestSide(r.Backtest), Replay: replaySide(r.Replay)})
	}
	wr.Summary = SummarizeWindows(wr.Rows)
	return wr
}

func backtestSide(b Backtest) WindowSide {
	fig := func(r BacktestRun) Figures {
		return Figures{Net: r.NetUSD, ReturnPct: r.ReturnPct, WorstWeek: r.WorstWeekUSD, WorstWeekPct: r.WorstWeekPct, MaxDrawdown: r.MaxDrawdownUSD}
	}
	return side(b.Note, b.Base.Error, b.Proposed.Error, fig(b.Base), fig(b.Proposed))
}

func replaySide(rp Replay) WindowSide {
	fig := func(r ReplayRun) Figures {
		return Figures{Net: r.Net, ReturnPct: r.ReturnPct, WorstWeek: r.WorstWeek, WorstWeekPct: r.WorstWeekPct, MaxDrawdown: r.MaxDrawdown}
	}
	return side(rp.Note, rp.Base.Error, rp.Proposed.Error, fig(rp.Base), fig(rp.Proposed))
}

func side(note, baseErr, propErr string, base, prop Figures) WindowSide {
	switch {
	case note != "":
		return WindowSide{Note: note}
	case baseErr != "":
		return WindowSide{Note: "base: " + baseErr}
	case propErr != "":
		return WindowSide{Note: "proposed: " + propErr}
	}
	return WindowSide{Ran: true, Base: base, Proposed: prop, Diff: prop.minus(base)}
}

// SummarizeWindows counts, per source, the windows it ran in; the others are
// listed as missing and counted nowhere.
func SummarizeWindows(rows []WindowRow) WindowSummary {
	s := WindowSummary{
		Backtest: summarize(rows, func(r WindowRow) WindowSide { return r.Backtest }),
		Replay:   summarize(rows, func(r WindowRow) WindowSide { return r.Replay }),
	}
	for _, r := range rows {
		if r.Backtest.Ran && r.Replay.Ran && sign(r.Backtest.Diff.Net)*sign(r.Replay.Diff.Net) < 0 {
			s.OrderFlips = append(s.OrderFlips, r.Label)
		}
	}
	return s
}

func summarize(rows []WindowRow, of func(WindowRow) WindowSide) SourceSummary {
	var s SourceSummary
	var diffs, diffsPP []float64
	for _, r := range rows {
		side := of(r)
		if !side.Ran {
			s.Missing = append(s.Missing, r.Label)
			continue
		}
		s.Run++
		switch sign(side.Diff.Net) {
		case 1:
			s.ProposedWins++
		case -1:
			s.BaseWins++
		}
		diffs, diffsPP = append(diffs, side.Diff.Net), append(diffsPP, side.Diff.ReturnPct)
		if s.WorstBase == nil || side.Base.Net < s.WorstBase.Net {
			s.WorstBase = &WorstOf{Label: r.Label, Net: side.Base.Net, ReturnPct: side.Base.ReturnPct}
		}
		if s.WorstProp == nil || side.Proposed.Net < s.WorstProp.Net {
			s.WorstProp = &WorstOf{Label: r.Label, Net: side.Proposed.Net, ReturnPct: side.Proposed.ReturnPct}
		}
	}
	if len(diffs) > 0 {
		s.MedianDiff, s.MedianDiffPP = median(diffs), median(diffsPP)
		sort.Float64s(diffs)
		s.WorstDiff, s.BestDiff = diffs[0], diffs[len(diffs)-1]
	}
	return s
}

// sign is −1, 0 or 1, with |x| ≤ tie as 0.
func sign(x float64) int {
	switch {
	case x > tie:
		return 1
	case x < -tie:
		return -1
	}
	return 0
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
