package tests

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #103: margin alone sized ETH's entries at up to 9× a strike's whole mainnet
// open interest (1,454 sent on ETH-6NOV26-2800-C, OI 164, 152 filled). A new
// leg — entry or top-up — is now at most liquidity_cap_oi_pct (10 %) of its
// strike's mainnet open interest.

func TestLiquidityCapQty(t *testing.T) {
	cases := []struct {
		name            string
		oi, pct, lot    float64
		want            float64
		wantUnboundedOK bool
	}{
		{"ETH 2800-C: 10 % of 164", 164, 10, 1, 16, false},
		{"ETH 2950-C: 10 % of 1,059", 1059, 10, 1, 105, false},
		{"BTC 76000-P: 10 % of 7.5, 0.1 lots", 7.5, 10, 0.1, 0.7, false},
		{"under one lot", 5, 10, 1, 0, false},
		{"no open interest: nothing", 0, 10, 1, 0, false},
		{"negative open interest: nothing", -3, 10, 1, 0, false},
		{"no lot size: nothing", 164, 10, 0, 0, false},
		{"cap off", 164, 0, 1, 0, true},
	}
	for _, c := range cases {
		got := strategy.LiquidityCapQty(c.oi, c.pct, c.lot)
		if c.wantUnboundedOK {
			if !math.IsInf(got, 1) {
				t.Errorf("%s: got %v, want no cap (+Inf)", c.name, got)
			}
			continue
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: LiquidityCapQty(%v, %v, %v) = %v, want %v", c.name, c.oi, c.pct, c.lot, got, c.want)
		}
	}
}

func TestConfig_LiquidityCapIsFixedAtTenPercent(t *testing.T) {
	var c config.Config
	c.ApplyFixed()
	if c.LiquidityCapOIPct != 10 {
		t.Errorf("liquidity_cap_oi_pct = %v, want 10 (#103)", c.LiquidityCapOIPct)
	}
}

// withLiquidityCap turns the cap on at the bot's 10 % with the given mainnet
// open interest per instrument (an instrument left out has none).
func (f *strategyFixture) withLiquidityCap(oi map[string]float64) {
	f.cfg.LiquidityCapOIPct = config.FixedLiquidityCapOIPct
	f.oi = fakeOI{&gex.OISnapshot{AsOf: time.Now(), ByInstrument: oi}}
}

// Scenario: an order never exceeds what the market can take.
func TestLiquidityCap_OrderNeverExceedsWhatTheMarketCanTake(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.001 // margin alone: 2 BTC ÷ 0.001 = 2,000 lots (200 contracts)
	f.withLiquidityCap(map[string]float64{f.call: 164, f.put: 800})
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		// 10 % of the call's 164 binds both legs, so the strangle stays even.
		if math.Abs(o.Qty-16.4) > 1e-9 {
			t.Errorf("%s order = %v, want at most 10 %% of 164 = 16.4 on both legs", o.Instrument, o.Qty)
		}
	}
	if n := f.skips(strategy.SkipLiquidityCap); n != 1 {
		t.Fatalf("the cap must be journaled once, got %d", n)
	}
	for _, e := range f.journal.events(orders.EventSkipped) {
		if !strings.HasPrefix(e.reason, strategy.SkipLiquidityCap) {
			continue
		}
		for _, want := range []string{"capped to 16.4", "10% of mainnet open interest", f.call + " OI 164", f.put + " OI 800"} {
			if !strings.Contains(e.reason, want) {
				t.Errorf("journal %q lacks %q (the cap and the open interest it used)", e.reason, want)
			}
		}
	}
}

func TestLiquidityCap_UnderOneLotSendsNothing(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.001
	f.withLiquidityCap(map[string]float64{f.call: 0.5, f.put: 800}) // 10 % = 0.05 < one 0.1 lot
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips(strategy.SkipLiquidityCap) > 0 })
	time.Sleep(100 * time.Millisecond) // many cycles
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("a strike whose market takes less than a lot gets no order, got %d sells", n)
	}
	if n := f.skips(strategy.SkipLiquidityCap); n != 1 {
		t.Errorf("the skip is journaled once, not every cycle: %d", n)
	}
	if n := f.skips(strategy.SkipEntryRejected); n != 0 {
		t.Errorf("the cap is not an entry rejection: %d entry_rejected lines", n)
	}
}

