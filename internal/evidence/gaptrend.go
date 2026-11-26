package evidence

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The replay-vs-live gap over time (#820): one good week proves nothing about
// whether replay is a fair proxy for live, so the gap is followed week after
// week. Pure ScoreGap reads the last GapTrendWeeks weekly reports (the
// scoreboard's Inputs.Earlier, plus this week's own) and takes each one's
// Replay vs live gap and its split by cause (#488, #802) as written: no second
// replay, no figure recomputed. A partial or not-comparable week (market books
// under 90 % of the hours, a gap over 3 h) and a week without a gap are listed
// with their reason and left out of the statistics. The verdict line is
// computed here once; evidence_week.py posts it as written. A live account
// shows percentages only.

// GapTrendLabel names the section.
const GapTrendLabel = "replay-vs-live gap over time"

// GapTrendWeeks is the window: the weekly reports read, as the scoreboard's.
const GapTrendWeeks = ScoreboardWeeks

// GapStableRangePP is the widest the comparable weeks' gaps may range (the
// largest minus the smallest, pp of each week's starting equity) for the gap to
// read stable. An ASSUMPTION, about a typical week's live result: a gap that
// moves by more than that from week to week cannot stand in for live. Verify it
// in 2027-W02 with the sample floors (docs/agents/evidence.md "Gap over time"):
// compare it with the spread of the weekly live results over the same weeks.
const GapStableRangePP = 1.0

// A week's status in the trend.
const (
	GapWeekComparable    = "comparable"
	GapWeekPartial       = "partial"
	GapWeekNotComparable = "not comparable"
	GapWeekNoGap         = "no gap"
)

// The verdicts (ThinNote under SampleMinWeeks comparable weeks).
const (
	GapStable   = "stable"
	GapDrifting = "drifting"
)

// gapZeroPP is the gap, pp of equity, under which a week has no sign.
const gapZeroPP = 1e-9

// GapTrend is the section. Amounts (coin) are nil for a live account.
type GapTrend struct {
	Label        string          `json:"label"`
	Source       string          `json:"source"`
	Note         string          `json:"note,omitempty"` // why it was not computed
	Account      string          `json:"account"`
	AmountsShown bool            `json:"amounts_shown"`
	WeeksNeeded  int             `json:"weeks_needed"`
	RangeBarPP   float64         `json:"stable_range_pp"` // GapStableRangePP
	FirstWeek    string          `json:"first_week,omitempty"`
	LastWeek     string          `json:"last_week,omitempty"`
	Weeks        []GapWeek       `json:"weeks"` // oldest first
	Summary      GapTrendSummary `json:"summary"`
	Sample       Sample          `json:"sample"` // the comparable weeks
	Verdict      GapVerdict      `json:"verdict"`
}

// GapWeek is one weekly report's gap, as that report wrote it.
type GapWeek struct {
	Week     string         `json:"week"`
	Status   string         `json:"status"` // comparable | partial | not comparable | no gap
	Reason   string         `json:"reason,omitempty"`
	Counted  bool           `json:"counted"`         // in the statistics
	GapCoin  *float64       `json:"gap,omitempty"`   // replay − live, coin; testnet only
	EquityPP *float64       `json:"gap_equity_pp"`   // of that week's starting equity
	LivePct  *float64       `json:"gap_pct_of_live"` // of |live result|; nil when live was 0
	Parts    []GapTrendPart `json:"parts"`           // the split, empty when the report has none
}

// GapTrendPart is one cause of a week's gap.
type GapTrendPart struct {
	Part     string   `json:"part"`
	GapPct   *float64 `json:"gap_pct"`             // of that week's gap; nil when it was 0
	EquityPP *float64 `json:"equity_pp,omitempty"` // testnet only, as the pack's split
	pp       float64  // the report's pp, for the totals whatever the account
}

// GapTrendSummary is over the comparable weeks.
type GapTrendSummary struct {
	Weeks       int      `json:"weeks"` // comparable weeks counted
	MedianAbsPP *float64 `json:"median_abs_pp"`
	WorstAbsPP  *float64 `json:"worst_abs_pp"`
	WorstWeek   string   `json:"worst_week,omitempty"`
	RangePP     *float64 `json:"range_pp"`     // largest − smallest signed gap
	Above       int      `json:"replay_above"` // weeks replay > live (it overstates)
	Below       int      `json:"replay_below"`
	Zero        int      `json:"zero"`
	// The causes over the comparable weeks with a split: each part's pp
	// summed, as a % of the summed gap (nil when that sum is 0).
	SplitWeeks     int      `json:"split_weeks"`
	LargestCause   string   `json:"largest_cause,omitempty"` // the named part with the largest |sum|
	LargestPct     *float64 `json:"largest_cause_pct"`
	UnexplainedPct *float64 `json:"unexplained_pct"`
}

// GapVerdict is the line posted as written.
type GapVerdict struct {
	Status string `json:"status"` // stable | drifting | too few to conclude
	Line   string `json:"line"`
}

// ScoreGap is the trend as of the ISO week asOf over the last `needed`
// reports up to it (any order; one per week). Pure.
func ScoreGap(reports []Report, asOf string, needed int, account string) GapTrend {
	t := GapTrend{Label: GapTrendLabel, Account: accountOf(account), WeeksNeeded: needed, RangeBarPP: GapStableRangePP, Weeks: []GapWeek{}}
	var usable []Report
	for _, r := range reports {
		if r.Week <= asOf {
			usable = append(usable, r)
		}
		if r.RVL.Account == AccountLive {
			t.Account = AccountLive // an amount must not show because one report is out of date
		}
	}
	t.AmountsShown = t.Account != AccountLive
	sort.SliceStable(usable, func(i, j int) bool { return usable[i].Week < usable[j].Week })
	if len(usable) > needed {
		usable = usable[len(usable)-needed:]
	}
	if len(usable) > 0 {
		t.FirstWeek, t.LastWeek = usable[0].Week, usable[len(usable)-1].Week
	}
	for _, r := range usable {
		t.Weeks = append(t.Weeks, gapWeekOf(r, t.AmountsShown))
	}
	t.Summary = summariseGap(t.Weeks)
	t.Sample = WeeksSample(t.Summary.Weeks, nil, "")
	t.Verdict = gapVerdict(t.Summary, needed)
	return t
}

// gapWeekOf reads one report's gap as it was written.
func gapWeekOf(r Report, amounts bool) GapWeek {
	w := GapWeek{Week: r.Week, Parts: []GapTrendPart{}}
	v := r.RVL
	switch {
	case v.Label == "":
		w.Status, w.Reason = GapWeekNoGap, "a report written before the replay-vs-live section"
	case !r.Gap.Known:
		w.Status, w.Reason = GapWeekNoGap, v.Note
	case r.Gap.Partial != "":
		w.Status, w.Reason = GapWeekPartial, r.Gap.Partial
	case !v.Comparable:
		w.Status, w.Reason = GapWeekNotComparable, v.Note
	case v.GapEquityPP == nil:
		w.Status, w.Reason = GapWeekNoGap, "the starting equity is unknown"
	default:
		w.Status, w.Counted = GapWeekComparable, true
	}
	if w.Reason == "" && !w.Counted {
		w.Reason = "no reason in the report"
	}
	if !w.Counted {
		return w
	}
	w.EquityPP, w.LivePct = ptr(*v.GapEquityPP), copyPtr(v.GapPct)
	if amounts && v.GapCoin != nil {
		w.GapCoin = ptr(*v.GapCoin)
	}
	if r.Split.Known {
		for _, p := range r.Split.Parts {
			part := GapTrendPart{Part: p.Part, GapPct: copyPtr(p.GapPct), pp: p.EquityPP}
			if amounts {
				part.EquityPP = ptr(p.EquityPP)
			}
			w.Parts = append(w.Parts, part)
		}
	}
	return w
}

func copyPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return ptr(*v)
}

