package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Cohort retention over 12 weeks and the flattening check (#607).

// fourteenWeekFixture is a log over 2026-W30 … W43, reported as W43:
//
//	W30  user 900 starts (older than every cohort the report follows)
//	W31  users 1–12 start; back in +1 … +12: 9, 8, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6 → flattening
//	W35  users 201–206 start (6 users) → too few to judge
//	W37  users 101–112 start; back in +1 … +6: 10, 9, 8, 8, 8, 4 → falling (−33 pts in +6)
func fourteenWeekFixture(t *testing.T) []string {
	wk := func(label string) time.Time { return usageWeek(t, label).From }
	var lines []string
	visit := func(at time.Time, from, to int) {
		for n := from; n <= to; n++ {
			lines = append(lines, usageLine(t, "session_started", at.Add(time.Duration(n)*time.Minute), n, nil))
		}
	}
	const week = 7 * 24 * time.Hour
	visit(wk("2026-W30").Add(time.Hour), 900, 900)
	w31 := wk("2026-W31")
	visit(w31.Add(time.Hour), 1, 12)
	for j, back := range []int{9, 8, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6} {
		visit(w31.Add(time.Duration(j+1)*week+2*time.Hour), 1, back)
	}
	visit(wk("2026-W35").Add(time.Hour), 201, 206)
	w37 := wk("2026-W37")
	visit(w37.Add(time.Hour), 101, 112)
	for j, back := range []int{10, 9, 8, 8, 8, 4} {
		visit(w37.Add(time.Duration(j+1)*week+2*time.Hour), 101, 100+back)
	}
	return lines
}

func fourteenWeekReport(t *testing.T, flat usage.FlatRules) usage.Report {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.RunWith(writeUsageEvents(t, fourteenWeekFixture(t)), w.From, w.To, w.Label, t.TempDir(),
		usage.DefaultRules(), flat, usage.DropBaseWeeks)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func cohortOf(t *testing.T, r usage.Report, week string) usage.Cohort {
	t.Helper()
	for _, c := range r.Cohorts {
		if c.Week == week {
			return c
		}
	}
	t.Fatalf("no cohort %s in %d cohorts", week, len(r.Cohorts))
	return usage.Cohort{}
}

func TestUsageCohort_ScenarioACohortIsFollowedFor12Weeks(t *testing.T) {
	r := fourteenWeekReport(t, usage.DefaultFlatRules())
	if usage.CohortWeeks != 12 || len(r.Cohorts) != usage.CohortWeeks+1 || r.Cohorts[0].Week != "2026-W31" {
		t.Fatalf("cohorts = %d from %s, want 13 from 2026-W31", len(r.Cohorts), r.Cohorts[0].Week)
	}
	c := cohortOf(t, r, "2026-W31")
	want := []int{9, 8, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6}
	if c.Users != 12 || len(c.Retained) != 12 {
		t.Fatalf("W31 = %d users, %d weeks; want 12 and +1 … +12", c.Users, len(c.Retained))
	}
	for i, n := range want {
		if got := c.Retained[i]; got.Offset != i+1 || got.Users != n {
			t.Errorf("W31 +%d = %+v, want %d users", i+1, got, n)
		}
	}
	if c.Status != usage.StatusFlattening {
		t.Errorf("W31 status = %q, want flattening", c.Status)
	}
}

func TestUsageCohort_ScenarioAWeekThatHasNotEndedIsNotAZero(t *testing.T) {
	r := fourteenWeekReport(t, usage.DefaultFlatRules())
	c := cohortOf(t, r, "2026-W37")
	if len(c.Retained) != 6 || c.Retained[5].Offset != 6 {
		t.Fatalf("W37 retained = %+v, want +1 … +6 only", c.Retained)
	}
	if c.Status != usage.StatusFalling {
		t.Errorf("W37 status = %q, want falling (the ended +6 fell 33 pts; +7 … +12 not counted)", c.Status)
	}
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	text := md.String()
	head := "| Cohort | Users | +1 | +2 | +3 | +4 | +5 | +6 | +7 | +8 | +9 | +10 | +11 | +12 | Curve |"
	row := "| 2026-W37 | 12 | 10 (83 %) | 9 (75 %) | 8 (67 %) | 8 (67 %) | 8 (67 %) | 4 (33 %) | — | — | — | — | — | — | falling |"
	for _, s := range []string{head, row, "| 2026-W35 | 6 |", "too few to judge |", "| 2026-W31 | 12 | 9 (75 %) |", "6 (50 %) | flattening |",
		"Weeks with no first session, left out: 2026-W32"} {
		if !strings.Contains(text, s) {
			t.Errorf("markdown lacks %q:\n%s", s, text)
		}
	}
	// The JSON carries the status and the rules it was judged on.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"status":"falling"`) || !strings.Contains(string(b), `"cohort_rules":{"min_users":10,"weeks":3,"step_pts":5}`) {
		t.Errorf("report JSON lacks the status or its rules: %s", b)
	}
}

func TestUsageCohort_FlatRulesCanBeStudied(t *testing.T) {
	// cmd/usage's -flat-step-pts: with 40 points allowed, W37's 33-point fall is flat.
	flat := usage.DefaultFlatRules()
	flat.StepPts = 40
	r := fourteenWeekReport(t, flat)
	if c := cohortOf(t, r, "2026-W37"); c.Status != usage.StatusFlattening {
		t.Errorf("W37 at 40 pts = %q, want flattening", c.Status)
	}
	if r.CohortRules == nil || r.CohortRules.StepPts != 40 {
		t.Errorf("cohort rules = %+v, want the studied 40 pts", r.CohortRules)
	}
}

// retained is a cohort's ended weeks at these shares (users out of 100).
func retained(pcts ...float64) []usage.Retention {
	out := make([]usage.Retention, len(pcts))
	for i, p := range pcts {
		out[i] = usage.Retention{Offset: i + 1, Users: int(p), Pct: &p}
	}
	return out
}

// Scenarios: a flat curve is named; a falling curve is named; a small cohort is not judged.
func TestUsageCohort_Flattening(t *testing.T) {
	for _, tc := range []struct {
		name  string
		users int
		ret   []usage.Retention
		want  string
	}{
		{"a flat curve is named: 12 users, last 3 weeks within 5 pts", 12, retained(80, 50, 48, 45, 43), usage.StatusFlattening},
		{"exactly 5 pts is still flat", 12, retained(60, 55, 50, 45), usage.StatusFlattening},
		{"a falling curve is named: one of the last 3 fell more than 5 pts", 12, retained(80, 50, 48, 42, 41), usage.StatusFalling},
		{"a fall just past 5 pts is falling", 12, retained(60, 55, 50, 44.9), usage.StatusFalling},
		{"an older fall is not judged: only the last 3 weeks", 12, retained(90, 50, 49, 48, 47), usage.StatusFlattening},
		{"a rise past 5 pts is not flat yet", 12, retained(50, 42, 40, 48, 47), usage.StatusRising},
		{"a fall wins over a rise", 12, retained(50, 42, 40, 48, 40), usage.StatusFalling},
		{"a small cohort is not judged: 6 users", 6, retained(80, 50, 50, 50, 50), usage.StatusTooFew},
		{"one under the minimum", usage.FlatMinUsers - 1, retained(50, 50, 50, 50), usage.StatusTooFew},
		{"at the minimum", usage.FlatMinUsers, retained(50, 50, 50, 50), usage.StatusFlattening},
		{"3 ended weeks are too few", 12, retained(50, 50, 50), usage.StatusTooFew},
		{"no ended week", 12, nil, usage.StatusTooFew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := usage.Flattening(usage.Cohort{Week: "2026-W31", Users: tc.users, Retained: tc.ret})
			if got != tc.want {
				t.Errorf("Flattening = %q, want %q", got, tc.want)
			}
		})
	}
	if usage.StatusTooFew != "too few to judge" {
		t.Errorf("too few = %q", usage.StatusTooFew)
	}
}
