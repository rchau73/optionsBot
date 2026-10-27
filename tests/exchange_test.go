package tests

import (
	"reflect"
	"testing"

	"optionsbot/internal/exchange"
	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
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