// summariseGap is the statistics over the counted weeks.
func summariseGap(weeks []GapWeek) GapTrendSummary {
	var s GapTrendSummary
	var abs []float64
	lo, hi := math.Inf(1), math.Inf(-1)
	sumGap := 0.0
	sums := map[string]float64{}
	var order []string
	for _, w := range weeks {
		if !w.Counted {
			continue
		}
		g := *w.EquityPP
		s.Weeks++
		abs = append(abs, math.Abs(g))
		lo, hi = math.Min(lo, g), math.Max(hi, g)
		switch {
		case g > gapZeroPP:
			s.Above++
		case g < -gapZeroPP:
			s.Below++
		default:
			s.Zero++
		}
		if s.WorstAbsPP == nil || math.Abs(g) > *s.WorstAbsPP {
			s.WorstAbsPP, s.WorstWeek = ptr(math.Abs(g)), w.Week
		}
		if len(w.Parts) == 0 {
			continue
		}
		s.SplitWeeks++
		sumGap += g
		for _, p := range w.Parts {
			if _, seen := sums[p.Part]; !seen {
				order = append(order, p.Part)
			}
			sums[p.Part] += p.pp
		}
	}
	if s.Weeks == 0 {
		return s
	}
	s.MedianAbsPP, s.RangePP = ptr(median(abs)), ptr(hi-lo)
	if s.SplitWeeks == 0 {
		return s
	}
	for _, part := range order {
		if part == PartUnexplained {
			continue
		}
		if s.LargestCause == "" || math.Abs(sums[part]) > math.Abs(sums[s.LargestCause]) {
			s.LargestCause = part
		}
	}
	if math.Abs(sumGap) > gapZeroPP {
		if s.LargestCause != "" {
			s.LargestPct = ptr(100 * sums[s.LargestCause] / sumGap)
		}
		s.UnexplainedPct = ptr(100 * sums[PartUnexplained] / sumGap)
	}
	return s
}

