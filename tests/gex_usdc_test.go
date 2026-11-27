package tests

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/strategy"
)

// #835: SOL's and XRP's GEX come from the coin's USDC option book on mainnet
// (dollar gamma, the reference method); the liquidity cap reads the same open
// interest. BTC and ETH are unchanged.

// usdcGEXFixture is a recorded mainnet USDC book summary (2026-11-18 00:01
// UTC): SOL_USDC on its 5 nearest expiries, plus XRP_USDC and BTC_USDC rows
// with the most open interest on them, as the USDC list mixes every coin.
func usdcGEXFixture(t *testing.T) (rows []gex.SummaryRow, at time.Time) {
	t.Helper()
	data, err := os.ReadFile("testdata/gex_mainnet_usdc_sol.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		CapturedAtMS int64            `json:"captured_at_ms"`
		Summaries    []gex.SummaryRow `json:"summaries"`
	}
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatal(err)
	}
	return fx.Summaries, time.UnixMilli(fx.CapturedAtMS)
}

func botGEXParams(underlying string) gex.Params {
	return gex.Params{Underlying: underlying, NExpiries: 5, StrikeRangePct: config.FixedGEXStrikeRangePct, Method: config.FixedGEXMethod}
}

func rowsWithPrefix(rows []gex.SummaryRow, prefix string) []gex.SummaryRow {
	var out []gex.SummaryRow
	for _, r := range rows {
		if strings.HasPrefix(r.InstrumentName, prefix) {
			out = append(out, r)
		}
	}
	return out
}