func TestLiquidityCap_StrikeWithoutOpenInterestSendsNothing(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.001
	f.withLiquidityCap(map[string]float64{f.call: 800}) // the put is not in the mainnet book
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips(strategy.SkipLiquidityCap) > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("a strike with no open interest takes nothing, got %d sells", n)
	}
}

func TestLiquidityCap_NoOpenInterestSnapshotSendsNothing(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.LiquidityCapOIPct = config.FixedLiquidityCapOIPct
	f.oi = fakeOI{nil} // wired, but no refresh yet
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips(strategy.SkipLiquidityCap) > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("unknown market size: no order, got %d sells", n)
	}
}

func TestLiquidityCap_UnboundCapLeavesTheMarginSize(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.5 // 4 lots = 0.4
	f.withLiquidityCap(map[string]float64{f.call: 164, f.put: 800})
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.4) > 1e-9 {
			t.Errorf("%s order = %v, want the margin size 0.4", o.Instrument, o.Qty)
		}
	}
	if n := f.skips(strategy.SkipLiquidityCap); n != 0 {
		t.Errorf("a cap that does not bind is not journaled: %d", n)
	}
}

func TestLiquidityCap_TopUpIsCapped(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 0.5                                         // the 20 % limit fits 4 lots: a 0.3 top-up is due
	f.withLiquidityCap(map[string]float64{f.call: 2, f.put: 800}) // 10 % of 2 = 0.2
	f.startRun()

	eventually(t, 2*time.Second, "top-up submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if o.TriggerReason != orders.TriggerRebalanceUpsize || math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("top-up = %s %s %v, want a 0.2 upsize (10 %% of the call's OI 2)", o.TriggerReason, o.Instrument, o.Qty)
		}
	}
	eventually(t, time.Second, "cap journaled", func() bool { return f.skips(strategy.SkipLiquidityCap+": top-up") == 1 })
}

func TestLiquidityCap_TopUpUnderOneLotIsNotRetried(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 0.5
	f.cfg.RebalanceRetryMinutes = 0
	f.withLiquidityCap(map[string]float64{f.call: 0.5, f.put: 800})
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips(strategy.SkipLiquidityCap) > 0 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no top-up the market cannot take, got %d sells", n)
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalanceRetry)); n != 0 {
		t.Errorf("the slot stays short (#103): no retry, got %d", n)
	}
}

// Review of #211 (round 2): an entry the premium floor rejects is never sent,
// so the cap that would have bound it is not journaled — before, a capped
// entry under the floor wrote a false "capped" line every cycle.
func TestLiquidityCap_EntryRejectedByPremiumFloorJournalsNoCap(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.001                                         // margin alone: 200 contracts
	f.withLiquidityCap(map[string]float64{f.call: 164, f.put: 800}) // a cap of 16.4 would bind
	f.cfg.MinPremiumBTC = 1                                         // every leg is below the floor
	f.startRun()

	eventually(t, 2*time.Second, "entry rejected", func() bool { return f.skips(strategy.SkipEntryRejected) > 0 })
	time.Sleep(100 * time.Millisecond) // many cycles
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("an entry under the premium floor sends nothing, got %d sells", n)
	}
	if n := f.skips(strategy.SkipLiquidityCap); n != 0 {
		t.Errorf("no liquidity_cap line for an order never sent, got %d", n)
	}
}

// Review of #211: every capped top-up is journaled, not only the first one on
// its slot — the per-slot skip dedup is reset only by an entry into an empty
// slot, so a re-armed top-up capped again went unrecorded.
func TestLiquidityCap_RepeatTopUpCapIsJournaledEachTime(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 0.5         // a 0.3 top-up is due
	f.cfg.OrderFillTimeoutSec = 0 // it times out unfilled on the next cycle
	f.cfg.RebalanceRetryMinutes = 0
	f.withLiquidityCap(map[string]float64{f.call: 2, f.put: 800}) // 10 % of 2 = 0.2
	f.startRun()

	eventually(t, 3*time.Second, "the capped top-up re-sent after a short fill",
		func() bool { return len(f.exch.sells()) >= 4 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("every top-up is capped to 0.2, got %s %v", o.Instrument, o.Qty)
		}
	}
	eventually(t, time.Second, "one liquidity_cap line per capped top-up",
		func() bool { return f.skips(strategy.SkipLiquidityCap+": top-up") >= 2 })
}

