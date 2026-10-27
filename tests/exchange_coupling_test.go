package tests

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The Deribit coupling inventory (#253, #325): every "public/…" and
// "private/…" string literal under internal/ must be listed here with the
// packages allowed to use it, and in docs/exchange-adapter.md section 1.
// A new Deribit call is added to both tables in the same PR.
//
// Keys ending in "/" or "_" are the gateway's prefix rules (retry,
// rate-limit pool, public-only refusal), not methods.
var deribitMethods = map[string][]string{
	"public/auth":                         {"gateway"},
	"public/set_heartbeat":                {"gateway"},
	"public/test":                         {"gateway"},
	"public/subscribe":                    {"gateway"},
	"public/get_instruments":              {"marketdata"},
	"public/get_volatility_index_data":    {"marketdata"},
	"public/get_book_summary_by_currency": {"gex", "recorder"},
	"public/get_tradingview_chart_data":   {"orders"},
	"private/buy":                         {"gateway", "orders"},
	"private/sell":                        {"gateway", "orders"},
	"private/edit":                        {"gateway", "orders"},
	"private/close_position":              {"gateway"},
	"private/cancel":                      {"gateway", "orders"},
	"private/cancel_by_label":             {"orders"},
	"private/cancel_all_by_currency":      {"orders"},
	"private/cancel_all_by_instrument":    {"orders"},
	"private/get_order_state":             {"orders"},
	"private/get_user_trades_by_order":    {"orders"},
	"private/get_positions":               {"orders", "account"},
	"private/get_account_summary":         {"orders", "account"},
	"private/get_account_summaries":       {"account"},
	"private/simulate_portfolio":          {"orders"},
	"private/get_margins":                 {"orders"},
	"public/":                             {"gateway"},
	"private/":                            {"gateway"},
	"private/get_":                        {"gateway"},
}

const couplingDoc = "../docs/exchange-adapter.md"

// rpcLiteral matches the method at the start of a string literal; a log
// message such as "private/get_account_summaries unavailable; …" counts as
// its method.
var rpcLiteral = regexp.MustCompile(`^(public|private)/[a-z_]*`)

// rpcUse is one "public/…"/"private/…" literal found in the code.
type rpcUse struct {
	method string
	pkg    string // directory under the scanned root, e.g. "orders"
	file   string // path relative to the root
}

// scanRPCLiterals parses every .go file under root and returns its RPC
// method literals. Comments are not literals, so they never count.
func scanRPCLiterals(t *testing.T, root string) []rpcUse {
	t.Helper()
	var uses []rpcUse
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		pkg := filepath.ToSlash(filepath.Dir(rel))
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if m := rpcLiteral.FindString(s); m != "" {
				uses = append(uses, rpcUse{method: m, pkg: pkg, file: rel})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return uses
}

// couplingProblems compares the literals found under root with table and
// returns one line per problem, naming the method and the file.
func couplingProblems(uses []rpcUse, table map[string][]string) []string {
	var problems []string
	seen := map[string]map[string]bool{}
	for _, u := range uses {
		if u.pkg == "strategy" || strings.HasPrefix(u.pkg, "strategy/") {
			problems = append(problems, fmt.Sprintf(
				"%s: Deribit method %q in internal/strategy — strategy code must go through the interfaces in deps.go", u.file, u.method))
			continue
		}
		allowed, ok := table[u.method]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: Deribit method %q is not in the coupling inventory — add it to docs/exchange-adapter.md §1 and deribitMethods", u.file, u.method))
			continue
		}
		if !slices.Contains(allowed, u.pkg) {
			problems = append(problems, fmt.Sprintf(
				"%s: Deribit method %q used in package %s; the inventory allows %v — update docs/exchange-adapter.md §1 and deribitMethods", u.file, u.method, u.pkg, allowed))
			continue
		}
		if seen[u.method] == nil {
			seen[u.method] = map[string]bool{}
		}
		seen[u.method][u.pkg] = true
	}
	for method, pkgs := range table {
		for _, p := range pkgs {
			if !seen[method][p] {
				problems = append(problems, fmt.Sprintf(
					"inventory lists %q in package %s but no literal is there any more — update docs/exchange-adapter.md §1 and deribitMethods", method, p))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// Scenario (#253): the inventory is complete today.
// Scenario (#325): a grep of "public/" and "private/" in internal/ finds no
// method missing from the document.
func TestExchangeCoupling_InventoryIsComplete(t *testing.T) {
	for _, p := range couplingProblems(scanRPCLiterals(t, "../internal"), deribitMethods) {
		t.Error(p)
	}

	doc, err := os.ReadFile(couplingDoc)
	if err != nil {
		t.Fatal(err)
	}
	for method := range deribitMethods {
		if !strings.Contains(string(doc), "`"+method+"`") && !strings.Contains(string(doc), "`\""+method+"\"`") {
			t.Errorf("%s does not list %q", couplingDoc, method)
		}
	}
}

// Scenario (#325): every method of the gateway's call sites appears once
// with its package in the inventory table.
func TestExchangeCoupling_EachMethodHasOneRow(t *testing.T) {
	doc, err := os.ReadFile(couplingDoc)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	row := regexp.MustCompile("(?m)^\\| `((?:public|private)/[a-z_]+)` \\| ([^|]+) \\|")
	for _, m := range row.FindAllStringSubmatch(string(doc), -1) {
		rows[m[1]]++
		for _, pkg := range deribitMethods[m[1]] {
			if !strings.Contains(m[2], pkg+" /") {
				t.Errorf("%s: row %q does not name package %s", couplingDoc, m[1], pkg)
			}
		}
	}
	for method := range deribitMethods {
		if strings.HasSuffix(method, "/") || strings.HasSuffix(method, "_") {
			continue // prefix rules are listed in the prose under the table
		}
		if rows[method] != 1 {
			t.Errorf("%s: %q has %d rows in the inventory table, want 1", couplingDoc, method, rows[method])
		}
	}
}

// writeGo writes a one-literal Go file under root/pkg.
func writeGo(t *testing.T, root, pkg, literal string) {
	t.Helper()
	dir := filepath.Join(root, pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf("package %s\n\nconst method = %q\n", filepath.Base(pkg), literal)
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Scenario (#253): a new Deribit call fails the test until it is listed.
func TestExchangeCoupling_NewCallFailsUntilListed(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "account", "private/get_transaction_log")
	table := map[string][]string{}

	problems := couplingProblems(scanRPCLiterals(t, root), table)
	if len(problems) != 1 {
		t.Fatalf("want 1 problem, got %v", problems)
	}
	if !strings.Contains(problems[0], "private/get_transaction_log") || !strings.Contains(problems[0], "account/x.go") {
		t.Errorf("problem must name the method and the file: %s", problems[0])
	}

	table["private/get_transaction_log"] = []string{"account"}
	if p := couplingProblems(scanRPCLiterals(t, root), table); len(p) != 0 {
		t.Errorf("listed method still fails: %v", p)
	}

	// The same method in another package fails until that package is allowed.
	writeGo(t, root, "recorder", "private/get_transaction_log")
	if p := couplingProblems(scanRPCLiterals(t, root), table); len(p) != 1 || !strings.Contains(p[0], "recorder/x.go") {
		t.Errorf("method in an unlisted package: %v", p)
	}
}

// Scenario (#253): the strategy stays exchange-neutral.
func TestExchangeCoupling_StrategyHasNoDeribitLiteral(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "strategy", "private/get_positions")
	// Even a listed method is refused in the strategy.
	table := map[string][]string{"private/get_positions": {"strategy"}}

	problems := couplingProblems(scanRPCLiterals(t, root), table)
	found := false
	for _, p := range problems {
		if strings.Contains(p, "strategy/x.go") && strings.Contains(p, "deps.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("a private/ literal under strategy must fail, naming deps.go: %v", problems)
	}
}

// Comments are not calls: a method named only in a comment is not coupling.
func TestExchangeCoupling_CommentsDoNotCount(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "orders")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package orders\n\n// calls \"private/get_transaction_log\" one day\nconst x = 1\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if uses := scanRPCLiterals(t, root); len(uses) != 0 {
		t.Errorf("comment counted as a literal: %v", uses)
	}
}

// Scenario (#325): the interface is checkable — the proposed interface in the
// document's Go block compiles against today's types (deps.go included).
func TestExchangeCoupling_ProposedInterfaceCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	doc, err := os.ReadFile(couplingDoc)
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllSubmatch(doc, -1)
	if len(blocks) != 1 {
		t.Fatalf("%s: want exactly one Go block (the proposed interface), found %d", couplingDoc, len(blocks))
	}
	// Inside the module so its imports resolve; a leading "_" keeps it out
	// of ./... for any other command running meanwhile.
	dir, err := os.MkdirTemp(".", "_adapterproposal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "proposal.go"), blocks[0][1], 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "vet", "./"+filepath.ToSlash(dir)).CombinedOutput()
	if err != nil {
		t.Fatalf("the proposed interface in %s does not compile against today's types:\n%s", couplingDoc, out)
	}
}
