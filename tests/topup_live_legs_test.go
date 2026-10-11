package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// 2026-10-11 BTC: the puts closed by hand on 10-09 came back through the
// rebalance top-up. A closed leg stays linked to its strangle with its last
// size, and the top-up read the link: it saw a complete 1.2 strangle and,
// during a few minutes of neutral trend, sold a complement strangle — puts
// included — while repair was holding that put as a stop-out.

func anyPutSells(f *strategyFixture) int {
	n := 0
	for _, o := range f.exch.sells() {
		if strings.HasSuffix(o.Instrument, "-P") {
			n++
		}
	}
	return n
}

// closePutByHand closes the fixture's put from the monitor (bear trend,
// confirmed negative regime) with a top-up due and held by the regime-side
// rule, re-armed every pass, then turns the trend neutral.
func (f *strategyFixture) closePutByHandThenNeutral(t *testing.T, id string) {
	t.Helper()
	f.strat.SetManualClose(true, "")
	eventually(t, 2*time.Second, "top-up held by the regime side", func() bool { return f.skips(strategy.SkipRegimeSide) > 0 })
	res, err := f.strat.ManualClose(context.Background(), []string{id}, "test")
	if err != nil || len(res) != 1 || res[0].Error != "" {
		t.Fatalf("ManualClose = %+v, %v", res, err)
	}
	f.market.mu.Lock()
	f.market.price = 106000 // above SMA9, below SMA21: neutral, the regime-side block lifts
	f.market.mu.Unlock()
	eventually(t, 2*time.Second, "trend neutral", func() bool { return f.strat.View().Trend == "neutral" })
}

func TestRebalance_TopUpSkipsAStrangleWithAClosedLeg(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 0.5 // the 20 % limit fits 4 lots: a top-up is due
	f.cfg.RebalanceRetryMinutes = 0
	f.withBearTrendAboveFlip()
	f.startRun()

	eventually(t, 2*time.Second, "positions loaded", func() bool { _, ok := legView(t, f.strat, f.put); return ok })
	put, _ := legView(t, f.strat, f.put)
	f.closePutByHandThenNeutral(t, put.PositionID)

	eventually(t, 2*time.Second, "repair held", func() bool {
		return f.skips(strategy.SkipRepairHeld+": put stopped out, confirmed negative gamma regime") > 0
	})
	time.Sleep(200 * time.Millisecond) // many cycles and rebalance passes on a neutral trend
	if n := anyPutSells(f); n != 0 {
		t.Errorf("the closed put must not come back through a top-up: %d put sells", n)
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("a one-legged strangle is completed by repair, never topped up: %d sells", n)
	}
}

// The slot holds a second, complete strangle: its top-up would re-sell the
// put side of the slot while the first strangle's put is held.
func TestRebalance_TopUpHeldWhileASlotLegIsStopped(t *testing.T) {
	f := newStrategyFixture(t)
	call2, put2 := "BTC-"+f.expiry.Format("2Jan06")+"-115000-C", "BTC-"+f.expiry.Format("2Jan06")+"-85000-P"
	call2, put2 = strings.ToUpper(call2), strings.ToUpper(put2)
	f.market.mu.Lock()
	for name, src := range map[string]string{call2: f.call, put2: f.put} {
		cp := *f.market.instruments[src]
		cp.Name, cp.UpdatedAt = name, time.Now()
		if strings.HasSuffix(name, "-C") {
			cp.Strike, cp.Greeks.Delta = 115000, 0.13
		} else {
			cp.Strike, cp.Greeks.Delta = 85000, -0.13
		}
		f.market.instruments[name] = &cp
	}
	f.market.mu.Unlock()
	f.exch.positions = []orders.RawPosition{
		{InstrumentName: f.call, Size: -0.2, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.16},
		{InstrumentName: f.put, Size: -0.2, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.16},
		{InstrumentName: call2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.13},
		{InstrumentName: put2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.13},
	}
	share := 20.0 / 100 * f.exch.summary.MarginBalance
	f.exch.imPerLot = share / 10 // the slot fits 10 lots: a top-up is due
	f.cfg.RebalanceRetryMinutes = 0
	f.cfg.RepairCooldownHours = 72
	f.withBearTrendAboveFlip()
	f.startRun()

	eventually(t, 2*time.Second, "positions loaded", func() bool { _, ok := legView(t, f.strat, put2); return ok })
	put, _ := legView(t, f.strat, f.put)
	f.closePutByHandThenNeutral(t, put.PositionID)

	time.Sleep(300 * time.Millisecond) // many cycles and rebalance passes on a neutral trend
	if n := anyPutSells(f); n != 0 {
		t.Errorf("no put may be sold into the slot while its stopped put is held: %d put sells", n)
	}
	if f.skips(strategy.SkipRepairHeld+": top-up held: put stopped out, confirmed negative gamma regime") == 0 {
		t.Error("the held top-up must be journaled with its reason")
	}
}
