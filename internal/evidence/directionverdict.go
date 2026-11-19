package evidence

import (
	"fmt"
	"math"
	"strings"
)

// The direction trial's verdict (#788): the decision rule of
// docs/direction-trial.md applied to the weekly scorecards' own figures —
// replay net_pct and max_drawdown_pct, live_minus_replay_pp and the missing
// book days — and, for the kill rule's live half (#850), each bot's live max
// drawdown % from its weekly evidence report — never a new calculation. A verdict only proposes: it files
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
	fmt.Sprintf("kill rule: %d consecutive counted weeks of bias under sma by more than noise, or stable's live max drawdown over %g× mvp's", DirectionKillStreak, DirectionLiveDDRatio),
	fmt.Sprintf("span too short: the sign of bias − sma changes on at least half the week-to-week steps (%d weeks or more) → extend by %d weeks", DirectionFlipMinWeeks, DirectionExtendWeeks),
	"review date 2026-12-07",
}

// DirectionLiveDrawdownNote starts NotChecked when a reviewed week lacks a
// figure of the kill rule's live half (#850); the weeks and reasons follow.
const DirectionLiveDrawdownNote = "the kill rule's live half (stable BTC's live max drawdown over 2× mvp BTC's)"

// DirectionLiveDDRatio is the live half's threshold: stable's live max
// drawdown over this many times mvp's fires it (an assumption of the brief).
const DirectionLiveDDRatio = 2.0

// The live half's outcomes, overall and per week.
const (
	DirectionLiveFired      = "fired"
	DirectionLiveNotFired   = "not fired"
	DirectionLiveNotChecked = "not checked"
)

// The halves of the kill rule, as the line names them.
const (
	DirectionHalfReplay = "replay streak"
	DirectionHalfLive   = "live drawdown"
)

// DirectionWeekCard is one trial week and its scorecard (nil = none written),
// with the two bots' live max drawdown copied from their weekly evidence
// reports (nil = none read).
type DirectionWeekCard struct {
	Week string
	Card *DirectionReport
	Live *DirectionLiveDD `json:",omitempty"`
}

// DirectionLiveDD is one week's live max drawdown of stable BTC (bias) and
// mvp BTC (sma), % of each one's starting equity (≤ 0, as perf writes it),
// copied from their weekly reports (Live.Perf.MaxDrawdownPct), never
// recomputed; nil = no figure, the note says why.
type DirectionLiveDD struct {
	StablePct  *float64 `json:"stable_pct"`
	StableNote string   `json:"stable_note,omitempty"`
	MVPPct     *float64 `json:"mvp_pct"`
	MVPNote    string   `json:"mvp_note,omitempty"`
}

// DirectionLiveHalf is the kill rule's live half over the reviewed weeks:
// fired in Weeks, not fired, or not checked when no week had both figures.
// Unchecked lists each week that lacked one, with the reason.
type DirectionLiveHalf struct {
	Status    string   `json:"status"`
	Weeks     []string `json:"fired_weeks,omitempty"`
	Unchecked []string `json:"unchecked_weeks,omitempty"`
	Sample    Sample   `json:"sample"` // the weeks that had both figures (#772)
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
	KillWeeks        []string               `json:"kill_weeks,omitempty"` // the replay streak's weeks
	LiveDrawdown     DirectionLiveHalf      `json:"live_drawdown"`
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
	// The kill rule's live half this week: both bots' live max drawdown (%)
	// and its outcome, with the reason when not checked.
	StableLiveDDPct *float64 `json:"stable_live_max_drawdown_pct"`
	MVPLiveDDPct    *float64 `json:"mvp_live_max_drawdown_pct"`
	LiveHalf        string   `json:"live_half"`
	LiveWhy         string   `json:"live_half_why,omitempty"`
}

