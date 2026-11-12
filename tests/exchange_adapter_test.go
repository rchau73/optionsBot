package tests

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"optionsbot/internal/exchange"
	"optionsbot/internal/exchange/deribit"
	gw "optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Scenario (#761): the adapter satisfies the interface.
// internal/exchange/deribit.go holds the compile-time assertion; this repeats
// it at run time, with the strategy's own futures interface.
func TestExchangeAdapter_SatisfiesTheInterface(t *testing.T) {
	adapter := reflect.TypeOf((*deribit.Adapter)(nil))
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*exchange.Exchange)(nil)).Elem(),
		reflect.TypeOf((*exchange.Connection)(nil)).Elem(),
		reflect.TypeOf((*exchange.Trading)(nil)).Elem(),
		reflect.TypeOf((*exchange.MarketData)(nil)).Elem(),
		reflect.TypeOf((*strategy.FuturesTrader)(nil)).Elem(),
	} {
		if !adapter.Implements(iface) {
			t.Errorf("*deribit.Adapter does not implement %v", iface)
		}
	}
	_ = strategy.Deps{Exchange: (*deribit.Adapter)(nil), Market: (*deribit.Adapter)(nil), Futures: (*deribit.Adapter)(nil)}
}

// methodNames lists the exported methods of typ.
func methodNames(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// Scenario (#761): behaviour is unchanged — the adapter has no method of its
// own: its method set is exactly the gateway's, the executor's and the
// manager's, with no name shared (an ambiguous name would silently drop out
// of the set), so every call it takes is the composed type's own.
func TestExchangeAdapter_AddsNoMethodOfItsOwn(t *testing.T) {
	seen := map[string]string{}
	var union []string
	for _, part := range []reflect.Type{
		reflect.TypeOf((*gw.Gateway)(nil)),
		reflect.TypeOf((*orders.Executor)(nil)),
		reflect.TypeOf((*marketdata.Manager)(nil)),
	} {
		for _, name := range methodNames(part) {
			if owner, dup := seen[name]; dup {
				t.Errorf("%s is a method of both %s and %v: the adapter would not promote it", name, owner, part)
			}
			seen[name] = part.String()
			union = append(union, name)
		}
	}
	sort.Strings(union)
	if got := methodNames(reflect.TypeOf((*deribit.Adapter)(nil))); !reflect.DeepEqual(got, union) {
		t.Errorf("adapter methods\n got %v\nwant %v", got, union)
	}

	g := gw.New(testGatewayConfig())
	a := deribit.NewAdapter(testGatewayConfig(), g)
	if a.Gateway != g || a.Executor == nil || a.Manager == nil {
		t.Fatalf("adapter not composed over the given gateway: %+v", a)
	}
}

// wireLines returns method and params of every request m received after skip.
func wireLines(t *testing.T, m *mockDeribit, skip int) []string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	var lines []string
	for _, req := range m.received[skip:] {
		params, err := json.Marshal(req.Params)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, req.Method+" "+string(params))
	}
	return lines
}

// Scenario (#761): behaviour is unchanged — the same scripted cycle of order,
// account and margin calls sends the same requests, in the same order, through
// the adapter as through the executor main wired before.
func TestExchangeAdapter_SameRequestsAsTheExecutor(t *testing.T) {
	script := func(ex exchange.Trading) {
		ctx := context.Background()
		order := orders.Order{Instrument: "BTC-27DEC30-90000-P", Direction: "sell", OrderType: "limit",
			Qty: 0.2, LimitPrice: 0.0125, TickSize: 0.0005, Label: "s1-30d-0.15-entry-x"}
		_, _ = ex.Submit(ctx, order)
		_, _ = ex.GetOrderState(ctx, "ord-1")
		_ = ex.AmendOrder(ctx, "ord-1", 0.2, 0.012)
		_ = ex.Cancel(ctx, "ord-1")
		_, _ = ex.CancelByLabel(ctx, "BTC", order.Label)
		_, _ = ex.GetPositions(ctx, "BTC")
		_, _ = ex.GetAccountSummary(ctx, "BTC")
		_, _ = ex.SimulatePortfolio(ctx, "BTC", map[string]float64{order.Instrument: -0.2})
	}
	run := func(build func(*gw.Gateway) exchange.Trading) []string {
		m := newMockDeribit(t)
		defer m.close()
		g := connectGateway(t, m, testGatewayConfig())
		m.mu.Lock()
		skip := len(m.received)
		m.mu.Unlock()
		script(build(g))
		return wireLines(t, m, skip)
	}

	before := run(func(g *gw.Gateway) exchange.Trading { return orders.NewExecutor(g) })
	after := run(func(g *gw.Gateway) exchange.Trading { return deribit.NewAdapter(testGatewayConfig(), g) })
	if len(before) < 8 {
		t.Fatalf("the script sent only %d requests: %q", len(before), before)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("requests changed\nbefore %q\n after %q", before, after)
	}
}

