package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/orders"
)

// 2026-10-06 night: a rebalance complement filled on the same instruments as
// the strangle it tops up, so the book held two positions per instrument. The
// drift check compared the exchange's sum with only one of them, "adopted" the
// sum into that one (doubling the book), and balanceStrangles bought back the
// phantom excess at the ask while repair re-sold it at the bid — for hours.

func (f *strategyFixture) bookQty(instrument string) float64 {
	sum := 0.0
	for _, p := range f.state.AllPositions() {
		if p.Instrument == instrument {
			sum += p.Qty
		}
	}
	return sum
}

func TestDrift_TwoPositionsOnOneInstrumentAreNotDrift(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // 50 % band: 0.1 complement
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		return orders.Fill{Qty: o.Qty, FillPrice: 0.02} // everything fills at once
	}
	f.startRun()

	eventually(t, 2*time.Second, "complement booked", func() bool { return len(f.state.AllStrangles()) == 2 })
	reconciledAtStart := len(f.journal.events(orders.EventReconciled))
	time.Sleep(300 * time.Millisecond) // ~30 cycles: drift needs 2

	if n := len(f.journal.events(orders.EventReconciled)) - reconciledAtStart; n != 0 {
		t.Errorf("book and exchange agree (0.2 each): no adoption expected, got %d", n)
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("nothing to buy back, got %d buys", n)
	}
	for _, inst := range []string{f.call, f.put} {
		if q := f.bookQty(inst); math.Abs(q-0.2) > 1e-9 {
			t.Errorf("%s: book holds %v, want 0.2 (as the exchange)", inst, q)
		}
	}
}

// A real difference is still adopted, and split across the positions: a
// shortfall comes off the newest position first.
func TestDrift_ShortfallTakenFromNewestPosition(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80)
	sold := 0
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionSell {
			if sold++; sold > 2 {
				return orders.Fill{} // later sells (the repair) rest unfilled
			}
		}
		return orders.Fill{Qty: o.Qty, FillPrice: 0.02}
	}
	f.startRun()
	eventually(t, 2*time.Second, "complement booked", func() bool { return len(f.state.AllStrangles()) == 2 })
	var original *orders.Position // the oldest call: the reconciled one
	for _, p := range f.state.AllPositions() {
		if p.Instrument == f.call && (original == nil || p.EntryTime.Before(original.EntryTime)) {
			original = p
		}
	}

	// Someone closes 0.1 of the call outside the bot.
	f.exch.mu.Lock()
	for i := range f.exch.positions {
		if f.exch.positions[i].InstrumentName == f.call {
			f.exch.positions[i].Size = -0.1
		}
	}
	f.exch.mu.Unlock()

	eventually(t, 2*time.Second, "call adopted at 0.1", func() bool { return math.Abs(f.bookQty(f.call)-0.1) < 1e-9 })
	time.Sleep(200 * time.Millisecond)
	if q := f.bookQty(f.call); math.Abs(q-0.1) > 1e-9 {
		t.Errorf("after adoption the book must stay at the exchange's 0.1, got %v", q)
	}
	if _, ok := f.state.GetPosition(original.ID); !ok {
		t.Error("the shortfall must come off the newest position, not the original")
	}
}
