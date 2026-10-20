package tests

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The E5 discovery kit (docs/product/discovery/, #178) is what a person uses to
// run the cycle-holder survey: a short written form, no calls and no
// recordings (the product owner on #178). These tests keep its contract true
// as the files are edited: the form stays short, every question asks about
// past behaviour, the synthesis measures the PRD §6 kill criterion, and
// nothing in the kit invites personal data into the public repository.

const discoveryDir = "../docs/product/discovery/"

var discoveryFiles = []string{"screener.md", "survey.md", "consent.md", "synthesis.md"}

func readDiscovery(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(discoveryDir + name)
	if err != nil {
		t.Fatalf("discovery kit file missing: %v", err)
	}
	return string(b)
}

// Scenario: ready to run — the four files exist and each says answers and
// contacts stay in the gitignored data/product/interviews/.
func TestDiscoveryKit_ReadyToRun_FilesExistAndPointToPrivateNotes(t *testing.T) {
	ignore, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^/data/$`).Match(ignore) {
		t.Fatal(".gitignore no longer ignores /data/: survey answers would be committable")
	}
	for _, name := range discoveryFiles {
		doc := readDiscovery(t, name)
		if !strings.Contains(doc, "`data/product/interviews/`") || !strings.Contains(doc, "gitignored") {
			t.Errorf("%s does not say answers and contacts go only in the gitignored data/product/interviews/", name)
		}
	}
}

// Public-safe: no e-mail address and no money amount in any kit file.
func TestDiscoveryKit_PublicSafe(t *testing.T) {
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	money := regexp.MustCompile(`(?i)[$€£]\s?\d|\d\s?(usd|usdc|usdt|brl|eur|btc|eth)\b`)
	for _, name := range discoveryFiles {
		doc := readDiscovery(t, name)
		if m := email.FindString(doc); m != "" {
			t.Errorf("%s contains an e-mail address %q", name, m)
		}
		if m := money.FindString(doc); m != "" {
			t.Errorf("%s contains an amount %q", name, m)
		}
	}
}

// The product owner on #178: no recorded interviews, a simple written form.
// Apart from saying "no calls" / "no recordings", the kit never mentions
// recording, audio, video or a call.
func TestDiscoveryKit_NoCallsNoRecordings(t *testing.T) {
	allowed := regexp.MustCompile(`(?i)\b(no calls|no recordings|covered calls)\b`)
	banned := regexp.MustCompile(`(?i)\b(record|recording|recordings|recorded|audio|video|call|calls|interviewer)\b`)
	for _, name := range append(discoveryFiles, "interview-guide.md") {
		doc := allowed.ReplaceAllString(readDiscovery(t, name), "")
		if m := banned.FindString(doc); m != "" {
			t.Errorf("%s mentions %q: the kit is a written survey, no calls and no recordings", name, m)
		}
	}
}

// Scenario: ready to run — the form is short (≤ 7 minutes, ≤ 20 items, ≤ 2
// free-text answers) and ends with the fake door.
func TestDiscoveryKit_ReadyToRun_SurveyIsShortAndEndsWithFakeDoor(t *testing.T) {
	survey := readDiscovery(t, "survey.md")
	rows := planTable(t, survey, "2")
	total := 0
	for _, r := range rows {
		if len(r) < 2 {
			t.Fatalf("outline row %v has no minutes", r)
		}
		m, err := strconv.Atoi(r[1])
		if err != nil || m <= 0 {
			t.Fatalf("outline row %q: minutes %q is not a positive number", r[0], r[1])
		}
		total += m
	}
	if total > 7 {
		t.Errorf("the form's parts add up to %d minutes, want at most 7", total)
	}
	if last := rows[len(rows)-1][0]; !strings.Contains(strings.ToLower(last), "fake door") {
		t.Errorf("the last part must be the fake door, got %q", last)
	}

	questions := planTable(t, survey, "3")
	screener := planTable(t, readDiscovery(t, "screener.md"), "2")
	const extra = 3 // N1, F1, F2
	if items := len(screener) + len(questions) + extra; items > 20 {
		t.Errorf("the form has %d items, want at most 20", items)
	}
	freeText := 1 // F2
	for _, r := range questions {
		if len(r) >= 4 && strings.Contains(r[3], "free text") {
			freeText++
		}
	}
	if freeText > 2 {
		t.Errorf("the form has %d free-text answers, want at most 2", freeText)
	}
	for _, want := range []string{"N1.", "0 = not at all likely, 10 = extremely likely", "**F1.**", "**F2.**"} {
		if !strings.Contains(survey, want) {
			t.Errorf("survey.md does not contain %q", want)
		}
	}
}

// Scenario: ready to run — every question asks about past behaviour, and none
// leads the participant or pitches the product.
func TestDiscoveryKit_ReadyToRun_EveryQuestionAsksAboutPastBehaviour(t *testing.T) {
	past := regexp.MustCompile(`(?i)\b(last time|did you|have you|happened|walk me through|tell me about|when did)\b`)
	leading := regexp.MustCompile(`(?i)\b(would|wouldn't|will you|do you think|don't you|imagine|if there (was|were)|ideally|agree|our|we|us|optionsbot)\b`)
	rows := planTable(t, readDiscovery(t, "survey.md"), "3")
	if len(rows) < 6 {
		t.Fatalf("the survey has %d questions, want at least 6", len(rows))
	}
	for _, r := range rows {
		if len(r) < 3 {
			t.Fatalf("question row %v has no text", r)
		}
		id, q := r[0], r[2]
		if !past.MatchString(q) {
			t.Errorf("%s does not ask about past behaviour: %q", id, q)
		}
		if m := leading.FindString(q); m != "" {
			t.Errorf("%s leads or pitches (%q): %q", id, m, q)
		}
	}
}

// Scenario: ready to run — the synthesis template measures the kill criterion
// of PRD §6 on cycle holders, from the fake-door outcome.
func TestDiscoveryKit_ReadyToRun_SynthesisMeasuresKillCriterion(t *testing.T) {
	doc := readDiscovery(t, "synthesis.md")
	prd, err := os.ReadFile("../docs/product/PRD.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prd), "<30 % of interviewed cycle holders would let software trade their account") {
		t.Fatal("PRD §6 kill criterion changed: update synthesis.md and this test together")
	}
	rows := planTable(t, doc, "4")
	measures := map[string]bool{}
	for _, r := range rows {
		measures[r[0]] = true
	}
	for _, m := range []string{"N", "Joined", "Share"} {
		if !measures[m] {
			t.Errorf("kill-criterion table has no %q measure", m)
		}
	}
	for _, want := range []string{"Joined ÷ N", "Share < 30 %", "N < 10", "cycle holders only", "Undecided counts as not joined", "NPS = % of scores 9–10 − % of scores 0–6"} {
		if !strings.Contains(doc, want) {
			t.Errorf("synthesis.md does not state %q", want)
		}
	}
}

func TestDiscoveryKit_ScreenerQuotasAndConsentFlag(t *testing.T) {
	screener := readDiscovery(t, "screener.md")
	for _, want := range []string{"| Cycle holders | **≥ 10**", "| Active option traders | **≤ 5**"} {
		if !strings.Contains(screener, want) {
			t.Errorf("screener.md quota missing: %q", want)
		}
	}
	consent := readDiscovery(t, "consent.md")
	for _, want := range []string{"needs legal review", "LGPD", "GDPR"} {
		if !strings.Contains(consent, want) {
			t.Errorf("consent.md does not mention %q", want)
		}
	}
}
