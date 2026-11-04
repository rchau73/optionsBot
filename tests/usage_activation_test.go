package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Activation (time-to-value, #620): new people who opened the Overview and
// then a worst-case view, a trade's reason or a tip or the assistant within
// 24 h of their first session.

// activationFixture is a log reported as 2026-W43 with `users` people
// (1 … users) starting in W43, person n at Monday + n h:
//
//	1–3   Overview, then the worst-case page        at +2n min
//	4–5   Overview, then Why? on a trade            at +2n min
//	6     Overview, then a Details trade row        at +12 min
//	7     Overview, then a tip                      at +14 min
//	8     Overview, then a question to the assistant at +16 min
//	9     Overview only
//	10    a tip, never the Overview
//	11    Overview, then the worst-case page 25 h later (outside the day)
//	12+   a session only
//
// and, before: person 50 starts in W38 and comes back in W40 (a week with
// events and no new person); person 51 starts on W42's last hour and does
// the steps in W43, within the day (counted in W42, not W43). W32–W37, W39 and W41 have no event.
func activationFixture(t *testing.T, users int) []string {
	t.Helper()
	wk := func(label string) time.Time { return usageWeek(t, label).From }
	var lines []string
	add := func(event string, at time.Time, n int, props map[string]any) {
		lines = append(lines, usageLine(t, event, at, n, props))
	}
	overview := map[string]any{"screen": "overview"}
	worst := map[string]any{"screen": "worst_case"}
	add("session_started", wk("2026-W38").Add(time.Hour), 50, nil)
	add("session_started", wk("2026-W40").Add(time.Hour), 50, nil)
	w43 := wk("2026-W43")
	add("session_started", w43.Add(-time.Hour), 51, nil) // Sunday 23:00 of W42
	add("screen_viewed", w43.Add(time.Hour), 51, overview)
	add("screen_viewed", w43.Add(time.Hour+time.Minute), 51, worst)
	for n := 1; n <= users; n++ {
		t0 := w43.Add(time.Duration(n) * time.Hour)
		step := t0.Add(time.Duration(2*n) * time.Minute)
		add("session_started", t0, n, map[string]any{"entry_screen": "monitor", "viewport": "desktop"})
		if n != 10 && n <= 11 {
			add("screen_viewed", t0.Add(time.Minute), n, overview)
		}
		switch {
		case n <= 3:
			add("screen_viewed", step, n, worst)
		case n <= 5:
			add("assistant_open", step, n, map[string]any{"entry": "why"})
		case n == 6:
			add("details_trade_expand", step, n, nil)
		case n == 7:
			add("tip_opened", step, n, map[string]any{"tip_id": "tip.gamma_flip", "screen": "overview"})
		case n == 8:
			add("assistant_ask", step, n, map[string]any{"entry": "menu"})
		case n == 10:
			add("tip_opened", step, n, map[string]any{"tip_id": "tip.gamma_flip", "screen": "positions"})
		case n == 11:
			add("screen_viewed", t0.Add(25*time.Hour), n, worst)
		}
	}
	return lines
}

func activationReport(t *testing.T, lines []string) usage.Report {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.Activation == nil || len(r.Activation.Weeks) != usage.ActivationWeeks {
		t.Fatalf("activation = %+v, want %d weeks", r.Activation, usage.ActivationWeeks)
	}
	return r
}

func activationWeek(t *testing.T, r usage.Report, week string) usage.ActivationWeek {
	t.Helper()
	for _, w := range r.Activation.Weeks {
		if w.Week == week {
			return w
		}
	}
	t.Fatalf("no activation week %s", week)
	return usage.ActivationWeek{}
}

func TestUsageActivation_ScenarioANewPersonReachesTheAnswersInTheFirstSession(t *testing.T) {
	r := activationReport(t, activationFixture(t, 12))
	w := activationWeek(t, r, "2026-W43")
	if w.Users != 12 || w.Overview != 10 || w.Activated != 8 {
		t.Fatalf("W43 = %d new, %d Overview, %d activated; want 12, 10, 8", w.Users, w.Overview, w.Activated)
	}
	if w.Pct == nil || int(*w.Pct+0.5) != 67 || w.Note != "" {
		t.Errorf("share = %v note %q, want 67 %%", w.Pct, w.Note)
	}
	// Minutes 2, 4, … 16 from the first start: the median of 8 is 9.
	if w.MedianMinutes == nil || *w.MedianMinutes != 9 {
		t.Errorf("median = %v, want 9 min", w.MedianMinutes)
	}
	want := map[string]int{usage.StepWorstCase: 3, usage.StepTradeReason: 3, usage.StepHelp: 2}
	for k, n := range want {
		if w.ByStep[k] != n {
			t.Errorf("by step %s = %d, want %d", k, w.ByStep[k], n)
		}
	}
	// Person 51 started in W42: counted there, not here.
	if w42 := activationWeek(t, r, "2026-W42"); w42.Users != 1 || w42.Activated != 1 {
		t.Errorf("W42 = %+v, want person 51 activated there", w42)
	}

	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Activation in the first day (Q2, Q4)",
		"| Week | New people | Opened the Overview | Activated | Share | Median minutes | worst_case | trade_reason | help |",
		"| 2026-W43 | 12 | 10 | 8 | 67 % | 9.0 | 3 | 3 | 2 |"} {
		if !strings.Contains(md.String(), s) {
			t.Errorf("markdown lacks %q:\n%s", s, md.String())
		}
	}
	if got := w.Line(); got != "activation 2026-W43: 8 of 12 new people activated (67 %), median 9.0 min to the first answer" {
		t.Errorf("cmd/usage line = %q", got)
	}
}

