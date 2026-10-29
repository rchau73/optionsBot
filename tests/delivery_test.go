package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/delivery"
)

// The fixture (tests/testdata/delivery/) is a small repo history: PRs #1–#8
// merged from 2026-10-02, a merge with no PR number, mvp deploys recorded from
// 2026-10-17 with a rollback (and its "deploy failed" issue), a revert (#6 of
// #5), a regression bug #100 caused by #7 and fixed by #8, two releases and a
// tag on a commit not on main, and malformed lines.

func deliveryFixture(t *testing.T) delivery.Report {
	t.Helper()
	return deliveryFixtureWith(t, func(*delivery.Inputs) {})
}

// deliveryFixtureWith builds the fixture after edit changes its inputs.
func deliveryFixtureWith(t *testing.T, edit func(*delivery.Inputs)) delivery.Report {
	t.Helper()
	dir := filepath.Join("testdata", "delivery")
	in, err := delivery.Load(delivery.Files{
		Main: filepath.Join(dir, "main.tsv"), Tags: filepath.Join(dir, "tags.tsv"),
		PRs: filepath.Join(dir, "prs.json"), Issues: filepath.Join(dir, "issues.json"),
		Deploys: filepath.Join(dir, "deploys.tsv"), Promotions: filepath.Join(dir, "promotions.tsv"),
	})
	if err != nil {
		t.Fatal(err)
	}
	in.From = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	edit(&in)
	return delivery.Build(in, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
}

func dAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func dTimeIs(p *time.Time, want string) bool {
	if want == "" {
		return p == nil
	}
	return p != nil && p.Equal(dAt(want))
}

func dWeek(t *testing.T, r delivery.Report, name string) delivery.Week {
	t.Helper()
	for _, w := range r.Weeks {
		if w.Week == name {
			return w
		}
	}
	t.Fatalf("week %s not in the report", name)
	return delivery.Week{}
}

func dNear(p *float64, want float64) bool { return p != nil && *p > want-0.011 && *p < want+0.011 }

// Scenario: every merged PR is listed once, with its merge time, its first
// mvp deploy (or "not recorded" before recording began) and its release.
func TestDelivery_EveryMergedPRListedOnceWithLiveTimes(t *testing.T) {
	r := deliveryFixture(t)
	cases := []struct {
		num                  int
		epic, merged, first  string
		mvp, mvpNote, stable string
		release, stableNote  string
	}{
		{1, "E2", "2026-10-02T10:00:00Z", "2026-10-01T10:00:00Z", "", "not recorded", "2026-10-17T12:00:00Z", "v0.1.0", ""},
		{2, "E6", "2026-10-08T12:00:00Z", "2026-10-08T06:00:00Z", "", "not recorded", "2026-10-17T12:00:00Z", "v0.1.0", ""},
		{3, "E6", "2026-10-16T09:00:00Z", "2026-10-15T09:00:00Z", "", "not recorded", "2026-10-17T12:00:00Z", "v0.1.0", ""},
		{4, "E6", "2026-10-19T10:00:00Z", "2026-10-19T08:00:00Z", "2026-10-19T10:04:00Z", "", "2026-10-26T10:00:00Z", "v0.2.0", ""},
		// The 10:05 rollback is not a healthy deploy: #5 went live at 11:00.
		{5, "", "2026-10-20T10:00:00Z", "2026-10-20T09:00:00Z", "2026-10-20T11:00:00Z", "", "2026-10-26T10:00:00Z", "v0.2.0", ""},
		{6, "", "2026-10-20T12:30:00Z", "2026-10-20T11:30:00Z", "2026-10-20T12:34:00Z", "", "2026-10-26T10:00:00Z", "v0.2.0", ""},
		{7, "E1", "2026-10-22T10:00:00Z", "2026-10-21T10:00:00Z", "2026-10-22T10:03:00Z", "", "2026-10-26T10:00:00Z", "v0.2.0", ""},
		{8, "E1", "2026-10-23T09:00:00Z", "2026-10-23T08:30:00Z", "2026-10-23T09:05:00Z", "", "", "", "not released yet"},
	}
	if len(r.PRs) != len(cases) {
		t.Fatalf("PRs listed = %d, want %d: %+v", len(r.PRs), len(cases), r.PRs)
	}
	seen := map[int]bool{}
	for i, c := range cases {
		p := r.PRs[i]
		if seen[p.Number] {
			t.Errorf("PR #%d listed twice", p.Number)
		}
		seen[p.Number] = true
		if p.Number != c.num || p.Epic != c.epic || !p.Merged.Equal(dAt(c.merged)) || !dTimeIs(p.FirstCommit, c.first) {
			t.Errorf("PR %d: got #%d epic %q merged %v first %v", c.num, p.Number, p.Epic, p.Merged, p.FirstCommit)
		}
		if !dTimeIs(p.MVPLive, c.mvp) || p.MVPNote != c.mvpNote {
			t.Errorf("PR #%d mvp: got %v %q, want %s %q", c.num, p.MVPLive, p.MVPNote, c.mvp, c.mvpNote)
		}
		if !dTimeIs(p.StableLive, c.stable) || p.Release != c.release || p.StableNote != c.stableNote {
			t.Errorf("PR #%d stable: got %v %s %q, want %s %s %q", c.num, p.StableLive, p.Release, p.StableNote, c.stable, c.release, c.stableNote)
		}
	}
	if r.PRs[5].Reverts != 5 || r.PRs[0].Title != "[E2] Feature one" || r.PRs[0].Branch != "agent/issue-1" {
		t.Errorf("revert / title / branch: %+v %+v", r.PRs[5], r.PRs[0])
	}
	if len(r.Releases) != 2 || r.Releases[0].Tag != "v0.1.0" || r.Releases[1].Tag != "v0.2.0" {
		t.Errorf("releases (v0.2.0 is in promotions.tsv and deploys.tsv: one release): %+v", r.Releases)
	}
	if !dTimeIs(r.MVPRecordedFrom, "2026-10-17T08:00:00Z") || !dTimeIs(r.StableRecordedFrom, "2026-10-17T12:00:00Z") {
		t.Errorf("recorded from: mvp %v stable %v", r.MVPRecordedFrom, r.StableRecordedFrom)
	}
}

// Scenario: a deploy followed by a rollback, or a regression bug that names
// PR #N, counts as a failure; time to restore runs to the next healthy deploy
// or until the fix is live.
func TestDelivery_FailuresAndTimeToRestore(t *testing.T) {
	r := deliveryFixture(t)
	type want struct {
		kind, env, at, restored string
		hours                   float64
		caused, fix, issue      int
	}
	wants := map[string]want{
		// The rollback and its "[E6] mvp deploy failed" issue are one failure.
		"rollback/mvp/2026-10-20T10:05:00Z": {delivery.FailRollback, "mvp", "2026-10-20T10:05:00Z", "2026-10-20T11:00:00Z", 0.92, 0, 0, 90},
		// From the revert PR's opening (after #5 went live) to the revert live.
		"revert/#6/mvp": {delivery.FailRevert, "mvp", "2026-10-20T12:00:00Z", "2026-10-20T12:34:00Z", 0.57, 5, 6, 0},
		// From the bug report to the fix PR live on mvp.
		"regression/#100/mvp": {delivery.FailRegression, "mvp", "2026-10-23T08:00:00Z", "2026-10-23T09:05:00Z", 1.08, 7, 8, 100},
		// On stable from v0.2.0 (which carries #7); the fix is not released.
		"regression/#100/stable": {delivery.FailRegression, "stable", "2026-10-26T10:00:00Z", "", 0, 7, 8, 100},
	}
	if len(r.Failures) != len(wants) {
		t.Fatalf("failures = %d, want %d: %+v", len(r.Failures), len(wants), r.Failures)
	}
	for _, f := range r.Failures {
		w, ok := wants[f.ID]
		if !ok {
			t.Errorf("unexpected failure %+v", f)
			continue
		}
		if f.Kind != w.kind || f.Env != w.env || !f.At.Equal(dAt(w.at)) || !dTimeIs(f.RestoredAt, w.restored) ||
			f.CausedBy != w.caused || f.FixPR != w.fix || f.Issue != w.issue {
			t.Errorf("%s: got %+v", f.ID, f)
		}
		if w.restored != "" && !dNear(f.RestoreHours, w.hours) {
			t.Errorf("%s: restore %v h, want %.2f", f.ID, f.RestoreHours, w.hours)
		}
	}
	// The revert shipped with its cause in v0.2.0: no stable failure for it.
	for _, p := range r.PRs {
		if p.Number == 5 && strings.Join(p.Failures, ",") != "revert/#6/mvp" {
			t.Errorf("#5 failures = %v", p.Failures)
		}
		if p.Number == 7 && strings.Join(p.Failures, ",") != "regression/#100/mvp,regression/#100/stable" {
			t.Errorf("#7 failures = %v", p.Failures)
		}
	}
	if !r.Releases[1].Failed || r.Releases[0].Failed {
		t.Errorf("v0.2.0 carries #7's regression: %+v", r.Releases)
	}
}

// Review of #474: a regression filed 2 h after its fix PR (#8) went live on
// mvp starts at the fix's live time (the earlier of detected and fix live),
// so its restore is 0 h, not −2 h, and the week's median stays ≥ 0.
func TestDelivery_RegressionFiledAfterFixLiveRestoresInZero(t *testing.T) {
	fixLive := dAt("2026-10-23T09:05:00Z")
	r := deliveryFixtureWith(t, func(in *delivery.Inputs) {
		for i := range in.Issues {
			if in.Issues[i].Number == 100 {
				in.Issues[i].CreatedAt = fixLive.Add(2 * time.Hour)
			}
		}
	})
	var found bool
	for _, f := range r.Failures {
		if f.RestoreHours != nil && *f.RestoreHours < 0 {
			t.Errorf("%s: negative restore %v h", f.ID, *f.RestoreHours)
		}
		if f.ID == "regression/#100/mvp" {
			found = true
			if !f.At.Equal(fixLive) || !dTimeIs(f.RestoredAt, "2026-10-23T09:05:00Z") || !dNear(f.RestoreHours, 0) {
				t.Errorf("regression/#100/mvp: at %v restored %v hours %v, want at and restored %v, 0 h", f.At, f.RestoredAt, f.RestoreHours, fixLive)
			}
		}
	}
	if !found {
		t.Fatalf("regression/#100/mvp missing: %+v", r.Failures)
	}
	// W43 restores: rollback 0.92, revert 0.57, regression 0 → median 0.57.
	if s := dWeek(t, r, "2026-W43").MVP.Restore; s.N != 3 || !dNear(s.MedianHours, 0.57) || *s.MedianHours < 0 {
		t.Errorf("W43 restore: %+v", s)
	}
}

// Review of #474: stable's frequency is releases per week, banded per week —
// one release in a 7-day week is 1 a week, high; mvp keeps deploys a day.
func TestDelivery_StableFrequencyIsReleasesPerWeek(t *testing.T) {
	r := deliveryFixture(t)
	// W44 so far: v0.2.0 in the 2.5 days to Wednesday 12:00 → 2.8 a week.
	w44 := dWeek(t, r, "2026-W44")
	if s := w44.Stable; s.PerDay != nil || !dNear(s.PerWeek, 2.8) || s.FrequencyBand != delivery.BandHigh {
		t.Errorf("W44 stable: per day %v per week %v band %s", s.PerDay, s.PerWeek, s.FrequencyBand)
	}
	if m := dWeek(t, r, "2026-W43").MVP; m.PerWeek != nil || m.PerDay == nil {
		t.Errorf("W43 mvp keeps deploys a day: %+v", m)
	}
	cases := []struct {
		name    string
		perWeek float64
		want    string
	}{
		{"1 release in a 7-day week", 1 * 7 / 7.0, delivery.BandHigh},
		{"daily", 7, delivery.BandElite},
		{"1 in 2 weeks", 0.5, delivery.BandMedium},
		{"1 in 5 weeks", 0.2, delivery.BandLow},
		{"none", 0, delivery.BandLow},
	}
	for _, c := range cases {
		if got := delivery.WeeklyFrequencyBand(c.perWeek); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDelivery_WeeklyMetrics(t *testing.T) {
	r := deliveryFixture(t)
	if len(r.Weeks) != 5 || r.Weeks[0].Week != "2026-W40" || r.Weeks[4].Week != "2026-W44" {
		t.Fatalf("weeks: %+v", r.Weeks)
	}
	// W43: 5 healthy deploys + 1 rollback = 6 attempts; failed = the rollback
	// + the deploys that carried #5 (reverted) and #7 (regression).
	w43 := dWeek(t, r, "2026-W43")
	m := w43.MVP
	if !m.Recorded || m.Deploys != 5 || !dNear(m.PerDay, 0.71) || m.FrequencyBand != delivery.BandHigh {
		t.Errorf("W43 frequency: %+v", m)
	}
	if m.Attempts != 6 || m.Failed != 3 || !dNear(m.FailureRatePct, 50) || m.FailureBand != delivery.BandLow {
		t.Errorf("W43 change failure rate: %+v", m)
	}
	if m.Restore.N != 3 || !dNear(m.Restore.MedianHours, 0.92) || m.Restore.Band != delivery.BandElite || m.Unrestored != 0 {
		t.Errorf("W43 restore: %+v", m.Restore)
	}
	if m.MergeToLive.N != 5 || !dNear(m.MergeToLive.MedianHours, 0.07) || m.MergeToLive.Band != delivery.BandElite {
		t.Errorf("W43 merge → mvp: %+v", m.MergeToLive)
	}
	if w43.Merged != 5 || w43.CommitToMerge.N != 5 {
		t.Errorf("W43 merged: %+v", w43)
	}
	if w43.Stable.Deploys != 0 || !dNear(w43.Stable.PerWeek, 0) || w43.Stable.FailureNote != "no release" {
		t.Errorf("W43 stable: %+v", w43.Stable)
	}

	// W42: recording began on Saturday 08:00 — 1 deploy in 40 h.
	w42 := dWeek(t, r, "2026-W42")
	if !w42.MVP.Recorded || w42.MVP.Deploys != 1 || !dNear(w42.MVP.PerDay, 0.6) || !strings.HasPrefix(w42.MVP.Note, "recorded from 2026-10-17") {
		t.Errorf("W42 mvp: %+v", w42.MVP)
	}
	// Merge → stable for #1–#3 (v0.1.0): 362 h, 216 h, 27 h → median 216 h.
	if s := w42.Stable.MergeToLive; s.N != 3 || !dNear(s.MedianHours, 216) || s.Band != delivery.BandMedium {
		t.Errorf("W42 merge → stable: %+v", s)
	}
	if w42.Stable.CommitToLive.N != 3 || w42.MVP.FailureNote == "" {
		t.Errorf("W42 commit → stable %+v; one deploy is too few for a rate: %+v", w42.Stable.CommitToLive, w42.MVP)
	}

	// W44: stable released v0.2.0 (failed, too few to band), not restored.
	w44 := dWeek(t, r, "2026-W44")
	if s := w44.Stable; s.Deploys != 1 || s.Failed != 1 || s.FailureBand != "" || !strings.HasPrefix(s.FailureNote, "too few") || s.Unrestored != 1 {
		t.Errorf("W44 stable: %+v", s)
	}
}

// Scenario: a week with no deploy has frequency 0; a week before recording
// began is "not recorded", not 0. Lead time to merge still counts there.
func TestDelivery_NoDeployIsZeroBeforeRecordingIsNotRecorded(t *testing.T) {
	r := deliveryFixture(t)
	w44 := dWeek(t, r, "2026-W44")
	if !w44.MVP.Recorded || w44.MVP.Deploys != 0 || !dNear(w44.MVP.PerDay, 0) || w44.MVP.FrequencyBand != delivery.BandLow || w44.MVP.FailureNote != "no deploy" {
		t.Errorf("W44 mvp (no deploy): %+v", w44.MVP)
	}
	for _, name := range []string{"2026-W40", "2026-W41"} {
		w := dWeek(t, r, name)
		if w.MVP.Recorded || w.MVP.PerDay != nil || w.MVP.Note != "deploy not recorded" || w.MVP.FailureRatePct != nil || w.MVP.MergeToLive.MedianHours != nil {
			t.Errorf("%s mvp: %+v", name, w.MVP)
		}
		if w.Stable.Recorded || w.Stable.Note != "release not recorded" {
			t.Errorf("%s stable: %+v", name, w.Stable)
		}
		if w.Merged != 1 || w.CommitToMerge.N != 1 || w.CommitToMerge.Band != "" || !strings.HasPrefix(w.CommitToMerge.Note, "too few") {
			t.Errorf("%s commit → merge: %+v", name, w.CommitToMerge)
		}
	}
	if s := dWeek(t, r, "2026-W40").CommitToMerge; !dNear(s.MedianHours, 24) {
		t.Errorf("W40 commit → merge = %v, want 24 h", s.MedianHours)
	}
}

// Negative: a malformed deploys.tsv line, a merge without a PR number, a tag
// (and a deploy) on a commit not on main, and regressions naming no merged PR
// are reported and skipped, never counted.
func TestDelivery_MalformedInputsReportedAndSkipped(t *testing.T) {
	r := deliveryFixture(t)
	wantProblems := []string{
		"deploys.tsv line 8:",
		"mvp deploy 2026-10-24T10:00:00Z: commit fffffff is not on main",
		"merge 1030000 (\"Merge branch 'main' into feature\") names no pull request",
		"tag v9.9.9: commit deadbee is not on main",
		"regression #101 names no PR",
		"regression #102: caused by #999",
	}
	all := strings.Join(r.Problems, "\n")
	for _, w := range wantProblems {
		if !strings.Contains(all, w) {
			t.Errorf("problem %q not reported; got:\n%s", w, all)
		}
	}
	if strings.Contains(all, "#103") {
		t.Errorf("a bug without the regression label is no failure and no problem:\n%s", all)
	}
	// The off-main deploy is not counted in W43 (5 healthy deploys).
	if d := dWeek(t, r, "2026-W43").MVP.Deploys; d != 5 {
		t.Errorf("W43 deploys = %d, want 5", d)
	}
}

// A revert found by title (its branch names no PR), and a run with only the
// git history: deploys and releases "not recorded", the missing files listed.
func TestDelivery_RevertByTitleAndMissingFiles(t *testing.T) {
	dir := filepath.Join("testdata", "delivery")
	in, err := delivery.Load(delivery.Files{Main: filepath.Join(dir, "main.tsv"), Deploys: filepath.Join(dir, "absent.tsv")})
	if err != nil {
		t.Fatal(err)
	}
	in.From = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	in.PRs = []delivery.PR{{Number: 5, Title: "Feature five"}, {Number: 6, Title: `Revert "Feature five"`, Branch: "fix-it"}}
	r := delivery.Build(in, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	all := strings.Join(r.Problems, "\n")
	for _, want := range []string{"deploys: testdata/delivery/absent.tsv does not exist", "tags: no file given", "issues: no file given"} {
		if !strings.Contains(all, want) {
			t.Errorf("problem %q missing:\n%s", want, all)
		}
	}
	for _, p := range r.PRs {
		if p.Number == 6 && p.Reverts != 5 {
			t.Errorf("#6 reverts %d, want 5 (by title)", p.Reverts)
		}
		if p.MVPNote != "not recorded" || p.StableNote != "not released yet" {
			t.Errorf("#%d: %q %q", p.Number, p.MVPNote, p.StableNote)
		}
	}
	if len(r.Failures) != 0 || r.MVPRecordedFrom != nil {
		t.Errorf("nothing went live, so no failure: %+v", r.Failures)
	}
	for _, w := range r.Weeks {
		if w.MVP.Recorded || w.Stable.Recorded {
			t.Errorf("%s recorded without deploys.tsv: %+v", w.Week, w)
		}
	}
}

func TestDelivery_ReadDeploysKinds(t *testing.T) {
	cases := []struct {
		name, line string
		ok         bool
		kind, tag  string
	}{
		{"mvp deploy", "2026-10-20T10:00:00Z\tmvp\tabc1234", true, delivery.KindDeploy, ""},
		{"rollback", "2026-10-20T10:00:00Z\tmvp\tabc1234\trollback", true, delivery.KindRollback, ""},
		{"stable release", "2026-10-20T10:00:00Z\tstable\tabc1234\trelease\tv0.3.0", true, delivery.KindRelease, "v0.3.0"},
		{"bad time", "yesterday\tmvp\tabc1234", false, "", ""},
		{"too few fields", "2026-10-20T10:00:00Z\tmvp", false, "", ""},
		{"unknown env", "2026-10-20T10:00:00Z\tprod\tabc1234", false, "", ""},
		{"unknown kind", "2026-10-20T10:00:00Z\tmvp\tabc1234\tmaybe", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines, problems, err := delivery.ReadDeploys(strings.NewReader(c.line + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			if c.ok != (len(lines) == 1) || c.ok == (len(problems) == 1) {
				t.Fatalf("lines %+v problems %v", lines, problems)
			}
			if c.ok && (lines[0].Kind != c.kind || lines[0].Tag != c.tag) {
				t.Errorf("got %+v", lines[0])
			}
		})
	}
}

func TestDelivery_Bands(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"2 deploys a day", delivery.FrequencyBand(2), delivery.BandElite},
		{"1 a day", delivery.FrequencyBand(1), delivery.BandElite},
		{"2 a week", delivery.FrequencyBand(2.0 / 7), delivery.BandHigh},
		{"1 in 2 weeks", delivery.FrequencyBand(1.0 / 14), delivery.BandMedium},
		{"none", delivery.FrequencyBand(0), delivery.BandLow},
		{"lead 4 min", delivery.LeadTimeBand(0.07), delivery.BandElite},
		{"lead 3 days", delivery.LeadTimeBand(73), delivery.BandHigh},
		{"lead 2 weeks", delivery.LeadTimeBand(14 * 24), delivery.BandMedium},
		{"lead 2 months", delivery.LeadTimeBand(60 * 24), delivery.BandLow},
		{"cfr 5", delivery.FailureRateBand(5), delivery.BandElite},
		{"cfr 10", delivery.FailureRateBand(10), delivery.BandHigh},
		{"cfr 15", delivery.FailureRateBand(15), delivery.BandMedium},
		{"cfr 50", delivery.FailureRateBand(50), delivery.BandLow},
		{"restore 30 min", delivery.RestoreBand(0.5), delivery.BandElite},
		{"restore 5 h", delivery.RestoreBand(5), delivery.BandHigh},
		{"restore 3 days", delivery.RestoreBand(72), delivery.BandMedium},
		{"restore 2 weeks", delivery.RestoreBand(14 * 24), delivery.BandLow},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %s, want %s", c.name, c.got, c.want)
		}
	}
	s := delivery.StatOf([]float64{1, 2, 3, 4, 100}, delivery.LeadTimeBand)
	if !dNear(s.MedianHours, 3) || !dNear(s.P90Hours, 100) || s.Band != delivery.BandElite {
		t.Errorf("StatOf: %+v", s)
	}
	if s := delivery.StatOf([]float64{1, 3}, delivery.LeadTimeBand); s.Band != "" || !dNear(s.MedianHours, 2) || !strings.HasPrefix(s.Note, "too few") {
		t.Errorf("StatOf too few: %+v", s)
	}
	if s := delivery.StatOf(nil, delivery.LeadTimeBand); s.MedianHours != nil || s.Note != "no sample" {
		t.Errorf("StatOf empty: %+v", s)
	}
}

func TestDelivery_WriteJSONAndWeekMarkdown(t *testing.T) {
	r := deliveryFixture(t)
	dir := t.TempDir()
	if err := delivery.Write(dir, r); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "dora.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back delivery.Report
	if err := json.Unmarshal(raw, &back); err != nil || len(back.PRs) != 8 || len(back.Weeks) != 5 {
		t.Fatalf("dora.json: %v, %d PRs, %d weeks", err, len(back.PRs), len(back.Weeks))
	}
	md, err := os.ReadFile(filepath.Join(dir, "2026-W43.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Delivery (DORA) — 2026-W43", "| Change failure rate | 50 % (3 of 6) · low |",
		"| #5 | — | Feature five | 2026-10-20 10:00 | 2026-10-20 11:00 | 2026-10-26 10:00 (v0.2.0) | revert/#6/mvp |",
		"mvp rollback at 2026-10-20 10:05"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("W43.md lacks %q:\n%s", want, md)
		}
	}
	// #473: the Analytics page's release train and weekly summary read these.
	if len(back.MVPDeploys) != 6 || !back.MVPDeploys[0].At.Equal(dAt("2026-10-17T08:00:00Z")) || !back.MVPDeploys[5].At.Equal(dAt("2026-10-23T09:05:00Z")) {
		t.Errorf("mvp_deploys (healthy deploys on main, by time; the rollback and the off-main line left out): %+v", back.MVPDeploys)
	}
	if w43 := dWeek(t, back, "2026-W43"); w43.Summary != delivery.SummaryMarkdown(w43) || !strings.Contains(w43.Summary, "**Delivery (DORA), week 2026-W43**") {
		t.Errorf("W43 summary is not what dora.sh posts:\n%s", w43.Summary)
	}
	w40, _ := os.ReadFile(filepath.Join(dir, "2026-W40.md"))
	if !strings.Contains(string(w40), "| Deployment frequency | deploy not recorded | release not recorded |") {
		t.Errorf("W40.md:\n%s", w40)
	}
}
