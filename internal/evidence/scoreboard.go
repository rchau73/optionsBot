package evidence

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The twelve-week scoreboard (#537): PRD §6's kill test — does the engine
// beat holding the coin, risk-adjusted, over the last 12 weekly reports —
// measured the way docs/product/go-no-go.md §3 says. This is the single
// calculation; scripts/product/go_no_go.py row 3 is pinned to it by the
// shared fixture tests/testdata/scoreboard/cases.json. Ratios, counts and
// weeks only: no coin or USD amount (a bot may run live).

// ScoreboardWeeks is PRD §6's window (go-no-go.md §3, E2_MIN_WEEKS in
// go_no_go.py). Changing it is a PRD pull request.
const ScoreboardWeeks = 12

// The scoreboard's statuses.
const (
	ScoreMeets          = "meets"
	ScoreFails          = "fails"
	ScoreNotEnoughWeeks = "not enough weeks"
	ScoreNoSpread       = "no spread"
)

// ScoreboardLabel sits next to the figures in the JSON and the Markdown.
const ScoreboardLabel = "past performance, net of fees — a measured window, not a promise of returns"

// noSpread is the standard deviation under which a series has no ratio
// (identical returns differ only by rounding).
const noSpread = 1e-9

// WeekReturns is one week's USD return of the engine and of holding the
// coin, in % (Hold.ReturnUSDPct and Hold.HoldReturnUSDPct).
type WeekReturns struct {
	Week   string  // ISO week, e.g. 2026-W42
	Engine float64 // return_usd_pct
	Hold   float64 // hold_return_usd_pct
}

// Scoreboard is the kill test as of one report's week.
type Scoreboard struct {
	Label        string   `json:"label"`
	Source       string   `json:"source"`
	Note         string   `json:"note,omitempty"` // why it was not computed
	Status       string   `json:"status"`         // meets | fails | not enough weeks | no spread ("" = not computed)
	WeeksCounted int      `json:"weeks_counted"`  // reports with both returns known, at most WeeksNeeded
	WeeksNeeded  int      `json:"weeks_needed"`
	WeeksToGo    int      `json:"weeks_to_go"` // WeeksNeeded − WeeksCounted
	FirstWeek    string   `json:"first_week,omitempty"`
	LastWeek     string   `json:"last_week,omitempty"`
	EngineRatio  *float64 `json:"engine_ratio"` // weekly Sharpe ratio; nil under a full window or with no spread
	HoldRatio    *float64 `json:"hold_ratio"`
	// The ISO weeks from the first counted week to the report's week with
	// no report or without both returns — listed, never filled.
	MissingWeeks []string `json:"missing_weeks"`
}

// ScoreWeeks reads the kill test from weeks (both returns known, any
// order) as of the ISO week asOf: the last `needed` weeks up to asOf.
func ScoreWeeks(weeks []WeekReturns, asOf string, needed int) Scoreboard {
	var usable []WeekReturns
	for _, w := range weeks {
		if w.Week <= asOf {
			usable = append(usable, w)
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].Week < usable[j].Week })
	if len(usable) > needed {
		usable = usable[len(usable)-needed:]
	}
	s := Scoreboard{Label: ScoreboardLabel, WeeksCounted: len(usable), WeeksNeeded: needed,
		WeeksToGo: needed - len(usable), MissingWeeks: []string{}}
	if len(usable) > 0 {
		s.FirstWeek, s.LastWeek = usable[0].Week, usable[len(usable)-1].Week
		s.MissingWeeks = weeksWithout(usable, asOf)
	}
	if len(usable) < needed {
		s.Status = ScoreNotEnoughWeeks
		return s
	}
	engine, hold := make([]float64, len(usable)), make([]float64, len(usable))
	for i, w := range usable {
		engine[i], hold[i] = w.Engine, w.Hold
	}
	s.EngineRatio, s.HoldRatio = SharpeRatio(engine), SharpeRatio(hold)
	switch {
	case s.EngineRatio == nil || s.HoldRatio == nil:
		s.Status = ScoreNoSpread
	case *s.EngineRatio > *s.HoldRatio:
		s.Status = ScoreMeets
	default:
		s.Status = ScoreFails
	}
	return s
}

