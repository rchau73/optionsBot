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

// Every user-visible change updates the guide (#483): each item a person can
// see or do in the monitor — the menu pages, the admin pages, the surveys,
// the state-changing routes and the analytics screens — is named in the
// guide, by a heading equal to its label or by a `<!-- guide: <key> -->`
// marker in its section (several keys separated by commas). An item that is
// neither is listed in guideAllowList with its reason, or the test fails.

// guideItem is one registry entry: its key (e.g. "survey:nps",
// "route:POST /api/consent") and, when it has one, the label a heading may use.
type guideItem struct{ key, label string }

// guideAllowList: items with no guide entry, each with the reason.
var guideAllowList = map[string]string{}

var (
	guideMarkerRe  = regexp.MustCompile(`<!--\s*guide:\s*(.*?)\s*-->`)
	menuKeyLabelRe = regexp.MustCompile(`\{ key: "([a-z-]+)", label: "([^"]+)"`)
	adminPageRe    = regexp.MustCompile(`\{ href: "(/admin/[a-z-]+)", key: "[a-z-]+", label: "([^"]+)" \}`)
	surveyIDRe     = regexp.MustCompile(`(?m)^\s+([a-z_]+): \{`)
	screenEnumRe   = regexp.MustCompile(`\bscreen: enumOf\(([^)]*)\)`)
	quotedRe       = regexp.MustCompile(`"([^"]+)"`)
	writeHandlerRe = regexp.MustCompile(`export (?:async )?(?:function|const) (POST|PUT|PATCH|DELETE)\b`)
)

// uncoveredGuideItems lists the keys of items with no heading and no marker
// in the guide, unless allow-listed.
func uncoveredGuideItems(items []guideItem, guide []byte, allow map[string]string) []string {
	headings, markers := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(guide), "\n") {
		if t := strings.TrimLeft(line, "#"); t != line && strings.HasPrefix(t, " ") {
			headings[strings.TrimSpace(t)] = true
		}
	}
	for _, m := range guideMarkerRe.FindAllStringSubmatch(string(guide), -1) {
		for _, k := range strings.Split(m[1], ",") {
			markers[strings.TrimSpace(k)] = true
		}
	}
	var out []string
	for _, it := range items {
		if markers[it.key] || (it.label != "" && headings[it.label]) || allow[it.key] != "" {
			continue
		}
		out = append(out, it.key)
	}
	return out
}