// Review of #211: the rebalance report shows the size actually sent.
func TestLiquidityCap_RebalanceReportShowsTheCappedSize(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.withLiquidityCap(map[string]float64{f.call: 2, f.put: 800}) // 10 % of 2 = 0.2
	f.startRun()
	eventually(t, 3*time.Second, "startup rebalance ran",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalance)) > 0 })
	f.exch.mu.Lock()
	f.exch.summary.MarginBalance *= 4 // the slot target becomes 0.4: a 0.3 top-up
	f.exch.summary.Equity *= 4
	f.exch.mu.Unlock()

	for _, dry := range []bool{true, false} {
		rep, err := f.strat.RequestRebalance(context.Background(), dry, testActor)
		if err != nil {
			t.Fatalf("rebalance (dry run %v): %v", dry, err)
		}
		ups := topUps(rep)
		if len(ups) != 1 || math.Abs(ups[0].Qty-0.2) > 1e-9 || !strings.Contains(ups[0].Detail, "sell 0.2") {
			t.Fatalf("dry run %v: want one top-up reported at the capped 0.2: %+v", dry, rep)
		}
	}
	eventually(t, 2*time.Second, "capped top-up sold", func() bool { return len(f.exch.sells()) >= 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("top-up sent %s %v, want 0.2", o.Instrument, o.Qty)
		}
	}
}

// Scenario: what must not change — the regime-side rule.
func TestLiquidityCap_NegativeRegimeBearTrendStillSellsNoPut(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.001
	f.withBearTrendAboveFlip()
	// The put's market is the thin one: it must not bind a call-only entry.
	f.withLiquidityCap(map[string]float64{f.call: 164, f.put: 1})
	f.startRun()

	eventually(t, 2*time.Second, "call submitted", func() bool { return len(f.exch.sells()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	for _, o := range f.exch.sells() {
		if o.Instrument != f.call {
			t.Errorf("no new puts in a confirmed negative regime with a bear trend, got %s", o.Instrument)
		}
		if math.Abs(o.Qty-16.4) > 1e-9 {
			t.Errorf("call = %v, want 16.4 (its own cap; the unsold put's does not apply)", o.Qty)
		}
	}
}

// Scenario: what must not change — stops buy back in full.
func TestLiquidityCap_StopLossIsNotCapped(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.stopTheCall()
	f.withLiquidityCap(map[string]float64{f.call: 0.1, f.put: 0.1}) // a cap would allow nothing
	f.startRun()

	eventually(t, 2*time.Second, "call stopped out", func() bool { return len(f.exch.buys()) >= 1 })
	if b := f.exch.buys()[0]; b.Instrument != f.call || math.Abs(b.Qty-0.1) > 1e-9 {
		t.Errorf("stop-loss = %s %v, want the whole 0.1 call", b.Instrument, b.Qty)
	}
}

// Scenario: what must not change — the MM cut.
func TestLiquidityCap_MMCutIsNotCapped(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(1.0, 0.02)
	f.exch.setMargin(1, 4) // MM 40 % > 35 %
	f.withLiquidityCap(map[string]float64{f.call: 0.1, f.put: 0.1})
	f.startRun()

	eventually(t, 2*time.Second, "both legs reduced", func() bool { return len(f.exch.buys()) >= 2 })
	f.exch.setMargin(1, 1)
	for _, b := range f.exch.buys()[:2] {
		if b.TriggerReason != orders.TriggerMarginMM || math.Abs(b.Qty-0.2) > 1e-9 {
			t.Errorf("MM reduction = %s %v, want 0.2 at market as without the cap", b.TriggerReason, b.Qty)
		}
	}
}

// Repairs are exempt: they restore the size of a leg already held.
func TestLiquidityCap_RepairIsNotCapped(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.5)
	f.withLiquidityCap(map[string]float64{f.call: 0.1, f.put: 0.1})
	f.startRun()

	eventually(t, 2*time.Second, "missing put sold", func() bool { return len(f.exch.sells()) == 1 })
	if o := f.exch.sells()[0]; o.Instrument != f.put || math.Abs(o.Qty-0.5) > 1e-9 {
		t.Errorf("repair = %s %v, want the call's size 0.5", o.Instrument, o.Qty)
	}
}
