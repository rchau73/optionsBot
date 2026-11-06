package tests

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"optionsbot/internal/exchange"
	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/strategy"
)

// Scenario: pure refactor (#182) — the strategy depends on the exchange
// boundary itself, not a copy of it: the Deps fields that reach the exchange
// have exactly the internal/exchange interface types (identical types, not
// merely equal method sets), and Deribit's types are what main wires into them.
// The behaviour half of the scenario is every other test in this package,
// unchanged by the refactor.
func TestExchange_PureRefactor_StrategyDependsOnTheBoundary(t *testing.T) {
	deps := reflect.TypeOf(strategy.Deps{})
	for field, want := range map[string]reflect.Type{
		"Exchange": reflect.TypeOf((*exchange.Trading)(nil)).Elem(),
		"Market":   reflect.TypeOf((*exchange.MarketData)(nil)).Elem(),
		"OI":       reflect.TypeOf((*exchange.OpenInterest)(nil)).Elem(),
	} {
		f, ok := deps.FieldByName(field)
		if !ok {
			t.Errorf("strategy.Deps has no field %s", field)
			continue
		}
		if f.Type != want {
			t.Errorf("strategy.Deps.%s is %v, want %v", field, f.Type, want)
		}
	}

	// Deribit is the only adapter: main wires these into those fields.
	_ = strategy.Deps{
		Exchange: (*orders.Executor)(nil),
		Market:   (*marketdata.Manager)(nil),
		OI:       (*gex.Manager)(nil),
	}
}

// Scenario: pure refactor (#182) — marketdata and account read the exchange
// connection through internal/exchange/transport, which the Deribit gateway
// implements, and exchange.Transport is built from the same parts.
func TestExchange_TransportParts(t *testing.T) {
	gw := reflect.TypeOf((*gateway.Gateway)(nil))
	tr := reflect.TypeOf((*exchange.Transport)(nil)).Elem()
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*transport.Caller)(nil)).Elem(),
		reflect.TypeOf((*transport.Streamer)(nil)).Elem(),
	} {
		if !gw.Implements(iface) {
			t.Errorf("gateway.Gateway does not implement transport.%s", iface.Name())
		}
		if !tr.Implements(iface) {
			t.Errorf("exchange.Transport does not include transport.%s", iface.Name())
		}
	}
}

// Scenario: Deribit satisfies the interfaces.
// internal/exchange/deribit.go holds the compile-time checks; this test
// repeats them at run time so the scenario shows up in the test report.
func TestExchange_DeribitSatisfiesTheInterfaces(t *testing.T) {
	impls := []struct {
		name  string
		value any
		ifs   []reflect.Type
	}{
		{"orders.Executor", (*orders.Executor)(nil), []reflect.Type{
			reflect.TypeOf((*exchange.OrderPlacer)(nil)).Elem(),
			reflect.TypeOf((*exchange.OrderTracker)(nil)).Elem(),
			reflect.TypeOf((*exchange.AccountReader)(nil)).Elem(),
			reflect.TypeOf((*exchange.PortfolioSimulator)(nil)).Elem(),
			reflect.TypeOf((*exchange.Trading)(nil)).Elem(),
		}},
		{"marketdata.Manager", (*marketdata.Manager)(nil), []reflect.Type{
			reflect.TypeOf((*exchange.OptionChain)(nil)).Elem(),
			reflect.TypeOf((*exchange.VolIndex)(nil)).Elem(),
			reflect.TypeOf((*exchange.MarketData)(nil)).Elem(),
		}},
		{"gex.Manager", (*gex.Manager)(nil), []reflect.Type{
			reflect.TypeOf((*exchange.OpenInterest)(nil)).Elem(),
		}},
		{"gateway.Gateway", (*gateway.Gateway)(nil), []reflect.Type{
			reflect.TypeOf((*exchange.Transport)(nil)).Elem(),
		}},
	}
	for _, impl := range impls {
		typ := reflect.TypeOf(impl.value)
		for _, iface := range impl.ifs {
			if !typ.Implements(iface) {
				t.Errorf("%s does not implement exchange.%s", impl.name, iface.Name())
			}
		}
	}
}

// The function-shaped parts of the boundary are today's Deribit functions,
// and they still behave as the bot relies on.
func TestExchange_DeribitFunctions(t *testing.T) {
	var parse exchange.OptionNameParser = marketdata.ParseOptionName
	und, expiry, strike, typ, err := parse("BTC-4OCT26-60000-P")
	if err != nil || und != "BTC" || strike != 60000 || typ != "put" || expiry.Day() != 4 {
		t.Errorf("parse daily option name: got %s %v %v %s %v", und, expiry, strike, typ, err)
	}
	if _, _, _, _, err := parse("BTC-PERPETUAL"); err == nil {
		t.Error("a perpetual is not an option name, want an error")
	}

	var matching exchange.RateLimitPool = gateway.IsMatchingEngine
	for method, want := range map[string]bool{
		"private/sell":            true,
		"private/edit":            true,
		"private/cancel_by_label": true,
		"private/get_positions":   false,
		"public/get_instruments":  false,
	} {
		if got := matching(method); got != want {
			t.Errorf("RateLimitPool(%q) = %v, want %v", method, got, want)
		}
	}

	var fee exchange.FeeModel = orders.OptionFee
	if got := fee(0.0003, 0.001, 2); !approxEq(got, 0.00025) {
		t.Errorf("fee capped at 12.5%% of the price: got %v, want 0.00025", got)
	}
	if got := fee(0.0003, 0.01, 2); !approxEq(got, 0.0006) {
		t.Errorf("fee per contract: got %v, want 0.0006", got)
	}
}