func readFrontend(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "frontend"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// menuPageItems: the menu-bar pages (key "page:<key>", label = the heading)
// and ADMIN_PAGES (key "page:<href>").
func menuPageItems(menuJS []byte) (menu, admin []guideItem) {
	for _, m := range menuKeyLabelRe.FindAllSubmatch(menuJS, -1) {
		menu = append(menu, guideItem{key: "page:" + string(m[1]), label: string(m[2])})
	}
	for _, m := range adminPageRe.FindAllSubmatch(menuJS, -1) {
		admin = append(admin, guideItem{key: "page:" + string(m[1]), label: string(m[2])})
	}
	return menu, admin
}

// surveyItems: the ids of the SURVEYS object of lib/analytics/surveys.js.
func surveyItems(surveysJS []byte) []guideItem {
	src := string(surveysJS)
	start := strings.Index(src, "export const SURVEYS = {")
	if start < 0 {
		return nil
	}
	body := src[start:]
	if end := strings.Index(body, "\n};"); end >= 0 {
		body = body[:end]
	}
	var out []guideItem
	for _, m := range surveyIDRe.FindAllStringSubmatch(body, -1) {
		out = append(out, guideItem{key: "survey:" + m[1]})
	}
	return out
}

// screenItems: the analytics `screen` values of lib/analytics/plan.js; a
// menu page's screen ("worst-case" → "worst_case") is also covered by its
// page's heading.
func screenItems(planJS []byte, menu []guideItem) []guideItem {
	labels := map[string]string{}
	for _, p := range menu {
		labels[strings.ReplaceAll(strings.TrimPrefix(p.key, "page:"), "-", "_")] = p.label
	}
	m := screenEnumRe.FindSubmatch(planJS)
	if m == nil {
		return nil
	}
	var out []guideItem
	for _, q := range quotedRe.FindAllSubmatch(m[1], -1) {
		out = append(out, guideItem{key: "screen:" + string(q[1]), label: labels[string(q[1])]})
	}
	return out
}

// routeItems: every POST, PUT, PATCH or DELETE handler of a route.js under
// apiDir, as "route:<METHOD> /api/<path>" (dynamic segments kept, e.g.
// "/api/bots/[bot]/close").
func routeItems(t *testing.T, apiDir string) []guideItem {
	t.Helper()
	var out []guideItem
	err := filepath.WalkDir(apiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "route.js" {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(apiDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		route := "/api/" + filepath.ToSlash(rel)
		for _, m := range writeHandlerRe.FindAllSubmatch(src, -1) {
			out = append(out, guideItem{key: "route:" + string(m[1]) + " " + route})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// guideRegistry reads every list the guide must cover, failing when a list
// reads as empty (a moved file or a changed shape would otherwise pass).
func guideRegistry(t *testing.T) map[string][]guideItem {
	t.Helper()
	menu, admin := menuPageItems(readFrontend(t, "lib", "menu.js"))
	reg := map[string][]guideItem{
		"menu pages":  menu,
		"ADMIN_PAGES": admin,
		"surveys":     surveyItems(readFrontend(t, "lib", "analytics", "surveys.js")),
		"routes":      routeItems(t, filepath.Join("..", "frontend", "app", "api")),
		"screens":     screenItems(readFrontend(t, "lib", "analytics", "plan.js"), menu),
	}
	for name, items := range reg {
		if len(items) == 0 {
			t.Fatalf("read no %s: has its file moved or changed shape?", name)
		}
	}
	return reg
}

// Scenario: today's repository passes with an empty or justified allow-list.
func TestMonitorGuide_EveryUserVisibleItemIsNamed(t *testing.T) {
	reg := guideRegistry(t)
	guide := helpSources(t).Guide
	known := map[string]bool{}
	for name, items := range reg {
		for _, it := range items {
			known[it.key] = true
		}
		if missing := uncoveredGuideItems(items, guide, guideAllowList); len(missing) > 0 {
			t.Errorf("%s with no guide entry in %s (add a section or a <!-- guide: key --> marker, #483): %v", name, helpkb.GuideFile, missing)
		}
	}
	for key, reason := range guideAllowList {
		if !known[key] {
			t.Errorf("guideAllowList names %q, which no longer exists: remove it", key)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("guideAllowList %q has no reason", key)
		}
	}
	for _, want := range []string{"route:POST /api/bots/[bot]/close", "route:DELETE /api/admin/users/[id]", "survey:nps", "page:/admin/users", "screen:login"} {
		if !known[want] {
			t.Errorf("the registry did not read %q", want)
		}
	}
}

// Scenario: a new survey id, an admin page or a POST route with no guide
// entry fails and is named.
func TestMonitorGuide_NewItemWithoutEntryIsNamed(t *testing.T) {
	guide := []byte("# Monitor guide\n\n## Overview\n\n<!-- guide: survey:nps, route:POST /api/consent -->\n\n### Users\n")
	items := []guideItem{
		{key: "page:overview", label: "Overview"},
		{key: "page:/admin/users", label: "Users"},
		{key: "survey:nps"},
		{key: "route:POST /api/consent"},
		{key: "survey:churn"},
		{key: "page:/admin/flags", label: "Flags"},
		{key: "route:POST /api/export"},
		{key: "screen:export"},
	}
	got := uncoveredGuideItems(items, guide, nil)
	want := []string{"survey:churn", "page:/admin/flags", "route:POST /api/export", "screen:export"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("uncovered = %v, want %v", got, want)
	}
	if got := uncoveredGuideItems(items, guide, map[string]string{"survey:churn": "not shown yet"}); len(got) != 3 {
		t.Errorf("an allow-listed item was still reported: %v", got)
	}

	// The readers pick up a new item of each list.
	menu, admin := menuPageItems([]byte(`{ key: "overview", label: "Overview", path: "/" },
export const ADMIN_PAGES = [
  { href: "/admin/flags", key: "flags", label: "Flags" },
];`))
	if len(menu) != 1 || len(admin) != 1 || admin[0] != (guideItem{key: "page:/admin/flags", label: "Flags"}) {
		t.Errorf("menu %v, admin %v", menu, admin)
	}
	surveys := surveyItems([]byte("export const SURVEYS = {\n  nps: { codes: {} },\n  churn: { codes: {} },\n};\nconst X = {\n  other: { a: 1 },\n};\n"))
	if len(surveys) != 2 || surveys[1].key != "survey:churn" {
		t.Errorf("surveys = %v", surveys)
	}
	screens := screenItems([]byte(`  screen: enumOf("overview", "export"),`), menu)
	if len(screens) != 2 || screens[0].label != "Overview" || screens[1] != (guideItem{key: "screen:export"}) {
		t.Errorf("screens = %v", screens)
	}

	api := t.TempDir()
	for path, src := range map[string]string{
		"export/route.js":     "export async function POST(request) {}\nexport async function GET() {}\n",
		"read/route.js":       "export async function GET() {}\n",
		"users/[id]/route.js": "export async function PATCH() {}\nexport async function DELETE() {}\n",
		"notes/helper.js":     "export async function POST() {}\n",
	} {
		if err := os.MkdirAll(filepath.Join(api, filepath.Dir(path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(api, path), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	routes := routeItems(t, api)
	var keys []string
	for _, it := range routes {
		keys = append(keys, it.key)
	}
	want = []string{"route:POST /api/export", "route:PATCH /api/users/[id]", "route:DELETE /api/users/[id]"}
	if strings.Join(keys, "|") != strings.Join(want, "|") {
		t.Errorf("routes = %v, want %v", keys, want)
	}
	if missing := uncoveredGuideItems(routes, guide, nil); len(missing) != 3 {
		t.Errorf("new routes with no entry: %v, want all 3 named", missing)
	}
}
