package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The E5 fake-door experiment (#234): the brief in docs/product/experiments/
// is decided before anything is built, and the landing-page copy draft makes
// no promise of returns and cannot be mistaken for publishable copy. These
// tests keep both files to that contract as they are edited.

const (
	fakeDoorBriefPath = "../docs/product/experiments/fake-door-landing.md"
	landingCopyPath   = "../docs/product/landing-copy-draft.md"
)

// section returns the text of "## <n>." up to the next "## " heading.
func section(t *testing.T, doc, n string) string {
	t.Helper()
	start := strings.Index(doc, "\n## "+n+".")
	if start < 0 {
		t.Fatalf("no section %s", n)
	}
	body := doc[start+1:]
	if next := strings.Index(body[3:], "\n## "); next >= 0 {
		body = body[:next+3]
	}
	return body
}

// Scenario: the brief is decision-ready.
func TestFakeDoorBrief_DecisionReady(t *testing.T) {
	brief := readDoc(t, fakeDoorBriefPath)
	for _, heading := range []string{"## 1. Hypothesis", "## 4. Metrics", "## 5. Sample and duration", "## 6. Decision rule", "## 7. Guardrails"} {
		if !strings.Contains(brief, heading) {
			t.Errorf("brief has no %q", heading)
		}
	}
	if !strings.Contains(section(t, brief, "4"), "**Primary:** visit → sign-up rate") {
		t.Error("brief §4 does not name the primary metric visit → sign-up")
	}
	sample := section(t, brief, "5")
	for _, want := range []string{"**Sample: at least 400 visits.**", "**Duration: at least 2 weeks, at most 4**", "**Assumption:**"} {
		if !strings.Contains(sample, want) {
			t.Errorf("brief §5 does not state %q", want)
		}
	}

	// Every outcome has a threshold and the decision it leads to.
	rows := planTable(t, brief, "6")
	outcomes := map[string]bool{}
	for _, r := range rows {
		if len(r) < 3 || r[1] == "" || r[2] == "" {
			t.Errorf("decision row %v lacks its threshold or its decision", r)
			continue
		}
		outcomes[strings.Trim(r[0], "*")] = true
	}
	for _, o := range []string{"Success", "Iterate", "Kill", "Not enough traffic"} {
		if !outcomes[o] {
			t.Errorf("decision rule has no %q outcome", o)
		}
	}

	// The thresholds sit beside PRD §6's kill criterion, quoted as it is.
	const kill = "< 30 % of interviewed cycle holders would let software trade their account"
	if !strings.Contains(readDoc(t, prdPath), "<30 % of interviewed cycle holders would let software trade their account") {
		t.Fatal("PRD §6 kill criterion changed: update the brief and this test together")
	}
	if !strings.Contains(section(t, brief, "6"), kill) {
		t.Error("brief §6 does not tie its thresholds to PRD §6's kill criterion")
	}

	// Unpaid channels only, and an honest end state with no checkout.
	for _, want := range []string{"**No paid acquisition**", "There is **no fake checkout**", "no payment form"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief does not state %q", want)
		}
	}
}

var (
	lNumber    = regexp.MustCompile(`\bL\d+\b`)
	briefLRow  = regexp.MustCompile(`(?m)^\| (L\d+) \|`)
	digit      = regexp.MustCompile(`\d+`)
	returnWord = regexp.MustCompile(`(?i)%|\bapy\b|\bapr\b|\byields?\b|\breturns?\b|\bprofits?\b|\bperformance figures?\b`)
)

// copyRows returns the rows of the copy draft's page (§1) and value points (§2).
func copyRows(t *testing.T) (page, values [][]string) {
	t.Helper()
	doc := readDoc(t, landingCopyPath)
	page, values = planTable(t, doc, "1"), planTable(t, doc, "2")
	if len(page) == 0 || len(values) != 3 {
		t.Fatalf("copy draft has %d page rows and %d value points, want some and 3", len(page), len(values))
	}
	return page, values
}

// Scenario: no promised returns — no figure of any kind in the copy, every
// benefit has its risk beside it, testnet is always labelled as testnet.
func TestLandingCopy_NoPromisedReturns(t *testing.T) {
	page, values := copyRows(t)
	var copyText []string
	for _, r := range page {
		copyText = append(copyText, r[2])
	}
	for _, r := range values {
		if len(r) < 4 || strings.TrimSpace(r[2]) == "" {
			t.Errorf("value point %s has no risk beside it", r[0])
			continue
		}
		copyText = append(copyText, r[1]+" "+r[2])
	}
	for _, text := range copyText {
		if m := returnWord.FindString(text); m != "" {
			t.Errorf("copy promises or quotes a return (%q): %s", m, text)
		}
		for _, d := range digit.FindAllString(text, -1) {
			if d != "18" { // the age limit of the consent line
				t.Errorf("copy contains a figure %q: %s", d, text)
			}
		}
		if strings.Contains(strings.ToLower(text), "testnet") && !strings.Contains(text, "no real money") {
			t.Errorf("copy mentions testnet without \"no real money\": %s", text)
		}
	}
}

// Scenario: legal gate visible — the header forbids publishing before legal
// review, and every element names legal-brief questions that exist.
func TestLandingCopy_LegalGateVisible(t *testing.T) {
	doc := readDoc(t, landingCopyPath)
	if head := doc[:min(len(doc), 400)]; !strings.Contains(head, "**Needs legal review: not to be published.**") {
		t.Error("copy draft header does not say it needs legal review and must not be published")
	}
	known := map[string]bool{}
	for _, m := range briefLRow.FindAllStringSubmatch(readDoc(t, legalBriefPath), -1) {
		known[m[1]] = true
	}
	if len(known) == 0 {
		t.Fatal("legal brief lists no L-questions")
	}
	page, values := copyRows(t)
	for _, r := range append(page, values...) {
		legal := r[len(r)-1]
		ids := lNumber.FindAllString(legal, -1)
		if len(ids) == 0 {
			t.Errorf("element %s names no legal-brief question", r[0])
		}
		for _, id := range ids {
			if !known[id] {
				t.Errorf("element %s names %s, which is not in the legal brief", r[0], id)
			}
		}
	}
}

// Scenario: personal data named — the brief lists the data collected, the
// consent text and the open data-protection questions, and links the
// data-protection programme.
func TestFakeDoorBrief_PersonalDataNamed(t *testing.T) {
	data := section(t, readDoc(t, fakeDoorBriefPath), "8")
	if !strings.Contains(data, "`docs/legal/data-protection.md`") {
		t.Error("brief §8 does not link docs/legal/data-protection.md")
	}
	if _, err := os.Stat("../docs/legal/data-protection.md"); err != nil {
		t.Errorf("linked data-protection programme is missing: %v", err)
	}
	for _, want := range []string{"**Data collected:**", "| E-mail | yes |", "**Not collected:**", "**Consent text needed**", "needs legal review", "> ☐ I want to join the waiting list.", "**Open data-protection questions**", "the product owner's decision"} {
		if !strings.Contains(data, want) {
			t.Errorf("brief §8 does not contain %q", want)
		}
	}
	open := regexp.MustCompile(`(?m)^\d+\. `).FindAllString(data, -1)
	if len(open) < 3 {
		t.Errorf("brief §8 lists %d open data-protection questions, want at least 3", len(open))
	}
	for _, topic := range []string{"consent", "Retention", "stored"} {
		if !strings.Contains(data, topic) {
			t.Errorf("open questions do not cover %q", topic)
		}
	}
}
