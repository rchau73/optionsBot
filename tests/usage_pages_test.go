package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Per-page views and the assistant panel in the weekly usage report (#424).

// Scenario: the assistant answered "I don't know" → the weekly report counts
// it under dont_know and holds no question text.
func TestUsage_AssistantDontKnowCountedWithoutText(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	at := w.From.Add(time.Hour)
	const question = "When does the bot buy a leg back?"
	lines := []string{
		usageLine(t, "assistant_open", at, 1, map[string]any{"entry": "menu"}),
		usageLine(t, "assistant_ask", at.Add(time.Second), 1, map[string]any{"entry": "menu"}),
		usageLine(t, "assistant_answered", at.Add(2*time.Second), 1, map[string]any{"answer": "dont_know"}),
		usageLine(t, "assistant_open", at.Add(time.Minute), 2, map[string]any{"entry": "why"}),
		usageLine(t, "assistant_ask", at.Add(time.Minute+time.Second), 2, map[string]any{"entry": "why"}),
		usageLine(t, "assistant_answered", at.Add(time.Minute+2*time.Second), 2, map[string]any{"answer": "answered"}),
		usageLine(t, "assistant_flag", at.Add(2*time.Minute), 2, nil),
		// A line with the question's text is outside the plan: rejected, never in the report.
		usageLine(t, "assistant_ask", at.Add(3*time.Minute), 3, map[string]any{"entry": "menu", "question": question}),
		usageLine(t, "assistant_answered", at.Add(3*time.Minute), 3, map[string]any{"answer": "I don't know."}),
	}
	path := writeUsageEvents(t, lines)
	r, paths, err := usage.Run(path, w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := r.Assistant
	if a == nil {
		t.Fatal("no assistant section")
	}
	if a.Answers["dont_know"] != 1 || a.Answers["answered"] != 1 || a.Answers["refused"] != 0 || a.Answers["fallback"] != 0 || a.Replies != 2 {
		t.Fatalf("answers = %v, replies %d", a.Answers, a.Replies)
	}
	if a.AnsweredPct == nil || *a.AnsweredPct != 50 {
		t.Fatalf("answered pct = %v", a.AnsweredPct)
	}
	if a.Opened["menu"] != 1 || a.Opened["why"] != 1 || a.Asked["menu"] != 1 || a.Asked["why"] != 1 || a.Flagged != 1 {
		t.Fatalf("assistant = %+v", a)
	}
	if r.Source.RejectedTotal != 2 {
		t.Fatalf("rejected = %v, want the two lines with text", r.Source.Rejected)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, bad := range []string{question, "I don't know."} {
			if strings.Contains(s, bad) {
				t.Fatalf("%s holds %q", p, bad)
			}
		}
		if strings.HasSuffix(p, ".md") {
			for _, want := range []string{"## Assistant (Q5)", "| dont_know | 1 |", "| answered | 1 |", "answered with a citation: 50 %", "flagged as wrong: 1"} {
				if !strings.Contains(s, want) {
					t.Fatalf("markdown lacks %q:\n%s", want, s)
				}
			}
		}
	}
}

// Every page of the menu is counted per screen; the old single "monitor"
// value is refused.
func TestUsage_ScreenViewsPerPage(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	at := w.From.Add(time.Hour)
	var lines []string
	for i, s := range usage.Screens {
		lines = append(lines, usageLine(t, "screen_viewed", at.Add(time.Duration(i)*time.Minute), 1, map[string]any{"screen": s}))
	}
	lines = append(lines,
		usageLine(t, "screen_viewed", at.Add(time.Hour), 2, map[string]any{"screen": "income"}),
		usageLine(t, "screen_viewed", at.Add(time.Hour), 3, map[string]any{"screen": "monitor"}),
	)
	r, paths, err := usage.Run(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range usage.Screens {
		want := 1
		if s == "income" {
			want = 2
		}
		if r.Panels.Screens[s] != want {
			t.Errorf("screen %s = %d, want %d", s, r.Panels.Screens[s], want)
		}
	}
	if r.Source.Rejected[usage.RejectValue] != 1 {
		t.Fatalf("rejected = %v, want screen=monitor refused", r.Source.Rejected)
	}
	md, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "| worst_case | 1 |") || !strings.Contains(string(md), "| income | 2 |") {
		t.Fatalf("markdown lacks per-page views:\n%s", md)
	}
}
