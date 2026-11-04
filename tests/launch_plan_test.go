package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The E5 launch plan (#624): docs/product/launch-plan.md holds the three
// phases of PRD §6 with their gates, metrics and rollback. These tests keep it
// to the ticket's contract as it is edited.

const launchPlanPath = "../docs/product/launch-plan.md"

// phaseSections splits the plan at its "### 5.n" headings.
func phaseSections(t *testing.T, doc string) []string {
	t.Helper()
	parts := regexp.MustCompile(`(?m)^### 5\.\d`).Split(doc, -1)
	if len(parts) != 4 {
		t.Fatalf("want 3 phase sections, got %d", len(parts)-1)
	}
	var out []string
	for i, p := range parts[1:] {
		if i == 2 {
			if end := strings.Index(p, "\n## "); end >= 0 {
				p = p[:end]
			}
		}
		out = append(out, p)
	}
	return out
}

// Scenario: every phase has gates and a rollback.
func TestLaunchPlan_EveryPhaseHasGatesAndRollback(t *testing.T) {
	for i, sec := range phaseSections(t, readDoc(t, launchPlanPath)) {
		for _, want := range []string{"**Entry gate**", "**Exit gate", "| Day 1 |", "| Week 1 |", "| Month 1 |", "**Rollback", "**Kill criterion:**"} {
			if !strings.Contains(sec, want) {
				t.Errorf("phase %d has no %q", i+1, want)
			}
		}
		for _, row := range strings.Split(sec, "\n") {
			if !regexp.MustCompile(`^\| (Day 1|Week 1|Month 1) \|`).MatchString(row) {
				continue
			}
			// When | Metric | Definition | Source | Owner | Target, plus the edges.
			if cells := strings.Split(row, "|"); len(cells) != 8 {
				t.Errorf("phase %d metric row needs 6 columns: %.60s", i+1, row)
				continue
			} else {
				for j := 2; j <= 5; j++ {
					if strings.TrimSpace(cells[j]) == "" {
						t.Errorf("phase %d metric row has an empty cell: %.60s", i+1, row)
					}
				}
			}
		}
	}
}

// Scenario: gates point to existing sources.
func TestLaunchPlan_ReferencesExist(t *testing.T) {
	doc := readDoc(t, launchPlanPath)
	dirs := []string{"../docs/product", "../docs/product/analytics", "../docs/product/assistant", "../scripts/product", ".."}
	paths := regexp.MustCompile("`([A-Za-z0-9_./-]+\\.(?:md|py|sh|jsonl))`").FindAllStringSubmatch(doc, -1)
	if len(paths) < 8 {
		t.Fatalf("expected the plan to reference its sources, found %d", len(paths))
	}
	for _, m := range paths {
		found := false
		for _, d := range dirs {
			if _, err := os.Stat(filepath.Join(d, m[1])); err == nil {
				found = true
				break
			}
		}
		if !found {
			// A path relative to docs/product subfolders (discovery/, education/, ...).
			if g, _ := filepath.Glob(filepath.Join("../docs/product/*", m[1])); len(g) > 0 {
				found = true
			}
		}
		if !found && !strings.HasPrefix(m[1], "data/") && m[1] != "pnl_history.jsonl" { // a runtime file, not in the repo
			t.Errorf("referenced file %q does not exist", m[1])
		}
	}
	for _, id := range regexp.MustCompile(`\bL(\d+)\b`).FindAllStringSubmatch(doc, -1) {
		if n := id[1]; len(n) > 2 || n == "0" || (len(n) == 2 && n > "24") {
			t.Errorf("L%s is not a legal-brief question (L1–L24)", n)
		}
	}
	for _, ev := range regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`").FindAllStringSubmatch(doc, -1) {
		if strings.Contains(ev[1], "go_no_go") || strings.Contains(ev[1], "pnl_history") {
			continue
		}
		if strings.HasPrefix(ev[1], "utilisation") || ev[1] == "survey_tally" {
			continue
		}
		plan := readDoc(t, "../docs/product/analytics/event-plan.md")
		if !strings.Contains(plan, "`"+ev[1]+"`") {
			t.Errorf("event or name %q is not in the E13 event plan", ev[1])
		}
	}
}

// Scenario: no promise of returns, no private data.
func TestLaunchPlan_NoPromiseNoPrivateData(t *testing.T) {
	doc := readDoc(t, launchPlanPath)
	for _, bad := range []string{"$", "R$", "€", "BRL", "expected return", "guaranteed", "monthly income of", "APY of"} {
		if strings.Contains(doc, bad) {
			t.Errorf("launch plan contains %q", bad)
		}
	}
	if regexp.MustCompile(`\d+(\.\d+)?\s*(USD|BTC|ETH)\b`).MatchString(doc) {
		t.Error("launch plan holds an amount")
	}
	if !strings.Contains(doc, "not a market result") || !strings.Contains(doc, "labelled") {
		t.Error("launch plan must label testnet figures and say a testnet result is not a market result")
	}
}

// Scenario: legal gates are explicit.
func TestLaunchPlan_LegalGatesExplicit(t *testing.T) {
	secs := phaseSections(t, readDoc(t, launchPlanPath))
	for i, sec := range secs[1:] { // phases with real users or a fee
		if !strings.Contains(sec, "**Needs legal review") {
			t.Errorf("phase %d has no 'Needs legal review' step", i+2)
		}
		if !regexp.MustCompile(`Needs legal review[^\n]*L\d`).MatchString(sec) {
			t.Errorf("phase %d legal step names no L-question", i+2)
		}
	}
	if !strings.Contains(secs[2], "nothing live, and no fee, ships before counsel's written answer") {
		t.Error("paid beta must let nothing ship before counsel's written answer")
	}
}

// Scenario: the kill criteria are quoted, not changed.
func TestLaunchPlan_KillCriteriaQuotedFromPRD(t *testing.T) {
	const kill = "<30 % of interviewed cycle holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis"
	if !strings.Contains(readDoc(t, "../docs/product/PRD.md"), kill) {
		t.Fatal("PRD §6 kill criteria moved: update this test and the plan together")
	}
	if !strings.Contains(readDoc(t, launchPlanPath), "> "+kill) {
		t.Error("launch plan does not quote PRD §6's kill criteria verbatim")
	}
}

// Scenario: docs rules hold.
func TestLaunchPlan_OneSentencePerLine(t *testing.T) {
	for _, path := range []string{launchPlanPath} {
		for i, line := range strings.Split(readDoc(t, path), "\n") {
			if len(line) > 300 && !strings.HasPrefix(line, "|") {
				t.Errorf("%s:%d prose line over 300 characters", path, i+1)
			}
			if strings.HasPrefix(line, "|") || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") {
				continue
			}
			if regexp.MustCompile(`[a-z)]\. [A-Z]`).MatchString(line) {
				t.Errorf("%s:%d more than one sentence on a line: %.60s", path, i+1, line)
			}
		}
	}
}
