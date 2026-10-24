package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #242: a restart must leave a vacant slot vacant. 2026-10-21 17:20 BTC: the
// 25-DTE slot was empty after its time roll (no expiry in 16–35), and the
// restart handed it the 50-DTE slot's 27NOV strangle (36 DTE), so both slots
// sat on 27NOV and IM ran to 24.4 % of 20 %.

var (
	btcSlots   = []config.StrangleSlot{{TargetDTE: 25, EntryDelta: 0.16}, {TargetDTE: 50, EntryDelta: 0.16}}
	restart242 = time.Date(2026, 10, 21, 17, 20, 0, 0, time.UTC) // 27NOV 08:00 is 36.6 days out
	slot50     = orders.SlotRef{DTE: 50, Delta: 0.16}
)

// The book at the restart: 95000-C 1.5 (its put closed by delta drift) and
// the 93000-C / 73000-P 0.2 pair, all on 27NOV.
func book242() []*orders.Position {
	return []*orders.Position{
		pos("BTC-27NOV26-95000-C", "call", 95000, nov27, 1.5, 0.12),
		pos("BTC-27NOV26-93000-C", "call", 93000, nov27, 0.2, 0.15),
		pos("BTC-27NOV26-73000-P", "put", 73000, nov27, 0.2, -0.13),
	}
}

func assertAllInSlot(t *testing.T, got []strategy.ReconciledStrangle, dte int) {
	t.Helper()
	for _, g := range got {
		if g.Slot.TargetDTE != dte {
			t.Errorf("%s / %s in slot %d, want %d", legName(g.Call), legName(g.Put), g.Slot.TargetDTE, dte)
		}
	}
}

func assertVacant(t *testing.T, got []strategy.ReconciledStrangle, slots []config.StrangleSlot, dte int, reason string) {
	t.Helper()
	v := strategy.VacantSlots(got, restart242, slots, maxDev)
	if len(v) != 1 || v[0].Slot.TargetDTE != dte || !strings.Contains(v[0].Reason, reason) {
		t.Errorf("vacant slots %+v, want only slot %d (%q)", v, dte, reason)
	}
}

// Scenario: a vacant slot stays vacant across a restart (both strangles
// opened by slot 50 per the journal).
func TestGroupPositions_VacantSlotStaysVacantAcrossARestart(t *testing.T) {
	at := restart242.Add(-20 * 24 * time.Hour)
	opened := strategy.OpenSlotsFromTrades([]orders.Trade{
		openTrade("BTC-27NOV26-95000-C", slot50, at), openTrade("BTC-27NOV26-72000-P", slot50, at),
		openTrade("BTC-27NOV26-93000-C", slot50, at), openTrade("BTC-27NOV26-73000-P", slot50, at),
	})
	ps := book242()
	got := strategy.GroupPositions(ps, restart242, btcSlots, maxDev, opened)
	everyPositionOnce(t, ps, got)
	if len(got) != 2 {
		t.Fatalf("want the 1.5 call and the 0.2 pair, got %d strangles", len(got))
	}
	assertAllInSlot(t, got, 50)
	for _, g := range got {
		if !g.SlotFromJournal {
			t.Errorf("%s: slot must come from the journal", legName(g.Call))
		}
	}
	assertVacant(t, got, btcSlots, 25, "no strangle within its window (DTE <= 35)")
}

// The journal wins even when its slot ends up holding two strangles and the
// other slot's window would take one (a 30-DTE strangle fits slot 25's).
func TestGroupPositions_JournalSlotSharedRatherThanFillAVacantSlot(t *testing.T) {
	now := nov27.Add(-30*24*time.Hour - 12*time.Hour)
	at := now.Add(-20 * 24 * time.Hour)
	opened := strategy.OpenSlotsFromTrades([]orders.Trade{
		openTrade("BTC-27NOV26-95000-C", slot50, at),
		openTrade("BTC-27NOV26-93000-C", slot50, at), openTrade("BTC-27NOV26-73000-P", slot50, at),
	})
	got := strategy.GroupPositions(book242(), now, btcSlots, maxDev, opened)
	assertAllInSlot(t, got, 50)
}

// Scenario: no journal record, expiry outside the window.
func TestGroupPositions_NoJournalExpiryOutsideTheWindow(t *testing.T) {
	ps := []*orders.Position{
		pos("BTC-27NOV26-95000-C", "call", 95000, nov27, 1.5, 0.16),
		pos("BTC-27NOV26-72000-P", "put", 72000, nov27, 1.5, -0.16),
	}
	for name, opened := range map[string]strategy.OpenSlots{"nil": nil, "empty": {}} {
		got := strategy.GroupPositions(ps, restart242, btcSlots, maxDev, opened)
		if len(got) != 1 || got[0].Slot.TargetDTE != 50 || got[0].SlotFromJournal {
			t.Errorf("%s: %+v, want one strangle in slot 50 by distance", name, got)
		}
		assertVacant(t, got, btcSlots, 25, "no strangle within its window (DTE <= 35)")
	}
	// The whole 10-21 book without a journal: still slot 50, slot 25 vacant.
	got := strategy.GroupPositions(book242(), restart242, btcSlots, maxDev, nil)
	assertAllInSlot(t, got, 50)
	assertVacant(t, got, btcSlots, 25, "DTE <= 35")
}

// At the top of the window (35 DTE) the expiry is the slot's own.
func TestGroupPositions_ExpiryAtTheWindowTopFitsTheSlot(t *testing.T) {
	now := nov27.Add(-35*24*time.Hour - time.Hour) // 35 whole days
	got := strategy.GroupPositions([]*orders.Position{
		pos("BTC-27NOV26-95000-C", "call", 95000, nov27, 1, 0.16),
		pos("BTC-27NOV26-72000-P", "put", 72000, nov27, 1, -0.16),
	}, now, btcSlots, maxDev, nil)
	assertAllInSlot(t, got, 25)
}

