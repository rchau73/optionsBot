package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The PRD's decision log is one file per decision (#464), so two PRD pull
// requests that each add a decision do not conflict.

const decisionsDir = "../docs/product/decisions"

// decisionsBeforeSplit are the slugs of the rows the PRD's table held when it
// became a folder: none may go missing.
var decisionsBeforeSplit = []string{
	"2026-10-17-prd-approved-by-merge",
	"2026-10-17-prototypes-are-artifacts",
	"2026-10-17-sensitive-material-stays-private",
	"2026-10-18-primary-persona",
	"2026-10-18-product-not-study",
	"2026-10-18-agents-autonomy-and-core-policies",
	"2026-10-18-roles-and-no-money-handling",
	"2026-10-18-new-epics-e7-e13",
	"2026-10-18-pricing-owner-decides",
	"2026-10-20-e9-manager-scope",
	"2026-10-27-e10-assistant-request-drafts",
	"2026-10-28-e13-monthly-nps-and-bell",
}

var (
	decisionFileName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-[a-z0-9]+(-[a-z0-9]+)*\.md$`)
	decisionEpic     = regexp.MustCompile(`^(PRD|E\d+)$`)
)

// decisionField returns the value of a "- **Name:** value" line, or "".
func decisionField(body, name string) string {
	prefix := "- **" + name + ":** "
	for line := range strings.SplitSeq(body, "\n") {
		if value, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Scenario: every row of the old decision log is in exactly one file, and
// every file is well formed.
func TestPRDDecisions_EachFileComplete(t *testing.T) {
	files, err := filepath.Glob(decisionsDir + "/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no decision files in %s: %v", decisionsDir, err)
	}
	epics := map[string]bool{}
	for _, e := range prdEpicIDs(t, readDoc(t, prdPath)) {
		epics[e] = true
	}
	have := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		m := decisionFileName.FindStringSubmatch(name)
		if m == nil {
			t.Errorf("%s: name must be YYYY-MM-DD-<slug>.md", name)
			continue
		}
		have[strings.TrimSuffix(name, ".md")] = true
		body := readDoc(t, f)
		if !strings.HasPrefix(body, "# ") {
			t.Errorf("%s: no title", name)
		}
		if got := decisionField(body, "Date"); got != m[1] {
			t.Errorf("%s: Date %q does not match the file name", name, got)
		}
		for _, field := range []string{"Decision", "Options", "Reason", "Decided by"} {
			if decisionField(body, field) == "" {
				t.Errorf("%s: missing %s", name, field)
			}
		}
		epic := decisionField(body, "Epic")
		if !decisionEpic.MatchString(epic) {
			t.Errorf("%s: Epic %q must be PRD or an epic id", name, epic)
		} else if epic != "PRD" && !epics[epic] {
			t.Errorf("%s: Epic %s is not an epic of the PRD", name, epic)
		}
	}
	for _, slug := range decisionsBeforeSplit {
		if !have[slug] {
			t.Errorf("decision %s from the old log is missing", slug)
		}
	}
}

// Scenario: the PRD links to the folder and holds no table of decisions, so a
// new decision cannot make two PRD pull requests touch the same lines.
func TestPRDDecisions_PRDLinksToFolder(t *testing.T) {
	prd := readDoc(t, prdPath)
	i := strings.Index(prd, "## 12. Decision log")
	if i < 0 {
		t.Fatal("PRD has no §12")
	}
	section := prd[i:]
	if !strings.Contains(section, "(decisions/)") {
		t.Error("PRD §12 does not link to decisions/")
	}
	if strings.Contains(section, "|---") {
		t.Error("PRD §12 holds a table again: a decision is a file in decisions/")
	}
	if _, err := os.Stat(decisionsDir); err != nil {
		t.Error(err)
	}
}

// prdEpicIDs returns the ids of the PRD's "### E<n>" epic headings.
func prdEpicIDs(t *testing.T, prd string) []string {
	t.Helper()
	var ids []string
	re := regexp.MustCompile(`(?m)^###\s+(E\d+)\b`)
	for _, m := range re.FindAllStringSubmatch(prd, -1) {
		ids = append(ids, m[1])
	}
	if len(ids) == 0 {
		t.Fatal("no epics parsed from the PRD: its format changed?")
	}
	return ids
}
