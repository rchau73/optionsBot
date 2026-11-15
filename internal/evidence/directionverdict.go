package evidence

import (
	"fmt"
	"math"
	"strings"
)

// The direction trial's verdict (#788): the decision rule of
// docs/direction-trial.md applied to the weekly scorecards' own figures —
// replay net_pct and max_drawdown_pct, live_minus_replay_pp and the missing
// book days — never a new calculation. A verdict only proposes: it files
// nothing and changes no config (a change of gex_direction is a config PR a
// person merges). Every threshold below is an assumption of the brief.

// DirectionTrialRule is the document the verdict follows.
const DirectionTrialRule = "docs/direction-trial.md"

// The brief's thresholds (assumptions, docs/direction-trial.md).
const (
	DirectionTrialStart          = "2026-W43" // the first complete week with both setups live
	DirectionTrialWeeks          = 6          // counted weeks the span needs
	DirectionTrialMaxMissingDays = 2          // a week missing more book days does not count
	DirectionKillStreak          = 3          // consecutive counted weeks of bias under sma by more than noise
	DirectionExtendWeeks         = 4          // the extension when the sign flips with no trend
	DirectionFlipMinWeeks        = 3          // weeks with a sign the flip check needs
)

// The verdict's statuses.
const (
	DirectionVerdictMoreWeeks = "needs more weeks"
	DirectionVerdictNoDiff    = "no difference"
	DirectionVerdictBias      = "bias better"
	DirectionVerdictSMA       = "sma better"
	DirectionVerdictKill      = "kill rule hit"
)

// DirectionAssumptions are the thresholds the verdict uses, named in the
// scorecard so nobody reads them as measured.
var DirectionAssumptions = []string{
	fmt.Sprintf("span: %d counted weeks from %s", DirectionTrialWeeks, DirectionTrialStart),
	fmt.Sprintf("a week missing more than %d days of recorded books does not count", DirectionTrialMaxMissingDays),
	"noise = the mean |live − replay| of the two live rows stands for the replay's own error",
	fmt.Sprintf("kill rule: %d consecutive counted weeks of bias under sma by more than noise, or stable's live max drawdown over 2× mvp's", DirectionKillStreak),
	fmt.Sprintf("span too short: the sign of bias − sma changes on at least half the week-to-week steps (%d weeks or more) → extend by %d weeks", DirectionFlipMinWeeks, DirectionExtendWeeks),
	"review date 2026-12-07",
}

// DirectionLiveDrawdownNote says what the verdict cannot check: the
// scorecard has no live max drawdown.
const DirectionLiveDrawdownNote = "the kill rule's live half (stable BTC's live max drawdown over 2× mvp BTC's) is not in the scorecard: read it in the weekly evidence reports"

// DirectionWeekCard is one trial week and its scorecard (nil = none written).
type DirectionWeekCard struct {
	Week string
	Card *DirectionReport
}

// DirectionVerdict is the rule's outcome, the figures it used and every
// week with why it counted or not. Percentages and counts only.
type DirectionVerdict struct {
	Status  string `json:"status"`
	Why     string `json:"why"`
	Line    string `json:"line"` // the one line the E2 comment carries
	Rule    string `json:"rule"`
	Counted int    `json:"weeks_counted"`
	Needed  int    `json:"weeks_needed"`
	// Mean of the counted weeks' bias − sma replay net, and of their
	// |live − replay| gaps; nil = no counted week (no gap).
	DPP     *float64 `json:"d_pp"`
	NoisePP *float64 `json:"noise_pp"`
	// The tail: the lowest weekly replay net and the largest weekly replay
	// max drawdown of each source over the counted weeks.
	BiasWorstWeekPct *float64               `json:"bias_worst_week_pct"`
	SMAWorstWeekPct  *float64               `json:"sma_worst_week_pct"`
	BiasMaxDDPct     *float64               `json:"bias_max_drawdown_pct"`
	SMAMaxDDPct      *float64               `json:"sma_max_drawdown_pct"`
	KillWeeks        []string               `json:"kill_weeks,omitempty"`
	SpanTooShort     bool                   `json:"span_too_short"`
	SignChanges      int                    `json:"sign_changes"`
	SignSteps        int                    `json:"sign_steps"`
	NotChecked       string                 `json:"not_checked"`
	Assumptions      []string               `json:"assumptions"`
	Weeks            []DirectionVerdictWeek `json:"weeks"`
}

