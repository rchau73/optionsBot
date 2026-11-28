package tests

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/usage"
)

// The activation-drop rule (#918, event-plan.md §7): the share of the week's
// new people activated in their first day, 20 points or more under the 3
// weeks before's pooled share, each base 10 new people or more.

// activationDropReport is a report with only the figures the drop check
// reads, its own week's activation set to `activated` of `users` new people.
func activationDropReport(week string, users, activated int) usage.Report {
	r := usageReport(week, 40, 0, 0)
	w := usage.ActivationWeek{Week: week, From: r.From, Users: users, Activated: activated}
	r.Activation = &usage.Activation{Weeks: []usage.ActivationWeek{w}, WindowHours: 24, MinUsers: usage.ActivationMinUsers}
	return r
}

func activationDrop(a usage.Alert) (usage.Drop, bool) {
	for _, d := range a.Drops {
		if d.Metric == usage.MetricActivation {
			return d, true
		}
	}
	return usage.Drop{}, false
}

// Scenario: activation falls 20 points or more — 24 of 40 (60 %) in the 3
// weeks before, 3 of 12 (25 %) this week.
func TestUsageActivationDrop_ScenarioActivationFalls(t *testing.T) {
	prev := []usage.Report{activationDropReport("2026-W40", 14, 8), activationDropReport("2026-W41", 13, 8), activationDropReport("2026-W42", 13, 8)}
	a := usage.Drops(activationDropReport("2026-W43", 12, 3), prev, usage.DefaultRules())
	d, ok := activationDrop(a)
	if !ok {
		t.Fatalf("no activation drop: %+v", a)
	}
	if d.Kind != "funnel" || d.Current != 25 || d.Base != 60 || d.ChangePts != -35 || d.Starts != 12 || d.BaseStart != 40 {
		t.Fatalf("drop = %+v, want 25 %% of 12, was 60 %% of 40, −35 pts", d)
	}
	if got := d.String(); got != "activation: 25 % of 12 new people, was 60 % of 40 (-35 pts)" {
		t.Errorf("line = %q", got)
	}
	// The threshold: 20 points under is a drop, 19 points under is not (base 60 %: 20 of 50 = 40 %, 41 of 100 = 41 %).
	at := []usage.Report{activationDropReport("2026-W40", 50, 30), activationDropReport("2026-W41", 50, 30)}
	if _, ok := activationDrop(usage.Drops(activationDropReport("2026-W43", 50, 20), at, usage.DefaultRules())); !ok {
		t.Errorf("60 %% → 40 %% (−20 pts) raised nothing")
	}
	if _, ok := activationDrop(usage.Drops(activationDropReport("2026-W43", 100, 41), at, usage.DefaultRules())); ok {
		t.Errorf("60 %% → 41 %% (−19 pts) raised a drop")
	}
}

// The same scenario end to end, from an events file through usage.Run (what
// cmd/usage -alert prints and the report's JSON carries as `drops`).
func TestUsageActivationDrop_ScenarioActivationFallsFromEvents(t *testing.T) {
	var lines []string
	person := 0
	week := func(label string, users, activated int) {
		from := usageWeek(t, label).From
		for i := 0; i < users; i++ {
			person++
			t0 := from.Add(time.Duration(i+1) * time.Hour)
			lines = append(lines, usageLine(t, "session_started", t0, person, map[string]any{"entry_screen": "monitor", "viewport": "desktop"}))
			if i < activated {
				lines = append(lines, usageLine(t, "screen_viewed", t0.Add(time.Minute), person, map[string]any{"screen": "overview"}),
					usageLine(t, "screen_viewed", t0.Add(5*time.Minute), person, map[string]any{"screen": "worst_case"}))
			}
		}
	}
	week("2026-W40", 14, 8)
	week("2026-W41", 13, 8)
	week("2026-W42", 13, 8)
	week("2026-W43", 12, 3)
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, ok := activationDrop(*r.Drops)
	if !ok || d.Current != 25 || d.Base != 60 || d.Starts != 12 || d.BaseStart != 40 {
		t.Fatalf("drop = %+v (found %v), drops %+v", d, ok, r.Drops)
	}
	js, err := json.Marshal(r.Drops)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= person; n++ {
		uid, sid := usageUser(n)
		if strings.Contains(string(js), uid) || strings.Contains(string(js), sid) {
			t.Fatalf("person %d's id in the drop check", n)
		}
	}
}