// SharpeRatio is mean ÷ sample standard deviation, no risk-free rate; nil
// under two values or with no spread.
func SharpeRatio(xs []float64) *float64 {
	if len(xs) < 2 {
		return nil
	}
	var mean float64
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / float64(len(xs)-1))
	if sd <= noSpread {
		return nil
	}
	r := mean / sd
	return &r
}

// weeksWithout are the ISO weeks from usable's first to asOf missing from
// usable (sorted).
func weeksWithout(usable []WeekReturns, asOf string) []string {
	have := map[string]bool{}
	for _, w := range usable {
		have[w.Week] = true
	}
	out := []string{}
	first, err := ParseWeek(usable[0].Week)
	if err != nil {
		return out
	}
	for from := first.From; ; from = from.AddDate(0, 0, 7) {
		label := weekLabel(from)
		if label > asOf {
			return out
		}
		if !have[label] {
			out = append(out, label)
		}
	}
}

// buildScoreboard is the scoreboard of the report r being built: its own
// week's returns (r.Hold) and the earlier weekly reports'.
func buildScoreboard(in Inputs, h Hold) Scoreboard {
	w := in.Window
	if wk, err := ParseWeek(w.Label); err != nil || wk.Label != w.Label {
		return Scoreboard{Label: ScoreboardLabel, Source: in.WeeklySource, WeeksNeeded: ScoreboardWeeks,
			Note: "a -from/-to report is not an ISO week", MissingWeeks: []string{}}
	}
	var weeks []WeekReturns
	for _, r := range in.Earlier {
		if r.Week < w.Label && r.Hold.Known && r.Hold.StrategyKnown {
			weeks = append(weeks, WeekReturns{Week: r.Week, Engine: r.Hold.ReturnUSDPct, Hold: r.Hold.HoldReturnUSDPct})
		}
	}
	if h.Known && h.StrategyKnown {
		weeks = append(weeks, WeekReturns{Week: w.Label, Engine: h.ReturnUSDPct, Hold: h.HoldReturnUSDPct})
	}
	s := ScoreWeeks(weeks, w.Label, ScoreboardWeeks)
	s.Source = in.WeeklySource + " (hold.return_usd_pct and hold.hold_return_usd_pct) and this week's; docs/product/go-no-go.md §3"
	return s
}

// writeScoreboard is the scoreboard's Markdown section: ratios, counts and
// weeks only.
func writeScoreboard(p func(string, ...any), s Scoreboard) {
	p("## Twelve-week scoreboard: engine vs holding the coin\n\n")
	p("**Past performance, net of fees — a measured window, not a promise of returns.** ")
	p("PRD §6's test: the engine's weekly Sharpe ratio (mean ÷ standard deviation of its weekly USD returns, no risk-free rate) above holding the coin's, over the last %d weekly reports. Source: %s.\n\n",
		s.WeeksNeeded, s.Source)
	if s.Status == "" {
		p("Not computed: %s.\n\n", s.Note)
		return
	}
	p("- Status: **%s** — %d of %d weeks counted", s.Status, s.WeeksCounted, s.WeeksNeeded)
	if s.FirstWeek != "" {
		p(" (%s → %s)", s.FirstWeek, s.LastWeek)
	}
	if s.WeeksToGo > 0 {
		p("; %d more week(s) needed", s.WeeksToGo)
	}
	p("\n")
	switch s.Status {
	case ScoreMeets, ScoreFails:
		p("- Engine ratio **%s** · hold ratio **%s** (past performance, net of fees)\n", ratioText(s.EngineRatio), ratioText(s.HoldRatio))
	case ScoreNoSpread:
		p("- Engine ratio %s · hold ratio %s: a series with no spread has no ratio, so the test cannot be read\n", ratioText(s.EngineRatio), ratioText(s.HoldRatio))
	}
	if len(s.MissingWeeks) > 0 {
		p("- Weeks missing (no report, or a return not known): %s\n", strings.Join(s.MissingWeeks, ", "))
	} else {
		p("- Weeks missing: none\n")
	}
	p("\n")
}

func ratioText(r *float64) string {
	if r == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *r)
}
