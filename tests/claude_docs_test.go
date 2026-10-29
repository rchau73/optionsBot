package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// CLAUDE.md split (#460): a short root CLAUDE.md plus one CLAUDE.md per folder,
// one sentence per line. The sentence-by-sentence check against a frozen copy of
// the old file guarded the split itself (PR #480) and was retired once it merged:
// it failed every later, intended change to CLAUDE.md (#471).

const (
	claudeRootMaxBytes = 25 * 1024 // "under 25 KB"
	claudeMaxProseLine = 300       // runes, outside code fences
)

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
