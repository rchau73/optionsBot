package tests

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// Regression for 2026-10-04: a repair sell reached Deribit as the connection
// dropped. The bot read "connection lost" as "not placed", sent a
// replacement later, and both filled: twice the call it should hold.

func TestMaybePlaced(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"connection lost":        {fmt.Errorf("private/sell: %w", gateway.ErrConnectionLost), true},
		"reply timed out":        {fmt.Errorf("private/sell: %w", gateway.ErrRequestTimeout), true},
		"Deribit rejected it":    {&gateway.RPCError{Code: 10009, Message: "not_enough_funds"}, false},
		"circuit open: not sent": {fmt.Errorf("x: %w", gateway.ErrCircuitOpen), false},
		"no error":               {nil, false},
	}
	for name, c := range cases {
		if got := orders.MaybePlaced(c.err); got != c.want {
			t.Errorf("%s: MaybePlaced = %v, want %v", name, got, c.want)
		}
	}
}

// A lone call needs its put repaired; the repair's reply is lost although the
// order is on the book.
func TestStrategy_LostSubmitReplyIsCancelledByLabelAndNotDoubled(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	lost := true
	f.exch.loseReply = func(o orders.Order) bool {
		if o.Instrument == f.put && lost {
			lost = false
			return true
		}
		return false
	}
	f.startRun()

	eventually(t, 2*time.Second, "unconfirmed order cancelled by its label", func() bool {
		f.exch.mu.Lock()
		defer f.exch.mu.Unlock()
		return len(f.exch.cancelledLabels) == 1
	})
	f.exch.mu.Lock()
	label := f.exch.cancelledLabels[0]
	f.exch.mu.Unlock()
	if label != f.exch.sells()[0].Label {
		t.Errorf("must cancel exactly the lost order's label: %q vs %q", label, f.exch.sells()[0].Label)
	}
	// The lost order is cancelled, so the repair is sent again — once.
	eventually(t, 2*time.Second, "repair re-sent after the cancel", func() bool { return len(f.exch.sells()) == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("exactly one replacement after the lost order is cancelled, got %d sells", n)
	}
}

// The lost order filled before the bot could cancel it: the position check
// adopts it, the strangle is complete, and nothing more is sold.
func TestStrategy_OrphanFillIsAdoptedAndNotRepairedAgain(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.exch.loseReply = func(o orders.Order) bool { return o.Instrument == f.put }
	f.exch.cancelLabelErr = errors.New("connection lost") // can't cancel yet
	f.startRun()

	eventually(t, 2*time.Second, "lost order sent", func() bool { return len(f.exch.sells()) == 1 })
	f.exch.fill("o-1", 0.1, 0.021) // it fills on the exchange, untracked
	eventually(t, 2*time.Second, "orphan fill adopted into the book", func() bool {
		p := f.position(f.put)
		return p != nil && math.Abs(p.Qty-0.1) < 1e-9
	})
	st := f.state.AllStrangles()
	if len(st) != 1 || st[0].PutLeg == nil || st[0].CallLeg == nil {
		t.Fatalf("the adopted put must complete the strangle: %+v", st)
	}
	f.exch.mu.Lock()
	f.exch.cancelLabelErr = nil
	f.exch.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.sells()); n != 1 {
		t.Errorf("an adopted leg must not be repaired again: %d sells", n)
	}
	if n := len(f.journal.events(orders.EventReconciled)); n != 2 {
		t.Errorf("the adoption must be journaled: want the startup call + the adopted put, got %d", n)
	}
}

// A position closed outside the bot (or a missed close fill) leaves the book.
func TestStrategy_PositionGoneFromExchangeLeavesTheBook(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "loaded", func() bool { return len(f.state.AllPositions()) == 2 })

	f.exch.mu.Lock()
	f.exch.positions = f.exch.positions[:1] // the put was bought back by hand
	f.exch.mu.Unlock()
	eventually(t, 2*time.Second, "put removed from the book", func() bool { return f.position(f.put) == nil })
	if f.position(f.call) == nil {
		t.Error("the call is still on the exchange and must stay")
	}
}

// A stop-loss whose reply is lost must not be sent again (that could leave
// the account long); the book follows the exchange instead.
func TestStrategy_LostCloseReplyIsNotSentTwice(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.01)
	f.market.setQuote(f.call, 0.039, 0.041) // stop-loss
	first := true
	f.exch.loseReply = func(o orders.Order) bool {
		if o.Direction == orders.DirectionBuy && first {
			first = false
			return true
		}
		return false
	}
	f.startRun()
	eventually(t, 2*time.Second, "stop-loss sent", func() bool { return len(f.exch.buys()) == 1 })
	f.exch.fill("o-1", 0.1, 0.04) // it executed; the reply was lost
	eventually(t, 2*time.Second, "book follows the exchange", func() bool { return f.position(f.call) == nil })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.buys()); n != 1 {
		t.Errorf("the close must not be sent twice: %d buys", n)
	}
}
