package tests

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
)

// The futures hedge experiment's pure rules (#657).

// Scenario: long delta is sold down with the perpetual (the issue's example).
func TestHedgeOrder_LongDeltaSells(t *testing.T) {
	usd, side := hedge.HedgeOrder(0.2305, 0, 0.058, 76775, 10)
	if side != hedge.SideSell || usd != 17700 {
		t.Fatalf("got %s %.0f, want sell 17700", side, usd)
	}
	if after := 0.2305 + hedge.PerpDelta(-usd, 76775); math.Abs(after) >= 0.058 {
		t.Errorf("net delta after the trade %.4f, want inside the band", after)
	}
}

// Scenario: the combined payoff includes the hedge (#666): the issue's −10 %
// row, and the perpetual's inverse P&L at every move.
func TestBookScenarios_CombinedPayoffIncludesTheHedge(t *testing.T) {
	const spot, delta, gamma, perp = 76030.0, 0.4244, -0.00012, -32310.0
	rows := hedge.BookScenarios(spot, delta, gamma, perp)
	if len(rows) != len(hedge.ScenarioMoves) {
		t.Fatalf("%d rows, want %d", len(rows), len(hedge.ScenarioMoves))
	}
	if r := rows[0]; r.MovePct != -10 || math.Abs(r.OptionsUSD+6695) > 1 || math.Abs(r.BookUSD+3464) > 1 {
		t.Errorf("−10 %% row %+v, want about −3,464 with the hedge and −6,695 options only", r)
	}
	for _, r := range rows {
		// A short inverse perpetual gains size × |move| in USD at the moved price.
		if want := r.OptionsUSD + perp*r.MovePct/100; math.Abs(r.BookUSD-want) > 1e-6 {
			t.Errorf("%+v: book USD, want %.2f", r, want)
		}
		if math.Abs(r.BookCoin*r.Spot-r.BookUSD) > 1e-6 {
			t.Errorf("%+v: coin and USD disagree at the moved price", r)
		}
	}
	for _, r := range hedge.BookScenarios(spot, delta, gamma, 0) {
		if r.BookUSD != r.OptionsUSD || r.BookCoin != r.OptionsCoin {
			t.Errorf("no perpetual: %+v, want the book to be the options", r)
		}
	}
	if got := hedge.BookScenarios(0, delta, gamma, perp); got == nil || len(got) != 0 {
		t.Errorf("no spot: %v, want an empty list, not null", got)
	}
}

// Scenario: short delta is bought back up.
func TestHedgeOrder_ShortDeltaBuys(t *testing.T) {
	if usd, side := hedge.HedgeOrder(-0.10, 0, 0.058, 80000, 10); side != hedge.SideBuy || usd != 8000 {
		t.Fatalf("got %s %.0f, want buy 8000", side, usd)
	}
}

// Scenario: inside the band nothing trades; the perpetual already held counts.
func TestHedgeOrder_InsideBandAndHeldHedge(t *testing.T) {
	if usd, side := hedge.HedgeOrder(0.03, 0, 0.058, 80000, 10); usd != 0 || side != "" {
		t.Errorf("|net| 0.03 under band 0.058: got %s %.0f", side, usd)
	}
	// Options +0.2305 hedged by a short of 17,700 USD: net ≈ 0.
	if usd, _ := hedge.HedgeOrder(0.2305, hedge.PerpDelta(-17700, 76775), 0.058, 76775, 10); usd != 0 {
		t.Errorf("an already hedged book trades nothing, got %.0f", usd)
	}
	// Over-hedged: the perpetual is bought back down.
	if usd, side := hedge.HedgeOrder(0.10, hedge.PerpDelta(-17700, 76775), 0.058, 76775, 10); side != hedge.SideBuy || usd != 10020 {
		t.Errorf("over-hedged by 0.1305: got %s %.0f, want buy 10020", side, usd)
	}
}

// Negative cases: no price, no contract, less than a contract.
func TestHedgeOrder_NothingWithoutPriceOrContract(t *testing.T) {
	for _, c := range []struct{ spot, contract, band float64 }{{0, 10, 0}, {80000, 0, 0}, {80000, 10, 0}} {
		usd, _ := hedge.HedgeOrder(0.00001, 0, c.band, c.spot, c.contract)
		if usd != 0 {
			t.Errorf("%+v: got %.0f, want nothing", c, usd)
		}
	}
	if usd, _ := hedge.HedgeOrder(math.NaN(), 0, 0.05, 80000, 10); usd != 0 {
		t.Error("an unknown delta trades nothing")
	}
}

