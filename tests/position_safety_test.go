package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Guards added after the 2026-10-06 testnet night (see strategy/safety.go).

func (f *fakeExchange) setPosition(instrument string, size float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.positions {
		if f.positions[i].InstrumentName == instrument {
			f.positions[i].Size = size
			f.positions[i].Direction = orders.DirectionSell
			if size > 0 {
				f.positions[i].Direction = orders.DirectionBuy
			}
		}
	}
}

func (f *fakeExchange) ordersOn(instrument, direction string) (qty float64, n int) {
	for _, o := range f.ordersWhere(func(o orders.Order) bool { return o.Instrument == instrument && o.Direction == direction }) {
		qty += o.Qty
		n++
	}
	return qty, n
}

// The book holds 0.2 of the call, the exchange only 0.1 (the drift check has
// not confirmed it yet) and the stop-loss fires: it buys 0.1, never 0.2 —
// buying the book's size would leave the account long 0.1.
func TestSafety_BuyBackCappedAtTheExchangeShort(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.2, 0.01)
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return math.Abs(f.bookQty(f.call)-0.2) < 1e-9 })

	f.exch.setPosition(f.call, -0.1)
	f.market.setQuote(f.call, 0.039, 0.041) // mark 0.04 = 4× premium: stop-loss

	eventually(t, 2*time.Second, "stop-loss sent", func() bool { _, n := f.exch.ordersOn(f.call, orders.DirectionBuy); return n > 0 })
	time.Sleep(100 * time.Millisecond)
	if qty, _ := f.exch.ordersOn(f.call, orders.DirectionBuy); qty > 0.1+1e-9 {
		t.Errorf("bought back %v, the exchange only held 0.1 short", qty)
	}
}

// A long on an instrument the bot trades is sold at the bid; a long the bot
// never traded (a hand-placed hedge) is left alone.
func TestSafety_LongOnTradedInstrumentIsSold(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: "BTC-25DEC26-60000-P", Size: 1.7, Direction: orders.DirectionBuy, AveragePrice: 0.067})
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllPositions()) == 2 })

	f.exch.setPosition(f.call, 0.3) // flipped long by an over-sized buy-back

	eventually(t, 2*time.Second, "long sold", func() bool {
		return len(f.exch.ordersWhere(func(o orders.Order) bool { return o.TriggerReason == orders.TriggerCloseLong })) > 0
	})
	o := f.exch.ordersWhere(func(o orders.Order) bool { return o.TriggerReason == orders.TriggerCloseLong })[0]
	if o.Instrument != f.call || o.Direction != orders.DirectionSell || math.Abs(o.Qty-0.3) > 1e-9 ||
		o.LimitPrice != 0.019 || o.TimeInForce != orders.TimeInForceIOC {
		t.Errorf("close_long order = %+v, want IOC sell 0.3 %s at the bid 0.019", o, f.call)
	}
	if _, n := f.exch.ordersOn("BTC-25DEC26-60000-P", orders.DirectionSell); n != 0 {
		t.Error("a long the bot never traded must be left alone")
	}
}

func TestChurnTripped(t *testing.T) {
	cases := []struct {
		buys, sells, max int
		want             bool
	}{
		{3, 3, 3, true}, {3, 2, 3, false}, {2, 3, 3, false}, {9, 9, 0, false},
	}
	for _, c := range cases {
		if got := strategy.ChurnTripped(c.buys, c.sells, c.max); got != c.want {
			t.Errorf("ChurnTripped(%d, %d, %d) = %v, want %v", c.buys, c.sells, c.max, got, c.want)
		}
	}
}

// A deterministic churn loop: every re-sold put is booked at 0.02 while the
// market marks it at 0.005 (75 % profit), so it is rolled at once and repair
// re-sells it. The breaker pauses the slot after 2 round trips.
func TestSafety_ChurnBreakerPausesTheSlot(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ChurnMaxRoundTrips = 2
	f.cfg.ChurnWindowMinutes = 60
	f.withOpenStrangle(0.1, 0.02)
	f.market.setQuote(f.put, 0.004, 0.006)
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.006}
		}
		return orders.Fill{Qty: o.Qty, FillPrice: 0.02}
	}
	f.startRun()

	eventually(t, 3*time.Second, "slot paused", func() bool { return f.skips(strategy.SkipChurnPaused) > 0 })
	_, sellsAtPause := f.exch.ordersOn(f.put, orders.DirectionSell)
	time.Sleep(300 * time.Millisecond) // ~30 cycles
	if _, n := f.exch.ordersOn(f.put, orders.DirectionSell); n != sellsAtPause || n > 2 {
		t.Errorf("no re-sell while paused: %d put sells at the pause, %d now (want ≤ 2)", sellsAtPause, n)
	}
}