func TestUsageActivation_ScenarioTooFewNewPeople(t *testing.T) {
	r := activationReport(t, activationFixture(t, 6))
	w := activationWeek(t, r, "2026-W43")
	if w.Users != 6 || w.Activated != 6 || w.Note != usage.TooFew || w.Pct != nil || w.MedianMinutes != nil {
		t.Fatalf("W43 = %+v, want 6 of 6, too few to judge, no share or median", w)
	}
	if !strings.Contains(w.Line(), "too few to judge") {
		t.Errorf("line = %q", w.Line())
	}
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "| 2026-W43 | 6 | 6 | 6 | — (too few to judge: under 10 new people) | — |") {
		t.Errorf("markdown lacks the too-few row:\n%s", md.String())
	}
	// No alert: the drop check watches its own metrics only.
	for _, d := range r.Drops.Drops {
		if strings.HasPrefix(d.Metric, "activation") {
			t.Errorf("drop raised on activation: %+v", d)
		}
	}
	for _, s := range r.Drops.Skipped {
		if strings.HasPrefix(s.Metric, "activation") {
			t.Errorf("drop check judged activation: %+v", s)
		}
	}
}

func TestUsageActivation_ScenarioConsentRespected(t *testing.T) {
	// A person who has not said yes sends nothing (the collector answers 403,
	// event-plan.md §5), so the file only holds lines that are not theirs; a
	// line that fails the plan (here a malformed user id) is rejected, never
	// counted, even when it claims a first session and every step.
	lines := activationFixture(t, 12)
	w43 := usageWeek(t, "2026-W43").From
	for _, ev := range []string{"session_started", "screen_viewed"} {
		line := usageLine(t, ev, w43.Add(30*time.Hour), 77, map[string]any{"screen": "overview"})
		uid, _ := usageUser(77)
		lines = append(lines, strings.Replace(line, uid, "not-a-pseudonym", 1))
	}
	r := activationReport(t, lines)
	if w := activationWeek(t, r, "2026-W43"); w.Users != 12 || w.Activated != 8 {
		t.Errorf("W43 = %d of %d, want only the 12 consented people's lines (8 of 12)", w.Activated, w.Users)
	}
	if r.Source.RejectedTotal != 2 {
		t.Errorf("rejected = %d, want the 2 lines outside the plan", r.Source.RejectedTotal)
	}
}

func TestUsageActivation_ScenarioAdminPageWeeksWithoutEventsAreNotZero(t *testing.T) {
	r := activationReport(t, activationFixture(t, 12))
	weeks := r.Activation.Weeks
	if weeks[0].Week != "2026-W32" || weeks[len(weeks)-1].Week != "2026-W43" {
		t.Fatalf("weeks %s … %s, want the 12 weeks 2026-W32 … W43", weeks[0].Week, weeks[len(weeks)-1].Week)
	}
	noEvents := map[string]bool{}
	for _, w := range weeks {
		if w.NoEvents {
			noEvents[w.Week] = true
			if w.Pct != nil || w.Users != 0 {
				t.Errorf("%s has no events but %+v", w.Week, w)
			}
		}
	}
	for _, wk := range []string{"2026-W32", "2026-W37", "2026-W39", "2026-W41"} {
		if !noEvents[wk] {
			t.Errorf("%s should be noted as having no events", wk)
		}
	}
	// W40 has an event (a return visit) and no new person: a real 0, not a missing week.
	if w40 := activationWeek(t, r, "2026-W40"); w40.NoEvents || w40.Users != 0 || w40.Note != usage.TooFew {
		t.Errorf("W40 = %+v, want events, 0 new people, too few", w40)
	}
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	text := md.String()
	if !strings.Contains(text, "Weeks with no events, left out (not zero): 2026-W32, 2026-W33") || strings.Contains(text, "| 2026-W41 |") {
		t.Errorf("markdown should list the weeks with no events apart:\n%s", text)
	}
	if !strings.Contains(activationWeek(t, r, "2026-W41").Line(), "no events in the week") {
		t.Errorf("W41 line = %q", activationWeek(t, r, "2026-W41").Line())
	}
}
