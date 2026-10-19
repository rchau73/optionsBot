package tests

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The E5 discovery kit (docs/product/discovery/, #178) is what a person uses to
// run the cycle-holder interviews. These tests keep its contract true as the
// files are edited: the guide fits 45 minutes, every question asks about past
// behaviour, the synthesis measures the PRD §6 kill criterion, and nothing in
// the kit invites personal data into the public repository.

const discoveryDir = "../docs/product/discovery/"

var discoveryFiles = []string{"screener.md", "interview-guide.md", "consent.md", "synthesis.md"}

func readDiscovery(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(discoveryDir + name)
	if err != nil {
		t.Fatalf("discovery kit file missing: %v", err)
	}
	return string(b)
}

// Scenario: ready to run — the four files exist and each says raw notes and
// contacts stay in the gitignored data/product/interviews/.
func TestDiscoveryKit_ReadyToRun_FilesExistAndPointToPrivateNotes(t *testing.T) {
	ignore, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^/data/$`).Match(ignore) {
		t.Fatal(".gitignore no longer ignores /data/: interview notes would be committable")
	}
	for _, name := range discoveryFiles {
		doc := readDiscovery(t, name)
		if !strings.Contains(doc, "`data/product/interviews/`") || !strings.Contains(doc, "gitignored") {
			t.Errorf("%s does not say raw notes and contacts go only in the gitignored data/product/interviews/", name)
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

// Scenario: ready to run — the guide fits 45 minutes and ends with the fake door.
func TestDiscoveryKit_ReadyToRun_GuideFits45MinutesAndEndsWithFakeDoor(t *testing.T) {
	rows := planTable(t, readDiscovery(t, "interview-guide.md"), "2")
	total := 0
	for _, r := range rows {
		if len(r) < 2 {
			t.Fatalf("timing row %v has no minutes", r)
		}
		m, err := strconv.Atoi(r[1])
		if err != nil || m <= 0 {
			t.Fatalf("timing row %q: minutes %q is not a positive number", r[0], r[1])
		}
		total += m
	}
	if total != 45 {
		t.Errorf("the guide's parts add up to %d minutes, want 45", total)
	}
	if len(rows) < 2 || !strings.Contains(strings.ToLower(rows[len(rows)-2][0]), "fake door") {
		t.Errorf("the part before the close must be the fake door, got %q", rows[len(rows)-2][0])
	}
}

// Scenario: ready to run — every question asks about past behaviour, and none
// leads the participant or pitches the product.
func TestDiscoveryKit_ReadyToRun_EveryQuestionAsksAboutPastBehaviour(t *testing.T) {
	past := regexp.MustCompile(`(?i)\b(last time|did you|have you|happened|walk me through|tell me about|when did)\b`)
	leading := regexp.MustCompile(`(?i)\b(would|wouldn't|will you|do you think|don't you|imagine|if there (was|were)|ideally|agree|our|we|us|optionsbot)\b`)
	rows := planTable(t, readDiscovery(t, "interview-guide.md"), "3")
	if len(rows) < 10 {
		t.Fatalf("the guide has %d questions, want at least 10 for 45 minutes", len(rows))
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
	for _, want := range []string{"Joined ÷ N", "Share < 30 %", "N < 10", "cycle holders only", "Undecided counts as not joined"} {
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
