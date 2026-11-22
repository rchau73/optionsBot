package tests

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
)

// #838 (E8, SOL/XRP step 1): the bot reads USDC-settled option names and
// loads a linear coin's chain and book summary from Deribit's currency=USDC,
// keeping only its own names; BTC and ETH requests do not change.

// Scenario: USDC-settled option names are understood.
func TestUSDCNames_AreUnderstood(t *testing.T) {
	nov27 := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	for name, want := range map[string]marketdata.Instrument{
		"SOL_USDC-27NOV26-110-C":  {Underlying: "SOL", Settlement: "USDC", Strike: 110, OptionType: "call", Expiry: nov27},
		"XRP_USDC-27NOV26-1d15-P": {Underlying: "XRP", Settlement: "USDC", Strike: 1.15, OptionType: "put", Expiry: nov27},
		"SOL_USDC-25DEC26-80-C":   {Underlying: "SOL", Settlement: "USDC", Strike: 80, OptionType: "call", Expiry: nov27.AddDate(0, 0, 28)},
		"BTC-27NOV26-110000-C":    {Underlying: "BTC", Settlement: "BTC", Strike: 110000, OptionType: "call", Expiry: nov27},
	} {
		got, err := marketdata.InstrumentFromName(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Underlying != want.Underlying || got.Settlement != want.Settlement || got.Strike != want.Strike ||
			got.OptionType != want.OptionType || !got.Expiry.Equal(want.Expiry) {
			t.Errorf("%s → %+v, want %+v", name, got, want)
		}
	}
}

func TestUSDCNames_SettlementAndPrefix(t *testing.T) {
	for _, c := range []struct {
		coin, settlement, prefix string
		linear                   bool
	}{
		{"BTC", "BTC", "", false},
		{"ETH", "ETH", "", false},
		{"SOL", "USDC", "SOL_USDC-", true},
		{"XRP", "USDC", "XRP_USDC-", true},
		{"sol", "USDC", "SOL_USDC-", true}, // the config's case does not matter
		{"USDC", "USDC", "", false},        // the whole USDC list, as the recorder reads it
	} {
		if got := marketdata.IsLinear(c.coin); got != c.linear {
			t.Errorf("IsLinear(%s) = %v", c.coin, got)
		}
		if got := marketdata.SettlementCurrency(c.coin); got != c.settlement {
			t.Errorf("SettlementCurrency(%s) = %q, want %q", c.coin, got, c.settlement)
		}
		if c.linear {
			if got := marketdata.OptionPrefix(c.coin); got != c.prefix {
				t.Errorf("OptionPrefix(%s) = %q, want %q", c.coin, got, c.prefix)
			}
		}
	}
	for part, want := range map[string][2]string{"SOL_USDC": {"SOL", "USDC"}, "BTC": {"BTC", "BTC"}, "": {"", ""}} {
		if coin, settlement := marketdata.SplitUnderlying(part); coin != want[0] || settlement != want[1] {
			t.Errorf("SplitUnderlying(%q) = %q, %q, want %q", part, coin, settlement, want)
		}
	}
}

// usdcInstrumentsJSON is Deribit's currency=USDC option list: every linear
// coin's options together, each with its own contract size.
func usdcInstrumentsJSON(exp time.Time) string {
	rows := []string{}
	for _, o := range []struct {
		name     string
		strike   float64
		contract float64
	}{
		{"BTC_USDC-25DEC26-96000-C", 96000, 0.01},
		{"ETH_USDC-25DEC26-3200-P", 3200, 0.1},
		{"SOL_USDC-25DEC26-80-C", 80, 10},
		{"SOL_USDC-25DEC26-80-P", 80, 10},
		{"XRP_USDC-25DEC26-2d2-P", 2.2, 1000},
	} {
		typ := "call"
		if strings.HasSuffix(o.name, "-P") {
			typ = "put"
		}
		rows = append(rows, fmt.Sprintf(
			`{"instrument_name":%q,"strike":%v,"expiration_timestamp":%d,"option_type":%q,"settlement_currency":"USDC","contract_size":%v,"tick_size":0.01,"min_trade_amount":1}`,
			o.name, o.strike, exp.UnixMilli(), typ, o.contract))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// Scenario: a SOL chain holds only SOL options.
func TestUSDCChain_SOLChainHoldsOnlySOLOptions(t *testing.T) {
	for _, c := range []struct {
		coin     string
		names    []string
		contract float64
	}{
		{"SOL", []string{"SOL_USDC-25DEC26-80-C", "SOL_USDC-25DEC26-80-P"}, 10},
		{"XRP", []string{"XRP_USDC-25DEC26-2d2-P"}, 1000},
	} {
		gw := newFakeFeedGateway()
		gw.results["public/get_instruments"] = usdcInstrumentsJSON(time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC))
		got, err := deribit.NewChainSource(gw).Instruments(context.Background(), c.coin)
		if err != nil {
			t.Fatal(err)
		}
		want := feedCall{"public/get_instruments", map[string]any{"currency": "USDC", "kind": "option", "expired": false}, gateway.PriorityLow}
		if len(gw.calls) != 1 || !reflect.DeepEqual(gw.calls[0], want) {
			t.Errorf("%s: requests %+v, want %+v", c.coin, gw.calls, want)
		}
		var names []string
		for _, inst := range got {
			names = append(names, inst.Name)
			if inst.Underlying != c.coin || inst.Settlement != "USDC" || inst.ContractSize != c.contract || inst.TickSize != 0.01 {
				t.Errorf("%s: %s is %s settled in %s, contract %v, tick %v; want %s, USDC, %v, 0.01",
					c.coin, inst.Name, inst.Underlying, inst.Settlement, inst.ContractSize, inst.TickSize, c.coin, c.contract)
			}
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, c.names) {
			t.Errorf("%s chain = %v, want %v", c.coin, names, c.names)
		}
	}
}

// A SOL manager loads only SOL's options and subscribes to SOL's USDC index
// and SOL's tickers.
func TestUSDCChain_SOLManagerSubscribesToItsOwnTickers(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = usdcInstrumentsJSON(time.Now().Add(24 * 24 * time.Hour))
	cfg := newMarketCfg()
	cfg.Underlying = "SOL"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newFeedManager(cfg, gw)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(m.AllInstruments()); n != 2 {
		t.Errorf("SOL manager holds %d instruments, want its 2", n)
	}
	subs := gw.subscriptions()
	for _, want := range []string{"deribit_price_index.sol_usdc", "ticker.SOL_USDC-25DEC26-80-C.100ms", "ticker.SOL_USDC-25DEC26-80-P.100ms"} {
		if !strings.Contains(strings.Join(subs, " "), want) {
			t.Errorf("missing %s in %q", want, subs)
		}
	}
	for _, ch := range subs {
		if strings.HasPrefix(ch, "ticker.") && !strings.HasPrefix(ch, "ticker.SOL_USDC-") {
			t.Errorf("a SOL bot subscribed to another coin's ticker: %s", ch)
		}
	}
}

// A linear coin's book summary is the USDC list, its own rows only; the whole
// USDC list (the recorder) and BTC/ETH are read as before.
func TestUSDCChain_BookSummaryOfALinearCoin(t *testing.T) {
	reply := `[{"instrument_name":"BTC_USDC-25DEC26-96000-C","open_interest":1},{"instrument_name":"SOL_USDC-25DEC26-80-C","open_interest":7},` +
		`{"instrument_name":"XRP_USDC-25DEC26-2d2-P","open_interest":9}]`
	for _, c := range []struct {
		currency, sent string
		rows           []string
	}{
		{"SOL", "USDC", []string{"SOL_USDC-25DEC26-80-C"}},
		{"XRP", "USDC", []string{"XRP_USDC-25DEC26-2d2-P"}},
		{"USDC", "USDC", []string{"BTC_USDC-25DEC26-96000-C", "SOL_USDC-25DEC26-80-C", "XRP_USDC-25DEC26-2d2-P"}},
	} {
		gw := &bookCall{result: reply}
		rows, err := deribit.NewBookReader(gw).BookSummary(context.Background(), c.currency)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{`public/get_book_summary_by_currency {"currency":"` + c.sent + `","kind":"option"}`}
		if !reflect.DeepEqual(gw.calls, want) {
			t.Errorf("%s: sent %q, want %q", c.currency, gw.calls, want)
		}
		var names []string
		for _, r := range rows {
			names = append(names, r.InstrumentName)
		}
		if !reflect.DeepEqual(names, c.rows) {
			t.Errorf("%s: rows %v, want %v", c.currency, names, c.rows)
		}
	}
}

// Scenario: BTC and ETH requests do not change — the chain, the book summary
// and the index channel are today's for both coins, and their instruments
// settle in the coin with a contract of one coin.
func TestUSDCChain_BTCAndETHRequestsDoNotChange(t *testing.T) {
	for _, coin := range []string{"BTC", "ETH"} {
		gw := newFakeFeedGateway()
		gw.results["public/get_instruments"] = strings.ReplaceAll(instrumentsJSON(time.Now(), 24), "BTC-", coin+"-")
		got, err := deribit.NewChainSource(gw).Instruments(context.Background(), coin)
		if err != nil {
			t.Fatal(err)
		}
		want := feedCall{"public/get_instruments", map[string]any{"currency": coin, "kind": "option", "expired": false}, gateway.PriorityLow}
		if len(gw.calls) != 1 || !reflect.DeepEqual(gw.calls[0], want) {
			t.Errorf("%s chain requests %+v, want %+v", coin, gw.calls, want)
		}
		if len(got) != 2 {
			t.Fatalf("%s: %d instruments, want every one of the list (2)", coin, len(got))
		}
		for _, inst := range got {
			if inst.Settlement != coin || inst.ContractSize != 1 {
				t.Errorf("%s: %s settles in %q, contract %v; want %s, 1", coin, inst.Name, inst.Settlement, inst.ContractSize, coin)
			}
		}
		if ch := deribit.NewChainSource(gw).IndexChannel(coin); ch != "deribit_price_index."+strings.ToLower(coin)+"_usd" {
			t.Errorf("%s index channel %q", coin, ch)
		}

		books := &bookCall{result: `[{"instrument_name":"` + coin + `-25DEC26-3000-P"}]`}
		rows, err := deribit.NewBookReader(books).BookSummary(context.Background(), coin)
		if err != nil || len(rows) != 1 {
			t.Errorf("%s book summary: %v %v", coin, rows, err)
		}
		if w := []string{`public/get_book_summary_by_currency {"currency":"` + coin + `","kind":"option"}`}; !reflect.DeepEqual(books.calls, w) {
			t.Errorf("%s book summary sent %q, want %q", coin, books.calls, w)
		}
	}
}
