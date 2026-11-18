package tests

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/orders"
)

// perpetualLiterals returns "file:line" for every string literal under root
// that holds Deribit's perpetual suffix. Comments are not literals.
func perpetualLiterals(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "-PERPETUAL") {
				found = append(found, fset.Position(lit.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return found
}

// Scenario (#786): the strategy names no exchange instrument — no string
// literal "-PERPETUAL" appears under internal/strategy.
func TestExchangeCoupling_StrategyNamesNoExchangeInstrument(t *testing.T) {
	for _, at := range perpetualLiterals(t, "../internal/strategy") {
		t.Errorf("%s: a Deribit instrument name in internal/strategy — ask the exchange (PerpetualName)", at)
	}
}

// Scenario (#786): … and a test fails if one is added — a planted literal is
// found, a comment is not.
func TestExchangeCoupling_PerpetualLiteralIsFound(t *testing.T) {
	root := t.TempDir()
	src := "package strategy\n\n// named \"BTC-PERPETUAL\" in a comment only\nfunc perp(c string) string {\n\treturn c + \"-PERPETUAL\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := perpetualLiterals(t, root); len(got) != 1 || !strings.Contains(got[0], "x.go:5") {
		t.Errorf("planted literal: got %v, want one at x.go:5", got)
	}
}

// Scenario (#786): Deribit's perpetual is BTC-PERPETUAL for BTC and
// ETH-PERPETUAL for ETH, through the executor and the adapter alike; a linear
// coin's is its USDC perpetual (#838).
func TestExchangeAdapter_PerpetualName(t *testing.T) {
	a := deribit.NewAdapter(testGatewayConfig(), nil)
	for currency, want := range map[string]string{"BTC": "BTC-PERPETUAL", "ETH": "ETH-PERPETUAL",
		"SOL": "SOL_USDC-PERPETUAL", "XRP": "XRP_USDC-PERPETUAL"} {
		if got := a.PerpetualName(currency); got != want {
			t.Errorf("adapter PerpetualName(%s) = %q, want %q", currency, got, want)
		}
		if got := orders.NewExecutor(nil).PerpetualName(currency); got != want {
			t.Errorf("executor PerpetualName(%s) = %q, want %q", currency, got, want)
		}
	}
}

// Scenario (#786): the daily-close history is unchanged — the request for
// the adapter's perpetual is the chart-data read of <COIN>-PERPETUAL, as the
// strategy sent before it asked the adapter for the name.
func TestExchangeAdapter_DailyClosesAskForThePerpetual(t *testing.T) {
	for _, currency := range []string{"BTC", "ETH"} {
		m := newMockDeribit(t)
		g := connectGateway(t, m, testGatewayConfig())
		m.mu.Lock()
		skip := len(m.received)
		m.mu.Unlock()

		a := deribit.NewAdapter(testGatewayConfig(), g)
		_, _ = a.GetDailyCloses(context.Background(), a.PerpetualName(currency), 23)

		lines := wireLines(t, m, skip)
		m.close()
		want := fmt.Sprintf(`"instrument_name":"%s-PERPETUAL"`, currency)
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "public/get_tradingview_chart_data ") || !strings.Contains(lines[0], want) {
			t.Errorf("%s: requests %q, want one chart-data read with %s", currency, lines, want)
		}
	}
}

// recordingHedge keeps the instrument each hedge report suggested.
type recordingHedge struct {
	mu    sync.Mutex
	insts []string
}

func (r *recordingHedge) MaybeReport(_, _ float64, inst string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insts = append(r.insts, inst)
}

func (r *recordingHedge) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.insts) == 0 {
		return ""
	}
	return r.insts[len(r.insts)-1]
}

// Scenario (#786): the strategy takes the perpetual's name from the exchange
// — the daily-close seed and the hedge report use the name the exchange
// gives (Deribit's by default; any other name an exchange gives, too).
func TestStrategy_PerpetualNameComesFromTheExchange(t *testing.T) {
	for _, tc := range []struct {
		suffix, want string
	}{
		{"", "BTC-PERPETUAL"},
		{"-PERP-OTHER", "BTC-PERP-OTHER"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.exch.perpSuffix = tc.suffix
			rec := &recordingHedge{}
			f.hedge = rec
			f.startRun()

			eventually(t, 2*time.Second, "a hedge report", func() bool { return rec.last() != "" })
			if got := rec.last(); got != tc.want {
				t.Errorf("hedge report instrument %q, want %q", got, tc.want)
			}
			f.exch.mu.Lock()
			asked := append([]string(nil), f.exch.dailyClosesFor...)
			f.exch.mu.Unlock()
			if len(asked) != 1 || asked[0] != tc.want {
				t.Errorf("daily closes asked for %v, want [%s]", asked, tc.want)
			}
		})
	}
}
