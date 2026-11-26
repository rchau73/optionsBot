package tests

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// cmd/stress -replay on a USDC-settled coin's recorded book (#837): the
// prices are USDC per coin, so the replay's margin scan, fees, perpetual and
// P&L are in USDC, orders are whole contracts (10 SOL, 1,000 XRP), and the
// minute GEX records beside the books (the BTC book's) are not read.

// solBook is five days of hourly SOL_USDC books at spot 70: one expiry 45
// days out (the 50-DTE slot's window), the 0.16 Δ call 80 and put 60 and
// the ATM pair, every quote two-sided with deep open interest.
func solBook(t *testing.T) []recorder.Snapshot {
	t.Helper()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expiry := start.AddDate(0, 0, 45).Add(8 * time.Hour)
	label := strings.ToUpper(expiry.Format("2Jan06"))
	type leg struct {
		typ           string
		strike, delta float64
		bid, ask      float64
	}
	legs := []leg{{"call", 80, 0.16, 1.00, 1.20}, {"put", 60, -0.16, 0.90, 1.10}, {"call", 70, 0.52, 5.0, 5.4}, {"put", 70, -0.48, 4.8, 5.2}}
	var snaps []recorder.Snapshot
	for h := range 5 * 24 { // the regime confirms on 2 daily closes before entries

		s := recorder.Snapshot{At: start.Add(time.Duration(h) * time.Hour)}
		for _, l := range legs {
			s.Quotes = append(s.Quotes, recorder.Quote{
				Instrument: fmt.Sprintf("SOL_USDC-%s-%g-%s", label, l.strike, strings.ToUpper(l.typ[:1])), Underlying: "SOL_USDC",
				UnderlyingPrice: 70, Strike: l.strike, Expiry: expiry, OptionType: l.typ, Bid: l.bid, Ask: l.ask,
				Mid: (l.bid + l.ask) / 2, Mark: (l.bid + l.ask) / 2, Delta: l.delta, Gamma: 0.02, IV: 0.60, OpenInterest: 100000,
			})
		}
		snaps = append(snaps, s)
	}
	return snaps
}

func solReplay(t *testing.T) (replay.Result, []recorder.Snapshot) {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "config_sol.yaml")) // as replayConfig: a backtest needs no key
	if err != nil {
		t.Fatal(err)
	}
	snaps := solBook(t)
	var closes []orders.DailyClose // flat closes: a neutral trend
	for d := 40; d >= 1; d-- {
		closes = append(closes, orders.DailyClose{Date: snaps[0].At.AddDate(0, 0, -d), Close: 70})
	}
	res, err := replay.Run(snaps, replay.Settings{Cfg: cfg, From: snaps[0].At, Equity: 5000, Closes: closes, ChainIVMinDays: config.FixedChainIVMinDays})
	if err != nil {
		t.Fatal(err)
	}
	return res, snaps
}

func TestReplayLinear_SOLTradesWholeContractsInUSDC(t *testing.T) {
	t.Parallel()
	res, _ := solReplay(t)
	if !res.Linear || res.Equity0 != 5000 {
		t.Fatalf("linear %v, start %v; want a USDC replay from 5,000", res.Linear, res.Equity0)
	}
	opens := 0
	for _, f := range res.Fills {
		if f.Kind != "open" {
			continue
		}
		opens++
		if f.Qty < 10 || math.Mod(f.Qty, 10) != 0 {
			t.Errorf("%s ×%v: want whole 10-SOL contracts", f.Instrument, f.Qty)
		}
		// 0.03 % of the 70 USDC index per SOL (0.021), capped at 12.5 % of the price.
		if want := orders.LinearOptionFee(70, f.Price, f.Qty); math.Abs(f.Fee-want) > 1e-9 {
			t.Errorf("%s fee %v USDC, want %v (linear)", f.Instrument, f.Fee, want)
		}
	}
	if opens < 2 {
		for _, d := range res.Days {
			t.Logf("day %+v", d) // why nothing entered
		}
		t.Fatalf("%d legs opened, want the 50-DTE strangle (fills %+v)", opens, res.Fills)
	}
	// The lowest band (20 %, chain IV under 90 days) of a USDC balance: a
	// strangle priced in coin terms would read a margin ~70× too small and
	// be sized far past it.
	if res.MaxIMPct <= 0 || res.MaxIMPct > 20.5 {
		t.Errorf("IM max %.2f %% of the balance, want within the 20 %% limit", res.MaxIMPct)
	}
	if res.NetInUSD() != res.Net {
		t.Errorf("USD %v, want the USDC net %v as it is", res.NetInUSD(), res.Net)
	}
}

// What must not change: a BTC replay is still in the coin, its USD at spot,
// its lot 0.1 BTC.
func TestReplayLinear_InverseUnchanged(t *testing.T) {
	if replay.LotFor("BTC") != 0.1 || replay.LotFor("ETH") != 1 || replay.LotFor("SOL") != 10 || replay.LotFor("xrp") != 1000 {
		t.Errorf("lots BTC %v ETH %v SOL %v XRP %v", replay.LotFor("BTC"), replay.LotFor("ETH"), replay.LotFor("SOL"), replay.LotFor("xrp"))
	}
	r := replay.Result{Net: 0.01, Days: []replay.Day{{PnL: 0.01, Spot: 100000}}}
	if r.NetInUSD() != 1000 {
		t.Errorf("BTC net in USD %v, want 0.01 × 100,000", r.NetInUSD())
	}
	if got := replay.LinearFee(70, 1.0, 10); math.Abs(got-0.21) > 1e-12 {
		t.Errorf("SOL fee %v, want 0.21 USDC (0.03 %% of 70 × 10)", got)
	}
	if got := replay.Fee(0.02, 1); got != 0.0003 {
		t.Errorf("BTC fee %v, want 0.0003 BTC", got)
	}
}

// A linear coin's replay reads no minute GEX by default: market_gex beside
// the books is the BTC book's (spot ~77,000 for SOL's ~70, 2026-11 run).
func TestReplayLinear_DefaultGEXDir(t *testing.T) {
	books := filepath.Join("data", "btc", "data", "market_usdc_sol")
	if got := replay.DefaultGEXDir(books, "SOL"); got != "none" {
		t.Errorf("SOL: %q, want none", got)
	}
	if got := replay.DefaultGEXDir(books, "XRP"); got != "none" {
		t.Errorf("XRP: %q, want none", got)
	}
	if got, want := replay.DefaultGEXDir(filepath.Join("data", "btc", "data", "market"), "BTC"), filepath.Join("data", "btc", "data", "market_gex"); got != want {
		t.Errorf("BTC: %q, want %q", got, want)
	}
}
