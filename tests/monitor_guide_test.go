package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"optionsbot/internal/helpkb"
)

// The Monitor guide (#418): frontend/content/monitor-guide.md has one ## section
// per page of the menu (frontend/lib/menu.js), and the help index carries it
// and docs/parameters.md.

var menuLabelRe = regexp.MustCompile(`\{ key: "[a-z-]+", label: "([^"]+)"`)

func menuLabels(t *testing.T, menuJS []byte) []string {
	t.Helper()
	var out []string
	for _, m := range menuLabelRe.FindAllSubmatch(menuJS, -1) {
		out = append(out, string(m[1]))
	}
	if len(out) < 8 {
		t.Fatalf("read %d menu labels from lib/menu.js, expected at least 8", len(out))
	}
	return out
}

// missingGuideSections lists the menu labels with no ## section of that name.
func missingGuideSections(labels []string, guide []byte) []string {
	have := map[string]bool{}
	for _, line := range strings.Split(string(guide), "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			have[strings.TrimSpace(h)] = true
		}
	}
	var out []string
	for _, l := range labels {
		if !have[l] {
			out = append(out, l)
		}
	}
	return out
}

// Scenario: a page added to lib/menu.js without a guide section fails.
func TestMonitorGuide_EveryMenuPageHasASection(t *testing.T) {
	menuJS, err := os.ReadFile(filepath.Join("..", "frontend", "lib", "menu.js"))
	if err != nil {
		t.Fatal(err)
	}
	labels := menuLabels(t, menuJS)
	guide := helpSources(t).Guide
	if missing := missingGuideSections(labels, guide); len(missing) > 0 {
		t.Errorf("menu pages with no ## section in %s: %v", helpkb.GuideFile, missing)
	}

	added := append(append([]string(nil), labels...), "Reports")
	if missing := missingGuideSections(added, guide); len(missing) != 1 || missing[0] != "Reports" {
		t.Errorf("a new page without a section was not caught: %v", missing)
	}
	if missing := missingGuideSections(labels, []byte("# Monitor guide\n\n## Overview\n")); len(missing) != len(labels)-1 {
		t.Errorf("a guide with one section reported %d missing, want %d", len(missing), len(labels)-1)
	}
}

// Scenario: the guide and the settings reference are in the index and the
// best passages of their questions are in the top 3.
func TestMonitorGuide_SearchFindsGuideAndParameters(t *testing.T) {
	ix := helpIndex(t)
	cases := []struct{ question, prefix string }{
		{"How do I read the Worst case page?", "monitor-guide.md#worst-case"},
		{"What does repair_cooldown_hours do?", "parameters.md#"},
	}
	for _, c := range cases {
		found := false
		for _, h := range ix.Search(c.question, 3) {
			found = found || strings.HasPrefix(h.Source, c.prefix)
		}
		if !found {
			t.Errorf("%q: no %s passage in the top 3: %v", c.question, c.prefix, sources(ix.Search(c.question, 3)))
		}
	}
}
