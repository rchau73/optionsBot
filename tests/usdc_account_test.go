package tests

import (
	"context"
	"math"
	"reflect"
	"testing"

	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// #833 (E8, SOL/XRP step 3): a SOL/XRP bot reads its margin from the USDC
// account, simulates under USDC, and at startup, in the kill switch and in
// reconcile touches only its own coin's instruments — never every USDC order
// or position. BTC and ETH send the requests they always did.

// Scenario: startup leaves other coins' orders alone.
func TestUSDCAccount_StartupCancelsOnlyItsCoin(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `[]`}, {result: `[]`}}}
	if err := orders.NewExecutor(fc).CancelAllOrders(context.Background(), "SOL"); err != nil {
		t.Fatal(err)
	}
	if len(fc.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (options, then futures)", len(fc.calls))
	}
	for i, kind := range []string{"option", "future"} {
		c := fc.calls[i]
		want := map[string]any{"currency_pair": "sol_usdc", "kind": kind}
		if c.method != "private/cancel_all_by_currency_pair" || !reflect.DeepEqual(c.params, want) || c.priority != gateway.PriorityHigh {
			t.Errorf("call %d = %s %v prio %d, want cancel_all_by_currency_pair %v high", i, c.method, c.params, c.priority, want)
		}
	}
}

// A failed cancel of one kind still tries the other, and the error says which.
func TestUSDCAccount_CancelTriesBothKinds(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{err: &gateway.RPCError{Code: 10000, Message: "boom"}}, {result: `[]`}}}
	err := orders.NewExecutor(fc).CancelAllOrders(context.Background(), "XRP")
	if err == nil || len(fc.calls) != 2 {
		t.Fatalf("err = %v, calls = %d; want an error after both calls", err, len(fc.calls))
	}
	if fc.calls[0].params["currency_pair"] != "xrp_usdc" {
		t.Errorf("pair = %v, want xrp_usdc", fc.calls[0].params["currency_pair"])
	}
}

// BTC still cancels by its own currency, the request unchanged.
func TestUSDCAccount_BTCCancelUnchanged(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `[]`}}}
	if err := orders.NewExecutor(fc).CancelAllOrders(context.Background(), "BTC"); err != nil {
		t.Fatal(err)
	}
	c := fc.calls[0]
	if len(fc.calls) != 1 || c.method != "private/cancel_all_by_currency" || !reflect.DeepEqual(c.params, map[string]any{"currency": "BTC"}) {
		t.Errorf("calls = %+v", fc.calls)
	}
}

const usdcPositionsJSON = `[
 {"instrument_name":"SOL_USDC-25DEC26-80-C","size":-10,"direction":"sell","kind":"option"},
 {"instrument_name":"XRP_USDC-25DEC26-2d2-P","size":-500,"direction":"sell","kind":"option"},
 {"instrument_name":"SOL_USDC-PERPETUAL","size":3,"direction":"buy","kind":"future"},
 {"instrument_name":"XRP_USDC-PERPETUAL","size":-100,"direction":"sell","kind":"future"}
]`

func rowNames(rows []orders.RawPosition) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.InstrumentName)
	}
	return out
}

// Scenario: reconcile adopts only its coin.
func TestUSDCAccount_PositionsAreOnlyItsCoin(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: usdcPositionsJSON}, {result: usdcPositionsJSON}}}
	exec := orders.NewExecutor(fc)
	opts, err := exec.GetPositions(context.Background(), "SOL")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture mixes kinds on purpose: the filter is by name, the kind is Deribit's.
	if got, want := rowNames(opts), []string{"SOL_USDC-25DEC26-80-C", "SOL_USDC-PERPETUAL"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SOL positions = %v, want %v (never XRP's)", got, want)
	}
	if want := map[string]any{"currency": "USDC", "kind": "option"}; !reflect.DeepEqual(fc.calls[0].params, want) {
		t.Errorf("params = %v, want %v", fc.calls[0].params, want)
	}
	futs, err := exec.GetFuturePositions(context.Background(), "XRP")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rowNames(futs), []string{"XRP_USDC-25DEC26-2d2-P", "XRP_USDC-PERPETUAL"}; !reflect.DeepEqual(got, want) {
		t.Errorf("XRP rows = %v, want %v", got, want)
	}
	if want := map[string]any{"currency": "USDC", "kind": "future"}; !reflect.DeepEqual(fc.calls[1].params, want) {
		t.Errorf("params = %v, want %v", fc.calls[1].params, want)
	}
}

func TestUSDCAccount_OwnRows(t *testing.T) {
	rows := []orders.RawPosition{{InstrumentName: "SOL_USDC-25DEC26-80-C"}, {InstrumentName: "XRP_USDC-25DEC26-2d2-P"},
		{InstrumentName: "SOLX_USDC-25DEC26-1-C"}}
	if got := rowNames(orders.OwnRows("sol", rows)); !reflect.DeepEqual(got, []string{"SOL_USDC-25DEC26-80-C"}) {
		t.Errorf("OwnRows(sol) = %v", got)
	}
	if got := orders.OwnRows("SOL", nil); len(got) != 0 {
		t.Errorf("OwnRows(nil) = %v", got)
	}
	btc := []orders.RawPosition{{InstrumentName: "BTC-25DEC26-100000-C"}, {InstrumentName: "BTC-PERPETUAL"}}
	if got := orders.OwnRows("BTC", btc); !reflect.DeepEqual(got, btc) {
		t.Errorf("OwnRows(BTC) = %v, want every row as given", got)
	}
}