func median(xs []float64) float64 {
	v := append([]float64{}, xs...)
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// gapVerdict is the one line: too few to conclude under SampleMinWeeks
// comparable weeks; stable when the gaps keep one sign and range within
// GapStableRangePP; drifting otherwise.
func gapVerdict(s GapTrendSummary, needed int) GapVerdict {
	if s.Weeks < SampleMinWeeks {
		return GapVerdict{Status: ThinNote, Line: fmt.Sprintf("%s (%s): %d comparable of the %d weeks the verdict needs",
			ThinNote, plural(s.Weeks, "week"), s.Weeks, max(needed, SampleMinWeeks))}
	}
	rng := *s.RangePP
	oneSign := s.Above == 0 || s.Below == 0
	signs := fmt.Sprintf("replay above live on %d of %d weeks, below on %d", s.Above, s.Weeks, s.Below)
	within := rng <= GapStableRangePP+1e-12
	word := "over"
	if within {
		word = "within"
	}
	bar := fmt.Sprintf("the weekly gap ranged %.2f pp of equity, %s %.2f pp (an assumption)", rng, word, GapStableRangePP)
	if oneSign && within {
		return GapVerdict{Status: GapStable, Line: fmt.Sprintf("%s: %s; %s", GapStable, signs, bar)}
	}
	why := []string{}
	if !oneSign {
		why = append(why, "the gap changed sign ("+signs+")")
	} else {
		why = append(why, signs)
	}
	why = append(why, bar)
	return GapVerdict{Status: GapDrifting, Line: GapDrifting + ": " + strings.Join(why, "; ")}
}

// buildGapTrend is the trend of the report r being built: the earlier weekly
// reports and r itself (its Gap, RVL and Split already set).
func buildGapTrend(in Inputs, r Report) GapTrend {
	w := in.Window
	if wk, err := ParseWeek(w.Label); err != nil || wk.Label != w.Label {
		t := GapTrend{Label: GapTrendLabel, Source: in.WeeklySource, Account: accountOf(in.Account), WeeksNeeded: GapTrendWeeks,
			RangeBarPP: GapStableRangePP, Weeks: []GapWeek{}, Note: "a -from/-to report is not an ISO week"}
		t.AmountsShown = t.Account != AccountLive
		return t
	}
	var reports []Report
	for _, e := range in.Earlier {
		if e.Week < w.Label {
			reports = append(reports, e)
		}
	}
	t := ScoreGap(append(reports, r), w.Label, GapTrendWeeks, in.Account)
	t.Source = in.WeeklySource + " (replay_vs_live and gap_split) and this week's"
	return t
}

// writeGapTrend writes the section.
func writeGapTrend(p func(string, ...any), t GapTrend, coin string) {
	p("## Replay-vs-live gap over time\n\n")
	p("Method: the Replay vs live gap (replay − live, positive = the replay did better) and its split by cause of the last %d weekly reports, as each report wrote them; nothing is replayed again. "+
		"A partial or not-comparable week and a week without a gap are listed with their reason and left out of the statistics. "+
		"Stable = the comparable weeks' gaps keep one sign and range within %.2f pp of equity (an assumption, to verify in 2027-W02); drifting otherwise; under %d comparable weeks, too few to conclude. "+
		"The causes' shares are each part's pp summed over the weeks with a split, against the summed gap. A live Deribit account shows percentages only. Source: %s.\n\n",
		t.WeeksNeeded, t.RangeBarPP, SampleMinWeeks, t.Source)
	if t.Note != "" {
		p("Not computed: %s.\n\n", t.Note)
		return
	}
	p("- **Verdict: %s**\n\n", t.Verdict.Line)
	if t.AmountsShown {
		p("| Week | Status | Gap %s | pp of equity | %% of the live result | Split (%% of the gap) | Reason |\n|---|---|---|---|---|---|---|\n", coin)
	} else {
		p("Live account: percentages only.\n\n| Week | Status | pp of equity | %% of the live result | Split (%% of the gap) | Reason |\n|---|---|---|---|---|---|\n")
	}
	for _, w := range t.Weeks {
		pp, live, split := "—", "—", "—"
		if w.EquityPP != nil {
			pp = fmt.Sprintf("%+.2f", *w.EquityPP)
			live = "the live result is 0"
			if w.LivePct != nil {
				live = fmt.Sprintf("%+.1f %%", *w.LivePct)
			}
		}
		if len(w.Parts) > 0 {
			var parts []string
			for _, pt := range w.Parts {
				share := "the gap is 0"
				if pt.GapPct != nil {
					share = fmt.Sprintf("%+.1f %%", *pt.GapPct)
				}
				parts = append(parts, pt.Part+" "+share)
			}
			split = strings.Join(parts, " · ")
		}
		reason := packCell(strings.ReplaceAll(w.Reason, "\n", " "))
		if t.AmountsShown {
			c := "—"
			if w.GapCoin != nil {
				c = fmt.Sprintf("%+.4f", *w.GapCoin)
			}
			p("| %s | %s | %s | %s | %s | %s | %s |\n", w.Week, w.Status, c, pp, live, split, reason)
		} else {
			p("| %s | %s | %s | %s | %s | %s |\n", w.Week, w.Status, pp, live, split, reason)
		}
	}
	p("\n")
	s := t.Summary
	p("- comparable weeks: %d of %d listed · %s\n", s.Weeks, len(t.Weeks), t.Sample.Text())
	if s.Weeks == 0 {
		p("- no comparable week: no statistics\n\n")
		return
	}
	p("- median absolute gap %.2f pp of equity · worst %.2f pp (%s) · range %.2f pp\n", *s.MedianAbsPP, *s.WorstAbsPP, s.WorstWeek, *s.RangePP)
	p("- sign: replay above live on %d week(s), below on %d, equal on %d\n", s.Above, s.Below, s.Zero)
	switch {
	case s.SplitWeeks == 0:
		p("- causes: no comparable week carries a split\n")
	case s.LargestPct == nil || s.UnexplainedPct == nil:
		p("- causes (%d week(s) with a split): the summed gap is 0, no share\n", s.SplitWeeks)
	default:
		p("- causes (%d week(s) with a split): the largest is %s, %+.1f %% of the summed gap · unexplained %+.1f %%\n",
			s.SplitWeeks, s.LargestCause, *s.LargestPct, *s.UnexplainedPct)
	}
	p("\n")
}
