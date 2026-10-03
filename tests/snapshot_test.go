package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestMoneyness(t *testing.T) {
	tests := []struct {
		name         string
		typ          string
		spot, strike float64
		label        string
		distPct      float64
	}{
		{"OTM call", "call", 100000, 110000, "OTM", 10},
		{"ITM call", "call", 100000, 90000, "ITM", -10},
		{"OTM put", "put", 100000, 90000, "OTM", 10},
		{"ITM put", "put", 100000, 110000, "ITM", -10},
		{"ATM within 1%", "call", 100000, 100500, "ATM", 0.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, dist := strategy.Moneyness(tc.typ, tc.spot, tc.strike)
			if label != tc.label || !near(dist, tc.distPct, 1e-9) {
				t.Errorf("got %s %.4f%%, want %s %.4f%%", label, dist, tc.label, tc.distPct)
			}
		})
	}
}

// Intrinsic value of an inverse option is the USD payoff divided by spot,
// so it can be compared with the option's BTC price.
func TestIntrinsicCoin(t *testing.T) {
	if got := strategy.IntrinsicCoin("call", 100000, 90000); !near(got, 0.1, 1e-12) {
		t.Errorf("ITM call intrinsic = %v BTC, want 0.1 (10,000 / 100,000)", got)
	}
	if got := strategy.IntrinsicCoin("put", 100000, 120000); !near(got, 0.2, 1e-12) {
		t.Errorf("ITM put intrinsic = %v BTC, want 0.2", got)
	}
	if strategy.IntrinsicCoin("call", 100000, 110000) != 0 || strategy.IntrinsicCoin("put", 0, 1) != 0 {
		t.Error("OTM (or unknown spot) intrinsic is 0")
	}
}

func TestMaxPainStrike(t *testing.T) {
	rows := []strategy.StrikeOI{
		{Strike: 90000, CallOI: 50, PutOI: 0},
		{Strike: 100000, CallOI: 30, PutOI: 10},
		{Strike: 110000, CallOI: 0, PutOI: 40},
	}
	// pain(90k)  = puts: 10×10k + 40×20k = 900k
	// pain(100k) = calls: 50×10k = 500k; puts: 40×10k = 400k → 900k
	// pain(110k) = calls: 50×20k + 30×10k = 1.3M
	// tie between 90k and 100k → lower strike
	if got := strategy.MaxPainStrike(rows); got != 90000 {
		t.Errorf("max pain = %v, want 90000", got)
	}
	if strategy.MaxPainStrike(nil) != 0 {
		t.Error("no open interest → 0")
	}
}

