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

// AGENTIC.md split (#461): AGENTIC.md is the overview and docs/agents/*.md holds
// one file per agent. The split's one-off "nothing lost" check against the old
// file was retired after it merged; what stays is that every link to a section
// resolves, the overview indexes every agent file, and the agent files keep one
// sentence per line.

const (
	agenticMaxProse    = 400 // runes, outside code fences and tables
	agenticAgentsDir   = "docs/agents"
	agenticOverviewMax = 40 * 1024
)

var (
	// [text](target) links and the quoted form: AGENTIC.md "Section".
	agenticLinkRe   = regexp.MustCompile(`\]\(([^)\s]*)\)`)
	agenticQuotedRe = regexp.MustCompile(`([A-Za-z0-9_./-]*AGENTIC\.md|docs/agents/[A-Za-z0-9_-]+\.md) "([^"]+)"`)
	agenticHeadRe   = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)
)

func agenticRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func agenticFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", agenticAgentsDir, "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s/*.md files (%v)", agenticAgentsDir, err)
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, agenticAgentsDir+"/"+filepath.Base(f))
	}
	return out
}

// agenticSlug is GitHub's anchor for a heading: lower case, spaces to dashes, punctuation dropped.
func agenticSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ' || r == '-':
			b.WriteRune('-')
		case r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r > 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// agenticHeadings returns the heading titles and anchors of a file, outside code fences.
func agenticHeadings(text string) (titles, slugs map[string]bool) {
	titles, slugs = map[string]bool{}, map[string]bool{}
	inCode := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
		}
		if inCode {
			continue
		}
		if m := agenticHeadRe.FindStringSubmatch(line); m != nil {
			h := strings.ReplaceAll(m[1], "`", "")
			titles[h] = true
			slugs[agenticSlug(h)] = true
		}
	}
	return titles, slugs
}

func agenticTrackedFiles(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "*.md", "*.sh", "*.py", "*.go", "*.js", "*.jsx")
	cmd.Dir = ".."
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	return strings.Fields(string(b))
}

// Acceptance "every link resolves": a link to AGENTIC.md or docs/agents/ and its
// section, in scripts, skills and docs: [text](path#anchor) and AGENTIC.md "Section".
func TestAgenticDocs_LinksResolve(t *testing.T) {
	known := map[string]string{"AGENTIC.md": agenticRead(t, "AGENTIC.md")}
	for _, f := range agenticFiles(t) {
		known[f] = agenticRead(t, f)
	}
	titlesOf, slugsOf := map[string]map[string]bool{}, map[string]map[string]bool{}
	for f, text := range known {
		titlesOf[f], slugsOf[f] = agenticHeadings(text)
	}
	for _, f := range agenticTrackedFiles(t) {
		if f == "tests/agentic_docs_test.go" {
			continue
		}
		text := agenticRead(t, f)
		_, own := known[f]
		for _, m := range agenticLinkRe.FindAllStringSubmatch(text, -1) {
			path, anchor, _ := strings.Cut(m[1], "#")
			var target string
			switch {
			case path == "" && own:
				target = f
			case path == "" || strings.Contains(path, "://"):
				continue
			default:
				target = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(f), path)))
			}
			if _, ok := known[target]; ok && anchor != "" && !slugsOf[target][anchor] {
				t.Errorf("%s links to %s#%s: no such section", f, target, anchor)
			}
		}
		for _, m := range agenticQuotedRe.FindAllStringSubmatch(text, -1) {
			target := m[1]
			if !strings.HasPrefix(target, agenticAgentsDir) {
				target = "AGENTIC.md"
			}
			if _, ok := known[target]; !ok {
				t.Errorf("%s names %s %q: no such file", f, m[1], m[2])
			} else if !titlesOf[target][m[2]] {
				t.Errorf("%s names %s %q: no such section", f, target, m[2])
			}
		}
	}
}

// The anchor rule the link check uses agrees with GitHub's.
func TestAgenticDocs_SlugMatchesGitHubAnchors(t *testing.T) {
	for heading, want := range map[string]string{
		"Delivery report (DORA)":                  "delivery-report-dora",
		"The agents, one file each":               "the-agents-one-file-each",
		"Check failures go back to the Dev agent": "check-failures-go-back-to-the-dev-agent",
		"Nightly flaky-test hunt":                 "nightly-flaky-test-hunt",
	} {
		if got := agenticSlug(heading); got != want {
			t.Errorf("agenticSlug(%q) = %q, want %q", heading, got, want)
		}
	}
}

// AGENTIC.md is the overview: it stays small and indexes every agent file.
func TestAgenticDocs_OverviewIndexesEveryAgentFile(t *testing.T) {
	root := agenticRead(t, "AGENTIC.md")
	if len(root) >= agenticOverviewMax {
		t.Errorf("AGENTIC.md is %d bytes, want under %d", len(root), agenticOverviewMax)
	}
	for _, f := range agenticFiles(t) {
		if !strings.Contains(root, "("+f+")") {
			t.Errorf("%s is not linked from the AGENTIC.md index", f)
		}
	}
}

// One sentence per line: no prose line over the limit in the agent files.
func TestAgenticDocs_NoLongProseLineInAgentFiles(t *testing.T) {
	for _, f := range agenticFiles(t) {
		inCode := false
		for i, line := range strings.Split(agenticRead(t, f), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inCode = !inCode
				continue
			}
			if inCode || strings.HasPrefix(trimmed, "|") {
				continue
			}
			line = docBareURL.ReplaceAllString(docLinkTarget.ReplaceAllString(line, "]"), "")
			if n := utf8.RuneCountInString(line); n > agenticMaxProse {
				t.Errorf("%s:%d is %d characters, want at most %d", f, i+1, n, agenticMaxProse)
			}
		}
	}
}
