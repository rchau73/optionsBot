package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// 2025-10-10 in the stress tool: the first price after a 2-minute wick is the
// bottom of an empty book (ask 35× the premium). The stop now waits while the
// ask is far above the mid and re-checks each cycle.

func TestStopDeferReason(t *testing.T) {
	now := time.Date(2026, 10, 8, 21, 0, 0, 0, time.UTC)
	wait := 5 * time.Minute
	cases := []struct {
		name     string
		bid, ask float64
		since    time.Time
		guard    float64
		want     string // substring; "" = fire now
	}{
		{"normal spread", 0.026, 0.029, time.Time{}, 20, ""},
		{"empty book", 0.114, 0.342, time.Time{}, 20, "50% above mid 0.2280"},
		{"no bid", 0, 0.342, time.Time{}, 20, "no bid"},
		{"no ask: at market as before", 0.1, 0, time.Time{}, 20, ""},
		{"guard off", 0.114, 0.342, time.Time{}, -1, ""},
		{"still empty within the wait", 0.114, 0.342, now.Add(-4 * time.Minute), 20, "above mid"},
		{"waited long enough: at market", 0.114, 0.342, now.Add(-5 * time.Minute), 20, ""},
	}
	for _, c := range cases {
		got := strategy.StopDeferReason(c.bid, c.ask, c.since, now, c.guard, wait)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: reason = %q, want %q", c.name, got, c.want)
		}
	}
}

// guardStop: the call is worth 4× its premium (a stop) but its book is empty.
func (f *strategyFixture) guardStop() {
	f.cfg.StopSpreadGuardPct = 20
	f.cfg.StopSpreadMaxWaitMin = 5
	f.stopTheCall()
	f.market.setQuote(f.call, 0.01, 0.07) // mid 0.04, ask 75 % above it
}

func TestStrategy_StopWaitsForTheSpreadThenFires(t *testing.T) {
	f := newStrategyFixture(t)
	f.guardStop()
	f.startRun()

	eventually(t, 2*time.Second, "stop deferred and journaled", func() bool { return f.skips("stop_deferred") > 0 })
	time.Sleep(100 * time.Millisecond) // many cycles
	if n := len(f.exch.buys()); n != 0 {
		t.Fatalf("no buy-back into an empty book: %d buys", n)
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no new risk while a stop waits: %d sells", n)
	}
	if n := f.skips("stop_deferred"); n != 1 {
		t.Errorf("the wait is journaled once, not every cycle: %d", n)
	}

	f.market.setQuote(f.call, 0.039, 0.041) // the book refills, the stop still applies
	eventually(t, 2*time.Second, "stop fires once the spread is normal", func() bool {
		b := f.exch.buys()
		return len(b) == 1 && b[0].Instrument == f.call && b[0].TriggerReason == orders.TriggerStopLoss200Pct
	})
}

func TestStrategy_DeferredStopIsDroppedWhenThePriceComesBack(t *testing.T) {
	f := newStrategyFixture(t)
	f.guardStop()
	f.startRun()

	eventually(t, 2*time.Second, "stop deferred", func() bool { return f.skips("stop_deferred") > 0 })
	f.market.setQuote(f.call, 0.019, 0.021) // the wick reverted: back under the stop
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("the price came back under the stop: keep the leg, got %d buys", n)
	}
	if f.position(f.call) == nil {
		t.Error("the call stays in the book")
	}
}

// 2026-10-08 ETH: a drift roll of the call filled in pieces, and between them
// balance legs bought back puts to match — the strangle shrank 503 → 303.
func TestStrategy_BalanceWaitsWhileTheSmallerLegIsBeingRolled(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions[1].Size = -0.3 // the put is larger
	f.market.mu.Lock()
	f.market.instruments[f.call].Greeks.Delta = 0.01 // drift: the call is being rolled
	f.market.mu.Unlock()
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { return orders.Fill{} } // the roll does not fill yet
	f.startRun()

	eventually(t, 2*time.Second, "drift roll attempted", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == f.call && b.TriggerReason == orders.TriggerRolloutDelta {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	for _, b := range f.exch.buys() {
		if b.Instrument == f.put {
			t.Fatalf("the put must not be trimmed while the call is being rolled: %+v", b)
		}
	}
	if math.Abs(f.qty(f.put)-0.3) > 1e-9 {
		t.Errorf("put = %v, want 0.3", f.qty(f.put))
	}
}

// 2026-10-08 BTC: a put top-up sent before a freeze filled two minutes into
// it. Now a freeze cancels working entries and top-ups.
func TestStrategy_FreezeCancelsWorkingEntries(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()

	eventually(t, 2*time.Second, "entry resting", func() bool { return len(f.exch.sells()) == 2 })
	callID, putID := f.exch.orderIDFor(f.call, 0), f.exch.orderIDFor(f.put, 0)
	f.market.setIVHistory([]float64{10, 10, 10}, 90) // DVOL jumps bands: new risk frozen

	eventually(t, 2*time.Second, "both legs cancelled by the freeze", func() bool {
		return f.exch.wasCancelled(callID) && f.exch.wasCancelled(putID)
	})
	n := 0
	for _, e := range f.journal.events(orders.EventCancelled) {
		if e.trigger == orders.TriggerRiskFrozen {
			n++
		}
	}
	if n != 2 {
		t.Errorf("both cancels journaled as risk_frozen: %d", n)
	}
	time.Sleep(100 * time.Millisecond)
	if s := len(f.exch.sells()); s != 2 {
		t.Errorf("nothing new while frozen: %d sells", s)
	}
}
