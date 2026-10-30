package tests

import (
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

func (f *strategyFixture) qty(instrument string) float64 {
	if p := f.position(instrument); p != nil {
		return p.Qty
	}
	return 0
}

// The 2026-10-04 case: the exchange holds 2.4 calls against 1.2 puts (here
// 0.3 vs 0.1). The bot buys back the excess call, at the ask, IOC.
func TestStrategy_UnevenStrangleFromStartupIsBalanced(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions[0].Size = -0.3 // the call
	f.startRun()

	eventually(t, 2*time.Second, "legs balanced", func() bool { return math.Abs(f.qty(f.call)-0.1) < 1e-9 })
	buys := f.exch.buys()
	if len(buys) != 1 {
		t.Fatalf("one buy-back expected, got %d", len(buys))
	}
	b := buys[0]
	if b.Instrument != f.call || math.Abs(b.Qty-0.2) > 1e-9 || b.TriggerReason != orders.TriggerRebalanceLegs ||
		b.OrderType != orders.TypeLimit || b.TimeInForce != orders.TimeInForceIOC || b.LimitPrice != 0.021 {
		t.Errorf("buy-back = %+v, want 0.2 of the call, IOC limit at the ask 0.021", b)
	}
	if f.qty(f.put) != 0.1 {
		t.Errorf("the smaller leg is never touched: put %v", f.qty(f.put))
	}
	closed := f.journal.events(orders.EventClosed)
	if len(closed) != 1 || closed[0].trigger != orders.TriggerRebalanceLegs {
		t.Errorf("the buy-back must be journaled as rebalance_legs: %+v", closed)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.buys()); n != 1 {
		t.Errorf("a balanced strangle needs nothing more: %d buys", n)
	}
}

// A partial entry fill books uneven legs; the next cycles even them out.
func TestStrategy_PartialEntryFillIsBalanced(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 1
	f.exch.imPerLot = 0.5 // entry of 0.4 per leg
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	f.exch.partial(f.exch.orderIDFor(f.call, 0), 0.3, 0.021)
	f.exch.partial(f.exch.orderIDFor(f.put, 0), 0.1, 0.021)
	eventually(t, 4*time.Second, "uneven fill booked then balanced", func() bool {
		return f.qty(f.put) > 0 && math.Abs(f.qty(f.call)-f.qty(f.put)) < 1e-9
	})
	if f.qty(f.put) != 0.1 {
		t.Errorf("balanced down to the smaller leg (0.1), got call %v put %v", f.qty(f.call), f.qty(f.put))
	}
}

func TestStrategy_BalanceWaitsForAnAsk(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions[0].Size = -0.3
	// No ask on the book, but the mark is still there (as on Deribit).
	f.market.mu.Lock()
	f.market.instruments[f.call].Ask = 0
	f.market.mu.Unlock()
	f.startRun()
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.buys()); n != 0 {
		t.Fatalf("no buy-back without an ask: %d buys", n)
	}
	f.market.setQuote(f.call, 0.019, 0.021)
	eventually(t, 2*time.Second, "balanced once quoted", func() bool { return math.Abs(f.qty(f.call)-0.1) < 1e-9 })
}

// Only uneven strangles with both legs are touched: a one-legged strangle is
// repair's job, not a call to buy back the leg that is there.
func TestStrategy_BalanceLeavesOneLeggedStranglesToRepair(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.3)
	f.startRun()
	eventually(t, 2*time.Second, "repair sells the missing put", func() bool { return len(f.exch.sells()) >= 1 })
	for _, b := range f.exch.buys() {
		if strings.Contains(b.TriggerReason, "rebalance_legs") {
			t.Errorf("a one-legged strangle must not be trimmed: %+v", b)
		}
	}
	if f.qty(f.call) != 0.3 {
		t.Errorf("the lone call stays: %v", f.qty(f.call))
	}
}

