package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The legal-review brief (docs/product/legal-brief.md, #179) is what counsel
// quotes against: these tests keep it in step with the PRD's §11 risks.

const (
	legalBriefPath = "../docs/product/legal-brief.md"
	prdPath        = "../docs/product/PRD.md"
)

func readDoc(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

var boldLead = regexp.MustCompile(`^- \*\*([^*]+)\*\*`)

// prdRisks returns the name of every bullet of PRD §11: its bold lead, cut
// before a parenthesis, a dash or a colon ("Legal gate (needs legal review) —
// blocks E4 …:" → "Legal gate").
func prdRisks(t *testing.T, prd string) []string {
	t.Helper()
	start := strings.Index(prd, "\n## 11.")
	if start < 0 {
		t.Fatal("PRD has no §11")
	}
	body := prd[start+1:]
	if next := strings.Index(body[3:], "\n## "); next >= 0 {
		body = body[:next+3]
	}
	var risks []string
	for _, line := range strings.Split(body, "\n") {
		m := boldLead.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		for _, sep := range []string{" (", " —", ":"} {
			if i := strings.Index(name, sep); i >= 0 {
				name = name[:i]
			}
		}
		risks = append(risks, strings.TrimSpace(name))
	}
	return risks
}

var epicMarker = regexp.MustCompile(`<!-- epic: \{"id": "(E\d+)"`)
var epicID = regexp.MustCompile(`E\d+`)

// Scenario: brief ready for counsel — every PRD §11 risk has at least one
// question, each question names the epic it gates and a fallback.
func TestLegalBrief_ReadyForCounsel(t *testing.T) {
	brief := readDoc(t, legalBriefPath)
	prd := readDoc(t, prdPath)

	epics := map[string]bool{}
	for _, m := range epicMarker.FindAllStringSubmatch(prd, -1) {
		epics[m[1]] = true
	}
	risks := prdRisks(t, prd)
	if len(risks) < 5 || len(epics) < 10 {
		t.Fatalf("parsed %d risks and %d epics from the PRD: its format changed?", len(risks), len(epics))
	}

	questions := planTable(t, brief, "3")
	perRisk := map[string]int{}
	for _, q := range questions {
		if len(q) != 6 {
			t.Fatalf("question row %v: want 6 cells (ID, Risk, Where, Question, Gates, Fallback)", q)
		}
		id, risk, question, gates, fallback := q[0], q[1], q[3], q[4], q[5]
		perRisk[risk]++
		if question == "" {
			t.Errorf("%s has no question", id)
		}
		named := epicID.FindAllString(gates, -1)
		if len(named) == 0 {
			t.Errorf("%s names no epic it gates (%q)", id, gates)
		}
		for _, e := range named {
			if !epics[e] {
				t.Errorf("%s gates %s, which is not an epic of the PRD", id, e)
			}
		}
		if fallback == "" || fallback == "—" || fallback == "-" {
			t.Errorf("%s has no fallback", id)
		}
	}
	for _, r := range risks {
		if perRisk[r] == 0 {
			t.Errorf("PRD §11 risk %q has no question in the brief", r)
		}
	}
	for r := range perRisk {
		known := false
		for _, p := range risks {
			known = known || p == r
		}
		if !known {
			t.Errorf("questions filed under %q, which is not a PRD §11 risk", r)
		}
	}
}

var moneyFigure = regexp.MustCompile(`(R\$|US\$|[$€£])\s?\d|\d\s?(USD|BRL|EUR)\b`)

// The brief is public: it says plainly that it is not legal advice and holds
// no prices (counsel's quotes go in data/product/).
func TestLegalBrief_NotAdviceAndNoPrices(t *testing.T) {
	brief := readDoc(t, legalBriefPath)
	if !strings.Contains(brief, "Not legal advice") {
		t.Error("brief lacks the \"Not legal advice\" notice")
	}
	if m := moneyFigure.FindString(brief); m != "" {
		t.Errorf("brief contains a money figure %q: prices belong in data/product/", m)
	}
}

// The §11 parser and the money check catch what they should, so the tests
// above are not passing by accident.
func TestLegalBrief_ParsersCatchWhatTheyShould(t *testing.T) {
	prd := "x\n## 11. Risks\n\n- **Legal gate (needs review) — blocks E4:** a\n- **Key security:** b\n- plain bullet\n## 12. Log\n- **Not a risk:** c\n"
	got := prdRisks(t, prd)
	if strings.Join(got, "|") != "Legal gate|Key security" {
		t.Errorf("prdRisks = %v", got)
	}
	for _, bad := range []string{"R$ 5.000", "$300", "€ 90", "250 USD"} {
		if !moneyFigure.MatchString(bad) {
			t.Errorf("%q not caught", bad)
		}
	}
	for _, ok := range []string{"Law 14.478/2022", "E4, E5", "2× premium"} {
		if moneyFigure.MatchString(ok) {
			t.Errorf("%q caught", ok)
		}
	}
}
