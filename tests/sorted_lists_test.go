package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Lists that every PR adds to are kept in order (#465): a new entry then lands
// beside its neighbours, not at the end where two PRs collide. The frontend's
// lists (plan.js, the event plan's tables, the responsive audit) are checked
// by Jest next to their own tests.

// outOfOrder names the first pair of names out of ascending order, or "".
func outOfOrder(names []string) string {
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			return strconv.Quote(names[i]) + " comes after " + strconv.Quote(names[i-1])
		}
	}
	return ""
}

// glossaryKeys is glossary.json's top-level keys in file order.
func glossaryKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("glossary: not an object (%v)", err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// goldenIDs is golden.jsonl's ids in file order.
func goldenIDs(t *testing.T, raw []byte) []string {
	t.Helper()
	var ids []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var q struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			t.Fatalf("golden: %v", err)
		}
		ids = append(ids, q.ID)
	}
	return ids
}

// mapLiteralKeys is the string keys, in source order, of the map literal
// assigned to the package variable `name` in a Go file.
func mapLiteralKeys(t *testing.T, src []byte, name string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "plan.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if id.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s is not a literal", name)
				}
				var keys []string
				for _, el := range lit.Elts {
					k, err := strconv.Unquote(el.(*ast.KeyValueExpr).Key.(*ast.BasicLit).Value)
					if err != nil {
						t.Fatal(err)
					}
					keys = append(keys, k)
				}
				return keys
			}
		}
	}
	t.Fatalf("no var %s", name)
	return nil
}

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Scenario: the shipped lists pass — each is in ascending order.
func TestSortedLists_ShippedFilesAreInOrder(t *testing.T) {
	plan := readRepoFile(t, "internal/usage/plan.go")
	for _, c := range []struct {
		name  string
		names []string
	}{
		{"frontend/content/glossary.json keys", glossaryKeys(t, readRepoFile(t, "frontend/content/glossary.json"))},
		{"docs/product/assistant/golden.jsonl ids", goldenIDs(t, readRepoFile(t, "docs/product/assistant/golden.jsonl"))},
		{"internal/usage/plan.go Properties", mapLiteralKeys(t, plan, "Properties")},
		{"internal/usage/plan.go Events", mapLiteralKeys(t, plan, "Events")},
	} {
		if len(c.names) < 5 {
			t.Errorf("%s: only %d entries read", c.name, len(c.names))
		}
		if bad := outOfOrder(c.names); bad != "" {
			t.Errorf("%s out of order: %s — move it to its alphabetical place", c.name, bad)
		}
	}
}

// Scenario: an entry out of order fails — each reader, given a file with an
// entry appended at the end, reports it.
func TestSortedLists_AnEntryOutOfOrderFails(t *testing.T) {
	for _, c := range []struct {
		name  string
		names []string
		want  string
	}{
		{"glossary", glossaryKeys(t, []byte(`{"call": {"short": "x"}, "put": {}, "delta": {"labels": ["Δ"]}}`)), `"delta" comes after "put"`},
		{"golden", goldenIDs(t, []byte("{\"id\":\"how-01\"}\n{\"id\":\"how-03\"}\n\n{\"id\":\"how-02\"}\n")), `"how-02" comes after "how-03"`},
		{"plan.go", mapLiteralKeys(t, []byte("package p\nvar Events = map[string][]string{\"a\": {}, \"c\": {}, \"b\": {}}\n"), "Events"), `"b" comes after "c"`},
	} {
		if got := outOfOrder(c.names); got != c.want {
			t.Errorf("%s: outOfOrder = %q, want %q", c.name, got, c.want)
		}
	}
	if got := outOfOrder([]string{"a", "a", "b"}); got != "" {
		t.Errorf("equal neighbours are in order, got %q", got)
	}
}

// numberedGolden is the highest number each golden.jsonl prefix reached
// before new ids were named (#596): two PRs that each took the next number
// collided (#580, both ng-14; its ng-15 merged as the rule did). A numbered id above it fails.
var numberedGolden = map[string]int{"adv": 8, "fct": 6, "how": 17, "jar": 8, "ng": 15, "par": 13, "pg": 12, "pm": 10, "req": 4, "risk": 7, "unk": 6, "why": 17}

var (
	numberedID = regexp.MustCompile(`^([a-z]+)-([0-9]+)$`)
	namedID    = regexp.MustCompile(`^[a-z]+-[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
)

// goldenIDProblem says what is wrong with a golden id, given the ids seen so far, or "".
func goldenIDProblem(id string, seen map[string]bool) string {
	if seen[id] {
		return "used twice"
	}
	if m := numberedID.FindStringSubmatch(id); m != nil {
		n, _ := strconv.Atoi(m[2])
		if limit, ok := numberedGolden[m[1]]; ok && n <= limit {
			return ""
		}
		return "numbered: name it <prefix>-<a few words>, e.g. ng-consent-analytics"
	}
	if !namedID.MatchString(id) {
		return "not <prefix>-<a few words> (lower case, digits, hyphens)"
	}
	return ""
}

// Scenario: golden ids are unique, and new ones are named, not numbered.
func TestSortedLists_GoldenIDsAreUniqueAndNamed(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range goldenIDs(t, readRepoFile(t, "docs/product/assistant/golden.jsonl")) {
		if p := goldenIDProblem(id, seen); p != "" {
			t.Errorf("golden id %q: %s", id, p)
		}
		seen[id] = true
	}
	for id, want := range map[string]string{
		"ng-15":                "",
		"ng-consent-analytics": "",
		"ng-16":                "numbered",
		"new-01":               "numbered",
		"how-01":               "used twice",
		"NG_x":                 "not <prefix>",
	} {
		got := goldenIDProblem(id, map[string]bool{"how-01": true})
		if (want == "") != (got == "") || !strings.HasPrefix(got, want) {
			t.Errorf("goldenIDProblem(%q) = %q, want prefix %q", id, got, want)
		}
	}
}
