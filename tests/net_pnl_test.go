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
	f := newStrategyFixture(t)
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
	eventually(t, 2*time.Second, "call stopped", func() bool { return len(f.journal.events(orders.EventClosed)) > 0 })

	want := 0.02*qty - 0.05*qty - 0.0003*qty - 0.0003*qty
	var realised float64
	for _, l := range f.strat.View().PnL {
		if l.Slot != nil {
			realised += l.Realised
		}
	}
	if math.Abs(realised-want) > 1e-9 {
		t.Errorf("realised = %v, want %v (premium − buy-back − both fees)", realised, want)
	}
}
