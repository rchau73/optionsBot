package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// One new-risk sizer at a time (#97): an entry and a rebalance top-up sized
// in the same minutes, each against the book without the other's orders,
// filled BTC's IM to 25.7 % of a 20 % limit (2026-10-15), and the next pass
// bought the overshoot back. A due rebalance now waits for a working entry,
// and an entry waits for a working top-up.

// withVacantSecondSlot adds a vacant 25-DTE slot on its own expiry next to
// the fixture's 45-DTE slot, which holds a one-lot strangle sized for the
// 20 % band. It returns the vacant slot's call and put.
func (f *strategyFixture) withVacantSecondSlot() (call, put string) {
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{
		{DTE: 45, Deltas: []float64{0.16}},
		{DTE: 25, Deltas: []float64{0.16}},
	}
	f.withOpenStrangle(0.1, 0.02)
	expiry := time.Now().UTC().AddDate(0, 0, 25).Truncate(24 * time.Hour).Add(8 * time.Hour)
	label := strings.ToUpper(expiry.Format("2Jan06"))
	call, put = fmt.Sprintf(testCall, label), fmt.Sprintf(testPut, label)
	f.market.mu.Lock()
	f.market.instruments[call] = fixtureInstrument(call, "call", expiry, 110000, 0.16)
	f.market.instruments[put] = fixtureInstrument(put, "put", expiry, 90000, -0.16)
	f.market.mu.Unlock()
	return call, put
}

// sellsOn counts sells of the given instruments.
func (f *strategyFixture) sellsOn(names ...string) int {
	n := 0
	for _, o := range f.exch.sells() {
		for _, name := range names {
			if o.Instrument == name {
				n++
			}
		}
	}
	return n
}

// fillWorkingSells fills every resting sell of the given instruments.
func (f *strategyFixture) fillWorkingSells(names ...string) {
	for _, name := range names {
		for i := 0; ; i++ {
			id := f.exch.orderIDFor(name, i)
			if id == "" {
				break
			}
			f.exch.mu.Lock()
			st, o := f.exch.orderStates[id], f.exch.byID[id]
			f.exch.mu.Unlock()
			if st.State == "open" && o.Direction == orders.DirectionSell {
				f.exch.fill(id, o.Qty, 0.02)
			}
		}
	}
}

// entryWorkingThenUpshift starts with the 45-DTE slot at its 20 % target and
// the 25-DTE slot vacant: the entry is sent and rests. Then the 50 % band is
// confirmed, so a rebalance pass comes due while the entry works.
func (f *strategyFixture) entryWorkingThenUpshift(t *testing.T) (call, put string) {
	t.Helper()
	call, put = f.withVacantSecondSlot()
	f.startRun()
	eventually(t, 3*time.Second, "entry sent and resting", func() bool { return f.sellsOn(call, put) == 2 })
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // 50 % band confirmed: the 45-DTE slot is short
	return call, put
}

// Scenario: no top-up while an entry is working.
func TestOneSizer_NoTopUpWhileAnEntryIsWorking(t *testing.T) {
	f := newStrategyFixture(t)
	call, put := f.entryWorkingThenUpshift(t)

	eventually(t, 3*time.Second, "rebalance_wait_entry journaled", func() bool { return f.skips("rebalance_wait_entry") > 0 })
	time.Sleep(100 * time.Millisecond) // ~10 cycles
	if n := f.sellsOn(f.call, f.put); n != 0 {
		t.Fatalf("no complement while the entry works, got %d sells on the 45-DTE slot", n)
	}
	if n := f.skips("rebalance_wait_entry"); n != 1 {
		t.Errorf("the wait is journaled once, got %d lines", n)
	}

	f.fillWorkingSells(call, put)
	eventually(t, 3*time.Second, "the pass runs once the entry has finished",
		func() bool { return f.sellsOn(f.call, f.put) == 2 })
	for _, o := range f.exch.sells() {
		if (o.Instrument == f.call || o.Instrument == f.put) && o.TriggerReason != orders.TriggerRebalanceUpsize {
			t.Errorf("45-DTE sell %s is %q, want a rebalance upsize", o.Instrument, o.TriggerReason)
		}
	}
}

// Scenario: no entry while a top-up is working.
func TestOneSizer_NoEntryWhileATopUpIsWorking(t *testing.T) {
	f := newStrategyFixture(t)
	call, put := f.withVacantSecondSlot()
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // 50 % band: the startup pass tops up the 45-DTE slot
	f.startRun()

	eventually(t, 3*time.Second, "complement sent", func() bool { return f.sellsOn(f.call, f.put) == 2 })
	eventually(t, 3*time.Second, "entry_wait_topup journaled", func() bool { return f.skips("entry_wait_topup") > 0 })
	time.Sleep(100 * time.Millisecond) // ~10 cycles
	if n := f.sellsOn(call, put); n != 0 {
		t.Fatalf("no entry while the top-up works, got %d sells on the vacant slot", n)
	}

	f.fillWorkingSells(f.call, f.put)
	eventually(t, 3*time.Second, "entry sent once the top-up has finished",
		func() bool { return f.sellsOn(call, put) == 2 })
}

// Scenario: what must not change — an MM breach while an entry works.
func TestOneSizer_MMCutActsWhileAnEntryIsWorking(t *testing.T) {
	f := newStrategyFixture(t)
	f.entryWorkingThenUpshift(t)
	eventually(t, 3*time.Second, "rebalance waiting", func() bool { return f.skips("rebalance_wait_entry") > 0 })

	f.exch.setMargin(4, 4) // MM 40 % of the margin balance, above the 35 % limit
	eventually(t, 3*time.Second, "MM cut bought back at once", func() bool {
		for _, b := range f.exch.buys() {
			if b.TriggerReason == orders.TriggerMarginMM {
				return true
			}
		}
		return false
	})
}

// Scenario: what must not change — a stop-loss while an entry works.
func TestOneSizer_StopLossActsWhileAnEntryIsWorking(t *testing.T) {
	f := newStrategyFixture(t)
	f.entryWorkingThenUpshift(t)
	eventually(t, 3*time.Second, "rebalance waiting", func() bool { return f.skips("rebalance_wait_entry") > 0 })

	f.market.setQuote(f.call, 0.059, 0.061) // 3× the 0.02 premium: stop-loss
	eventually(t, 3*time.Second, "stop-loss bought the call back", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == f.call {
				return true
			}
		}
		return false
	})
	// Repairs keep working as today (the fixture has no repair cooldown).
	eventually(t, 3*time.Second, "repair sent while the entry works", func() bool {
		for _, o := range f.exch.sells() {
			if o.TriggerReason == orders.TriggerRepair {
				return true
			}
		}
		return false
	})
}
