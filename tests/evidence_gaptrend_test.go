package tests

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"optionsbot/internal/evidence"
)

// The replay-vs-live gap over time (#820): the last 12 weekly reports' gap
// and its split by cause, read as written, with a stable / drifting verdict.

// gapReport is a weekly report whose replay ran and was comparable: its gap
// in pp of equity, coin (pp ÷ 100 × 2 BTC equity, so a distinctive amount) and
// % of the live result, and a split whose fill price is 60 %, fees 30 % and
// unexplained 10 % of the gap.
func gapReport(week string, pp float64) evidence.Report {
	ptr := func(v float64) *float64 { return &v }
	coin, live := pp/100*2.0, 2*pp
	part := func(name string, share float64) evidence.GapPart {
		return evidence.GapPart{Part: name, Coin: ptr(coin * share), EquityPP: pp * share, GapPct: ptr(100 * share)}
	}
	return evidence.Report{Week: week, Equity: 2,
		Gap: evidence.Gap{Known: true, Gap: coin},
		RVL: evidence.ReplayVsLive{Label: evidence.ReplayVsLiveLabel, Account: evidence.AccountTestnet, AmountsShown: true, Comparable: true,
			GapCoin: ptr(coin), GapEquityPP: ptr(pp), GapPct: ptr(live)},
		Split: evidence.GapSplit{Label: evidence.GapSplitLabel, Known: true, Parts: []evidence.GapPart{
			part(evidence.PartFillPrice, 0.6), part(evidence.PartFees, 0.3), part(evidence.PartOneSided, 0),
			part(evidence.PartMarks, 0), part(evidence.PartUnexplained, 0.1)}}}
}

func partialWeek(week string) evidence.Report {
	r := gapReport(week, 9)
	r.Gap.Partial = "partial: the market books cover 70.0 % of the week's hours, under 90 %"
	r.RVL.Comparable, r.RVL.Note, r.RVL.GapEquityPP = false, "the market books cover 70.0 % of the week's hours, under 90 %", nil
	return r
}

func notComparableWeek(week string) evidence.Report {
	r := gapReport(week, 9)
	r.RVL.Comparable, r.RVL.Note, r.RVL.GapEquityPP = false, "the market books have a gap over 3 h (5.0 h from 2026-10-14 03:00 UTC)", nil
	return r
}

func noGapWeek(week string) evidence.Report {
	return evidence.Report{Week: week, RVL: evidence.ReplayVsLive{Label: evidence.ReplayVsLiveLabel, Note: "the replay did not run: no config"}}
}

// gapSeries is 12 comparable weeks ending 2026-W42 with the given gaps (pp).
func gapSeries(pps ...float64) []evidence.Report {
	var out []evidence.Report
	for i, pp := range pps {
		out = append(out, gapReport(weekBefore(len(pps)-1-i), pp))
	}
	return out
}

// earlierGaps are comparable reports of the weeks before evWeek (2026-W42),
// the last one 2026-W41: Build's earlier reports.
func earlierGaps(pps ...float64) []evidence.Report {
	var out []evidence.Report
	for i, pp := range pps {
		out = append(out, gapReport(weekBefore(len(pps)-i), pp))
	}
	return out
}

