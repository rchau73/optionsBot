package evidence

import (
	"fmt"
	"math"
	"strings"
)

// Sample size (#772): every figure the pack and the weekly report render says
// how much data stands behind it — days, whole weeks and, where the figure
// depends on them, the tail events — and is Thin under a floor, so a result
// over nine days does not read like one over twelve weeks. The number stays
// visible; Thin only adds ThinNote beside it.

// The floors, in one place. They are ASSUMPTIONS taken from the rules already
// in this package, to be revisited after 8 more weeks of history (from
// 2026-11-13, so in 2027-W02): docs/agents/evidence.md "Sample size".
const (
	// SampleMinWeeks is the PRD §6 kill test's own window (ScoreboardWeeks):
	// fewer weeks than the go/no-go reads are too few to conclude anything.
	SampleMinWeeks = ScoreboardWeeks
	// SampleMinDays is the same window in days.
	SampleMinDays = 7 * SampleMinWeeks
	// SampleMinEvents is the fewest tail events (leg closes by a stop-loss, a
	// delta exit or a GEX shed; for the hedge trial, hedge orders and shed
	// legs) a figure that depends on them needs: a short strangle's result is
	// made by its few bad days, and under ten of them nothing says how the
	// next ones go.
	SampleMinEvents = 10
)

// ThinNote is printed beside a Thin figure, with its days.
const ThinNote = "too few to conclude"

// The events counted, as printed.
const (
	TailEventsName  = "stops, delta exits and sheds"
	HedgeEventsName = "hedge orders and shed legs"
)

// TailReasons are the journal's close reasons counted as tail events
// (orders.CloseReasonLabel): stop-loss, delta exit, GEX shed.
var TailReasons = []string{"stop_loss", "delta_exit", "gamma_regime"}

// Sample is how much data stands behind one figure. Events is nil when the
// figure does not depend on events.
type Sample struct {
	Days       int    `json:"days"`
	Weeks      int    `json:"weeks"`
	Events     *int   `json:"events,omitempty"`
	EventsName string `json:"events_name,omitempty"`
	Thin       bool   `json:"thin"`
}

// SpanSample is a figure over days of data; its weeks are the whole weeks.
func SpanSample(days int, events *int, eventsName string) Sample {
	return newSample(days, days/7, events, eventsName)
}

// WeeksSample is a figure over a series of weekly reports, 7 days each.
func WeeksSample(weeks int, events *int, eventsName string) Sample {
	return newSample(7*weeks, weeks, events, eventsName)
}

func newSample(days, weeks int, events *int, eventsName string) Sample {
	s := Sample{Days: max(days, 0), Weeks: max(weeks, 0), Events: events}
	if events != nil {
		s.EventsName = eventsName
	}
	s.Thin = s.Days < SampleMinDays || s.Weeks < SampleMinWeeks || (events != nil && *events < SampleMinEvents)
	return s
}

