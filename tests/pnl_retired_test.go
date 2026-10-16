package tests

import (
	"bytes"
	"io"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #108: a close booked to a slot later removed from the matrix counted in
// the total only, so the slot rows of /api/pnl summed to 7 % less than it.

func TestComputePnL_RetiredSlotsGetTheirOwnRows(t *testing.T) {
	slots := []orders.SlotRef{{DTE: 25, Delta: 0.16}}
	retired := orders.SlotRef{DTE: 45, Delta: 0.16}
	open := []*orders.Position{
		{ID: "a", Qty: 0.1, PremiumReceived: 0.002, CurrentMid: 0.005},  // +0.0015, configured slot
		{ID: "r", Qty: 0.1, PremiumReceived: 0.002, CurrentMid: 0.010},  // +0.0010, retired slot
		{ID: "orphan", Qty: 0.1, PremiumReceived: 0.001, CurrentMid: 0}, // +0.0010, no slot
	}
	slotOf := map[string]*orders.SlotRef{"a": &slots[0], "r": &retired}
	realised := map[orders.SlotRef]float64{slots[0]: 0.0008, retired: -0.0042, {}: -0.0002}
	closed := map[orders.SlotRef]int{slots[0]: 1, retired: 2, {}: 1}

	lines := strategy.ComputePnL(slots, open, slotOf, realised, closed)
	if len(lines) != 4 {
		t.Fatalf("want 1 slot + 2 retired + total, got %+v", lines)
	}
	want := []struct {
		slot                 *orders.SlotRef
		retired              bool
		realised, unrealised float64
		open, closed         int
	}{
		{&slots[0], false, 0.0008, 0.0015, 1, 1},
		{&orders.SlotRef{}, true, -0.0002, 0.0010, 1, 1}, // no known slot sorts first
		{&retired, true, -0.0042, 0.0010, 1, 2},
		{nil, false, -0.0036, 0.0035, 3, 4},
	}
	for i, w := range want {
		l := lines[i]
		if (l.Slot == nil) != (w.slot == nil) || (l.Slot != nil && *l.Slot != *w.slot) || l.Retired != w.retired ||
			!near(l.Realised, w.realised, 1e-12) || !near(l.Unrealised, w.unrealised, 1e-12) || l.OpenLegs != w.open || l.ClosedLegs != w.closed {
			t.Errorf("line %d = %+v (slot %v), want %+v", i, l, l.Slot, w)
		}
	}
	assertRowsAddUp(t, lines)
}

func TestComputePnL_NoRetiredRowWithoutRetiredPnL(t *testing.T) {
	slots := []orders.SlotRef{{DTE: 25, Delta: 0.16}}
	lines := strategy.ComputePnL(slots, nil, nil, map[orders.SlotRef]float64{slots[0]: 0.001}, map[orders.SlotRef]int{slots[0]: 1})
	if len(lines) != 2 || lines[0].Retired || lines[1].Slot != nil {
		t.Errorf("want the slot then the total, got %+v", lines)
	}
}

func assertRowsAddUp(t *testing.T, lines []strategy.PnLLine) {
	t.Helper()
	var r, u float64
	var o, c int
	for _, l := range lines[:len(lines)-1] {
		r, u, o, c = r+l.Realised, u+l.Unrealised, o+l.OpenLegs, c+l.ClosedLegs
	}
	tot := lines[len(lines)-1]
	if !near(r, tot.Realised, 1e-12) || !near(u, tot.Unrealised, 1e-12) || o != tot.OpenLegs || c != tot.ClosedLegs {
		t.Errorf("rows sum to realised %v unrealised %v open %d closed %d, total %+v", r, u, o, c, tot)
	}
}

// Scenario: slot rows add up to the total.
func TestScenario_SlotRowsAddUpToTheTotal(t *testing.T) {
	// The journal holds closes booked to slots 60d/0.18 and 25d/0.16; the
	// fixture's matrix configures only 45d/0.16.
	var buf bytes.Buffer
	writeJournal(orders.NewWriterLogger(&buf, 0), time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	rp, err := orders.ReplayJournal(&buf, 10)
	if err != nil {
		t.Fatal(err)
	}

	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.logger = orders.NewWriterLogger(io.Discard, 0.05)
	f.beforeRun = func(s *strategy.Strategy) { s.RestorePnL(rp.Realised, rp.Closed) }
	f.startRun()
	h := api.New(f.strat, f.logger).Handler()
	eventually(t, 2*time.Second, "positions visible", func() bool { return len(f.strat.View().Strangles) == 1 })

	_, body := getJSON(t, h, "/api/pnl")
	rows, _ := body["pnl"].([]any)
	if len(rows) != 4 {
		t.Fatalf("want slot 45 + 2 retired + total, got %v", rows)
	}
	var sum float64
	var retiredDTEs []float64
	for _, row := range rows[:len(rows)-1] {
		m := row.(map[string]any)
		sum += m["realised"].(float64)
		if m["retired"] == true {
			retiredDTEs = append(retiredDTEs, m["slot"].(map[string]any)["dte"].(float64))
		}
	}
	total := rows[len(rows)-1].(map[string]any)
	if total["slot"] != nil || total["retired"] != nil {
		t.Errorf("last row must be the total: %v", total)
	}
	if len(retiredDTEs) != 2 || retiredDTEs[0] != 25 || retiredDTEs[1] != 60 {
		t.Errorf("retired rows for DTE %v, want [25 60]", retiredDTEs)
	}
	want := (0.0124 - 0.011*0.8) + (0.006 - 0.012)
	if !near(sum, total["realised"].(float64), 1e-12) || !near(sum, want, 1e-12) {
		t.Errorf("rows sum to %v, total %v, want %v", sum, total["realised"], want)
	}
	assertRowsAddUp(t, f.strat.PnLReport())
}

// Scenario: unmanaged longs stay untouched.
func TestScenario_UnmanagedLongsStayUntouched(t *testing.T) {
	const foreign = "BTC-25DEC26-60000-P" // the hand-placed long of #108
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: foreign, Size: 1.7, Direction: orders.DirectionBuy, AveragePrice: 0.067})
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(200 * time.Millisecond) // ~20 cycles, past the two-cycle drift check

	if n := len(f.exch.ordersWhere(func(o orders.Order) bool { return o.Instrument == foreign })); n != 0 {
		t.Errorf("%d orders placed on %s, a long the bot never traded", n, foreign)
	}
	for _, p := range f.state.AllPositions() {
		if p.Instrument == foreign {
			t.Errorf("%s adopted into the book: %+v", foreign, p)
		}
	}
}