func TestBuildMarketSnapshot_FullInstrument(t *testing.T) {
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	exp := now.AddDate(0, 0, 30)
	mk := func(name, typ string, strike, iv float64) *marketdata.Instrument {
		return &marketdata.Instrument{Name: name, OptionType: typ, Strike: strike, Expiry: exp, Greeks: marketdata.Greeks{IV: iv}}
	}
	chain := []*marketdata.Instrument{
		mk("C-100", "call", 100000, 0.50),
		mk("C-110", "call", 110000, 0.55),
		mk("P-100", "put", 100000, 0.52),
		mk("P-90", "put", 90000, 0.60),
		mk("C-OTHER-EXPIRY", "call", 110000, 0.9),
	}
	chain[4].Expiry = exp.AddDate(0, 1, 0)
	inst := &marketdata.Instrument{
		Name: "C-110", OptionType: "call", Strike: 110000, Expiry: exp,
		Bid: 0.010, Ask: 0.012, Mid: 0.011, Greeks: marketdata.Greeks{Delta: 0.2, IV: 0.55},
	}
	oi := &gex.OISnapshot{AsOf: now.Add(-30 * time.Second), ByInstrument: map[string]float64{
		"C-100": 100, "C-110": 400, "P-100": 50, "P-90": 300, "C-OTHER-EXPIRY": 9999,
	}}
	snap := strategy.BuildMarketSnapshot(strategy.SnapshotInput{
		Now: now, Spot: 100000, DVOL: 52, IVPercentile: 40,
		Instrument: inst, Chain: chain, OI: oi,
		GEX: &gex.Snapshot{Regime: "POSITIVE/PINNING", GammaFlip: 95000, GammaFlipFound: true},
	})

	checks := []struct {
		name      string
		got, want float64
	}{
		{"dte", snap.DTE, 30},
		{"distance %", snap.DistanceToStrikePct, 10},
		// ln(1.1) / (0.55 × √(30/365)) ≈ 0.6012
		{"distance SD", snap.DistanceToStrikeSD, math.Log(1.1) / (0.55 * math.Sqrt(30.0/365))},
		{"spread %", snap.SpreadPct, 0.002 / 0.011 * 100},
		{"intrinsic", snap.Intrinsic, 0},
		{"extrinsic", snap.Extrinsic, 0.011},
		{"ATM IV (call closest to spot)", snap.ATMIV, 0.50},
		{"skew", snap.Skew, 0.05},
		{"instrument OI", snap.InstrumentOI, 400},
		{"strike OI", snap.StrikeOI, 400},
		{"expiry OI (other expiries excluded)", snap.ExpiryOI, 850},
		{"spot to flip %", snap.SpotToFlipPct, (100000 - 95000) / 95000.0 * 100},
	}
	for _, c := range checks {
		if !near(c.got, c.want, 1e-9) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if snap.Moneyness != "OTM" || snap.StrikeOIRank != 1 || snap.GEXRegime != "POSITIVE/PINNING" {
		t.Errorf("labels: moneyness %s rank %d regime %s", snap.Moneyness, snap.StrikeOIRank, snap.GEXRegime)
	}
	if !snap.OIAsOf.Equal(oi.AsOf) || snap.DVOL != 52 || snap.IVPercentile != 40 {
		t.Errorf("as-of / vol fields: %+v", snap)
	}
	// Open interest by strike: 90k put 300 · 100k call 100 + put 50 · 110k call 400.
	// Holders' payout if settled at 90k: put@100k 50×10k = 500k; at 100k: nothing
	// is in the money → 0; at 110k: call@100k 100×10k = 1M. Max pain = 100k.
	if snap.MaxPainStrike != 100000 {
		t.Errorf("max pain = %v, want 100000", snap.MaxPainStrike)
	}
}

func TestBuildMarketSnapshot_MarketOnlyAndMissingData(t *testing.T) {
	now := time.Now()
	snap := strategy.BuildMarketSnapshot(strategy.SnapshotInput{Now: now, Spot: 100000, DVOL: 50, IVPercentile: 10})
	if snap.Spot != 100000 || snap.Moneyness != "" || snap.StrikeOI != 0 || snap.GEXRegime != "" {
		t.Errorf("market-only snapshot should leave instrument fields empty: %+v", snap)
	}

	// No index price yet: fall back to the option's underlying price; no IV → no SD distance.
	inst := &marketdata.Instrument{Name: "P", OptionType: "put", Strike: 110000, UnderlyingPrice: 100000, Expiry: now.AddDate(0, 0, 10), Mid: 0.11}
	snap = strategy.BuildMarketSnapshot(strategy.SnapshotInput{Now: now, Instrument: inst})
	if snap.Spot != 100000 || snap.Moneyness != "ITM" || snap.DistanceToStrikeSD != 0 {
		t.Errorf("fallbacks: %+v", snap)
	}
	if !near(snap.Intrinsic, 0.1, 1e-12) || !near(snap.Extrinsic, 0.01, 1e-12) {
		t.Errorf("ITM put: intrinsic %v extrinsic %v, want 0.1 / 0.01 BTC", snap.Intrinsic, snap.Extrinsic)
	}
	if snap.SpreadPct != 0 {
		t.Error("no quotes → no spread")
	}
}

func TestComputePnL(t *testing.T) {
	slots := []orders.SlotRef{{DTE: 25, Delta: 0.16}, {DTE: 45, Delta: 0.16}}
	open := []*orders.Position{
		{ID: "a", Qty: 0.1, PremiumReceived: 0.002, CurrentMid: 0.005},  // +0.0015
		{ID: "b", Qty: 0.1, PremiumReceived: 0.002, CurrentMid: 0.030},  // −0.0010
		{ID: "orphan", Qty: 0.1, PremiumReceived: 0.001, CurrentMid: 0}, // +0.0010, no slot
	}
	slotOf := map[string]*orders.SlotRef{"a": &slots[0], "b": &slots[1]}

	// Realised: slot 25d +0.0008 (one close), unknown slot (zero SlotRef) −0.0002.
	realised := map[orders.SlotRef]float64{slots[0]: 0.0008, {}: -0.0002}
	closed := map[orders.SlotRef]int{slots[0]: 1, {}: 1}

	lines := strategy.ComputePnL(slots, open, slotOf, realised, closed)
	if len(lines) != 3 || lines[2].Slot != nil {
		t.Fatalf("want 2 slots + total, got %+v", lines)
	}
	want := []struct {
		realised, unrealised float64
		open, closed         int
	}{
		{0.0008, 0.0015, 1, 1},
		{0, -0.0010, 1, 0},
		{0.0006, 0.0015, 3, 2}, // total includes the orphan and the unknown-slot close
	}
	for i, w := range want {
		l := lines[i]
		if !near(l.Realised, w.realised, 1e-12) || !near(l.Unrealised, w.unrealised, 1e-12) || l.OpenLegs != w.open || l.ClosedLegs != w.closed {
			t.Errorf("line %d = %+v, want %+v", i, l, w)
		}
	}
}