// BuildDirectionVerdict applies the brief's rule to the trial weeks, oldest
// first (a week before DirectionTrialStart is the caller's to leave out).
// Checks run in the brief's order: the kill rule (either half, any week), the
// span, then sma better (d, then the tail guard alone), no difference, bias
// better.
func BuildDirectionVerdict(weeks []DirectionWeekCard) DirectionVerdict {
	v := DirectionVerdict{Rule: DirectionTrialRule, Needed: DirectionTrialWeeks, Assumptions: DirectionAssumptions}
	var counted []DirectionVerdictWeek
	var gaps []float64
	for _, wc := range weeks {
		w := readTrialWeek(wc)
		readLiveHalf(&w, wc.Live)
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
	v.LiveDrawdown = liveHalf(v.Weeks)
	if u := v.LiveDrawdown.Unchecked; len(u) > 0 {
		v.NotChecked = DirectionLiveDrawdownNote + " in " + strings.Join(u, "; ")
	}

	set := func(status, why string, a ...any) DirectionVerdict {
		v.Status, v.Why = status, fmt.Sprintf(why, a...)
		v.Line = v.line()
		return v
	}
	var fired []string
	if v.NoisePP != nil {
		if run := killRun(counted, *v.NoisePP); run != nil {
			v.KillWeeks = run
			fired = append(fired, fmt.Sprintf("%s: bias's replay net was below sma's by more than the noise (%.2f pp) in %d consecutive counted weeks: %s",
				DirectionHalfReplay, *v.NoisePP, len(run), strings.Join(run, ", ")))
		}
	}
	if v.LiveDrawdown.Status == DirectionLiveFired {
		fired = append(fired, fmt.Sprintf("%s: stable BTC's live max drawdown was over %g× mvp BTC's in %s",
			DirectionHalfLive, DirectionLiveDDRatio, v.liveFiredText()))
	}
	if len(fired) > 0 {
		return set(DirectionVerdictKill, "%s — it proposes the decision ticket, nothing else", strings.Join(fired, "; "))
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

// readLiveHalf sets the week's live half from the two bots' figures: not
// checked without both or with a zero mvp drawdown (never a pass or a fail),
// fired over DirectionLiveDDRatio × mvp's, else not fired.
func readLiveHalf(w *DirectionVerdictWeek, dd *DirectionLiveDD) {
	w.LiveHalf = DirectionLiveNotChecked
	if dd == nil {
		w.LiveWhy = "no weekly evidence report read"
		return
	}
	w.StableLiveDDPct, w.MVPLiveDDPct = dd.StablePct, dd.MVPPct
	var why []string
	if dd.StablePct == nil {
		why = append(why, "stable BTC: "+orNote(dd.StableNote))
	}
	if dd.MVPPct == nil {
		why = append(why, "mvp BTC: "+orNote(dd.MVPNote))
	} else if *dd.MVPPct == 0 {
		why = append(why, "mvp BTC's live max drawdown is 0 %: no ratio")
	}
	if len(why) > 0 {
		w.LiveWhy = strings.Join(why, "; ")
		return
	}
	w.LiveHalf = DirectionLiveNotFired
	if math.Abs(*dd.StablePct) > DirectionLiveDDRatio*math.Abs(*dd.MVPPct) { // perf's drawdown is ≤ 0: sizes compared
		w.LiveHalf = DirectionLiveFired
	}
}

func orNote(note string) string {
	if note == "" {
		return "no live max drawdown"
	}
	return note
}

// liveHalf is the live half over the weeks: fired in any week, else not
// fired when at least one week was checked, else not checked.
func liveHalf(weeks []DirectionVerdictWeek) DirectionLiveHalf {
	h := DirectionLiveHalf{Status: DirectionLiveNotChecked}
	checked := 0
	for _, w := range weeks {
		switch w.LiveHalf {
		case DirectionLiveFired:
			h.Weeks = append(h.Weeks, w.Week)
			checked++
		case DirectionLiveNotFired:
			checked++
		default:
			h.Unchecked = append(h.Unchecked, fmt.Sprintf("%s (%s)", w.Week, w.LiveWhy))
		}
	}
	switch {
	case len(h.Weeks) > 0:
		h.Status = DirectionLiveFired
	case checked > 0:
		h.Status = DirectionLiveNotFired
	}
	h.Sample = WeeksSample(checked, nil, "")
	return h
}

// liveFiredText lists the weeks the live half fired, each with both figures.
func (v DirectionVerdict) liveFiredText() string {
	var out []string
	for _, w := range v.Weeks {
		if w.LiveHalf == DirectionLiveFired {
			out = append(out, fmt.Sprintf("%s (%+.2f %% vs %+.2f %%)", w.Week, *w.StableLiveDDPct, *w.MVPLiveDDPct))
		}
	}
	return strings.Join(out, ", ")
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
