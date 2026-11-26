package tests

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Retention by activation (#818, event-plan.md §9): each cohort split into
// activated in the first day (§8's rule) and not, and who of each came back
// in weeks +1 … +4. Reported as 2026-W43.

// retentionCohort adds a cohort starting in `week`: people first … first+act-1
// are activated (Overview, then the worst-case page, within minutes), the next
// `other` people only start a session. back[j-1] = {activated, not activated}
// people (the lowest numbers of each group) with a panel_expanded in week +j.
func retentionCohort(t *testing.T, week string, first, act, other int, back [][2]int) []string {
	t.Helper()
	start := usageWeek(t, week).From
	var lines []string
	for i := 0; i < act+other; i++ {
		n, t0 := first+i, start.Add(time.Duration(i+1)*time.Hour)
		lines = append(lines, usageLine(t, "session_started", t0, n, map[string]any{"entry_screen": "monitor", "viewport": "desktop"}))
		if i < act {
			lines = append(lines,
				usageLine(t, "screen_viewed", t0.Add(time.Minute), n, map[string]any{"screen": "overview"}),
				usageLine(t, "screen_viewed", t0.Add(2*time.Minute), n, map[string]any{"screen": "worst_case"}))
		}
	}
	for j, b := range back {
		at := start.Add(time.Duration(j+1)*7*24*time.Hour + 30*time.Hour)
		for i := 0; i < b[0]; i++ {
			lines = append(lines, usageLine(t, "panel_expanded", at, first+i, map[string]any{"panel": "held"}))
		}
		for i := 0; i < b[1]; i++ {
			lines = append(lines, usageLine(t, "panel_expanded", at, first+act+i, map[string]any{"panel": "held"}))
		}
	}
	return lines
}

// retentionWeekEvent keeps the report's week non-empty: person 999's visit.
func retentionWeekEvent(t *testing.T) string {
	return usageLine(t, "session_started", usageWeek(t, "2026-W43").From.Add(50*time.Hour), 999, nil)
}

func retentionReport(t *testing.T, lines []string) (usage.Report, []string) {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	r, paths, err := usage.Run(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.ActivationRetention == nil || len(r.ActivationRetention.Weeks) != usage.ActivationWeeks {
		t.Fatalf("activation retention = %+v, want %d weeks", r.ActivationRetention, usage.ActivationWeeks)
	}
	return r, paths
}

func retentionWeek(t *testing.T, r usage.Report, week string) usage.ActivationCohort {
	t.Helper()
	for _, c := range r.ActivationRetention.Weeks {
		if c.Week == week {
			return c
		}
	}
	t.Fatalf("no retention cohort %s", week)
	return usage.ActivationCohort{}
}

func retentionMarkdown(t *testing.T, r usage.Report) string {
	t.Helper()
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	return md.String()
}

func TestUsageActivationRetention_ScenarioActivatedAndNotActivatedAreSplit(t *testing.T) {
	// 12 new people in W38, 8 activated and 4 not; weeks +1 … +4 = W39 … W42, all ended.
	lines := retentionCohort(t, "2026-W38", 1, 8, 4, [][2]int{{6, 1}, {5, 1}, {4, 0}, {3, 1}})
	r, _ := retentionReport(t, append(lines, retentionWeekEvent(t)))
	c := retentionWeek(t, r, "2026-W38")
	if c.Activated.Users != 8 || c.NotActivated.Users != 4 {
		t.Fatalf("W38 split = %d / %d, want 8 / 4", c.Activated.Users, c.NotActivated.Users)
	}
	wantBack := [][2]int{{6, 1}, {5, 1}, {4, 0}, {3, 1}}
	for j, want := range wantBack {
		a, o := c.Activated.Returns[j], c.NotActivated.Returns[j]
		if a.Offset != j+1 || a.Users != want[0] || o.Users != want[1] || a.NotYet || o.NotYet || a.NoEvents {
			t.Errorf("+%d = %+v / %+v, want %d / %d back", j+1, a, o, want[0], want[1])
		}
	}
	// Both groups are under ActivationMinUsers (10): counts, no share.
	for _, g := range []usage.ReturnGroup{c.Activated, c.NotActivated} {
		if g.Note != usage.TooFew {
			t.Errorf("group of %d note = %q, want %q", g.Users, g.Note, usage.TooFew)
		}
		for _, ret := range g.Returns {
			if ret.Pct != nil {
				t.Errorf("group of %d has a share %v at +%d", g.Users, *ret.Pct, ret.Offset)
			}
		}
	}
	md := retentionMarkdown(t, r)
	for _, s := range []string{"## Retention by activation (Q9)", "association, not proof",
		"| Cohort | Group | People | +1 | +2 | +3 | +4 |",
		"| 2026-W38 | activated | 8 (too few to judge: under 10) | 6 | 5 | 4 | 3 |",
		"| 2026-W38 | not activated | 4 (too few to judge: under 10) | 1 | 1 | 0 | 1 |",
		"| +4 | 1 | 3 of 8 (too few to judge) | 1 of 4 (too few to judge) |"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}

	// A group of 10 or more gets its shares; the group of 4 still none.
	lines = retentionCohort(t, "2026-W38", 1, 10, 4, [][2]int{{8, 1}, {6, 1}, {5, 0}, {4, 2}})
	r, _ = retentionReport(t, append(lines, retentionWeekEvent(t)))
	c = retentionWeek(t, r, "2026-W38")
	if c.Activated.Note != "" || c.Activated.Returns[1].Pct == nil || *c.Activated.Returns[1].Pct != 60 {
		t.Fatalf("activated group of 10 = %+v, want 60 %% back in +2", c.Activated)
	}
	if c.NotActivated.Note != usage.TooFew || c.NotActivated.Returns[1].Pct != nil {
		t.Fatalf("not activated group of 4 = %+v, want too few, no share", c.NotActivated)
	}
	if !strings.Contains(retentionMarkdown(t, r), "| 2026-W38 | activated | 10 | 8 (80 %) | 6 (60 %) | 5 (50 %) | 4 (40 %) |") {
		t.Errorf("markdown lacks the shares row:\n%s", retentionMarkdown(t, r))
	}
	want := "retention by activation: activated come back 40 % in week 4, not activated too few to judge (n = 10 / 4; association, not proof)"
	if got := r.ActivationRetention.Headline(); got != want {
		t.Errorf("headline = %q, want %q", got, want)
	}
}

func TestUsageActivationRetention_ScenarioWeeksNotEnded(t *testing.T) {
	// A W41 cohort: +1 = W42 and +2 = W43 (the report's week) have ended, +3 and +4 have not.
	lines := retentionCohort(t, "2026-W41", 1, 10, 10, [][2]int{{7, 3}, {6, 2}})
	r, _ := retentionReport(t, lines)
	c := retentionWeek(t, r, "2026-W41")
	for _, g := range []usage.ReturnGroup{c.Activated, c.NotActivated} {
		for _, ret := range g.Returns {
			ended := ret.Offset <= 2
			if ret.NotYet == ended || (!ended && (ret.Users != 0 || ret.Pct != nil)) {
				t.Errorf("+%d = %+v, ended %v", ret.Offset, ret, ended)
			}
		}
	}
	if p := c.Activated.Returns[0].Pct; p == nil || *p != 70 {
		t.Errorf("activated +1 share = %v, want 70 %%", p)
	}
	if !strings.Contains(retentionMarkdown(t, r), "| 2026-W41 | activated | 10 | 7 (70 %) | 6 (60 %) | not yet | not yet |") {
		t.Errorf("markdown lacks the not-yet row:\n%s", retentionMarkdown(t, r))
	}
	// The report's own cohort has no ended week; the pooled +3 and +4 hold no cohort.
	if w43 := retentionWeek(t, r, "2026-W43"); !w43.Activated.Returns[0].NotYet {
		t.Errorf("W43 +1 = %+v, want not yet", w43.Activated.Returns[0])
	}
	if p := r.ActivationRetention.Pooled[3]; p.Cohorts != 0 || p.Activated != 0 || p.ActivatedNote != usage.TooFew {
		t.Errorf("pooled +4 = %+v, want no cohort", p)
	}
	if got := r.ActivationRetention.Headline(); !strings.Contains(got, "no cohort has an ended week 4 yet") {
		t.Errorf("headline = %q", got)
	}
}

func TestUsageActivationRetention_ScenarioNoEventsInAWeek(t *testing.T) {
	// W38's cohort comes back in +2 … +4 but W39 (+1) has no event at all;
	// W35 has no event either. A W37 cohort (5 + 5) has +1 = W38, +2 = W39.
	lines := retentionCohort(t, "2026-W38", 1, 10, 10, [][2]int{{0, 0}, {5, 2}, {4, 1}, {3, 1}})
	lines = append(lines, retentionCohort(t, "2026-W37", 100, 5, 5, [][2]int{{2, 1}})...)
	r, _ := retentionReport(t, append(lines, retentionWeekEvent(t)))
	c := retentionWeek(t, r, "2026-W38")
	if a := c.Activated.Returns[0]; !a.NoEvents || a.Pct != nil || a.Users != 0 {
		t.Errorf("W38 +1 = %+v, want no events, no count or share", a)
	}
	if w35 := retentionWeek(t, r, "2026-W35"); !w35.NoEvents {
		t.Errorf("W35 = %+v, want noted as no events", w35)
	}
	// Pooled +1: W37 only (W38's +1 had no events) — W38's 10 + 10 are in no base.
	p1 := r.ActivationRetention.Pooled[0]
	if p1.Cohorts != 1 || p1.Activated != 5 || p1.NotActivated != 5 || p1.ActivatedBack != 2 {
		t.Errorf("pooled +1 = %+v, want W37's 5 / 5 only", p1)
	}
	// Pooled +2: W38 (10 / 10) and W37, whose +2 is W39 — no events, left out.
	if p2 := r.ActivationRetention.Pooled[1]; p2.Cohorts != 1 || p2.Activated != 10 || p2.ActivatedBack != 5 || p2.ActivatedPct == nil || *p2.ActivatedPct != 50 {
		t.Errorf("pooled +2 = %+v, want W38's 10 with 5 back (50 %%)", p2)
	}
	md := retentionMarkdown(t, r)
	for _, s := range []string{"| 2026-W38 | activated | 10 | no events | 5 (50 %) | 4 (40 %) | 3 (30 %) |",
		"Weeks with no events, left out (not zero): 2026-W32, 2026-W33, 2026-W34, 2026-W35, 2026-W36, 2026-W39"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}
	if strings.Contains(md, "| 2026-W39 | activated") {
		t.Errorf("a week with no events has a row:\n%s", md)
	}
}

func TestUsageActivationRetention_ScenarioPrivacy(t *testing.T) {
	// No user id in any output; only lines that pass the plan (consented people) count.
	lines := retentionCohort(t, "2026-W38", 1, 10, 4, [][2]int{{8, 1}, {6, 1}, {5, 0}, {4, 2}})
	w38 := usageWeek(t, "2026-W38").From
	for _, ev := range []string{"session_started", "panel_expanded"} {
		line := usageLine(t, ev, w38.Add(30*time.Hour), 77, map[string]any{"panel": "held"})
		uid, _ := usageUser(77)
		lines = append(lines, strings.Replace(line, uid, "not-a-pseudonym", 1))
	}
	r, paths := retentionReport(t, append(lines, retentionWeekEvent(t)))
	if c := retentionWeek(t, r, "2026-W38"); c.Activated.Users != 10 || c.NotActivated.Users != 4 {
		t.Errorf("W38 = %d / %d, want only the plan's lines (10 / 4)", c.Activated.Users, c.NotActivated.Users)
	}
	js, err := json.Marshal(r.ActivationRetention)
	if err != nil {
		t.Fatal(err)
	}
	md := retentionMarkdown(t, r)
	section := md[strings.Index(md, "## Retention by activation"):]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	own := []string{string(js), section, r.ActivationRetention.Headline()}
	outputs := append([]string{}, own...)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, string(b))
	}
	for _, out := range outputs {
		for _, n := range []int{1, 4, 11, 14, 999} {
			if uid, sid := usageUser(n); strings.Contains(out, uid) || strings.Contains(out, sid) {
				t.Errorf("output holds person %d's id", n)
			}
		}
	}
	for _, out := range own {
		for _, bad := range []string{"not-a-pseudonym", "user_id", "session_id", "amount", "usd"} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Errorf("the section holds %q:\n%s", bad, out)
			}
		}
	}
}
