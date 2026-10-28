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
		// Each band's own list (#446): what works for 9–10, what to improve for 0–8.
		code := "hard_to_find"
		if sc >= 9 {
			code = "steady_income"
		}
		lines = append(lines,
			usageLine(t, "survey_shown", ts, 200+i, map[string]any{"survey_id": "nps"}),
			usageLine(t, "survey_answered", ts.Add(time.Second), 200+i, map[string]any{"survey_id": "nps", "score": strconv.Itoa(sc), "reason_code": code}),
		)
	}
	return lines
}

func npsAnswer(t *testing.T, ts time.Time, user, score int, code string) string {
	return usageLine(t, "survey_answered", ts, user, map[string]any{"survey_id": "nps", "score": strconv.Itoa(score), "reason_code": code})
}

// Scenario (#446): report counts both lists — answers 10 explains_trades,
// 9 clear_risk, 5 hard_to_find → why_promoters has 2 and why_others has 1.
func TestUsage_SurveyReportCountsBothNPSLists(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	r, written := surveyReport(t, []string{
		npsAnswer(t, at, 501, 10, "explains_trades"),
		npsAnswer(t, at.Add(time.Minute), 502, 9, "clear_risk"),
		npsAnswer(t, at.Add(2*time.Minute), 503, 5, "hard_to_find"),
	})
	s := r.Survey
	if sum(s.WhyPromoters) != 2 || s.WhyPromoters["explains_trades"] != 1 || s.WhyPromoters["clear_risk"] != 1 {
		t.Fatalf("why_promoters = %v", s.WhyPromoters)
	}
	if sum(s.WhyOthers) != 1 || s.WhyOthers["hard_to_find"] != 1 {
		t.Fatalf("why_others = %v", s.WhyOthers)
	}
	// why stays their sum, for old readers.
	if sum(s.Why) != 3 || s.Why["explains_trades"] != 1 || s.Why["clear_risk"] != 1 || s.Why["hard_to_find"] != 1 {
		t.Fatalf("why = %v", s.Why)
	}
	if s.Miscoded != 0 {
		t.Fatalf("miscoded = %d", s.Miscoded)
	}
	for _, want := range []string{"| NPS what we do best (9–10, why_promoters) | Count |", "| explains_trades | 1 |",
		"| NPS what to improve first (0–8, why_others) | Count |", "| hard_to_find | 1 |"} {
		if !strings.Contains(written, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, written)
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"why":`, `"why_promoters":`, `"why_others":`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("json lacks %s: %s", key, b)
		}
	}
}

// A code from the other band's list is counted under other, never guessed.
func TestUsage_SurveyNPSCodeOfTheOtherListCountsUnderOther(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	r, _ := surveyReport(t, []string{
		npsAnswer(t, at, 511, 10, "hard_to_find"),                  // a to-improve code on a promoter
		npsAnswer(t, at.Add(time.Minute), 512, 3, "steady_income"), // a what-works code on a detractor
	})
	s := r.Survey
	if s.WhyPromoters["other"] != 1 || s.WhyOthers["other"] != 1 || s.Miscoded != 2 || s.Why["other"] != 2 {
		t.Fatalf("promoters %v, others %v, why %v, miscoded %d", s.WhyPromoters, s.WhyOthers, s.Why, s.Miscoded)
	}
}

// The score counts each person's latest answer in the window: one person
// answering twice weighs once, by their last answer.
func TestUsage_SurveyNPSCountsEachPersonsLatestAnswer(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	r, _ := surveyReport(t, []string{
		npsAnswer(t, at.Add(time.Hour), 521, 10, "saves_time"), // latest, though written first
		npsAnswer(t, at, 521, 4, "price"),
		npsAnswer(t, at, 522, 6, "trust"),
	})
	s := r.Survey
	n := s.NPS
	if n.Answers != 2 || n.Promoters != 1 || n.Detractors != 1 {
		t.Fatalf("nps = %+v", n)
	}
	if s.WhyPromoters["saves_time"] != 1 || s.WhyOthers["price"] != 0 || s.WhyOthers["trust"] != 1 {
		t.Fatalf("promoters %v, others %v", s.WhyPromoters, s.WhyOthers)
	}
	// The funnel still counts every answer sent.
	if f := s.Surveys[0]; f.ID != "nps" || f.Answered != 3 {
		t.Fatalf("nps funnel = %+v", f)
	}
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
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
	if s.Why["steady_income"] != 7 || s.Why["hard_to_find"] != 5 || s.WhyPromoters["steady_income"] != 7 || s.WhyOthers["hard_to_find"] != 5 {
		t.Fatalf("why = %v, promoters %v, others %v", s.Why, s.WhyPromoters, s.WhyOthers)
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
