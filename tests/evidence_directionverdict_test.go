package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/evidence"
)

// The direction trial's verdict (#788): docs/direction-trial.md's rule over
// the weekly scorecards, one fixture per status under
// tests/testdata/direction_verdict/.

func dvWeeks(t *testing.T, name string) []evidence.DirectionWeekCard {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "direction_verdict", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var weeks []evidence.DirectionWeekCard
	if err := json.Unmarshal(data, &weeks); err != nil {
		t.Fatal(err)
	}
	return weeks
}

func dvMarkdown(t *testing.T, v evidence.DirectionVerdict) string {
	t.Helper()
	r := evidence.DirectionReport{Bot: "btc", Week: v.Weeks[len(v.Weeks)-1].Week, Verdict: &v}
	var b bytes.Buffer
	if err := evidence.WriteDirectionMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func dvContains(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s missing %q:\n%s", what, w, text)
		}
	}
}

func dvNear(a *float64, want float64) bool { return a != nil && *a > want-1e-9 && *a < want+1e-9 }

// Scenario: fewer than 6 counted weeks — 4 weeks with at most 2 missing
// days each (a 5th missing 3 is excluded) → needs more weeks, 2 missing, no
// figure presented as a conclusion.
func TestDirectionVerdict_FewerThanSixCountedWeeks(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "needs_more_weeks"))
	if v.Status != evidence.DirectionVerdictMoreWeeks || v.Counted != 4 {
		t.Fatalf("status %q, counted %d; want needs more weeks, 4", v.Status, v.Counted)
	}
	dvContains(t, "why", v.Why, "2 week(s) missing of the 6", "no figure is a conclusion yet")
	if v.Weeks[4].Counted || !strings.Contains(v.Weeks[4].Why, "excluded: 3 days of recorded books missing") {
		t.Errorf("W47 = %+v; want excluded for 3 missing book days", v.Weeks[4])
	}
	md := dvMarkdown(t, v)
	dvContains(t, "markdown", md, "## Verdict", "**needs more weeks**", "weeks counted: 4 of 6 (running figures, not a conclusion)")
	for _, conclusion := range []string{"no difference", "bias better", "sma better"} {
		if strings.Contains(md, "**"+conclusion+"**") {
			t.Errorf("markdown presents %q as a conclusion", conclusion)
		}
	}
}

// Scenario: difference inside the replay's own error → no difference, with
// d, noise and each week listed.
func TestDirectionVerdict_DifferenceInsideNoise(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "no_difference"))
	if v.Status != evidence.DirectionVerdictNoDiff || !dvNear(v.DPP, 0.2) || !dvNear(v.NoisePP, 0.5) {
		t.Fatalf("status %q d %v noise %v; want no difference, +0.2, 0.5", v.Status, v.DPP, v.NoisePP)
	}
	if v.SpanTooShort {
		t.Error("d never changed sign: the span is not too short")
	}
	md := dvMarkdown(t, v)
	dvContains(t, "markdown", md, "**no difference**", "+0.20 pp · noise (mean |live − replay|): 0.50 pp")
	for _, w := range []string{"2026-W43", "2026-W44", "2026-W45", "2026-W46", "2026-W47", "2026-W48"} {
		dvContains(t, "markdown", md, "| "+w+" | yes |")
	}
}

// Scenario: bias better with a clean tail; and the boundary — d exactly at
// the noise is still no difference.
func TestDirectionVerdict_BiasBetterWithCleanTail(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "bias_better"))
	if v.Status != evidence.DirectionVerdictBias || !dvNear(v.DPP, 1) || !dvNear(v.NoisePP, 0.3) {
		t.Fatalf("status %q d %v noise %v; want bias better, +1, 0.3", v.Status, v.DPP, v.NoisePP)
	}
	if !dvNear(v.BiasMaxDDPct, 1.1) || !dvNear(v.SMAMaxDDPct, 1.0) || !dvNear(v.BiasWorstWeekPct, 1.5) || !dvNear(v.SMAWorstWeekPct, 0.5) {
		t.Errorf("tail = %+v", v)
	}

	weeks := dvWeeks(t, "bias_better")
	for _, w := range weeks { // d = 1 pp exactly every week; gaps of 1 pp put noise exactly at d
		bias, sma, g := 1.5, 0.5, 1.0
		w.Card.Replay[0].NetPct, w.Card.Replay[1].NetPct = &bias, &sma
		for i := range w.Card.Live {
			w.Card.Live[i].GapPP = &g
		}
	}
	if v := evidence.BuildDirectionVerdict(weeks); v.Status != evidence.DirectionVerdictNoDiff {
		t.Errorf("|d| = noise: status %q, want no difference", v.Status)
	}
}