// BTC's position read is the request it always was, every row kept.
func TestUSDCAccount_BTCPositionsUnchanged(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `[{"instrument_name":"BTC-25DEC26-100000-C","size":-1,"direction":"sell"}]`}}}
	rows, err := orders.NewExecutor(fc).GetPositions(context.Background(), "BTC")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	if want := map[string]any{"currency": "BTC", "kind": "option"}; !reflect.DeepEqual(fc.calls[0].params, want) {
		t.Errorf("params = %v, want %v", fc.calls[0].params, want)
	}
}

// Scenario: the margin limit reads USDC figures.
func TestUSDCAccount_MarginReadsUSDC(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"currency":"USDC","initial_margin":400,"maintenance_margin":300,"margin_balance":5000}`}}}
	sum, err := orders.NewExecutor(fc).GetAccountSummary(context.Background(), "SOL")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"currency": "USDC", "extended": true}; !reflect.DeepEqual(fc.calls[0].params, want) {
		t.Errorf("params = %v, want %v", fc.calls[0].params, want)
	}
	u := sum.MarginUsage()
	if u.Unit != "USDC" || math.Abs(u.IMPct()-8) > 1e-9 {
		t.Errorf("usage = %+v (IM %.2f %%), want 8 %% in USDC", u, u.IMPct())
	}
}

// Under cross collateral a SOL bot's margin is the account-wide USD totals, as for any coin.
func TestUSDCAccount_CrossCollateralTotals(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"currency":"USDC","initial_margin":400,"margin_balance":5000,
		"cross_collateral_enabled":true,"total_initial_margin_usd":900,"total_margin_balance_usd":9000}`}}}
	sum, err := orders.NewExecutor(fc).GetAccountSummary(context.Background(), "XRP")
	if err != nil {
		t.Fatal(err)
	}
	if u := sum.MarginUsage(); u.Unit != "USD" || math.Abs(u.IMPct()-10) > 1e-9 {
		t.Errorf("usage = %+v, want 10 %% in USD", u)
	}
}

// A linear coin's summary or simulation in another currency cannot be its margin.
func TestUSDCAccount_AnswerInAnotherCurrencyIsRefused(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"currency":"BTC","initial_margin":0.1,"margin_balance":1}`}}}
	if _, err := orders.NewExecutor(fc).GetAccountSummary(context.Background(), "SOL"); err == nil {
		t.Error("a SOL summary answered in BTC was accepted")
	}
	fc = &fakeCaller{replies: []fakeReply{{result: `{"currency":"BTC","initial_margin":0.1,"margin_balance":1}`}}}
	if _, err := orders.NewExecutor(fc).SimulatePortfolio(context.Background(), "SOL", map[string]float64{"SOL_USDC-25DEC26-80-C": -10}); err == nil {
		t.Error("a SOL simulation answered in BTC was accepted")
	}
}

func TestUSDCAccount_SimulatesUnderUSDC(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"initial_margin":450,"margin_balance":5000}`}}}
	pos := map[string]float64{"SOL_USDC-25DEC26-80-C": -10}
	sum, err := orders.NewExecutor(fc).SimulateAlone(context.Background(), "SOL", pos)
	if err != nil {
		t.Fatal(err)
	}
	c := fc.calls[0]
	if c.method != "private/simulate_portfolio" || c.params["currency"] != "USDC" || !reflect.DeepEqual(c.params["simulated_positions"], pos) {
		t.Errorf("call = %+v", c)
	}
	if sum.Currency != "USDC" || sum.MarginUsage().Unit != "USDC" {
		t.Errorf("currency = %q, want USDC (filled in when the answer has none)", sum.Currency)
	}
}

// BTC's summary and simulation requests are unchanged, the currency filled in as before.
func TestUSDCAccount_BTCMarginUnchanged(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"initial_margin":0.1,"margin_balance":1}`}, {result: `{"currency":"BTC","initial_margin":0.1}`}}}
	exec := orders.NewExecutor(fc)
	sim, err := exec.SimulatePortfolio(context.Background(), "BTC", map[string]float64{"BTC-25DEC26-100000-C": -0.1})
	if err != nil || sim.Currency != "BTC" {
		t.Fatalf("sim = %+v, err = %v", sim, err)
	}
	if _, err := exec.GetAccountSummary(context.Background(), "BTC"); err != nil {
		t.Fatal(err)
	}
	if fc.calls[0].params["currency"] != "BTC" || !reflect.DeepEqual(fc.calls[1].params, map[string]any{"currency": "BTC", "extended": true}) {
		t.Errorf("calls = %+v", fc.calls)
	}
}

func TestUSDCAccount_CancelByLabelUnderUSDC(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `1`}, {result: `1`}}}
	exec := orders.NewExecutor(fc)
	if _, err := exec.CancelByLabel(context.Background(), "SOL", "short-strangle:30d:0.16:o-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.CancelByLabel(context.Background(), "ETH", "short-strangle:30d:0.16:o-2"); err != nil {
		t.Fatal(err)
	}
	if fc.calls[0].params["currency"] != "USDC" || fc.calls[1].params["currency"] != "ETH" {
		t.Errorf("currencies = %v, %v; want USDC, ETH", fc.calls[0].params["currency"], fc.calls[1].params["currency"])
	}
}