// Text is the sample as printed: "9 days · 1 week · 2 stops, delta exits and
// sheds · too few to conclude (9 days)".
func (s Sample) Text() string {
	parts := []string{plural(s.Days, "day"), plural(s.Weeks, "week")}
	if s.Events != nil {
		parts = append(parts, fmt.Sprintf("%d %s", *s.Events, s.EventsName))
	}
	if s.Thin {
		parts = append(parts, fmt.Sprintf("%s (%s)", ThinNote, plural(s.Days, "day")))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// tailEvents counts the closes of l by a tail reason.
func tailEvents(byReason map[string]ReasonStat) int {
	n := 0
	for _, reason := range TailReasons {
		n += byReason[reason].Closes
	}
	return n
}

// rvlSample is a replay-vs-live figure's: the replay's window (the week
// when it did not run) and the live week's tail events.
func rvlSample(r Report) Sample {
	from, to := r.From, r.To
	if r.Replay.Ran {
		from, to = r.Replay.Start, r.Replay.End
	}
	days := int(math.Round(to.Sub(from).Hours() / 24))
	return SpanSample(days, intPtr(tailEvents(r.Live.ByReason)), TailEventsName)
}

// hedgeSample is a hedge-trial figure's: the days measured and the hedge
// orders (real and paper) and shed legs; an unknown count is 0.
func hedgeSample(h HedgeTrial) *Sample {
	n := func(v *int) int {
		if v == nil {
			return 0
		}
		return *v
	}
	s := SpanSample(n(h.Days), intPtr(n(h.RealTrades)+n(h.PaperTrades)+n(h.Legs)), HedgeEventsName)
	return &s
}

// FigureSample is one figure of the weekly report with its sample.
type FigureSample struct {
	Figure  string `json:"figure"`
	Display string `json:"display"`
	Sample  Sample `json:"sample"`
}

// The weekly report's figures that carry a sample (Report.Samples).
const (
	FigureNetReturn   = "Net return, % of the starting equity, in coin"
	FigureMaxDrawdown = "Max drawdown, % of the starting equity"
	FigureWorst7Days  = "Worst 7 days, % of the starting equity"
	FigureHold        = "Holding the coin, in USD"
	FigureExcess      = "Excess over holding the coin"
	FigureGap         = "Gap (replay − live), of the starting equity"
	FigureHedge       = "Advantage of the hedge over the shed"
	FigureScoreboard  = "Engine ratio (weekly Sharpe)"
)

// buildSamples is the weekly report's sample table: its headline figures,
// as % of the starting equity (the hedge's advantage in coin on testnet
// only), each with its sample. Values are the report's, not recomputed.
func buildSamples(r Report, account string) []FigureSample {
	l, h := r.Live.Perf, r.Hold
	result := SpanSample(l.DaysCovered, intPtr(tailEvents(r.Live.ByReason)), TailEventsName)
	days := SpanSample(l.DaysCovered, nil, "")
	pctOr := func(v float64, known bool, why string) string {
		if !known {
			return why
		}
		return packDisplay(v, packPct)
	}
	noPoint := l.Points == 0
	equity := "the starting equity is unknown"
	if noPoint {
		equity = "no P&L history point"
	}
	worst := pctOr(l.WorstWeekPct, l.WorstWeekKnown && l.EquityKnown, equity)
	if !l.WorstWeekKnown {
		worst = "the history does not span 7 days"
	}
	excess := "needs the spot and the starting equity"
	if h.StrategyKnown {
		excess = packDisplay(h.ExcessReturnPct, packPP)
	}
	gap := NotReported
	switch {
	case r.RVL.Comparable && r.RVL.GapEquityPP != nil:
		gap = packDisplay(*r.RVL.GapEquityPP, packPP)
	case r.RVL.Label != "":
		gap = PackNotComparable
	}
	hedge := r.Hedge.Status
	if hedge == "" {
		hedge = NotReported
	}
	if r.Hedge.Advantage != nil && account != AccountLive && r.Hedge.Account != AccountLive {
		hedge = packDisplay(*r.Hedge.Advantage, strings.ToUpper(r.Bot))
	}
	score := "no ratio"
	if r.Score.EngineRatio != nil {
		score = packDisplay(*r.Score.EngineRatio, packRatio)
	}
	return []FigureSample{
		{FigureNetReturn, pctOr(l.ReturnPct, !noPoint && l.EquityKnown, equity), result},
		{FigureMaxDrawdown, pctOr(l.MaxDrawdownPct, !noPoint && l.EquityKnown, equity), result},
		{FigureWorst7Days, worst, result},
		{FigureHold, pctOr(h.HoldReturnUSDPct, h.Known, "no P&L history point with a spot"), days},
		{FigureExcess, excess, result},
		{FigureGap, gap, rvlSample(r)},
		{FigureHedge, hedge, *hedgeSample(r.Hedge)},
		{FigureScoreboard, score, WeeksSample(r.Score.WeeksCounted, nil, "")},
	}
}

// writeSamples writes the sample table: each figure, its value and the data
// behind it, a Thin one marked "too few to conclude" with its days.
func writeSamples(p func(string, ...any), samples []FigureSample) {
	p("## Sample size\n\n")
	p("How much data stands behind each headline figure. Under %d days, %d weeks or, where the figure depends on them, %d %s (hedge trial: %s), a figure is too few to conclude: the number stays, read it as one sample, not a result.\n\n",
		SampleMinDays, SampleMinWeeks, SampleMinEvents, TailEventsName, HedgeEventsName)
	p("| Figure | Value | Sample |\n|---|---|---|\n")
	for _, f := range samples {
		p("| %s | %s | %s |\n", f.Figure, packCell(f.Display), f.Sample.Text())
	}
	p("\n")
}

func intPtr(v int) *int { return &v }

func ptrTo[T any](v T) *T { return &v }
