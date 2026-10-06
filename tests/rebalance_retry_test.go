package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/orders"
)

// A rebalance complement that times out short of its size re-arms the
// rebalance: without it the book would stay under the IM limit until the next
// confirmed limit change or a restart (seen on testnet: IM 9.6 % vs 20 % for
// hours, no orders working).

// upshiftToTwoLots loads a one-lot strangle and confirms the 50 % band, which
// fits two lots: the startup rebalance sends a 0.1 complement.
func (f *strategyFixture) upshiftToTwoLots() {
	f.withOpenStrangle(0.1, 0.02)
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80)
	f.cfg.OrderFillTimeoutSec = 0 // complements time out on the next cycle
}

func TestRebalanceRetry_UnfilledComplementIsRetried(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 0
	f.startRun()

	eventually(t, 3*time.Second, "complement sent again after the first timed out",
		func() bool { return len(f.exch.sells()) >= 4 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.1) > 1e-9 {
			t.Errorf("every complement is the missing lot (0.1), got %v", o.Qty)
		}
	}
	if len(f.journal.riskChanges(orders.RiskRebalanceRetry)) == 0 {
		t.Error("the retry must be journaled")
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("an under-filled upsize never closes legs, got %d buys", n)
	}
}

func TestRebalanceRetry_WaitsForTheCooldown(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 60
	f.startRun()

	eventually(t, 3*time.Second, "retry scheduled after the timeout",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	time.Sleep(200 * time.Millisecond) // ~20 cycles
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("no new complement inside the cooldown: want 2 sells, got %d", n)
	}
}

// A complement that filled in part leaves a second strangle in the slot. The
// retry sizes the slot as a whole: it sends only what is still missing, not a
// full complement for each strangle.
func TestRebalanceRetry_PartialFillSendsOnlyTheRest(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 2.0 / 3 * 0.999 // slot share 2.0 fits 3 lots: complement 0.2
	f.cfg.OrderFillTimeoutSec = 0
	f.cfg.RebalanceRetryMinutes = 0
	sold := 0
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.01}
		}
		sold++
		if sold <= 2 { // the first complement fills 0.1 of 0.2 on each leg
			return orders.Fill{Qty: 0.1, FillPrice: 0.02}
		}
		return orders.Fill{}
	}
	f.startRun()

	eventually(t, 3*time.Second, "retry sent", func() bool { return len(f.exch.sells()) >= 4 })
	sells := f.exch.sells()
	for i, o := range sells {
		want := 0.1 // slot holds 0.2 of 0.3: one lot missing
		if i < 2 {
			want = 0.2
		}
		if math.Abs(o.Qty-want) > 1e-9 {
			t.Errorf("sell %d (%s) qty = %v, want %v", i, o.Instrument, o.Qty, want)
		}
	}
	if len(f.state.AllStrangles()) != 2 {
		t.Errorf("the filled part is its own strangle in the slot, got %d strangles", len(f.state.AllStrangles()))
	}
}
