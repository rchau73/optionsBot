package tests

import (
	"reflect"
	"testing"

	"optionsbot/internal/exchange"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The exchange boundary (#181) must describe exactly what the strategy asks:
// each pair below is assignable both ways, so the method sets are equal.
// A method added to the strategy's Deps without the boundary (or the other
// way round) fails the build.
var (
	_ strategy.Exchange = exchange.Trading(nil)
	_ exchange.Trading  = strategy.Exchange(nil)

	_ strategy.MarketData = exchange.MarketData(nil)
	_ exchange.MarketData = strategy.MarketData(nil)

	_ strategy.OISource     = exchange.OpenInterest(nil)
	_ exchange.OpenInterest = strategy.OISource(nil)
)

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