// Scenario: a small base raises nothing — a week of 6 new people, or base
// weeks of 9 in all, is too few to judge.
func TestUsageActivationDrop_ScenarioSmallBase(t *testing.T) {
	prev := []usage.Report{activationDropReport("2026-W40", 14, 8), activationDropReport("2026-W41", 13, 8), activationDropReport("2026-W42", 13, 8)}
	a := usage.Drops(activationDropReport("2026-W43", 6, 0), prev, usage.DefaultRules())
	if _, ok := activationDrop(a); ok || skipReason(a, usage.MetricActivation) != usage.TooFew {
		t.Fatalf("6 new people: drops %+v, skipped %+v", a.Drops, a.Skipped)
	}
	small := []usage.Report{activationDropReport("2026-W40", 3, 3), activationDropReport("2026-W41", 3, 3), activationDropReport("2026-W42", 3, 3)}
	a = usage.Drops(activationDropReport("2026-W43", 12, 0), small, usage.DefaultRules())
	if _, ok := activationDrop(a); ok || skipReason(a, usage.MetricActivation) != usage.TooFew {
		t.Fatalf("base of 9: drops %+v, skipped %+v", a.Drops, a.Skipped)
	}
	// A report without an activation block (written before #620) adds nothing to the base.
	a = usage.Drops(activationDropReport("2026-W43", 12, 0), []usage.Report{usageReport("2026-W42", 40, 0, 0)}, usage.DefaultRules())
	if skipReason(a, usage.MetricActivation) != usage.TooFew {
		t.Fatalf("base without activation: skipped %+v", a.Skipped)
	}
}

// Scenario: a week with no events is not zero — it is left out of the base.
func TestUsageActivationDrop_ScenarioWeekWithNoEventsLeftOut(t *testing.T) {
	w41, _ := evidence.ParseWeek("2026-W41")
	empty := usage.Report{Week: "2026-W41", From: w41.From, To: w41.To, NoEvents: usage.NoEventsWhy}
	prev := []usage.Report{activationDropReport("2026-W40", 20, 12), empty, activationDropReport("2026-W42", 20, 12)}
	a := usage.Drops(activationDropReport("2026-W43", 12, 3), prev, usage.DefaultRules())
	d, ok := activationDrop(a)
	if !ok || d.Base != 60 || d.BaseStart != 40 || !slices.Equal(a.Missing, []string{"2026-W41"}) {
		t.Fatalf("drop %+v (found %v), missing %v", d, ok, a.Missing)
	}
	// The week itself without events: nothing judged.
	w43, _ := evidence.ParseWeek("2026-W43")
	a = usage.Drops(usage.Report{Week: "2026-W43", From: w43.From, To: w43.To}, prev, usage.DefaultRules())
	if skipReason(a, usage.MetricActivation) != "no events in the week" {
		t.Fatalf("empty week: skipped %+v", a.Skipped)
	}
}

// Scenario: the share holds — 10 points under the pooled share raises nothing.
func TestUsageActivationDrop_ScenarioShareHolds(t *testing.T) {
	prev := []usage.Report{activationDropReport("2026-W40", 20, 12), activationDropReport("2026-W41", 20, 12)}
	a := usage.Drops(activationDropReport("2026-W43", 20, 10), prev, usage.DefaultRules())
	if _, ok := activationDrop(a); ok || skipReason(a, usage.MetricActivation) != "" {
		t.Fatalf("50 %% vs 60 %%: drops %+v, skipped %+v", a.Drops, a.Skipped)
	}
}
