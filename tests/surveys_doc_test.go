package tests

import (
	"os"
	"strings"
	"testing"
)

// The in-product survey content (docs/product/analytics/surveys.md, #251,
// #297) is the contract for the survey UI: these tests keep its promises
// written down and its stored fields inside the event plan's allow-list.

const surveysPath = "../docs/product/analytics/surveys.md"

func readSurveys(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(surveysPath)
	if err != nil {
		t.Fatalf("read surveys: %v", err)
	}
	return string(raw)
}

// docSection returns the text from the "## <n>." heading to the next "## ".
func docSection(t *testing.T, doc, n string) string {
	t.Helper()
	start := strings.Index(doc, "\n## "+n+".")
	if start < 0 {
		t.Fatalf("surveys.md has no section %q", n)
	}
	body := doc[start+1:]
	if next := strings.Index(body[3:], "\n## "); next >= 0 {
		body = body[:next+3]
	}
	return body
}

// Scenario: NPS, why questions and exit survey each have their wording,
// trigger, frequency cap and stored fields.
func TestSurveys_EachHasWordingTriggerCapAndFields(t *testing.T) {
	doc := readSurveys(t)
	for _, s := range []struct{ section, name, cap string }{
		{"1", "NPS", "once per 90 days"},
		{"2", "four answers", "once per 30 days"},
		{"3", "exit", "once per leaving"},
	} {
		body := docSection(t, doc, s.section)
		for _, want := range []string{"**Wording**", "| PT-BR | EN |", "Stored |", "**Events:**", s.cap} {
			if !strings.Contains(body, want) {
				t.Errorf("%s survey (§%s) does not contain %q", s.name, s.section, want)
			}
		}
		if !strings.Contains(body, "**Trigger and frequency cap:**") && !(strings.Contains(body, "**When:**") && strings.Contains(body, "**Frequency cap:**")) {
			t.Errorf("%s survey (§%s) has no trigger and frequency cap", s.name, s.section)
		}
		if rows := planTable(t, doc, s.section); len(rows) < 2 {
			t.Errorf("%s survey has %d questions, want at least 2", s.name, len(rows))
		}
	}
	nps := docSection(t, doc, "1")
	for _, want := range []string{"Never in the first 14 days", "≥ 3 sessions", "loss-making week", "Always dismissible"} {
		if !strings.Contains(nps, want) {
			t.Errorf("NPS trigger does not say %q", want)
		}
	}
}

// Scenario: the exit survey is skippable in one click and never blocks leaving.
func TestSurveys_ExitIsSkippableInOneClick(t *testing.T) {
	exit := docSection(t, readSurveys(t), "3")
	for _, want := range []string{"**Skipping it is one click, and it never delays or blocks leaving:**", "works with the question unanswered", "even if the survey answer cannot be stored"} {
		if !strings.Contains(exit, want) {
			t.Errorf("exit survey does not say %q", want)
		}
	}
}

// Scenario: every stored field, checked against the event plan, holds no
// amount, no secret and no free text in an analytics event.
func TestSurveys_StoredFieldsAreInTheAllowList(t *testing.T) {
	doc := readSurveys(t)
	p := loadEventPlan(t)
	rows := planTable(t, doc, "5")
	if len(rows) != 3 {
		t.Fatalf("§5 lists %d survey events, want 3", len(rows))
	}
	for _, r := range rows {
		ev := codeNames(r[0])
		if len(ev) != 1 {
			t.Fatalf("§5 row %v: want one event name", r)
		}
		planProps, ok := p.events[ev[0]]
		if !ok {
			t.Errorf("survey event %q is not in the event plan §2", ev[0])
			continue
		}
		if got := strings.Join(codeNames(r[1]), ","); got != strings.Join(planProps, ",") {
			t.Errorf("%s: surveys.md lists %s, the plan %v", ev[0], got, planProps)
		}
		for _, prop := range codeNames(r[1]) {
			if p.props[prop] != "enum" {
				t.Errorf("%s.%s has type %q in the plan: a survey field must be an enum (no amount, no free text)", ev[0], prop, p.props[prop])
			}
			if w := deniedWord(prop); w != "" {
				t.Errorf("%s.%s contains %q", ev[0], prop, w)
			}
		}
	}
	// Every reason code of §4 is a value of the plan's reason_code enum.
	plan, err := os.ReadFile(eventPlanPath)
	if err != nil {
		t.Fatal(err)
	}
	var enumCell string
	for _, r := range planTable(t, string(plan), "4") {
		if names := codeNames(r[0]); len(names) == 1 && names[0] == "reason_code" {
			enumCell = r[2]
		}
	}
	for _, r := range planTable(t, doc, "4") {
		code := codeNames(r[0])
		if len(code) != 1 || !strings.Contains(enumCell, "`"+code[0]+"`") {
			t.Errorf("reason code %v is not in the plan's reason_code enum", r[0])
		}
	}
}

// Scenario: the data-protection section is marked needs legal review with the
// questions for counsel.
func TestSurveys_DataProtectionNeedsLegalReview(t *testing.T) {
	dp := docSection(t, readSurveys(t), "6")
	for _, want := range []string{"needs legal review", "**Consent text**", "**Retention and deletion**", "**Questions for counsel:**", "No survey ships before"} {
		if !strings.Contains(dp, want) {
			t.Errorf("§6 does not contain %q", want)
		}
	}
	questions := 0
	for _, line := range strings.Split(dp, "\n") {
		if strings.HasPrefix(line, "- **C") {
			questions++
		}
	}
	if questions < 3 {
		t.Errorf("§6 lists %d questions for counsel, want at least 3", questions)
	}
}