// Scenario: the tail guard alone makes sma better, the reason naming the
// drawdown.
func TestDirectionVerdict_TailGuardAloneMakesSMABetter(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "sma_better_tail"))
	if v.Status != evidence.DirectionVerdictSMA || !dvNear(v.DPP, 1) {
		t.Fatalf("status %q d %v; want sma better with d +1", v.Status, v.DPP)
	}
	dvContains(t, "why", v.Why, "tail guard alone", "max drawdown 3.00 % is above sma's 1.00 %")
	if strings.Contains(v.Why, "worst week") {
		t.Errorf("why names the worst week, which is clean: %s", v.Why)
	}
}

// Scenario: kill rule — 3 consecutive counted weeks of bias below sma by
// more than noise → kill rule hit with the weeks named, before 6 weeks.
func TestDirectionVerdict_KillRule(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "kill_rule"))
	if v.Status != evidence.DirectionVerdictKill || v.Counted != 4 {
		t.Fatalf("status %q counted %d; want kill rule hit after 4 weeks", v.Status, v.Counted)
	}
	if got := strings.Join(v.KillWeeks, ","); got != "2026-W44,2026-W45,2026-W46" {
		t.Errorf("kill weeks %s", got)
	}
	dvContains(t, "line", v.Line, "kill rule hit", "2026-W44, 2026-W45, 2026-W46", "proposes")
	dvContains(t, "markdown", dvMarkdown(t, v), "kill rule: **hit** in 2026-W44, 2026-W45, 2026-W46", "it files nothing and changes no config")

	short := dvWeeks(t, "kill_rule")[:3] // two weeks under: no kill yet
	if v := evidence.BuildDirectionVerdict(short); v.Status == evidence.DirectionVerdictKill {
		t.Errorf("2 weeks under sma: status %q, want no kill", v.Status)
	}
}

// Scenario: a week with a missing replay counts toward neither d nor noise,
// and the weeks list says why.
func TestDirectionVerdict_WeekWithMissingReplay(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "missing_replay"))
	if v.Counted != 1 || !dvNear(v.DPP, 0.3) || !dvNear(v.NoisePP, 0.3) {
		t.Fatalf("counted %d d %v noise %v; want 1, +0.3, 0.3 (W44's gaps left out)", v.Counted, v.DPP, v.NoisePP)
	}
	w := v.Weeks[1]
	if w.Counted || w.DPP != nil || len(w.GapsPP) != 0 || w.Why != "no replay for bias: counts toward neither d nor noise" {
		t.Errorf("W44 = %+v", w)
	}
	if v.Weeks[2].Why != "no scorecard" {
		t.Errorf("W45 = %+v; want no scorecard", v.Weeks[2])
	}
	dvContains(t, "markdown", dvMarkdown(t, v), "| 2026-W44 | no | n/a | +5.00 | n/a |", "no replay for bias")
}

// Scenario: the sign of bias − sma alternates across the counted weeks →
// "span too short: extend by 4 weeks".
func TestDirectionVerdict_SignFlipsWithNoTrend(t *testing.T) {
	t.Parallel()
	v := evidence.BuildDirectionVerdict(dvWeeks(t, "sign_flip"))
	if !v.SpanTooShort || v.SignChanges != 5 || v.SignSteps != 5 {
		t.Fatalf("span too short %v, changes %d of %d", v.SpanTooShort, v.SignChanges, v.SignSteps)
	}
	if v.Status != evidence.DirectionVerdictNoDiff {
		t.Errorf("status %q, want no difference (d 0 inside a 2 pp noise)", v.Status)
	}
	dvContains(t, "line", v.Line, "span too short: extend by 4 weeks")
	dvContains(t, "markdown", dvMarkdown(t, v), "**span too short: extend by 4 weeks**")
}

