package tests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The tip placement map (#728, epic E11): docs/product/education/tip-placement.md
// says which glossary terms and tunable settings each monitor page shows, the
// tip for each and the video where one fits. The checks read the map, the
// menu (frontend/lib/menu.js), the glossary, tips.json and videos.json, and
// each problem names the page and the key.

const (
	placementUnplaced = "Not on any page"
	placementKindTerm = "term"
	placementKindSet  = "setting"
)

// menuItemKey reads the pages of the menu: `{ key: "x", label: "...", path:`.
var menuItemKey = regexp.MustCompile(`\{ key: "([^"]+)", label: "[^"]*", path:`)

type placementRow struct {
	kind, key, video, reason string
}

// placementSections parses the map: section name → its table rows. A row is
// `| spot | kind | key | video |`; in the unplaced section `| kind | key | reason |`.
func placementSections(doc string) (map[string][]placementRow, map[string]bool, []string) {
	sections := map[string][]placementRow{}
	hasRoles := map[string]bool{}
	var problems []string
	cur := ""
	for _, line := range strings.Split(doc, "\n") {
		if name, ok := strings.CutPrefix(line, "## "); ok {
			cur = strings.TrimSpace(name)
			sections[cur] = nil
			continue
		}
		if cur == "" {
			continue
		}
		if strings.HasPrefix(line, "Roles:") && strings.TrimSpace(strings.TrimPrefix(line, "Roles:")) != "" {
			hasRoles[cur] = true
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.HasPrefix(cells[0], "---") || cells[0] == "spot" || (cur == placementUnplaced && cells[0] == "kind") {
			continue
		}
		if cur == placementUnplaced {
			if len(cells) != 3 {
				problems = append(problems, fmt.Sprintf("%q: row %q needs | kind | key | reason |", cur, line))
				continue
			}
			sections[cur] = append(sections[cur], placementRow{kind: cells[0], key: cells[1], reason: cells[2]})
			continue
		}
		if len(cells) != 4 {
			problems = append(problems, fmt.Sprintf("page %q: row %q needs | spot | kind | key | video |", cur, line))
			continue
		}
		sections[cur] = append(sections[cur], placementRow{kind: cells[1], key: cells[2], video: cells[3]})
	}
	return sections, hasRoles, problems
}

// placementProblems checks the map against the menu's pages, the glossary,
// the tips and the videos. Each line names the page and the key or id.
func placementProblems(doc string, menuKeys []string, glossary map[string]glossaryTip, tips map[string]tip, videos map[string]json.RawMessage) []string {
	sections, hasRoles, problems := placementSections(doc)
	placed := map[string]bool{} // kind + "\x00" + key
	check := func(page string, rows []placementRow, unplaced bool) {
		prev := ""
		for _, r := range rows {
			id := r.key + "\x00" + r.kind
			if id <= prev {
				problems = append(problems, fmt.Sprintf("page %q: key %q is out of alphabetical order or repeated", page, r.key))
			}
			prev = id
			switch r.kind {
			case placementKindTerm:
				if e, ok := glossary[r.key]; !ok {
					problems = append(problems, fmt.Sprintf("page %q: term %q is not in the glossary", page, r.key))
				} else if e.Short == "" || e.RiskIncome == "" {
					problems = append(problems, fmt.Sprintf("page %q: term %q has no tip (short and risk_income) in the glossary", page, r.key))
				}
			case placementKindSet:
				if _, ok := tips[r.key]; !ok {
					problems = append(problems, fmt.Sprintf("page %q: setting %q has no tip key in tips.json", page, r.key))
				}
			default:
				problems = append(problems, fmt.Sprintf("page %q: key %q has kind %q, want term or setting", page, r.key, r.kind))
			}
			if unplaced {
				if r.reason == "" {
					problems = append(problems, fmt.Sprintf("page %q: %s %q is listed without a reason", page, r.kind, r.key))
				}
				continue
			}
			placed[r.kind+"\x00"+r.key] = true
			if r.video != "-" {
				if _, ok := videos[r.video]; !ok {
					problems = append(problems, fmt.Sprintf("page %q: key %q names video id %q, which is not in videos.json", page, r.key, r.video))
				}
			}
		}
	}
	for _, page := range menuKeys {
		rows, ok := sections[page]
		if !ok {
			problems = append(problems, fmt.Sprintf("page %q is in the menu but has no section in tip-placement.md", page))
			continue
		}
		if !hasRoles[page] {
			problems = append(problems, fmt.Sprintf("page %q: the section has no Roles: line", page))
		}
		check(page, rows, false)
	}
	check(placementUnplaced, sections[placementUnplaced], true)
	for _, r := range sections[placementUnplaced] {
		if placed[r.kind+"\x00"+r.key] {
			problems = append(problems, fmt.Sprintf("%s %q is on a page and also listed as on no page", r.kind, r.key))
		}
		placed[r.kind+"\x00"+r.key] = true
	}
	// Nothing is dropped: every term and setting is on a page or listed.
	// A glossary entry named like a setting (delta_exit_threshold) is that setting's, covered by its setting row.
	for term := range glossary {
		if !strings.ContainsAny(term, "_.") && !placed[placementKindTerm+"\x00"+term] {
			problems = append(problems, fmt.Sprintf("term %q is on no page and not in %q", term, placementUnplaced))
		}
	}
	for key := range tips {
		if !placed[placementKindSet+"\x00"+key] {
			problems = append(problems, fmt.Sprintf("setting %q is on no page and not in %q", key, placementUnplaced))
		}
	}
	sort.Strings(problems)
	return problems
}

func loadPlacementInputs(t *testing.T) (doc string, menuKeys []string, videos map[string]json.RawMessage) {
	t.Helper()
	for _, m := range menuItemKey.FindAllStringSubmatch(string(readRepoFile(t, "frontend/lib/menu.js")), -1) {
		menuKeys = append(menuKeys, m[1])
	}
	if len(menuKeys) == 0 {
		t.Fatal("no menu pages found in frontend/lib/menu.js")
	}
	if err := json.Unmarshal(readRepoFile(t, "docs/product/education/videos.json"), &videos); err != nil {
		t.Fatal(err)
	}
	return string(readRepoFile(t, "docs/product/education/tip-placement.md")), menuKeys, videos
}

// Scenario: Every page is mapped — and the shipped map is complete and valid.
func TestTipPlacement_EveryPageIsMapped(t *testing.T) {
	doc, menuKeys, videos := loadPlacementInputs(t)
	for _, p := range placementProblems(doc, menuKeys, loadGlossaryTips(t), loadTips(t), videos) {
		t.Error(p)
	}
}

// Scenario: Every page is mapped (negative) / A missing tip is caught /
// A bad video id is caught — each on a small broken map.
func TestTipPlacement_CatchesBrokenMaps(t *testing.T) {
	glossary := map[string]glossaryTip{"delta": {Short: "s", RiskIncome: "r"}, "empty": {}}
	tips := map[string]tip{"rollout_dte": {}}
	videos := map[string]json.RawMessage{"video.freeze": nil}
	const head = "## overview\n\nRoles: user.\n\n| spot | kind | key | video |\n| --- | --- | --- | --- |\n"
	const tail = "\n## " + placementUnplaced + "\n\n| kind | key | reason |\n| --- | --- | --- |\n"
	cases := []struct {
		name, doc string
		menu      []string
		want      string
	}{
		{"page without section", head + "| h | term | delta | - |\n" + tail, []string{"overview", "income"}, `page "income" is in the menu but has no section`},
		{"term not in glossary", head + "| h | term | nope | - |\n" + tail, []string{"overview"}, `page "overview": term "nope" is not in the glossary`},
		{"term without tip", head + "| h | term | empty | - |\n" + tail, []string{"overview"}, `page "overview": term "empty" has no tip`},
		{"setting without tip key", head + "| h | setting | nope_x | - |\n" + tail, []string{"overview"}, `page "overview": setting "nope_x" has no tip key in tips.json`},
		{"video id missing", head + "| h | term | delta | video.nope |\n" + tail, []string{"overview"}, `names video id "video.nope"`},
		{"keys out of order", head + "| h | term | empty | - |\n| h | term | delta | - |\n" + tail, []string{"overview"}, `key "delta" is out of alphabetical order`},
		{"term dropped silently", head + tail, []string{"overview"}, `term "delta" is on no page`},
		{"setting dropped silently", head + "| h | term | delta | - |\n| h | term | empty | - |\n" + tail, []string{"overview"}, `setting "rollout_dte" is on no page`},
		{"unplaced without reason", head + "| h | term | delta | - |\n" + tail + "| term | empty | |\n", []string{"overview"}, `term "empty" is listed without a reason`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(placementProblems(c.doc, c.menu, glossary, tips, videos), "\n")
			if !strings.Contains(got, c.want) {
				t.Errorf("want a problem containing %q, got:\n%s", c.want, got)
			}
		})
	}
}