// Scenario (#761): main wires from the adapter — cmd/bot builds the strategy's
// exchange, market data and futures from one deribit.NewAdapter value, and no
// longer builds an executor or a market-data manager of its own.
func TestExchangeCoupling_MainWiresTheStrategyFromTheAdapter(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../cmd/bot/main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	adapterVars := map[string]bool{}
	deps := map[string]string{} // Deps field → the identifier main passes
	var banned []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 && callee(n.Rhs[0]) == "deribit.NewAdapter" {
				if id, ok := n.Lhs[0].(*ast.Ident); ok {
					adapterVars[id.Name] = true
				}
			}
		case *ast.CallExpr:
			if c := callee(n); c == "orders.NewExecutor" || c == "marketdata.New" {
				banned = append(banned, fset.Position(n.Pos()).String()+" "+c)
			}
		case *ast.CompositeLit:
			if sel, ok := n.Type.(*ast.SelectorExpr); ok && exprName(sel) == "strategy.Deps" {
				for _, el := range n.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if id, ok := kv.Value.(*ast.Ident); ok {
						deps[kv.Key.(*ast.Ident).Name] = id.Name
					}
				}
			}
		}
		return true
	})
	if len(adapterVars) != 1 {
		t.Fatalf("want one deribit.NewAdapter value in main, found %v", adapterVars)
	}
	for _, field := range []string{"Exchange", "Market", "Futures"} {
		if !adapterVars[deps[field]] {
			t.Errorf("strategy.Deps.%s is %q, want the deribit.NewAdapter value", field, deps[field])
		}
	}
	for _, b := range banned {
		t.Errorf("%s: main builds a Deribit part outside the adapter", b)
	}
}

// callee names a call's function as pkg.Func ("" when it is not one).
func callee(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	return exprName(call.Fun)
}

func exprName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if x, ok := sel.X.(*ast.Ident); ok {
		return x.Name + "." + sel.Sel.Name
	}
	return ""
}

// gatewayImporters are the packages under internal/ that import the gateway
// today, each with its reason (#761's metric: non-adapter packages importing
// Deribit-specific code). The list may only shrink: a new importer fails, and
// a package that stops importing it must leave the list. The target is
// exchange/deribit alone (with cmd/bot), after the later steps and the
// decisions of docs/exchange-adapter.md §4–§5.
var gatewayImporters = map[string]string{
	"accounts":           "the account registry checks each key's scope over its own connection (#585)",
	"api":                "the monitor reads ConnStats and the key scope",
	"exchange":           "the boundary names gateway.ConnStats",
	"exchange/deribit":   "the adapter",
	"exchange/transport": "the connection's parts name the gateway's wire types",
	"keystore":           "reads gateway.KeyScope",
	"marketdata":         "decodes the gateway's push messages",
	"orders":             "the executor: every order, account and margin call (moves at a later step)",
}

// importsOf returns, per package directory under root, the module packages
// its non-test files import.
func importsOf(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := filepath.ToSlash(rel)
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !slices.Contains(out[pkg], p) {
				out[pkg] = append(out[pkg], p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Scenario (#761): the strategy sees only the interface — no package under
// internal/strategy imports the gateway, the adapter or the transport, or
// names Deribit's executor; and the gateway's importers are the pinned list.
// The strategy still imports internal/orders for the bot's own data types
// (Order, Fill, StateManager, the journal): those are not Deribit's.
func TestExchangeCoupling_StrategySeesOnlyTheInterface(t *testing.T) {
	imports := importsOf(t, "../internal")
	for pkg, imps := range imports {
		if pkg != "strategy" && !strings.HasPrefix(pkg, "strategy/") {
			continue
		}
		for _, banned := range []string{"optionsbot/internal/gateway", "optionsbot/internal/exchange/deribit", "optionsbot/internal/exchange/transport"} {
			if slices.Contains(imps, banned) {
				t.Errorf("internal/%s imports %s", pkg, banned)
			}
		}
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir("../internal/strategy", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if name := exprName(sel); name == "orders.Executor" || name == "orders.NewExecutor" {
					t.Errorf("%s: %s in internal/strategy — depend on exchange.Trading", fset.Position(sel.Pos()), name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for pkg, imps := range imports {
		if slices.Contains(imps, "optionsbot/internal/gateway") {
			got = append(got, pkg)
			if _, ok := gatewayImporters[pkg]; !ok {
				t.Errorf("internal/%s imports the gateway: go through internal/exchange/deribit, or add it to gatewayImporters with a reason", pkg)
			}
		}
	}
	for pkg := range gatewayImporters {
		if !slices.Contains(got, pkg) {
			t.Errorf("internal/%s no longer imports the gateway: remove it from gatewayImporters", pkg)
		}
	}
}