// Scenario: the verdict reaches the reader — the scorecard written by
// cmd/evidence carries the full block in its Markdown and the verdict in its
// JSON, read back from the earlier weeks' files; outside the trial, none.
func TestDirectionVerdict_ReachesTheScorecard(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	weeks := dvWeeks(t, "no_difference")
	for _, w := range weeks[:5] {
		_, js := evidence.DirectionOutPaths(root, w.Week)
		if err := os.MkdirAll(filepath.Dir(js), 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(w.Card)
		if err := os.WriteFile(js, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := evidence.WithDirectionVerdict(*weeks[5].Card, root, t.TempDir())
	if r.Verdict == nil || r.Verdict.Status != evidence.DirectionVerdictNoDiff || r.Verdict.Counted != 6 {
		t.Fatalf("verdict = %+v; want no difference over 6 weeks", r.Verdict)
	}
	md, js := evidence.DirectionOutPaths(root, r.Week)
	if err := evidence.WriteDirection(r, md, js); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(md)
	dvContains(t, "scorecard", string(text), "## Verdict", "**no difference**", "Assumptions (docs/direction-trial.md)", "review date 2026-12-07")
	var back struct {
		Verdict struct {
			Status string `json:"status"`
			Line   string `json:"line"`
		} `json:"verdict"`
	}
	data, _ := os.ReadFile(js)
	if err := json.Unmarshal(data, &back); err != nil || back.Verdict.Status != "no difference" || !strings.HasPrefix(back.Verdict.Line, "no difference — ") {
		t.Errorf("json verdict = %+v (%v)", back.Verdict, err)
	}

	before := evidence.DirectionReport{Week: "2026-W42"}
	custom := evidence.DirectionReport{Week: "2026-10-19_2026-10-25"}
	for _, r := range []evidence.DirectionReport{before, custom} {
		if got := evidence.WithDirectionVerdict(r, root, root); got.Verdict != nil {
			t.Errorf("%s: verdict %+v, want none outside the trial", r.Week, got.Verdict)
		}
	}
}

func TestDirectionTrialLabels(t *testing.T) {
	t.Parallel()
	if got := evidence.DirectionTrialLabels("2026-W45"); strings.Join(got, ",") != "2026-W43,2026-W44,2026-W45" {
		t.Errorf("W45: %v", got)
	}
	if got := evidence.DirectionTrialLabels("2027-W01"); got[len(got)-2] != "2026-W53" {
		t.Errorf("year boundary: %v", got[len(got)-3:])
	}
	if got := evidence.DirectionTrialLabels("2026-W42"); len(got) != 0 {
		t.Errorf("before the trial: %v", got)
	}
}

// The kill rule's live half (#850): stable BTC's live max drawdown over 2×
// mvp BTC's in any reviewed week, copied from the weekly evidence reports.

// dvLive gives every week both bots' live max drawdown (perf writes it ≤ 0),
// −1 % each unless over names the week.
func dvLive(weeks []evidence.DirectionWeekCard, over map[string]*evidence.DirectionLiveDD) []evidence.DirectionWeekCard {
	for i := range weeks {
		s, m := -1.0, -1.0
		weeks[i].Live = &evidence.DirectionLiveDD{StablePct: &s, MVPPct: &m}
		if dd, ok := over[weeks[i].Week]; ok {
			weeks[i].Live = dd
		}
	}
	return weeks
}

func dvDD(stable, mvp *float64) *evidence.DirectionLiveDD {
	return &evidence.DirectionLiveDD{StablePct: stable, MVPPct: mvp}
}

func dvF(v float64) *float64 { return &v }

// Scenario: live drawdown over 2× fires the live half — three weeks, stable
// 3× mvp in week 2 → kill rule hit naming the live half and week 2, in the
// line and the block; NotChecked cleared.
func TestDirectionVerdict_LiveDrawdownOverTwiceFires(t *testing.T) {
	t.Parallel()
	weeks := dvLive(dvWeeks(t, "no_difference")[:3], map[string]*evidence.DirectionLiveDD{"2026-W44": dvDD(dvF(-3), dvF(-1))})
	v := evidence.BuildDirectionVerdict(weeks)
	if v.Status != evidence.DirectionVerdictKill || v.LiveDrawdown.Status != evidence.DirectionLiveFired {
		t.Fatalf("status %q, live half %q; want kill rule hit, fired", v.Status, v.LiveDrawdown.Status)
	}
	if got := strings.Join(v.LiveDrawdown.Weeks, ","); got != "2026-W44" || len(v.KillWeeks) != 0 {
		t.Errorf("live weeks %s, replay weeks %v; want 2026-W44 and none", got, v.KillWeeks)
	}
	dvContains(t, "line", v.Line, "kill rule hit", "live drawdown", "2026-W44 (-3.00 % vs -1.00 %)", "proposes")
	if strings.Contains(v.Line, "replay streak") {
		t.Errorf("line names the replay streak, which did not fire: %s", v.Line)
	}
	if v.NotChecked != "" {
		t.Errorf("NotChecked = %q; every week had both figures", v.NotChecked)
	}
	md := dvMarkdown(t, v)
	dvContains(t, "markdown", md, "kill rule, live drawdown: **hit** in 2026-W44 (-3.00 % vs -1.00 %)", "| -3.00 / -1.00 · fired |", "too few to conclude (21 days)")
	if strings.Contains(md, "not checked here") {
		t.Errorf("markdown still says something is not checked:\n%s", md)
	}
}

// Scenario: at or under 2× does not fire — the verdict is today's, the live
// half reported as not fired.
func TestDirectionVerdict_LiveDrawdownAtTwiceDoesNotFire(t *testing.T) {
	t.Parallel()
	want := evidence.BuildDirectionVerdict(dvWeeks(t, "no_difference"))
	weeks := dvLive(dvWeeks(t, "no_difference"), map[string]*evidence.DirectionLiveDD{"2026-W45": dvDD(dvF(-2), dvF(-1))}) // exactly 2×
	v := evidence.BuildDirectionVerdict(weeks)
	if v.Status != want.Status || v.Line != want.Line || v.Why != want.Why {
		t.Fatalf("verdict changed: %q, want %q", v.Line, want.Line)
	}
	if v.LiveDrawdown.Status != evidence.DirectionLiveNotFired || v.NotChecked != "" {
		t.Errorf("live half %+v, not checked %q; want not fired, cleared", v.LiveDrawdown, v.NotChecked)
	}
	dvContains(t, "markdown", dvMarkdown(t, v), "kill rule, live drawdown: not hit", "| -2.00 / -1.00 · not fired |")
}

// Scenario: a missing or zero figure is never a pass or a fail — the week is
// not checked with its reason, NotChecked names it; and without any figure
// (the fixtures, a scorecard before #850) the live half is not checked.
func TestDirectionVerdict_LiveDrawdownMissingOrZero(t *testing.T) {
	t.Parallel()
	weeks := dvLive(dvWeeks(t, "no_difference")[:3], map[string]*evidence.DirectionLiveDD{
		"2026-W43": {StablePct: dvF(-5), MVPNote: "no weekly evidence report"},
		"2026-W44": dvDD(dvF(-5), dvF(0)),
	})
	v := evidence.BuildDirectionVerdict(weeks)
	if v.Status == evidence.DirectionVerdictKill || v.LiveDrawdown.Status != evidence.DirectionLiveNotFired {
		t.Fatalf("status %q, live half %q; a missing or zero mvp figure must neither fire nor pass", v.Status, v.LiveDrawdown.Status)
	}
	for i, why := range []string{"mvp BTC: no weekly evidence report", "mvp BTC's live max drawdown is 0 %: no ratio"} {
		if w := v.Weeks[i]; w.LiveHalf != evidence.DirectionLiveNotChecked || w.LiveWhy != why {
			t.Errorf("%s = %q (%q); want not checked (%q)", w.Week, w.LiveHalf, w.LiveWhy, why)
		}
	}
	dvContains(t, "not checked", v.NotChecked, "2026-W43 (mvp BTC: no weekly evidence report)", "2026-W44 (mvp BTC's live max drawdown is 0 %: no ratio)")
	if strings.Contains(v.NotChecked, "2026-W45") {
		t.Errorf("NotChecked names W45, which had both figures: %s", v.NotChecked)
	}
	dvContains(t, "markdown", dvMarkdown(t, v), "- not checked here: the kill rule's live half", "| -5.00 / +0.00 · not checked |")

	none := evidence.BuildDirectionVerdict(dvWeeks(t, "no_difference"))
	if none.LiveDrawdown.Status != evidence.DirectionLiveNotChecked || !strings.Contains(none.NotChecked, "2026-W48 (no weekly evidence report read)") {
		t.Errorf("no figures: live half %q, not checked %q", none.LiveDrawdown.Status, none.NotChecked)
	}
}

// Scenario: both halves fire — the line names both, the status is the
// stop-early proposal.
func TestDirectionVerdict_BothHalvesFire(t *testing.T) {
	t.Parallel()
	weeks := dvLive(dvWeeks(t, "kill_rule"), map[string]*evidence.DirectionLiveDD{"2026-W43": dvDD(dvF(-4.5), dvF(-1.5))})
	v := evidence.BuildDirectionVerdict(weeks)
	if v.Status != evidence.DirectionVerdictKill {
		t.Fatalf("status %q, want kill rule hit", v.Status)
	}
	dvContains(t, "line", v.Line, "replay streak: ", "2026-W44, 2026-W45, 2026-W46", "live drawdown: ", "2026-W43 (-4.50 % vs -1.50 %)", "proposes the decision ticket")
	dvContains(t, "markdown", dvMarkdown(t, v), "kill rule: **hit** in 2026-W44, 2026-W45, 2026-W46 (replay streak)", "kill rule, live drawdown: **hit** in 2026-W43")
}

// Scenario: a live account shows percentages only, and the figures are read
// from both clones' weekly reports as written — the scorecard's line, block
// and JSON carry the drawdown % and no coin amount.
func TestDirectionVerdict_LiveDrawdownFromWeeklyReports(t *testing.T) {
	t.Parallel()
	stable, mvp := t.TempDir(), t.TempDir()
	weeks := dvWeeks(t, "no_difference")[:3]
	write := func(js string, v any) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(js), 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(v)
		if err := os.WriteFile(js, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put := func(root, week string, ddCoin, ddPct float64, equityKnown bool) {
		t.Helper()
		r := evidence.Report{Env: "stable", Bot: "btc", Week: week}
		r.Live.Perf.Points, r.Live.Perf.EquityKnown, r.Live.Perf.Equity = 5, equityKnown, 1.234567
		r.Live.Perf.MaxDrawdown, r.Live.Perf.MaxDrawdownPct = ddCoin, ddPct
		_, js := evidence.OutPaths(root, week, "btc")
		write(js, r)
	}
	for _, w := range weeks[:2] {
		_, js := evidence.DirectionOutPaths(stable, w.Week)
		write(js, w.Card)
	}
	put(stable, "2026-W43", -0.0371, -3, true)
	put(mvp, "2026-W43", -0.0129, -1, true)
	put(stable, "2026-W44", -0.0111, -1, true)
	put(mvp, "2026-W44", -0.0122, -1, true)
	put(stable, "2026-W45", -0.0133, 0, false)
	put(mvp, "2026-W45", -0.0144, -1, true)

	card := *weeks[2].Card
	card.Bot = "btc"
	r := evidence.WithDirectionVerdict(card, stable, mvp)
	v := r.Verdict
	if v == nil || v.Status != evidence.DirectionVerdictKill || strings.Join(v.LiveDrawdown.Weeks, ",") != "2026-W43" {
		t.Fatalf("verdict = %+v; want the live half fired in 2026-W43", v)
	}
	if w := v.Weeks[2]; w.LiveHalf != evidence.DirectionLiveNotChecked || w.LiveWhy != "stable BTC: equity unknown: no percentage" {
		t.Errorf("W45 = %q (%q); want not checked, equity unknown", w.LiveHalf, w.LiveWhy)
	}
	md, js := evidence.DirectionOutPaths(stable, r.Week)
	if err := evidence.WriteDirection(r, md, js); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(md)
	data, _ := os.ReadFile(js)
	for _, out := range []string{string(text), string(data), v.Line} {
		for _, amount := range []string{"0371", "0129", "0111", "1.2345"} {
			if strings.Contains(out, amount) {
				t.Errorf("an amount (%s) reached the scorecard", amount)
			}
		}
	}
	dvContains(t, "json", string(data), `"live_drawdown"`, `"fired_weeks": [`, `"stable_live_max_drawdown_pct": -3`)
}