// DirectionVerdictWeek is one week as the verdict read it.
type DirectionVerdictWeek struct {
	Week       string    `json:"week"`
	Counted    bool      `json:"counted"`
	Why        string    `json:"why"`
	BiasNetPct *float64  `json:"bias_net_pct"`
	SMANetPct  *float64  `json:"sma_net_pct"`
	DPP        *float64  `json:"d_pp"` // bias − sma; counted weeks only
	BiasDDPct  *float64  `json:"bias_max_drawdown_pct"`
	SMADDPct   *float64  `json:"sma_max_drawdown_pct"`
	GapsPP     []float64 `json:"gaps_pp"` // |live − replay| of the live rows; counted weeks only
}

// BuildDirectionVerdict applies the brief's rule to the trial weeks, oldest
// first (a week before DirectionTrialStart is the caller's to leave out).
// Checks run in the brief's order: the kill rule (any week), the span, then
// sma better (d, then the tail guard alone), no difference, bias better.
func BuildDirectionVerdict(weeks []DirectionWeekCard) DirectionVerdict {
	v := DirectionVerdict{Rule: DirectionTrialRule, Needed: DirectionTrialWeeks,
		NotChecked: DirectionLiveDrawdownNote, Assumptions: DirectionAssumptions}
	var counted []DirectionVerdictWeek
	var gaps []float64
	for _, wc := range weeks {
		w := readTrialWeek(wc)
		v.Weeks = append(v.Weeks, w)
		if w.Counted {
			counted = append(counted, w)
			gaps = append(gaps, w.GapsPP...)
		}
	}
	v.Counted = len(counted)
	var ds []float64
	for _, w := range counted {
		ds = append(ds, *w.DPP)
		v.BiasWorstWeekPct = lowest(v.BiasWorstWeekPct, w.BiasNetPct)
		v.SMAWorstWeekPct = lowest(v.SMAWorstWeekPct, w.SMANetPct)
		v.BiasMaxDDPct = highest(v.BiasMaxDDPct, w.BiasDDPct)
		v.SMAMaxDDPct = highest(v.SMAMaxDDPct, w.SMADDPct)
	}
	v.DPP, v.NoisePP = mean(ds), mean(gaps)
	v.SignChanges, v.SignSteps = signChanges(ds)
	v.SpanTooShort = v.SignSteps+1 >= DirectionFlipMinWeeks && 2*v.SignChanges >= v.SignSteps

	set := func(status, why string, a ...any) DirectionVerdict {
		v.Status, v.Why = status, fmt.Sprintf(why, a...)
		v.Line = v.line()
		return v
	}
	if v.NoisePP != nil {
		if run := killRun(counted, *v.NoisePP); run != nil {
			v.KillWeeks = run
			return set(DirectionVerdictKill, "bias's replay net was below sma's by more than the noise (%.2f pp) in %d consecutive counted weeks: %s — it proposes the decision ticket, nothing else",
				*v.NoisePP, len(run), strings.Join(run, ", "))
		}
	}
	if v.Counted < DirectionTrialWeeks {
		return set(DirectionVerdictMoreWeeks, "%d week(s) missing of the %d the trial needs (%d counted): no figure is a conclusion yet",
			DirectionTrialWeeks-v.Counted, DirectionTrialWeeks, v.Counted)
	}
	if v.NoisePP == nil {
		return set(DirectionVerdictMoreWeeks, "noise not measured: no counted week has a live − replay gap, so d cannot be weighed")
	}
	d, noise := *v.DPP, *v.NoisePP
	if d < -noise {
		return set(DirectionVerdictSMA, "d %+.2f pp is below −noise (%.2f pp)", d, noise)
	}
	if tail := v.tailBreach(noise); tail != "" {
		return set(DirectionVerdictSMA, "the tail guard alone: %s (d %+.2f pp, noise %.2f pp)", tail, d, noise)
	}
	if math.Abs(d) <= noise {
		return set(DirectionVerdictNoDiff, "|d| %.2f pp is at or below the noise (%.2f pp): inside the replay's own error", math.Abs(d), noise)
	}
	return set(DirectionVerdictBias, "d %+.2f pp is above the noise (%.2f pp), and bias's worst week and max drawdown are within the noise of sma's", d, noise)
}