// #554, stable ETH 2026-10-30: an exit's IOC filled part of a leg at |Δ| on
// the threshold, the delta ticked back across it, and leg balancing trimmed
// the other leg before the exit's next try. A leg an exit closed in part is
// "closing": the other leg waits, and the exit retries while the rule fires.
func TestStrategy_ADriftRollFillingInPiecesIsNotTrimmed(t *testing.T) {
	cases := []struct {
		name     string
		trigger  string
		setup    func(f *strategyFixture)
		breach   float64 // the call's delta that fires the rule
		recovery float64 // one tick back: the rule stops firing
	}{
		{"delta drift", orders.TriggerRolloutDelta, func(f *strategyFixture) {
			f.market.setQuote(f.call, 0.021, 0.021) // no room for a mid rest: IOC at the ask at once
		}, 0.01, 0.06},
		{"delta exit", orders.TriggerDeltaExit, func(f *strategyFixture) {
			f.cfg.DeltaExitThreshold = 0.30
			f.market.setQuote(f.call, 0.029, 0.031)
		}, 0.35, 0.29},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.withOpenStrangle(0.3, 0.02)
			tc.setup(f)
			f.market.setDelta(f.call, tc.breach)
			var mu sync.Mutex
			pieces := 0
			f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
				if o.Direction != orders.DirectionBuy || o.Instrument != f.call {
					return orders.Fill{Qty: o.Qty, FillPrice: o.LimitPrice}
				}
				mu.Lock()
				defer mu.Unlock()
				pieces++
				if pieces == 1 {
					f.market.setDelta(f.call, tc.recovery) // the tick back
					return orders.Fill{Qty: 0.1, FillPrice: o.LimitPrice}
				}
				return orders.Fill{Qty: o.Qty, FillPrice: o.LimitPrice}
			}
			f.startRun()

			eventually(t, 2*time.Second, "first piece of the exit booked", func() bool {
				return math.Abs(f.qty(f.call)-0.2) < 1e-9
			})
			time.Sleep(150 * time.Millisecond) // many cycles with the rule silent
			for _, b := range f.exch.buys() {
				if b.Instrument == f.put {
					t.Fatalf("the put must not be trimmed while the call is closing: %+v", b)
				}
			}
			if f.qty(f.put) != 0.3 || math.Abs(f.qty(f.call)-0.2) > 1e-9 {
				t.Fatalf("call %v / put %v, want 0.2 / 0.3", f.qty(f.call), f.qty(f.put))
			}

			f.market.setDelta(f.call, tc.breach) // the rule fires again: the exit finishes
			eventually(t, 2*time.Second, "the call's remainder closed by its exit", func() bool {
				for _, b := range f.exch.buys() {
					if b.Instrument == f.call && b.TriggerReason == tc.trigger && math.Abs(b.Qty-0.2) < 1e-9 {
						return true
					}
				}
				return false
			})
			for _, b := range f.exch.buys() {
				if b.TriggerReason == orders.TriggerRebalanceLegs {
					t.Errorf("no leg balancing around a closing exit: %+v", b)
				}
			}
		})
	}
}

// The mark lasts while the rule fires, and ClosingHold after the last piece
// once it stops; then a still-uneven strangle is balanced as usual.
func TestClosingHeld(t *testing.T) {
	last := time.Date(2026, 10, 30, 0, 29, 21, 0, time.UTC)
	cases := []struct {
		name  string
		now   time.Time
		fires bool
		want  bool
	}{
		{"rule fires", last.Add(2 * time.Hour), true, true},
		{"rule silent, inside the hold", last.Add(time.Minute), false, true},
		{"rule silent, a second before the hold ends", last.Add(strategy.ClosingHold - time.Second), false, true},
		{"rule silent, the hold over", last.Add(strategy.ClosingHold), false, false},
	}
	for _, tc := range cases {
		if got := strategy.ClosingHeld(last, tc.now, strategy.ClosingHold, tc.fires); got != tc.want {
			t.Errorf("%s: ClosingHeld = %v, want %v", tc.name, got, tc.want)
		}
	}
	if strategy.ClosingHold != 30*time.Minute {
		t.Errorf("ClosingHold = %v, want 30 min (fixed.go)", strategy.ClosingHold)
	}
}
