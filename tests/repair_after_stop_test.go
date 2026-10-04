package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// A stress simulation (BTC −25 % / +31 % over two weeks) showed repair
// re-selling a stopped-out leg at once, into the same move, and losing it
// again: about half of all option losses. A stopped leg now waits for a calm
// market.

func TestRepairBlockReason(t *testing.T) {
	stop := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	calm := risk.Status{RegimeUsed: true}
	cases := []struct {
		name string
		now  time.Time
		st   risk.Status
		want string // substring; "" = may repair
	}{
		{"frozen", stop.Add(100 * time.Hour), risk.Status{Frozen: true, FreezeReason: "DVOL moved"}, "entries frozen"},
		{"negative gamma", stop.Add(100 * time.Hour), risk.Status{RegimeUsed: true, RegimeNegative: true}, "negative gamma"},
		{"cooldown", stop.Add(24 * time.Hour), calm, "cooldown after stop-loss until Oct 9 12:00 UTC"},
		{"calm and cooled down", stop.Add(72 * time.Hour), calm, ""},
		{"no GEX source: regime ignored", stop.Add(80 * time.Hour), risk.Status{RegimeNegative: true}, ""},
	}
	for _, c := range cases {
		got := strategy.RepairBlockReason(stop, c.now, 72*time.Hour, c.st)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: reason = %q, want %q", c.name, got, c.want)
		}
	}
}

// stopTheCall makes the fixture's call worth 4× its premium, so the first
// cycle stops it out (bought back in full).
func (f *strategyFixture) stopTheCall() {
	f.withOpenStrangle(0.1, 0.01)
	f.market.setQuote(f.call, 0.039, 0.041)
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.04}
		}
		return orders.Fill{}
	}
}

func (f *strategyFixture) callSells() int {
	n := 0
	for _, o := range f.exch.sells() {
		if o.Instrument == f.call {
			n++
		}
	}
	return n
}

func TestStrategy_StoppedLegIsNotResoldDuringCooldown(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.stopTheCall()
	f.startRun()

	eventually(t, 2*time.Second, "call stopped out", func() bool { return len(f.journal.events(orders.EventClosed)) >= 1 })
	time.Sleep(150 * time.Millisecond) // many cycles
	if n := f.callSells(); n != 0 {
		t.Errorf("a stopped-out call must not be re-sold during the cooldown: %d sells", n)
	}
	held := false
	for _, e := range f.journal.events(orders.EventSkipped) {
		held = held || strings.HasPrefix(e.reason, "repair_held: call stopped out, cooldown")
	}
	if !held {
		t.Error("the held repair must be journaled with its reason")
	}
}

func TestStrategy_StoppedLegIsNotResoldWhileFrozen(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 0 // cooldown already over: only the freeze holds it
	f.stopTheCall()
	f.market.setIVHistory([]float64{10, 10, 10}, 90) // DVOL just jumped bands → frozen
	f.startRun()

	eventually(t, 2*time.Second, "call stopped out", func() bool { return len(f.journal.events(orders.EventClosed)) >= 1 })
	time.Sleep(150 * time.Millisecond)
	if n := f.callSells(); n != 0 {
		t.Errorf("no re-sale of a stopped leg while entries are frozen: %d sells", n)
	}
}

func TestStrategy_StoppedLegIsResoldOnceCalm(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 0 // calm market, cooldown over
	f.stopTheCall()
	f.startRun()

	eventually(t, 2*time.Second, "stopped call re-sold by repair", func() bool { return f.callSells() >= 1 })
}