// Scenario: GEX from the USDC book.
func TestGEX_FromTheUSDCBook(t *testing.T) {
	rows, at := usdcGEXFixture(t)
	sol := rowsWithPrefix(rows, "SOL_USDC-")
	if len(sol) == 0 || len(sol) == len(rows) {
		t.Fatalf("fixture must mix SOL_USDC with other coins: %d of %d", len(sol), len(rows))
	}

	snap, st, err := gex.Build(rows, at, botGEXParams("SOL"))
	if err != nil {
		t.Fatalf("SOL from its USDC book: %v", err)
	}
	only, onlySt, err := gex.Build(sol, at, botGEXParams("SOL"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap, only) {
		t.Errorf("the XRP_USDC/BTC_USDC rows changed SOL's snapshot:\n mixed %+v\n  only %+v", snap, only)
	}
	if st.InstrumentsTotal != len(sol) || st.InstrumentsUsed != onlySt.InstrumentsUsed || st.Unparsed != len(rows)-len(sol) {
		t.Errorf("stats = %+v, want every one of the %d SOL rows and nothing else", st, len(sol))
	}

	solStrikes := map[float64]bool{}
	for _, r := range sol {
		_, _, k, _, err := marketdata.ParseOptionName(r.InstrumentName)
		if err != nil {
			t.Fatal(err)
		}
		solStrikes[k] = true
	}
	for _, s := range snap.Strikes {
		if !solStrikes[s.Strike] {
			t.Errorf("strike %v is not a SOL_USDC strike", s.Strike)
		}
	}
	// SOL traded ≈ 75 USDC; the flip and the walls are SOL prices.
	if snap.Spot < 60 || snap.Spot > 90 {
		t.Errorf("spot = %v, want SOL's ≈ 75", snap.Spot)
	}
	if snap.Regime == "" || snap.Regime == "NEUTRAL" || snap.RegimeScore == 0 {
		t.Errorf("regime = %s (score %v): want a signed dollar gamma", snap.Regime, snap.RegimeScore)
	}
	if snap.GammaFlipFound && (snap.GammaFlip < 40 || snap.GammaFlip > 120) {
		t.Errorf("flip = %v, outside SOL's strikes", snap.GammaFlip)
	}

	// The research tools key the books "SOL_USDC": the same snapshot.
	keyed, _, err := gex.Build(rows, at, botGEXParams("SOL_USDC"))
	if err != nil || !reflect.DeepEqual(keyed, snap) {
		t.Errorf("Underlying SOL_USDC: %v, want SOL's snapshot", err)
	}
}

// The XRP bot reads XRP_USDC only (strikes like 2d2 = 2.2).
func TestGEX_XRPFromTheUSDCBook(t *testing.T) {
	rows, at := usdcGEXFixture(t)
	xrp := rowsWithPrefix(rows, "XRP_USDC-")
	snap, st, err := gex.Build(rows, at, botGEXParams("XRP"))
	if err != nil {
		t.Fatalf("XRP from its USDC book: %v", err)
	}
	only, _, _ := gex.Build(xrp, at, botGEXParams("XRP"))
	if !reflect.DeepEqual(snap, only) || st.InstrumentsTotal != len(xrp) {
		t.Errorf("XRP's snapshot must come from its %d XRP_USDC rows only (got %d)", len(xrp), st.InstrumentsTotal)
	}
	if snap.Spot < 0.5 || snap.Spot > 2 {
		t.Errorf("spot = %v, want XRP's ≈ 0.98", snap.Spot)
	}
}

// A coin's inverse options are not its USDC ones: BTC never reads BTC_USDC,
// and SOL has no inverse book to fall back on.
func TestGEX_NoUSDCRowsForAnInverseCoin(t *testing.T) {
	rows, at := usdcGEXFixture(t)
	if _, _, err := gex.Build(rows, at, botGEXParams("BTC")); err == nil {
		t.Error("BTC_USDC rows must not build BTC's GEX: its book is currency=BTC")
	}
	inverse := []gex.SummaryRow{{InstrumentName: "SOL-25DEC26-80-C", OpenInterest: 100, UnderlyingPrice: 75, MarkIV: 70}}
	if _, _, err := gex.Build(inverse, at, botGEXParams("SOL")); err == nil {
		t.Error("a SOL- name is not a SOL_USDC option")
	}
}

// Scenario: the liquidity cap applies — a SOL strike with mainnet open
// interest 1,470 SOL: an entry on it is at most 140 SOL (10 %, floored to
// 10-SOL contracts). The manager reads the open interest from the USDC list,
// where XRP's strike of the same name shape is left out.
func TestLiquidityCap_AppliesToSOL(t *testing.T) {
	const call = "SOL_USDC-25DEC26-80-C"
	gw := &fakeBookSummary{result: `[
		{"instrument_name":"SOL_USDC-25DEC26-80-C","open_interest":1470,"underlying_price":75,"mark_iv":70},
		{"instrument_name":"XRP_USDC-25DEC26-2d2-P","open_interest":90000,"underlying_price":0.98,"mark_iv":90}]`}
	m := gex.NewManager(gw, botGEXParams("SOL"), 0)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	oi := m.OpenInterest()
	if oi == nil || oi.ByInstrument[call] != 1470 {
		t.Fatalf("open interest = %+v, want %s 1470", oi, call)
	}
	if _, ok := oi.ByInstrument["XRP_USDC-25DEC26-2d2-P"]; ok {
		t.Error("the SOL bot's open interest holds XRP's strikes")
	}
	const lot = 10 // SOL_USDC: 10 SOL per contract
	if got := strategy.LiquidityCapQty(oi.ByInstrument[call], config.FixedLiquidityCapOIPct, lot); got != 140 {
		t.Errorf("cap = %v SOL, want 140 (10 %% of 1,470 = 147, floored to 10)", got)
	}
}

// Scenario: BTC and ETH unchanged — their snapshots on recorded books equal
// today's, USDC rows in the list or not (the values themselves are pinned by
// gex_parity_test.go and gex_test.go).
func TestGEX_BTCAndETHUnchanged(t *testing.T) {
	usdc, _ := usdcGEXFixture(t)
	for _, file := range []string{"testdata/gex_mainnet_btc.json", "testdata/gex_mainnet_btc_empty_strike.json"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			CapturedAtMS int64            `json:"captured_at_ms"`
			Summaries    []gex.SummaryRow `json:"summaries"`
		}
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Fatal(err)
		}
		at := time.UnixMilli(fx.CapturedAtMS)
		// ETH's names on the same book: the match is by the coin, as for BTC.
		eth := make([]gex.SummaryRow, len(fx.Summaries))
		for i, r := range fx.Summaries {
			r.InstrumentName = strings.Replace(r.InstrumentName, "BTC-", "ETH-", 1)
			eth[i] = r
		}
		for _, c := range []struct {
			coin string
			rows []gex.SummaryRow
		}{{"BTC", fx.Summaries}, {"ETH", eth}} {
			want, _, err := gex.Build(c.rows, at, botGEXParams(c.coin))
			if err != nil {
				t.Fatalf("%s %s: %v", file, c.coin, err)
			}
			got, _, err := gex.Build(append(append([]gex.SummaryRow{}, c.rows...), usdc...), at, botGEXParams(c.coin))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: USDC rows changed the snapshot (%v)", file, c.coin, err)
			}
			if c.coin == "ETH" {
				btc, _, _ := gex.Build(fx.Summaries, at, botGEXParams("BTC"))
				if got.Regime != btc.Regime || got.GammaFlip != btc.GammaFlip || got.RegimeScore != btc.RegimeScore {
					t.Errorf("%s: ETH's names give %s/%v, BTC's %s/%v", file, got.Regime, got.GammaFlip, btc.Regime, btc.GammaFlip)
				}
			}
		}
	}
}
