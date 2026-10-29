package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// One sentence per line for the docs (#463, part of #459): no prose line of a
// Markdown file over docMaxProseLine characters. Table rows, code fences, link
// targets and bare URLs do not count. A file with lines over the limit is
// listed in docLongLineExceptions with how many it may keep and why; the
// number can only go down, so a new long line in such a file still fails.
const docMaxProseLine = 400 // runes

const (
	reasonOneSentence = "single long sentences (clauses joined by ; or —), kept whole so the text does not change (#463)"
	reasonNotReflowed = "not reflowed yet; one sentence per line is a follow-up of #459 (#463)"
	reasonAgentFile   = "agent instructions: a policy file, reflowed in its own change (#463)"
)

var docLongLineExceptions = map[string]struct {
	max    int
	reason string
}{
	"README.md":            {19, reasonOneSentence},
	"docs/operations.md":   {8, reasonOneSentence},
	"docs/architecture.md": {5, reasonOneSentence},
	"AGENTIC.md":           {40, reasonAgentFile},
	".claude/skills/product-manager/SKILL.md":                      {17, reasonAgentFile},
	".claude/skills/developer/SKILL.md":                            {15, reasonAgentFile},
	".claude/skills/option-bot-specialist/SKILL.md":                {8, reasonAgentFile},
	".claude/skills/legal-counsel/SKILL.md":                        {7, reasonAgentFile},
	".claude/skills/ux-designer/SKILL.md":                          {2, reasonAgentFile},
	".claude/skills/option-bot-specialist/references/dashboard.md": {1, reasonAgentFile},
	"frontend/README.md":                                           {50, reasonNotReflowed},
	"frontend/docs/responsive-audit.md":                            {8, reasonNotReflowed},
	"frontend/content/monitor-guide.md":                            {7, "the help index cuts this file by heading; reflowing it is a follow-up checked with go run ./cmd/helpindex (#463)"},
	"docs/product/analytics/surveys.md":                            {11, reasonNotReflowed},
	"docs/product/analytics/event-plan.md":                         {8, reasonNotReflowed},
	"docs/legal/jurisdictions/brazil.md":                           {9, reasonNotReflowed},
	"docs/legal/jurisdictions/eu.md":                               {2, reasonNotReflowed},
	"docs/legal/preliminary-answers.md":                            {2, reasonNotReflowed},
	"docs/legal/drafts/termos-de-uso.md":                           {3, reasonNotReflowed},
	"docs/legal/data-protection.md":                                {1, reasonNotReflowed},
	"docs/product/PRD.md":                                          {8, reasonNotReflowed},
	"docs/product/assistant/contract.md":                           {6, reasonNotReflowed},
	"docs/product/assistant/README.md":                             {2, reasonNotReflowed},
	"docs/product/experiments/fake-door-landing.md":                {4, reasonNotReflowed},
	"docs/product/discovery/survey.md":                             {2, reasonNotReflowed},
	"docs/product/discovery/consent.md":                            {1, reasonNotReflowed},
	"docs/product/education/video-storyboards.md":                  {2, reasonNotReflowed},
	"docs/product/education/empty-states.md":                       {1, reasonNotReflowed},
	"docs/product/pricing-options.md":                              {2, reasonNotReflowed},
	"docs/product/competitors/README.md":                           {1, reasonNotReflowed},
	"docs/product/lab/example-analysis.md":                         {1, reasonNotReflowed},
	"docs/product/legal-brief.md":                                  {1, reasonNotReflowed},
	"docs/usability/e3-investor-monitor-test.md":                   {4, reasonNotReflowed},
	"docs/ai-analysis.md":                                          {4, reasonNotReflowed},
	"docs/direction-trial.md":                                      {4, reasonNotReflowed},
	"docs/account-isolation.md":                                    {2, reasonNotReflowed},
	"docs/exchange-adapter.md":                                     {3, reasonNotReflowed},
	"scripts/product/README.md":                                    {4, reasonNotReflowed},
}

var (
	docLinkTarget = regexp.MustCompile(`\]\([^)]*\)`)
	docBareURL    = regexp.MustCompile(`https?://\S+`)
)

// docLongProseLines returns the 1-based numbers of the prose lines longer than
// limit runes: outside code fences and tables, with link targets and URLs removed.
func docLongProseLines(text string, limit int) []int {
	var out []int
	inCode := false
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode || strings.HasPrefix(trimmed, "|") {
			continue
		}
		line = docBareURL.ReplaceAllString(docLinkTarget.ReplaceAllString(line, "]"), "")
		if utf8.RuneCountInString(line) > limit {
			out = append(out, i+1)
		}
	}
	return out
}

func docMarkdownFiles(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "*.md")
	cmd.Dir = ".."
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	return strings.Fields(string(b))
}

// Acceptance: the check fails on a new 500-character prose line and passes on
// short prose, a long table row, a long code line and a long link target.
func TestDocs_LongProseLineIsFound(t *testing.T) {
	long := strings.Repeat("word ", 100) // 500 characters
	text := "short.\n" + long + "\n| " + long + " |\n```\n" + long + "\n```\n[a](https://example.com/" + strings.Repeat("x", 500) + ")\n"
	got := docLongProseLines(text, docMaxProseLine)
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("long lines = %v, want [2]", got)
	}
}

// One sentence per line: no prose line over the limit, but the listed exceptions.
func TestDocs_NoLongProseLine(t *testing.T) {
	for _, f := range docMarkdownFiles(t) {
		b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		lines := docLongProseLines(string(b), docMaxProseLine)
		allowed := docLongLineExceptions[f].max
		if len(lines) > allowed {
			t.Errorf("%s has %d prose lines over %d characters (allowed %d): lines %v — put one sentence per line (scripts/docs/reflow.py write %s)",
				f, len(lines), docMaxProseLine, allowed, lines, f)
		}
		if len(lines) < allowed {
			t.Errorf("%s has %d long lines but its exception allows %d: lower it in docLongLineExceptions", f, len(lines), allowed)
		}
	}
}

// Every exception names a file that exists and gives its reason.
func TestDocs_ExceptionsAreReal(t *testing.T) {
	for f, e := range docLongLineExceptions {
		if e.reason == "" || e.max <= 0 {
			t.Errorf("%s: exception needs a reason and a positive count", f)
		}
		if _, err := os.Stat(filepath.Join("..", filepath.FromSlash(f))); err != nil {
			t.Errorf("%s: exception for a file that does not exist", f)
		}
	}
}