func TestHedgeBand_CoinAndUSDBalances(t *testing.T) {
	if b := hedge.Band(1.157, false, 76775, 5); !near(b, 0.05785, 1e-9) {
		t.Errorf("coin balance: band %.5f", b)
	}
	if b := hedge.Band(88830, true, 76775, 5); !near(b, 0.05785, 1e-4) {
		t.Errorf("USD balance converted at spot: band %.5f", b)
	}
	if b := hedge.Band(1000, true, 0, 5); b != 0 {
		t.Errorf("no spot: band %.5f, want 0 (no hedge)", b)
	}
}

func TestPerpPnL_InverseContract(t *testing.T) {
	// Long 100 USD at 100, marked at 110: 100 × (1/100 − 1/110) coin.
	if p := hedge.PerpPnL(100, 100, 110); !near(p, 100*(0.01-1.0/110), 1e-12) {
		t.Errorf("long: %.8f", p)
	}
	if p := hedge.PerpPnL(-17700, 76775, 70000); p <= 0 {
		t.Errorf("a short gains when the price falls, got %.6f", p)
	}
}

func TestRateLimited_RollingHour(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	var sent []time.Time
	for i := 0; i < 12; i++ {
		sent = append(sent, now.Add(-time.Duration(i)*time.Minute))
	}
	if !hedge.RateLimited(sent, now, 12) {
		t.Error("12 orders in the hour: the 13th waits")
	}
	if hedge.RateLimited(sent, now.Add(50*time.Minute), 12) {
		t.Error("orders older than an hour no longer count")
	}
}

// Scenario: the switch-off preview (the issue's numbers).
func TestBuildPreview_IssueExample(t *testing.T) {
	p := hedge.BuildPreview(hedge.PreviewInput{
		Spot: 76775, OptionsDelta: 0.2305, ScenarioDelta: 0.2305, OptionsGamma: -0.000175,
		PerpUSD: -17700, PerpAvg: 76775, PerpMark: 76775, FeeRate: 0.0005,
	})
	if !near(p.DeltaNow, 0, 0.001) || p.DeltaAfter != 0.2305 {
		t.Errorf("delta now %.4f after %.4f", p.DeltaNow, p.DeltaAfter)
	}
	if len(p.Scenarios) != 4 || p.Scenarios[0].MovePct != -10 {
		t.Fatalf("scenarios %+v", p.Scenarios)
	}
	if down := p.Scenarios[0]; !near(down.PnLUSD, -6927, 2) || !near(down.PnLCoin, -6927/69097.5, 0.001) {
		t.Errorf("−10 %%: %.0f USD %.4f coin, want ≈ −6,927 USD ≈ −0.10 coin", down.PnLUSD, down.PnLCoin)
	}
	if !near(p.CloseFeeUSD, 17700*0.0005, 0.01) {
		t.Errorf("close fee %.2f USD", p.CloseFeeUSD)
	}
}

func TestPerpBook_AveragesAndRealises(t *testing.T) {
	var b hedge.PerpBook
	b.Book(-10000, 100000, 0)
	b.Book(-10000, 80000, 0)
	// Inverse average: 20,000 USD over (0.1 + 0.125) coin.
	if !near(b.AvgPrice, 20000/0.225, 1e-6) || b.SizeUSD != -20000 {
		t.Fatalf("avg %.2f size %.0f", b.AvgPrice, b.SizeUSD)
	}
	b.Book(20000, 90000, 0.0001)
	if b.SizeUSD != 0 || !near(b.Realised, hedge.PerpPnL(-20000, 20000/0.225, 90000), 1e-12) {
		t.Errorf("closed: size %.0f realised %.6f", b.SizeUSD, b.Realised)
	}
	if !near(b.PnL(90000), b.Realised-0.0001, 1e-12) {
		t.Error("P&L is net of fees")
	}
}

func TestCounterfactual_ValueOfHolding(t *testing.T) {
	c := hedge.Counterfactual{Qty: 2, ShedPrice: 0.03, LastPrice: 0.01}
	if v := c.Value(); !near(v, 0.04, 1e-12) {
		t.Errorf("the option got cheaper after the shed: holding is worth %.4f, want +0.04", v)
	}
	c.LastPrice = 0.05
	if v := c.Value(); !near(v, -0.04, 1e-12) {
		t.Errorf("the option got dearer: holding cost %.4f, want −0.04", v)
	}
	if v := (hedge.Counterfactual{Qty: 1, ShedPrice: 0.03}).Value(); v != 0 {
		t.Error("no later price yet: no value")
	}
}