// One expiry per slot: two strangles on one expiry, no journal, share a
// slot; a second expiry goes to the other slot.
func TestGroupPositions_NeverTwoSlotsOnOneExpiry(t *testing.T) {
	now := ethNow
	ps := []*orders.Position{
		pos("ETH-27NOV26-2950-C", "call", 2950, nov27, 100, 0.16),
		pos("ETH-27NOV26-2000-P", "put", 2000, nov27, 100, -0.16),
		pos("ETH-27NOV26-3100-C", "call", 3100, nov27, 50, 0.12),
		pos("ETH-27NOV26-1900-P", "put", 1900, nov27, 50, -0.12),
	}
	got := strategy.GroupPositions(ps, now, ethSlots, maxDev, nil)
	if len(got) != 2 || got[0].Slot != got[1].Slot {
		t.Fatalf("both 27NOV strangles must share one slot: %+v", got)
	}
	ps = append(ps, ethBook()[:2]...) // 6NOV at 19 DTE
	got = strategy.GroupPositions(ps, now, ethSlots, maxDev, nil)
	slotOf := map[time.Time]map[int]bool{}
	for _, g := range got {
		if slotOf[g.Expiry] == nil {
			slotOf[g.Expiry] = map[int]bool{}
		}
		slotOf[g.Expiry][g.Slot.TargetDTE] = true
	}
	if len(slotOf[nov27]) != 1 || !slotOf[nov6][30] || slotOf[nov27][30] {
		t.Errorf("want 6NOV in slot 30 and 27NOV in slot 50 alone: %v", slotOf)
	}
}

// Past maxExhaustiveStrangles the greedy fallback keeps the same rules.
func TestGroupPositions_LargeBookKeepsOneExpiryPerSlot(t *testing.T) {
	var ps []*orders.Position
	for i := range 9 {
		ps = append(ps,
			pos(fmt.Sprintf("BTC-27NOV26-%d-C", 95000+1000*i), "call", float64(95000+1000*i), nov27, float64(i+1), 0.16),
			pos(fmt.Sprintf("BTC-27NOV26-%d-P", 72000-1000*i), "put", float64(72000-1000*i), nov27, float64(i+1), -0.16))
	}
	got := strategy.GroupPositions(ps, restart242, btcSlots, maxDev, nil)
	everyPositionOnce(t, ps, got)
	if len(got) != 9 {
		t.Fatalf("want 9 strangles, got %d", len(got))
	}
	assertAllInSlot(t, got, 50)
}

// Run level, scenario "a vacant slot stays vacant across a restart": the
// restart rebuilds both 37-day strangles in slot 50, says in the reconciled
// lines why slot 25 is vacant, and slot 25 waits with no_expiry (the only
// listed expiries are outside its window) instead of trading.
func TestReconcile_VacantSlotWaitsAfterARestart(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.16}}, {DTE: 50, Deltas: []float64{0.16}}}
	f.cfg.RolloutDTE = 15
	f.withOpenStrangle(0.1, 0.02)      // sizes imPerLot; positions replaced below
	f.exch.summary.InitialMargin = 1.9 // within the startup band of the 20 % limit, with headroom for entries
	exp := strings.ToUpper(time.Now().UTC().AddDate(0, 0, 37).Truncate(24 * time.Hour).Add(8 * time.Hour).Format("2Jan06"))
	names := []string{"BTC-" + exp + "-95000-C", "BTC-" + exp + "-72000-P", "BTC-" + exp + "-93000-C", "BTC-" + exp + "-73000-P"}
	sizes := []float64{1.5, 1.5, 0.2, 0.2}
	f.exch.positions = nil
	for i, n := range names {
		delta := 0.016
		if strings.HasSuffix(n, "-P") {
			delta = -0.016
		}
		f.exch.positions = append(f.exch.positions,
			orders.RawPosition{InstrumentName: n, Size: -sizes[i], Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: delta * sizes[i] * 10})
	}
	at := time.Now().Add(-20 * 24 * time.Hour)
	f.beforeRun = func(s *strategy.Strategy) {
		var trades []orders.Trade
		for _, n := range names {
			trades = append(trades, openTrade(n, slot50, at))
		}
		s.RestoreStops(trades)
	}
	f.startRun()

	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllStrangles()) == 2 })
	eventually(t, 2*time.Second, "slot 25 waits with no_expiry", func() bool { return f.skips(strategy.SkipNoExpiry) > 0 })
	for _, st := range f.state.AllStrangles() {
		if st.TargetDTE != 50 {
			t.Errorf("strangle %s (%s) in slot %d, want 50", st.ID, legName(st.CallLeg), st.TargetDTE)
		}
	}
	rec := f.journal.events(orders.EventReconciled)
	if len(rec) != 4 {
		t.Fatalf("want 4 reconciled lines, got %d", len(rec))
	}
	for _, r := range rec {
		if !strings.Contains(r.ctx.Detail, "opened there (journal)") || !strings.Contains(r.ctx.Detail, "slot 25d/0.16 left vacant: no strangle within its window") {
			t.Errorf("reconciled %s detail %q must give the journal slot and why slot 25 is vacant", r.instrument, r.ctx.Detail)
		}
		// cmd/deployeffect finds startups by this prefix (#219).
		if !strings.HasPrefix(r.ctx.Detail, "strangle ") {
			t.Errorf("reconciled %s detail %q must start with \"strangle \"", r.instrument, r.ctx.Detail)
		}
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("restart sold %d orders: slot 25 must wait for an expiry in its own window", n)
	}
}
