package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/orders"
)

// P&L is net of Deribit's fees everywhere: the opening fee rides on the
// position, a close books its share plus the closing fee.

func TestClosedNetPnL(t *testing.T) {
	closed := &orders.Position{Qty: 0.5, PremiumReceived: 0.01, Fees: 0.00015} // half of a 1-lot position
	fill := orders.Fill{FillPrice: 0.004, Fee: 0.00015}
	// 0.01 premium − 0.002 buy-back − 0.00015 opening share − 0.00015 closing fee
	if got := orders.ClosedNetPnL(closed, fill); math.Abs(got-0.0077) > 1e-12 {
		t.Errorf("ClosedNetPnL = %v, want 0.0077", got)
	}
	p := &orders.Position{Qty: 1, PremiumReceived: 0.02, CurrentMid: 0.01, Fees: 0.0003}
	if got := p.NetPnL(); math.Abs(got-0.0097) > 1e-12 {
		t.Errorf("NetPnL = %v, want 0.0097 (0.02 − 0.01 − 0.0003)", got)
	}
}

func TestOptionFee(t *testing.T) {
	if got := orders.OptionFee(0.0003, 0.02, 2); math.Abs(got-0.0006) > 1e-12 {
		t.Errorf("fee = %v, want 0.0006 (0.0003 × 2)", got)
	}
	if got := orders.OptionFee(0.0003, 0.001, 2); math.Abs(got-0.00025) > 1e-12 {
		t.Errorf("fee = %v, want 0.00025 (capped at 12.5 %% of 0.001 × 2)", got)
	}
}

// End to end: an entry fills with fees, the stop-loss closes it, and the
// realised P&L in the view and the journal are both net of both fees.
func TestNetPnL_EntryAndCloseFeesAreBooked(t *testing.T) {
	f, qty := stopOneFeeChargedLeg(t)

	want := 0.02*qty - 0.05*qty - 0.0003*qty - 0.0003*qty
	if got := slotRealised(f); math.Abs(got-want) > 1e-9 {
		t.Errorf("realised = %v, want %v (premium − buy-back − both fees)", got, want)
	}
}

// Scenario (#165): a close fills while the next cycle starts → the realised
// P&L is booked exactly once. The cycles after the stop must not book it
// again (the stopped call is held by the repair cooldown, so nothing re-sells
// and re-stops it).
func TestNetPnL_CloseBookedOnceWhileNextCycleRuns(t *testing.T) {
	f, qty := stopOneFeeChargedLeg(t)
	booked := slotRealised(f)

	// Let the loop complete a few more cycles after the close.
	after := f.strat.View().LoopAt
	for i := 0; i < 3; i++ {
		eventually(t, 2*time.Second, "another decision cycle", func() bool { return f.strat.View().LoopAt.After(after) })
		after = f.strat.View().LoopAt
	}

	if n := len(f.journal.events(orders.EventClosed)); n != 1 {
		t.Errorf("%d closes journaled, want 1", n)
	}
	want := 0.02*qty - 0.05*qty - 0.0003*qty - 0.0003*qty
	if got := slotRealised(f); math.Abs(got-booked) > 1e-12 || math.Abs(got-want) > 1e-9 {
		t.Errorf("realised = %v after more cycles (%v at the close), want %v once", got, booked, want)
	}
	var closedLegs int
	for _, l := range f.strat.View().PnL {
		if l.Slot != nil {
			closedLegs += l.ClosedLegs
		}
	}
	if closedLegs != 1 {
		t.Errorf("closed legs = %d, want 1", closedLegs)
	}
}

// stopOneFeeChargedLeg opens a fee-charged strangle, stops its call and
// waits until the close is in the realised P&L. It returns the leg size.
// The repair cooldown is the shipped default (config.Load sets 72 h when the
// file leaves it out): with the fixture's zero, repair re-sold the stopped
// call at once and the stop closed it again — two real closes, not one booked
// twice (#165).
func stopOneFeeChargedLeg(t *testing.T) (*strategyFixture, float64) {
	t.Helper()
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.exch.feePerContract = 0.0003
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.05}
		}
		return orders.Fill{Qty: o.Qty, FillPrice: 0.02}
	}
	f.startRun()
	eventually(t, 2*time.Second, "strangle open", func() bool { return len(f.state.AllPositions()) == 2 })
	for _, p := range f.state.AllPositions() {
		if math.Abs(p.Fees-0.0003*p.Qty) > 1e-12 {
			t.Errorf("%s: opening fee %v, want %v", p.Instrument, p.Fees, 0.0003*p.Qty)
		}
	}
	qty := f.state.AllPositions()[0].Qty

	f.market.setQuote(f.call, 0.069, 0.071) // loss 2.5× the premium: stop-loss
	// The close is journaled just before it is booked: wait on the booking.
	eventually(t, 2*time.Second, "call stop booked", func() bool { return slotRealised(f) != 0 })
	return f, qty
}

// slotRealised sums the realised P&L of the slot rows of the view.
func slotRealised(f *strategyFixture) float64 {
	var realised float64
	for _, l := range f.strat.View().PnL {
		if l.Slot != nil {
			realised += l.Realised
		}
	}
	return realised
}