func TestLedger_SummaryAddsHedgeAndLegs(t *testing.T) {
	l := hedge.Ledger{}
	l.Real.Book(-10000, 100000, 0)
	l.AddLeg(hedge.Counterfactual{Qty: 1, ShedPrice: 0.03, LastPrice: 0.02})
	s := l.Summarise(0.0001, 90000)
	wantReal := hedge.PerpPnL(-10000, 100000, 90000) + 0.0001
	if !near(s.RealCoin, wantReal, 1e-12) || !near(s.LegsCoin, 0.01, 1e-12) || !near(s.AdvantageCoin, wantReal+0.01, 1e-12) {
		t.Errorf("summary %+v", s)
	}
	if s.Note == "" || len(s.Legs) != 1 {
		t.Error("the summary says what it is and lists its legs")
	}
}

// Scenario: OFF by default; the mode survives a restart; a bad file is OFF.
func TestModeFile_MissingIsOffAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "hedge_mode.json")
	st, err := hedge.LoadMode(path)
	if err != nil || st.Enabled {
		t.Fatalf("missing file: %+v %v, want OFF", st, err)
	}
	want := hedge.ModeState{Enabled: true, Since: time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC), By: hedge.Actor{User: "roberto", Role: "admin"}}
	want.Ledger.Real.Book(-17700, 76775, 0.0001)
	if err := hedge.SaveMode(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := hedge.LoadMode(path)
	if err != nil || !got.Enabled || got.By.User != "roberto" || got.Ledger.Real.SizeUSD != -17700 {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, err := hedge.LoadMode(path); err == nil || st.Enabled {
		t.Errorf("a corrupt file must be an error and OFF, got %+v %v", st, err)
	}
}

// The hedge holds the coin delta at zero: an inverse option's coin delta is
// its Black–Scholes delta less its mark in coin, so a short strangle is less
// short calls and more long puts than its USD delta says (#657 review).
func TestFuturesHedge_CoinDeltaIsPremiumAdjusted(t *testing.T) {
	st := orders.NewStateManager()
	st.AddPosition(&orders.Position{ID: "c", Instrument: "BTC-27NOV26-96000-C", OptionType: "call", Qty: 2,
		CurrentMid: 0.0125, CurrentGreeks: orders.Greeks{Delta: 0.18}})
	st.AddPosition(&orders.Position{ID: "p", Instrument: "BTC-27NOV26-66000-P", OptionType: "put", Qty: 2,
		CurrentMid: 0.0125, CurrentGreeks: orders.Greeks{Delta: -0.16}})
	if usd := st.TotalNetDelta(); !near(usd, -0.04, 1e-12) {
		t.Fatalf("USD delta %v, want −0.04", usd)
	}
	// −2·(0.18 − 0.0125) − 2·(−0.16 − 0.0125) = −0.335 + 0.345 = +0.01
	if coin := st.TotalCoinDelta(); !near(coin, 0.01, 1e-12) {
		t.Fatalf("coin delta %v, want +0.01", coin)
	}
	if legs := st.UnpricedLegs(); len(legs) != 0 {
		t.Fatalf("every leg has greeks, got %v", legs)
	}
	st.AddPosition(&orders.Position{ID: "n", Instrument: "BTC-27NOV26-70000-P", OptionType: "put", Qty: 1, CurrentMid: 0.02})
	if legs := st.UnpricedLegs(); len(legs) != 1 || legs[0] != "BTC-27NOV26-70000-P" {
		t.Fatalf("a leg with all-zero greeks is unpriced, got %v", legs)
	}
}

// KeptQty counts only the open kept legs of that instrument: not a real
// shed, not a finished leg, not another instrument.
func TestLedger_KeptQty(t *testing.T) {
	const put = "BTC-27NOV26-66000-P"
	l := hedge.Ledger{Legs: []hedge.Counterfactual{
		{Mode: hedge.CounterHedged, Instrument: put, Qty: 1},
		{Mode: hedge.CounterHedged, Instrument: put, Qty: 0.5},
		{Mode: hedge.CounterHedged, Instrument: put, Qty: 2, Done: true},
		{Mode: hedge.CounterShed, Instrument: put, Qty: 4},
		{Mode: hedge.CounterHedged, Instrument: "BTC-27NOV26-96000-C", Qty: 8},
	}}
	if got := l.KeptQty(put); got != 1.5 {
		t.Errorf("KeptQty %v, want 1.5", got)
	}
	if got := l.KeptQty("BTC-27NOV26-70000-P"); got != 0 {
		t.Errorf("KeptQty of an unknown instrument %v, want 0", got)
	}
}