func TestGapTrend_Verdicts(t *testing.T) {
	t.Parallel()
	twelve := func(f func(i int) float64) []float64 {
		out := make([]float64, 12)
		for i := range out {
			out[i] = f(i)
		}
		return out
	}
	withWeek := func(rs []evidence.Report, i int, r evidence.Report) []evidence.Report {
		r.Week = rs[i].Week
		rs[i] = r
		return rs
	}
	for _, c := range []struct {
		name    string
		reports []evidence.Report
		status  string
		line    string
		counted int
	}{
		// Scenario: twelve comparable weeks — one sign, range 0.55 pp ≤ 1 pp.
		{"stable", gapSeries(twelve(func(i int) float64 { return 0.2 + 0.05*float64(i) })...), evidence.GapStable,
			"stable: replay above live on 12 of 12 weeks, below on 0; the weekly gap ranged 0.55 pp of equity, within 1.00 pp (an assumption)", 12},
		// Small gaps that change sign: drifting, whatever the range.
		{"drifting: sign", gapSeries(twelve(func(i int) float64 { return 0.1 * float64(1-2*(i%2)) })...), evidence.GapDrifting,
			"drifting: the gap changed sign (replay above live on 6 of 12 weeks, below on 6); the weekly gap ranged 0.20 pp of equity, within 1.00 pp (an assumption)", 12},
		// One sign, growing: drifting on the range.
		{"drifting: range", gapSeries(twelve(func(i int) float64 { return -0.2 * float64(i+1) })...), evidence.GapDrifting,
			"drifting: replay above live on 0 of 12 weeks, below on 12; the weekly gap ranged 2.20 pp of equity, over 1.00 pp (an assumption)", 12},
		// Scenario: too few weeks.
		{"thin", gapSeries(0.3, 0.4, 0.5), evidence.ThinNote,
			"too few to conclude (3 weeks): 3 comparable of the 12 weeks the verdict needs", 3},
		// Scenario: a partial or not-comparable week — listed, not counted, so 11 < 12.
		{"partial week", withWeek(gapSeries(twelve(func(int) float64 { return 0.3 })...), 4, partialWeek("")), evidence.ThinNote,
			"too few to conclude (11 weeks): 11 comparable of the 12 weeks the verdict needs", 11},
		{"not comparable week", withWeek(gapSeries(twelve(func(int) float64 { return 0.3 })...), 0, notComparableWeek("")), evidence.ThinNote,
			"too few to conclude (11 weeks): 11 comparable of the 12 weeks the verdict needs", 11},
		{"no gap week", withWeek(gapSeries(twelve(func(int) float64 { return 0.3 })...), 11, noGapWeek("")), evidence.ThinNote,
			"too few to conclude (11 weeks): 11 comparable of the 12 weeks the verdict needs", 11},
		{"no report", nil, evidence.ThinNote, "too few to conclude (0 weeks): 0 comparable of the 12 weeks the verdict needs", 0},
	} {
		g := evidence.ScoreGap(c.reports, "2026-W42", evidence.GapTrendWeeks, evidence.AccountTestnet)
		if g.Verdict.Status != c.status || g.Verdict.Line != c.line || g.Summary.Weeks != c.counted {
			t.Errorf("%s: verdict %+v, %d counted; want %s %q, %d", c.name, g.Verdict, g.Summary.Weeks, c.status, c.line, c.counted)
		}
		if len(g.Weeks) != len(c.reports) || g.Sample.Weeks != c.counted || g.Sample.Thin != (c.counted < 12) {
			t.Errorf("%s: %d weeks listed of %d, sample %+v", c.name, len(g.Weeks), len(c.reports), g.Sample)
		}
	}
	if evidence.GapTrendWeeks != evidence.ScoreboardWeeks || evidence.GapStableRangePP != 1.0 {
		t.Error("the window is the scoreboard's 12 weeks and the bar 1 pp (docs/agents/evidence.md)")
	}
}

// Scenario: twelve comparable weeks — each week's gap in coin, % of the
// live result and split by cause; the summary's median, worst, signs and
// largest cause.
func TestGapTrend_TwelveComparableWeeks(t *testing.T) {
	t.Parallel()
	pps := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 1.05, 0.15}
	// Two older reports fall out of the window; a later week is ignored.
	reports := append([]evidence.Report{gapReport("2026-W20", 5), gapReport("2026-W21", 5)}, gapSeries(pps...)...)
	reports = append(reports, gapReport("2026-W43", -9))
	g := evidence.ScoreGap(reports, "2026-W42", 12, evidence.AccountTestnet)
	if len(g.Weeks) != 12 || g.FirstWeek != "2026-W31" || g.LastWeek != "2026-W42" {
		t.Fatalf("window %s → %s, %d weeks", g.FirstWeek, g.LastWeek, len(g.Weeks))
	}
	w := g.Weeks[2]
	if w.Status != evidence.GapWeekComparable || !w.Counted || w.GapCoin == nil || !gapNear(*w.GapCoin, 0.006) ||
		!gapNear(*w.EquityPP, 0.3) || !gapNear(*w.LivePct, 0.6) || len(w.Parts) != 5 ||
		w.Parts[0].Part != evidence.PartFillPrice || !gapNear(*w.Parts[0].GapPct, 60) || !gapNear(*w.Parts[0].EquityPP, 0.18) {
		t.Errorf("week %+v", w)
	}
	s := g.Summary
	if s.Weeks != 12 || !gapNear(*s.MedianAbsPP, 0.55) || !gapNear(*s.WorstAbsPP, 1.05) || s.WorstWeek != "2026-W41" ||
		s.Above != 12 || s.Below != 0 || s.SplitWeeks != 12 ||
		s.LargestCause != evidence.PartFillPrice || !gapNear(*s.LargestPct, 60) || !gapNear(*s.UnexplainedPct, 10) {
		t.Errorf("summary %+v", s)
	}
	if g.Verdict.Status != evidence.GapStable { // 1.05 − 0.1 = 0.95 ≤ 1
		t.Errorf("range 0.95 pp, one sign: stable, got %+v", g.Verdict)
	}
}

func gapNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Scenario: a partial or not-comparable week is listed with its reason and
// excluded; a week without a gap likewise; an older report says why.
func TestGapTrend_ExcludedWeeksListedWithReason(t *testing.T) {
	t.Parallel()
	old := evidence.Report{Week: "2026-W38"}
	reports := []evidence.Report{old, partialWeek("2026-W39"), notComparableWeek("2026-W40"), noGapWeek("2026-W41"), gapReport("2026-W42", 50)}
	g := evidence.ScoreGap(reports, "2026-W42", 12, evidence.AccountTestnet)
	want := []struct{ status, reason string }{
		{evidence.GapWeekNoGap, "a report written before the replay-vs-live section"},
		{evidence.GapWeekPartial, "partial: the market books cover 70.0 % of the week's hours, under 90 %"},
		{evidence.GapWeekNotComparable, "the market books have a gap over 3 h"},
		{evidence.GapWeekNoGap, "the replay did not run: no config"},
		{evidence.GapWeekComparable, ""},
	}
	for i, w := range g.Weeks {
		if w.Status != want[i].status || !strings.HasPrefix(w.Reason, want[i].reason) || w.Counted != (i == 4) {
			t.Errorf("%s: %+v, want %+v", w.Week, w, want[i])
		}
		if !w.Counted && (w.EquityPP != nil || w.GapCoin != nil || len(w.Parts) != 0) {
			t.Errorf("%s: an excluded week carries figures: %+v", w.Week, w)
		}
	}
	// The partial week's 9 pp never reaches the statistics.
	if s := g.Summary; s.Weeks != 1 || *s.WorstAbsPP != 50 || *s.MedianAbsPP != 50 {
		t.Errorf("summary %+v", s)
	}
}

// Through Build and the Markdown: the earlier reports and this week's own
// (whose replay did not run), the verdict and every week listed.
func TestGapTrend_BuildAndMarkdown(t *testing.T) {
	t.Parallel()
	r := evidence.Build(scoreInputs(earlierGaps(0.3, -0.4)...))
	g := r.GapTrend
	if g.Label != evidence.GapTrendLabel || len(g.Weeks) != 3 || g.Weeks[2].Week != "2026-W42" || g.Weeks[2].Status != evidence.GapWeekNoGap ||
		g.Summary.Weeks != 2 || !strings.HasPrefix(g.Verdict.Line, "too few to conclude (2 weeks)") {
		t.Fatalf("trend %+v", g)
	}
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	i := strings.Index(md, "## Replay-vs-live gap over time")
	if i < 0 {
		t.Fatalf("no section:\n%s", md)
	}
	section := md[i:]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	wantIn(t, section, "- **Verdict: too few to conclude (2 weeks): 2 comparable of the 12 weeks the verdict needs**",
		"| 2026-W40 | comparable | +0.0060 | +0.30 | +0.6 % | fill price +60.0 % · fees +30.0 %",
		"| 2026-W42 | no gap | — | — | — | — | the replay did not run:",
		"median absolute gap 0.35 pp of equity · worst 0.40 pp (2026-W41) · range 0.70 pp",
		"replay above live on 1 week(s), below on 1, equal on 0",
		"the largest is fill price, +60.0 % of the summed gap · unexplained +10.0 %",
		"(an assumption, to verify in 2027-W02)")

	in := scoreInputs()
	in.Window.Label = "2026-10-12_2026-10-18"
	if g := evidence.Build(in).GapTrend; !strings.Contains(g.Note, "not an ISO week") || len(g.Weeks) != 0 {
		t.Errorf("a -from/-to report: %+v", g)
	}
}