// readTrialWeek reads one week's scorecard: excluded for missing books,
// not counted without both replays, else counted.
func readTrialWeek(wc DirectionWeekCard) DirectionVerdictWeek {
	w := DirectionVerdictWeek{Week: wc.Week}
	c := wc.Card
	if c == nil {
		w.Why = "no scorecard"
		return w
	}
	bias, sma := replayRow(c, DirectionBias), replayRow(c, DirectionSMA)
	w.BiasNetPct, w.SMANetPct = bias.NetPct, sma.NetPct
	w.BiasDDPct, w.SMADDPct = bias.MaxDDPct, sma.MaxDDPct
	if n := len(c.Missing.BookDays); n > DirectionTrialMaxMissingDays {
		w.Why = fmt.Sprintf("excluded: %d days of recorded books missing (over %d)", n, DirectionTrialMaxMissingDays)
		return w
	}
	var none []string
	for _, rp := range []DirectionReplay{bias, sma} {
		if rp.NetPct == nil {
			none = append(none, rp.Source)
		}
	}
	if len(none) > 0 {
		w.Why = "no replay for " + strings.Join(none, " and ") + ": counts toward neither d nor noise"
		return w
	}
	d := *bias.NetPct - *sma.NetPct
	w.Counted, w.Why, w.DPP = true, "counted", &d
	for _, l := range c.Live {
		if l.GapPP != nil {
			w.GapsPP = append(w.GapsPP, math.Abs(*l.GapPP))
		}
	}
	return w
}

// replayRow is the scorecard's replay row of source (Source set, the rest
// empty when the row is absent).
func replayRow(c *DirectionReport, source string) DirectionReplay {
	for _, rp := range c.Replay {
		if rp.Source == source {
			return rp
		}
	}
	return DirectionReplay{Source: source}
}

// killRun is the first run of DirectionKillStreak or more consecutive
// counted weeks where bias's replay net is below sma's by more than noise.
func killRun(counted []DirectionVerdictWeek, noise float64) []string {
	var run []string
	for _, w := range counted {
		if *w.DPP < -noise {
			run = append(run, w.Week)
			continue
		}
		if len(run) >= DirectionKillStreak {
			return run
		}
		run = nil
	}
	if len(run) >= DirectionKillStreak {
		return run
	}
	return nil
}

// tailBreach names the tail figure where bias is worse than sma by more
// than noise; "" when neither is (a figure not measured is not compared).
func (v DirectionVerdict) tailBreach(noise float64) string {
	var out []string
	if v.BiasWorstWeekPct != nil && v.SMAWorstWeekPct != nil && *v.BiasWorstWeekPct < *v.SMAWorstWeekPct-noise {
		out = append(out, fmt.Sprintf("bias's worst week %+.2f %% is below sma's %+.2f %% by more than the noise", *v.BiasWorstWeekPct, *v.SMAWorstWeekPct))
	}
	if v.BiasMaxDDPct != nil && v.SMAMaxDDPct != nil && *v.BiasMaxDDPct > *v.SMAMaxDDPct+noise {
		out = append(out, fmt.Sprintf("bias's max drawdown %.2f %% is above sma's %.2f %% by more than the noise", *v.BiasMaxDDPct, *v.SMAMaxDDPct))
	}
	return strings.Join(out, "; ")
}

// line is the verdict in one line: status, why, and the extension when the
// span is too short.
func (v DirectionVerdict) line() string {
	s := v.Status + " — " + v.Why
	if v.SpanTooShort {
		s += fmt.Sprintf(" · span too short: extend by %d weeks", DirectionExtendWeeks)
	}
	return s
}

// signChanges counts how often the sign of ds changes from one week to the
// next, weeks with d = 0 left out, and the steps between signed weeks.
func signChanges(ds []float64) (changes, steps int) {
	prev := 0.0
	for _, d := range ds {
		if d == 0 {
			continue
		}
		if prev != 0 {
			steps++
			if (d > 0) != (prev > 0) {
				changes++
			}
		}
		prev = d
	}
	return changes, steps
}

func mean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	m := s / float64(len(xs))
	return &m
}

func lowest(cur, x *float64) *float64 {
	if x == nil || (cur != nil && *cur <= *x) {
		return cur
	}
	v := *x
	return &v
}

func highest(cur, x *float64) *float64 {
	if x == nil || (cur != nil && *cur >= *x) {
		return cur
	}
	v := *x
	return &v
}

// DirectionTrialLabels are the trial's ISO weeks from DirectionTrialStart to
// through, oldest first; none when through is before the trial or not an
// ISO week (a -from/-to scorecard has no verdict).
func DirectionTrialLabels(through string) []string {
	first, err1 := ParseWeek(DirectionTrialStart)
	last, err2 := ParseWeek(through)
	if err1 != nil || err2 != nil {
		return nil
	}
	var out []string
	for d := first.From; !d.After(last.From); d = d.AddDate(0, 0, 7) {
		out = append(out, weekLabel(d))
	}
	return out
}
