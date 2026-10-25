package tests

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// The survey tally of the usage report (#342): coded answers of #297's
// survey events, counts only.

func surveyReport(t *testing.T, lines []string) (usage.Report, string) {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	path := writeUsageEvents(t, lines)
	r, paths, err := usage.Run(path, w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var written string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		written += string(b)
	}
	return r, written
}

func npsAnswers(t *testing.T, at time.Time, scores ...int) []string {
	var lines []string
	for i, sc := range scores {
		ts := at.Add(time.Duration(i) * time.Minute)
		lines = append(lines,
			usageLine(t, "survey_shown", ts, 200+i, map[string]any{"survey_id": "nps"}),
			usageLine(t, "survey_answered", ts.Add(time.Second), 200+i, map[string]any{"survey_id": "nps", "score": strconv.Itoa(sc), "reason_code": "steady_income"}),
		)
	}
	return lines
}

// Scenario: 12 NPS events with scores 9, 10, 10, 8, 7, 6, 3, 9, 10, 9, 8, 10
// → 12 answers and the NPS computed from them.
func TestUsage_SurveyNPSFromTwelveAnswers(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	r, written := surveyReport(t, npsAnswers(t, at, 9, 10, 10, 8, 7, 6, 3, 9, 10, 9, 8, 10))
	s := r.Survey
	if s == nil {
		t.Fatal("no survey section")
	}
	n := s.NPS
	// promoters 7 (9, 10), passives 3 (7, 8), detractors 2 (6, 3): (7 − 2) / 12 = 41.67.
	if n.Answers != 12 || n.Promoters != 7 || n.Passives != 3 || n.Detractors != 2 || n.TooFew {
		t.Fatalf("nps = %+v", n)
	}
	if n.Score == nil || math.Abs(*n.Score-500.0/12) > 1e-9 {
		t.Fatalf("nps score = %v, want 41.67", n.Score)
	}
	if s.Why["steady_income"] != 12 {
		t.Fatalf("why = %v", s.Why)
	}
	if f := s.Surveys[0]; f.ID != "nps" || f.Shown != 12 || f.Answered != 12 || f.ResponsePct == nil || *f.ResponsePct != 100 {
		t.Fatalf("nps funnel = %+v", f)
	}
	if !strings.Contains(written, "Why they stay or leave") || !strings.Contains(written, "NPS: 42 ") {
		t.Fatalf("markdown lacks the NPS line:\n%s", written)
	}
}

// Scenario: fewer than 10 NPS answers → "too few answers", counts only.
func TestUsage_SurveyNPSTooFewAnswers(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	r, written := surveyReport(t, npsAnswers(t, at, 10, 9, 2))
	n := r.Survey.NPS
	if n.Answers != 3 || n.Promoters != 2 || n.Detractors != 1 || n.Score != nil || !n.TooFew {
		t.Fatalf("nps = %+v", n)
	}
	if !strings.Contains(written, "NPS: too few answers") {
		t.Fatalf("markdown:\n%s", written)
	}
}

// Scenario: exit reasons too_complex ×4 and price ×1 → listed most frequent first.
func TestUsage_SurveyExitReasonsMostFrequentFirst(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	var lines []string
	for i, code := range []string{"too_complex", "price", "too_complex", "too_complex", "too_complex"} {
		lines = append(lines, usageLine(t, "survey_answered", at.Add(time.Duration(i)*time.Minute), 300+i,
			map[string]any{"survey_id": "exit", "reason_code": code}))
	}
	// A promoter code on an exit answer counts under other, never guessed.
	lines = append(lines, usageLine(t, "survey_answered", at.Add(time.Hour), 310, map[string]any{"survey_id": "exit", "reason_code": "saves_time"}))
	r, written := surveyReport(t, lines)
	s := r.Survey
	want := []usage.CodeCount{{Code: "too_complex", Count: 4}, {Code: "other", Count: 1}, {Code: "price", Count: 1}}
	if len(s.TopExit) != len(want) {
		t.Fatalf("top exit = %+v", s.TopExit)
	}
	for i := range want {
		if s.TopExit[i] != want[i] {
			t.Fatalf("top exit = %+v, want %+v", s.TopExit, want)
		}
	}
	if s.Miscoded != 1 || s.ExitReasons["saves_time"] != 0 {
		t.Fatalf("miscoded = %d, exit = %v", s.Miscoded, s.ExitReasons)
	}
	first, third := strings.Index(written, "1. too_complex — 4"), strings.Index(written, "3. price — 1")
	if first < 0 || third < first {
		t.Fatalf("markdown order:\n%s", written)
	}
	if !strings.Contains(written, usage.OwnerLine) {
		t.Fatal("owner line missing")
	}
	// No NPS answer: no score.
	if !s.NPS.TooFew || s.NPS.Answers != 0 {
		t.Fatalf("nps = %+v", s.NPS)
	}
}

// Scenario: a field not on the allow-list → ignored, appears nowhere.
func TestUsage_SurveyFieldOutsideTheAllowListAppearsNowhere(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	lines := npsAnswers(t, at, 9)
	lines = append(lines, usageLine(t, "survey_answered", at.Add(time.Hour), 400,
		map[string]any{"survey_id": "exit", "reason_code": "other", "comment": "secret-free-text-xyz"}))
	r, written := surveyReport(t, lines)
	if strings.Contains(written, "secret-free-text-xyz") || strings.Contains(written, `"comment"`) {
		t.Fatal("a field outside the allow-list reached the report")
	}
	if r.Source.Rejected[usage.RejectProperty] != 1 || r.Survey.ExitReasons["other"] != 0 {
		t.Fatalf("rejected = %v, exit = %v", r.Source.Rejected, r.Survey.ExitReasons)
	}
}

// Scenario: no survey events → the section says no answers yet and every
// other section is identical to before.
func TestUsage_SurveyNoAnswersYetLeavesTheRestUnchanged(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	base := cohortFixture(t)
	r, written := surveyReport(t, base)
	if r.Survey != nil {
		t.Fatalf("survey = %+v", r.Survey)
	}
	if strings.Contains(written, `"survey"`) || !strings.Contains(written, "Survey: "+usage.NoSurveyAnswers) {
		t.Fatalf("written:\n%s", written)
	}
	// The same log plus survey events from the same users: only the survey
	// section (and the event counts) differ.
	with := append([]string{}, base...)
	with = append(with,
		usageLine(t, "survey_shown", w.From.Add(26*time.Hour+2*time.Minute), 1, map[string]any{"survey_id": "answers"}),
		usageLine(t, "survey_answered", w.From.Add(26*time.Hour+3*time.Minute), 1, map[string]any{"survey_id": "answers", "came_for": "income", "hardest_to_find": "worst_case"}),
	)
	r2, _ := surveyReport(t, with)
	if r2.Survey == nil || r2.Survey.CameFor["income"] != 1 || r2.Survey.HardestToFind["worst_case"] != 1 {
		t.Fatalf("survey = %+v", r2.Survey)
	}
	strip := func(r usage.Report) string {
		r.Survey, r.Source, r.EventsInWeek = nil, usage.Source{}, 0
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if strip(r) != strip(r2) {
		t.Fatalf("other sections changed:\n%s\n%s", strip(r), strip(r2))
	}
}
