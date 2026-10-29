package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// CLAUDE.md split (#460): a short root CLAUDE.md plus one CLAUDE.md per folder.
// The text moved, it was not rewritten: every sentence of the frozen copy of the
// old file (tests/testdata/claude_md_before.md) must still be found in the root
// plus the folder files, whitespace and line breaks normalised, list markers
// dropped, so a sentence split into sub-bullets still matches.

const (
	claudeBeforeFile   = "testdata/claude_md_before.md"
	claudeRootMaxBytes = 25 * 1024 // "under 25 KB"
	claudeMaxProseLine = 300       // runes, outside code fences
)

var (
	claudeHeadingRe = regexp.MustCompile(`^#+\s+`)
	claudeMarkerRe  = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
	claudeSpaceRe   = regexp.MustCompile(`\s+`)
)

// claudeNormLine trims a line and drops its heading or list marker.
func claudeNormLine(line string) string {
	line = strings.TrimSpace(line)
	line = claudeHeadingRe.ReplaceAllString(line, "")
	line = claudeMarkerRe.ReplaceAllString(line, "")
	return claudeSpaceRe.ReplaceAllString(line, " ")
}

// claudeSentences splits markdown into the units that must survive the move:
// each line of a code block whole, and each sentence of every prose block (a
// block ends at a blank line, a heading, a list item or a fence).
func claudeSentences(text string) []string {
	var out, block []string
	flush := func() {
		joined := strings.TrimSpace(strings.Join(block, " "))
		block = nil
		start := 0
		for i := 0; i < len(joined); i++ {
			c := joined[i]
			if (c == '.' || c == '!' || c == '?') && i+1 < len(joined) && joined[i+1] == ' ' {
				out = append(out, strings.TrimSpace(joined[start:i+1]))
				start = i + 2
			}
		}
		if s := strings.TrimSpace(joined[min(start, len(joined)):]); s != "" {
			out = append(out, s)
		}
	}
	inCode := false
	for raw := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			flush()
			inCode = !inCode
			continue
		}
		if inCode {
			if n := claudeNormLine(raw); n != "" {
				out = append(out, n)
			}
			continue
		}
		if trimmed == "" || claudeHeadingRe.MatchString(trimmed) || claudeMarkerRe.MatchString(trimmed) {
			flush()
		}
		if n := claudeNormLine(raw); n != "" {
			block = append(block, n)
		}
	}
	flush()
	return out
}

// claudeCorpus is every line of the given files, normalised and joined by one space.
func claudeCorpus(texts []string) string {
	var parts []string
	for _, text := range texts {
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				continue
			}
			if n := claudeNormLine(line); n != "" {
				parts = append(parts, n)
			}
		}
	}
	return " " + strings.Join(parts, " ") + " "
}

// claudeMissing lists the sentences of before found in none of after.
func claudeMissing(before string, after []string) []string {
	corpus := claudeCorpus(after)
	var out []string
	for _, s := range claudeSentences(before) {
		if !strings.Contains(corpus, s) {
			out = append(out, s)
		}
	}
	return out
}

// claudeFolderFiles returns every CLAUDE.md below the repository root but the
// root's own, as slash paths relative to the root.
func claudeFolderFiles(t *testing.T) []string {
	t.Helper()
	skip := map[string]bool{".git": true, "node_modules": true, ".next": true, "data": true, "testdata": true, "agent-worktrees": true}
	var out []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != ".." && skip[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "CLAUDE.md" && path != filepath.Join("..", "CLAUDE.md") {
			rel, _ := filepath.Rel("..", path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func claudeRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Acceptance "Nothing lost": every sentence of the old CLAUDE.md is in the new files.
func TestClaudeDocs_NothingLost(t *testing.T) {
	before, err := os.ReadFile(claudeBeforeFile)
	if err != nil {
		t.Fatal(err)
	}
	after := []string{claudeRead(t, "CLAUDE.md")}
	for _, f := range claudeFolderFiles(t) {
		after = append(after, claudeRead(t, f))
	}
	for _, s := range claudeMissing(string(before), after) {
		t.Errorf("sentence of the old CLAUDE.md in no CLAUDE.md now: %q", s)
	}
}

// The checker itself: a dropped sentence is listed; a sentence moved to another
// file, or split into sub-bullets, is not.
func TestClaudeDocs_CheckerCatchesADroppedSentence(t *testing.T) {
	before := "## Rules\n\n- **Slots:** one slot each; occupied by a strangle. Never two.\n\n```bash\nmake build   # build it\n```\n"
	moved := []string{
		"## Rules\n\n- **Slots:** one slot each;\n  - occupied by a strangle.\n",
		"```bash\nmake build # build it\n```\n- Never two.\n",
	}
	if got := claudeMissing(before, moved); len(got) != 0 {
		t.Errorf("moved and split text reported missing: %q", got)
	}
	dropped := []string{"## Rules\n\n- **Slots:** one slot each; occupied by a strangle.\n\nmake build # build it\n"}
	if got := claudeMissing(before, dropped); len(got) != 1 || got[0] != "Never two." {
		t.Errorf("dropped sentence not caught: %q", got)
	}
	reworded := []string{"Rules **Slots:** one slot each; held by a strangle. Never two. make build # build it"}
	if got := claudeMissing(before, reworded); len(got) != 1 {
		t.Errorf("reworded sentence not caught: %q", got)
	}
}

// Acceptance: the root is under 25 KB and names every folder file in its index.
func TestClaudeDocs_RootIsSmallAndIndexesEveryFolderFile(t *testing.T) {
	root := claudeRead(t, "CLAUDE.md")
	if len(root) >= claudeRootMaxBytes {
		t.Errorf("CLAUDE.md is %d bytes, want under %d", len(root), claudeRootMaxBytes)
	}
	files := claudeFolderFiles(t)
	if len(files) < 16 {
		t.Errorf("found %d folder CLAUDE.md files, want at least 16: %v", len(files), files)
	}
	for _, f := range files {
		if !strings.Contains(root, "`"+f+"`") {
			t.Errorf("%s is not named in the root CLAUDE.md index", f)
		}
	}
}

// One sentence per line: no prose line (outside code fences) over 300 characters.
func TestClaudeDocs_NoLongProseLine(t *testing.T) {
	for _, f := range append([]string{"CLAUDE.md"}, claudeFolderFiles(t)...) {
		inCode := false
		for i, line := range strings.Split(claudeRead(t, f), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCode = !inCode
				continue
			}
			if n := utf8.RuneCountInString(line); !inCode && n > claudeMaxProseLine {
				t.Errorf("%s:%d is %d characters, want at most %d", f, i+1, n, claudeMaxProseLine)
			}
		}
	}
}