// Scenario: live bot — the trend, the pack and its JSON carry percentages
// only, whichever file says live.
func TestGapTrend_LiveShowsPercentOnly(t *testing.T) {
	t.Parallel()
	reports := earlierGaps(0.2111, 0.3111)
	amounts := []string{"0.004222", "0.006222", "0.0042", "0.0062", " BTC"}
	for name, c := range map[string]struct {
		account string
		live    int // a report written while live
	}{
		"the clone says live":    {evidence.AccountLive, -1},
		"an earlier report says": {evidence.AccountTestnet, 0},
	} {
		rs := append([]evidence.Report{}, reports...)
		if c.live >= 0 {
			rs[c.live].RVL.Account = evidence.AccountLive
		}
		in := scoreInputs(rs...)
		in.Account = c.account
		r := evidence.Build(in)
		g := r.GapTrend
		if g.Account != evidence.AccountLive || g.AmountsShown {
			t.Fatalf("%s: account %q shown %v", name, g.Account, g.AmountsShown)
		}
		for _, w := range g.Weeks {
			if w.GapCoin != nil {
				t.Errorf("%s: coin on %s", name, w.Week)
			}
			for _, p := range w.Parts {
				if p.EquityPP != nil || p.GapPct == nil {
					t.Errorf("%s: %s part %+v: the share of the gap only, as the pack's split", name, w.Week, p)
				}
			}
		}
		data, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := evidence.WriteMarkdown(&b, r); err != nil {
			t.Fatal(err)
		}
		md := b.String()
		section := md[strings.Index(md, "## Replay-vs-live gap over time"):]
		section = section[:strings.Index(section[3:], "\n## ")+3]
		wantIn(t, section, "Live account: percentages only.", "+0.21 |")
		wantNotIn(t, string(data)+section, amounts...)
	}
	// On testnet the same reports show the coin: the check is not hollow.
	g := evidence.Build(scoreInputs(reports...)).GapTrend
	if g.Weeks[0].GapCoin == nil || !gapNear(*g.Weeks[0].GapCoin, 0.004222) {
		t.Errorf("testnet coin %+v", g.Weeks[0])
	}
}

// The pack copies the trend's verdict and statistics from the latest weekly
// report, each with its window and sample, no amount; the posted line is the
// same string.
func TestGapTrend_InThePack(t *testing.T) {
	t.Parallel()
	latest := evidence.Build(scoreInputs(earlierGaps(0.3, -0.4)...))
	for _, account := range []string{evidence.AccountTestnet, evidence.AccountLive} {
		p := evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Account: account, Latest: &latest, LatestPath: "data/evidence/2026-W42/btc.json"})
		s := packSection(t, p, "Replay vs live")
		v := packFigure(t, s, "Gap over time, verdict")
		if v.Display != latest.GapTrend.Verdict.Line || v.Window != "2026-W40 → 2026-W42" || v.Sample == nil || !v.Sample.Thin || v.Sample.Weeks != 2 {
			t.Errorf("%s: verdict row %+v", account, v)
		}
		wantValue(t, packFigure(t, s, "Gap over time, median absolute gap, of the starting equity"), *latest.GapTrend.Summary.MedianAbsPP)
		if f := packFigure(t, s, "Gap over time, worst absolute gap, of the starting equity"); f.Window != "2026-W41" || *f.Value != 0.4 {
			t.Errorf("%s: worst %+v", account, f)
		}
		wantValue(t, packFigure(t, s, "Gap over time, weeks the replay was above live"), 1)
		wantValue(t, packFigure(t, s, "Gap over time, weeks the replay was below live"), 1)
		wantValue(t, packFigure(t, s, "Gap over time, largest cause (fill price), % of the summed gap"), *latest.GapTrend.Summary.LargestPct)
		wantValue(t, packFigure(t, s, "Gap over time, unexplained, % of the summed gap"), *latest.GapTrend.Summary.UnexplainedPct)
		for _, f := range s.Figures {
			if strings.HasPrefix(f.Name, "Gap over time") && (f.Unit == "BTC" || f.Sample == nil || f.Source == "" || f.Caveat == "") {
				t.Errorf("%s: %+v", account, f)
			}
		}
	}
	// A report written before the trend adds no row.
	old := latest
	old.GapTrend = evidence.GapTrend{}
	p := evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Latest: &old})
	for _, f := range packSection(t, p, "Replay vs live").Figures {
		if strings.HasPrefix(f.Name, "Gap over time") {
			t.Errorf("an old report: %+v", f)
		}
	}
}