func approxEq(a, b float64) bool {
	d := a - b
	return d < 1e-12 && d > -1e-12
}

// capabilityDiffs names each field where got and want differ.
func capabilityDiffs(got, want exchange.Capabilities) []string {
	var diffs []string
	g, w := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := 0; i < g.NumField(); i++ {
		if g.Field(i).Bool() != w.Field(i).Bool() {
			diffs = append(diffs, g.Type().Field(i).Name)
		}
	}
	return diffs
}

// Scenario (#687): Deribit's capabilities are pinned. The pin is a literal of
// its own, so a change to the value needs a change here; the document's copy of
// the type is held to this one by TestExchangeCoupling_ProposedInterfaceCompiles.
func TestExchange_DeribitCapabilitiesArePinned(t *testing.T) {
	pinned := exchange.Capabilities{
		MarginSimulator:     true,
		VolIndex:            true,
		CancelByLabel:       true,
		AmendInPlace:        true,
		CancelAllByCurrency: true,
		PortfolioMargin:     true,
		InverseSettlement:   true,
		MainnetOpenInterest: true,
	}
	for _, field := range capabilityDiffs(exchange.DeribitCapabilities, pinned) {
		t.Errorf("Deribit capability %s changed: update the pin if that is deliberate", field)
	}

	// Every field is covered: flipping any one is reported by its own name.
	typ := reflect.TypeOf(pinned)
	for i := 0; i < typ.NumField(); i++ {
		changed := pinned
		reflect.ValueOf(&changed).Elem().Field(i).SetBool(false)
		if got := capabilityDiffs(changed, pinned); !reflect.DeepEqual(got, []string{typ.Field(i).Name}) {
			t.Errorf("changed %s reported as %v", typ.Field(i).Name, got)
		}
	}
}

// bookCall records the book-summary requests that reach the connection.
type bookCall struct {
	calls  []string // method and params as sent on the wire
	ctxs   []context.Context
	result string
	err    error
}

func (c *bookCall) Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error) {
	wire, err := json.Marshal(params)
	if err != nil {
		return gateway.JSONRPCResponse{}, err
	}
	line := method + " " + string(wire)
	if priority != gateway.PriorityLow {
		line += " NOT LOW PRIORITY"
	}
	c.calls = append(c.calls, line)
	c.ctxs = append(c.ctxs, ctx)
	return gateway.JSONRPCResponse{Result: json.RawMessage(c.result)}, c.err
}

// Scenario (#687): one call site for the book summary — the GEX manager and
// the market recorder send the same request as before, byte for byte, on the
// low-priority lane, and get the rows back.
func TestExchange_OneCallSiteForTheBookSummary(t *testing.T) {
	want := func(currency string) []string {
		return []string{`public/get_book_summary_by_currency {"currency":"` + currency + `","kind":"option"}`}
	}

	gexGW := &bookCall{result: `[{"instrument_name":"BTC-27DEC30-90000-P","open_interest":42,"bid_price":0.1}]`}
	mgr := gex.NewManager(gexGW, gex.Params{Underlying: "BTC"}, 0)
	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gexGW.calls, want("BTC")) {
		t.Errorf("GEX manager sent %q", gexGW.calls)
	}
	if rows, _ := mgr.LatestSummary(); len(rows) != 1 || rows[0].OpenInterest != 42 || rows[0].BidPrice != 0.1 {
		t.Errorf("GEX manager's rows = %+v", rows)
	}

	recGW := &bookCall{result: `[{"instrument_name":"BTC_USDC-27DEC30-90000-P","open_interest":2}]`}
	books := recorder.NewUSDCBooks(recGW)
	if err := books.FetchSummary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recGW.calls, want("USDC")) {
		t.Errorf("recorder sent %q", recGW.calls)
	}
	if _, ok := recGW.ctxs[0].Deadline(); !ok {
		t.Error("the recorder's fetch lost its timeout")
	}
	if rows, _ := books.Book("BTC").LatestSummary(); len(rows) != 1 || rows[0].OpenInterest != 2 {
		t.Errorf("recorder's rows = %+v", rows)
	}
}

// A refused call comes back as it is, so a caller can still read the gateway's
// error, and a reply that is not rows names the currency.
func TestExchange_BookSummaryFailures(t *testing.T) {
	down := errors.New("down")
	if _, err := deribit.NewBookReader(&bookCall{err: down}).BookSummary(context.Background(), "BTC"); !errors.Is(err, down) {
		t.Errorf("refused call: err = %v", err)
	}
	for _, reply := range []string{`{"not":"a list"}`, ``} {
		rows, err := deribit.NewBookReader(&bookCall{result: reply}).BookSummary(context.Background(), "USDC")
		if err == nil || rows != nil || !strings.Contains(err.Error(), "decode USDC book summary") {
			t.Errorf("reply %q: rows %v, err %v", reply, rows, err)
		}
	}
}
