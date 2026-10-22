package tests

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #156: a restart must rebuild the book that was running — each strangle in
// the slot that opened it, and no uneven pair where the exchange's legs are
// even.

var (
	ethSlots = []config.StrangleSlot{{TargetDTE: 30, EntryDelta: 0.16}, {TargetDTE: 50, EntryDelta: 0.16}}
	// 2026-10-17 19:14: 6NOV at 19 DTE, 27NOV at 40 DTE.
	ethNow = time.Date(2026, 10, 17, 19, 14, 0, 0, time.UTC)
	nov6   = time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
)

func ethBook() []*orders.Position {
	return []*orders.Position{
		pos("ETH-6NOV26-2800-C", "call", 2800, nov6, 152, 0.16),
		pos("ETH-6NOV26-2100-P", "put", 2100, nov6, 152, -0.16),
		pos("ETH-27NOV26-2950-C", "call", 2950, nov27, 100, 0.16),
		pos("ETH-27NOV26-2000-P", "put", 2000, nov27, 100, -0.16),
	}
}

func slotOfExpiry(t *testing.T, got []strategy.ReconciledStrangle, exp time.Time) strategy.ReconciledStrangle {
	t.Helper()
	for _, g := range got {
		if g.Expiry.Equal(exp) {
			return g
		}
	}
	t.Fatalf("no strangle on %s", exp.Format("2Jan06"))
	return strategy.ReconciledStrangle{}
}

func openTrade(name string, slot orders.SlotRef, at time.Time) orders.Trade {
	return orders.Trade{Kind: "open", Reason: orders.TriggerEntry, Instrument: name, Slot: &slot, At: at}
}

// Scenario: slots follow the opening slot.
func TestGroupPositions_SlotsFollowTheOpeningSlot(t *testing.T) {
	s30, s50 := orders.SlotRef{DTE: 30, Delta: 0.16}, orders.SlotRef{DTE: 50, Delta: 0.16}
	at := ethNow.Add(-10 * 24 * time.Hour)
	opened := strategy.OpenSlotsFromTrades([]orders.Trade{
		openTrade("ETH-6NOV26-2800-C", s30, at), openTrade("ETH-6NOV26-2100-P", s30, at),
		openTrade("ETH-27NOV26-2950-C", s50, at), openTrade("ETH-27NOV26-2000-P", s50, at),
	})
	ps := ethBook()
	got := strategy.GroupPositions(ps, ethNow, ethSlots, maxDev, opened)
	everyPositionOnce(t, ps, got)
	if g := slotOfExpiry(t, got, nov6); g.Slot.TargetDTE != 30 || !g.SlotFromJournal {
		t.Errorf("6NOV in slot %d (from journal %v), want 30 from the journal", g.Slot.TargetDTE, g.SlotFromJournal)
	}
	if g := slotOfExpiry(t, got, nov27); g.Slot.TargetDTE != 50 || !g.SlotFromJournal {
		t.Errorf("27NOV in slot %d (from journal %v), want 50 from the journal", g.Slot.TargetDTE, g.SlotFromJournal)
	}
}

// The journal wins over distance: a strangle opened by the 50-day slot on a
// stretched expiry stays there even when the 30-day slot is now nearer.
func TestGroupPositions_JournalSlotWinsOverDistance(t *testing.T) {
	s30, s50 := orders.SlotRef{DTE: 30, Delta: 0.16}, orders.SlotRef{DTE: 50, Delta: 0.16}
	at := ethNow.Add(-24 * time.Hour)
	opened := strategy.OpenSlotsFromTrades([]orders.Trade{
		openTrade("ETH-6NOV26-2800-C", s50, at), openTrade("ETH-6NOV26-2100-P", s50, at),
		openTrade("ETH-27NOV26-2950-C", s30, at), openTrade("ETH-27NOV26-2000-P", s30, at),
	})
	got := strategy.GroupPositions(ethBook(), ethNow, ethSlots, maxDev, opened)
	if g := slotOfExpiry(t, got, nov6); g.Slot.TargetDTE != 50 {
		t.Errorf("6NOV in slot %d, want 50 (journal)", g.Slot.TargetDTE)
	}
	if g := slotOfExpiry(t, got, nov27); g.Slot.TargetDTE != 30 {
		t.Errorf("27NOV in slot %d, want 30 (journal)", g.Slot.TargetDTE)
	}
}

// Scenario: no journal record, lowest total distance. 27NOV (40 DTE) is 10
// from both slots; taking it first into slot 30 sent 6NOV (19 DTE) 31 days
// away to slot 50 (total 41 instead of 21).
func TestGroupPositions_NoJournalLowestTotalDistance(t *testing.T) {
	for name, opened := range map[string]strategy.OpenSlots{"nil": nil, "empty": {}} {
		got := strategy.GroupPositions(ethBook(), ethNow, ethSlots, maxDev, opened)
		if g := slotOfExpiry(t, got, nov6); g.Slot.TargetDTE != 30 || g.SlotFromJournal {
			t.Errorf("%s: 19-DTE strangle in slot %d (from journal %v), want 30 by distance", name, g.Slot.TargetDTE, g.SlotFromJournal)
		}
		if g := slotOfExpiry(t, got, nov27); g.Slot.TargetDTE != 50 {
			t.Errorf("%s: 40-DTE strangle in slot %d, want 50", name, g.Slot.TargetDTE)
		}
	}
}

// A journal record of a slot no longer configured is ignored: distance decides.
func TestGroupPositions_JournalSlotNotConfiguredIgnored(t *testing.T) {
	old := orders.SlotRef{DTE: 45, Delta: 0.16}
	at := ethNow.Add(-24 * time.Hour)
	opened := strategy.OpenSlotsFromTrades([]orders.Trade{openTrade("ETH-6NOV26-2800-C", old, at), openTrade("ETH-27NOV26-2950-C", old, at)})
	got := strategy.GroupPositions(ethBook(), ethNow, ethSlots, maxDev, opened)
	if g := slotOfExpiry(t, got, nov6); g.Slot.TargetDTE != 30 || g.SlotFromJournal {
		t.Errorf("6NOV in slot %d (from journal %v), want 30 by distance", g.Slot.TargetDTE, g.SlotFromJournal)
	}
	if g := slotOfExpiry(t, got, nov27); g.Slot.TargetDTE != 50 {
		t.Errorf("27NOV in slot %d, want 50", g.Slot.TargetDTE)
	}
}

func TestOpenSlotsFromTrades(t *testing.T) {
	s30, s50 := orders.SlotRef{DTE: 30, Delta: 0.16}, orders.SlotRef{DTE: 50, Delta: 0.16}
	at := func(h int) time.Time { return ethNow.Add(time.Duration(h) * time.Hour) }
	trades := []orders.Trade{
		openTrade("A-C", s50, at(0)),
		{Kind: "close", Reason: orders.TriggerRebalanceLegs, Instrument: "A-C", Slot: &s50, At: at(1)}, // partial trim: kept
		openTrade("B-C", s50, at(0)),
		{Kind: "close", Reason: orders.CloseReasonLabel(orders.TriggerStopLoss200Pct), Instrument: "B-C", Slot: &s50, At: at(1)},
		openTrade("B-C", s30, at(2)), // re-opened later by another slot
		openTrade("C-C", s30, at(0)),
		openTrade("C-C", orders.SlotRef{DTE: 50, Delta: 0.1600001}, at(1)),        // a shared strike: both slots
		{Kind: "open", Reason: orders.TriggerEntry, Instrument: "D-C", At: at(0)}, // no slot: ignored
	}
	got := strategy.OpenSlotsFromTrades(trades)
	want := map[string][]orders.SlotRef{"A-C": {s50}, "B-C": {s30}, "C-C": {s30, s50}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, w := range want {
		if fmt.Sprint(got[k]) != fmt.Sprint(w) {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}
}

// Scenario: offsetting uneven pairs are split, not bought back (2026-10-17
// BTC 27NOV: true strangles 1.6/1.6, 0.5/0.5, 0.2/0.2).
func TestGroupPositions_OffsettingUnevenPairsAreSplit(t *testing.T) {
	c95 := pos("BTC-27NOV26-95000-C", "call", 95000, nov27, 2.1, 0.16)
	c95.PremiumReceived = 2.1 * 0.01
	in := []*orders.Position{
		c95,
		pos("BTC-27NOV26-93000-C", "call", 93000, nov27, 0.2, 0.19),
		pos("BTC-27NOV26-72000-P", "put", 72000, nov27, 1.8, -0.15),
		pos("BTC-27NOV26-73000-P", "put", 73000, nov27, 0.5, -0.17),
	}
	got := strategy.GroupPositions(in, reconNow, liveSlots, maxDev, nil)

	byInst := map[string]float64{}
	var premium float64
	for _, g := range got {
		if g.Call == nil || g.Put == nil {
			t.Fatalf("one-legged strangle %s / %s", legName(g.Call), legName(g.Put))
		}
		if math.Abs(g.Call.Qty-g.Put.Qty) > 1e-9 {
			t.Errorf("%s %v / %s %v is uneven: leg balancing would buy back hedged size",
				g.Call.Instrument, g.Call.Qty, g.Put.Instrument, g.Put.Qty)
		}
		byInst[g.Call.Instrument] += g.Call.Qty
		byInst[g.Put.Instrument] += g.Put.Qty
		if g.Call.Instrument == c95.Instrument {
			premium += g.Call.PremiumReceived
		}
	}
	for _, p := range in {
		if math.Abs(byInst[p.Instrument]-p.Qty) > 1e-9 {
			t.Errorf("%s rebuilt as %v, exchange holds %v", p.Instrument, byInst[p.Instrument], p.Qty)
		}
	}
	if math.Abs(premium-2.1*0.01) > 1e-12 || c95.Qty != 2.1 {
		t.Errorf("premium shared out (%v) and the input untouched (qty %v)", premium, c95.Qty)
	}
}

// A real excess (no other leg to offset it) stays uneven: leg balancing
// buys it back as before.
func TestGroupPositions_RealExcessStaysUneven(t *testing.T) {
	in := []*orders.Position{
		pos("BTC-27NOV26-95000-C", "call", 95000, nov27, 2.0, 0.16),
		pos("BTC-27NOV26-72000-P", "put", 72000, nov27, 1.7, -0.15),
	}
	got := strategy.GroupPositions(in, reconNow, liveSlots, maxDev, nil)
	if len(got) != 1 || got[0].Call.Qty != 2.0 || got[0].Put.Qty != 1.7 {
		t.Errorf("want one 2.0/1.7 strangle, got %+v", got)
	}
}

// Run level, scenario "offsetting uneven pairs are split, not bought back":
// a restart onto the 10-17 BTC 27NOV book sends no order.
func TestReconcile_OffsettingUnevenPairsRestartWithoutTrading(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(2.3, 0.02)
	f.exch.summary.InitialMargin = 2.0 // at the 20 % limit: no rebalance top-up
	call2 := strings.Replace(f.call, "110000-C", "112000-C", 1)
	put2 := strings.Replace(f.put, "90000-P", "89000-P", 1)
	f.market.mu.Lock()
	for name, src := range map[string]string{call2: f.call, put2: f.put} {
		cp := *f.market.instruments[src]
		cp.Name = name
		f.market.instruments[name] = &cp
	}
	f.market.mu.Unlock()
	f.exch.positions[0].Size = -2.1 // call
	f.exch.positions[1].Size = -1.8 // put
	f.exch.positions = append(f.exch.positions,
		orders.RawPosition{InstrumentName: call2, Size: -0.2, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.03},
		orders.RawPosition{InstrumentName: put2, Size: -0.5, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.08},
	)
	f.startRun()

	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllStrangles()) == 3 })
	time.Sleep(300 * time.Millisecond) // ~30 cycles: balancing, rebalance, repair
	for _, b := range f.exch.buys() {
		t.Errorf("restart bought back %v of %s (%s): the exchange's legs are even", b.Qty, b.Instrument, b.TriggerReason)
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("restart sold %d orders", n)
	}
	for _, st := range f.state.AllStrangles() {
		if st.CallLeg == nil || st.PutLeg == nil || math.Abs(st.CallLeg.Qty-st.PutLeg.Qty) > 1e-9 {
			t.Errorf("strangle %s not whole and even: %+v / %+v", st.ID, st.CallLeg, st.PutLeg)
		}
	}
}

// Run level, scenario "a real excess is still balanced": call 2.0 against a
// put of 1.7 and no other leg — leg balancing buys back 0.3 of the call.
func TestReconcile_RealExcessIsStillBalanced(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(1.7, 0.02)
	f.exch.summary.InitialMargin = 2.0
	f.exch.positions[0].Size = -2.0 // the call
	f.startRun()

	eventually(t, 2*time.Second, "legs balanced", func() bool { return math.Abs(f.qty(f.call)-1.7) < 1e-9 })
	buys := f.exch.buys()
	if len(buys) != 1 || buys[0].Instrument != f.call || math.Abs(buys[0].Qty-0.3) > 1e-9 || buys[0].TriggerReason != orders.TriggerRebalanceLegs {
		t.Fatalf("want one rebalance_legs buy-back of 0.3 of the call, got %+v", buys)
	}
}

// Run level, scenario "slots follow the opening slot": with the journal's
// opens restored, the 19-DTE strangle is rebuilt in the 30-day slot and the
// 40-DTE one in the 50-day slot, and the reconciled lines say why.
func TestReconcile_SlotsFollowTheJournalAcrossARestart(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 30, Deltas: []float64{0.16}}, {DTE: 50, Deltas: []float64{0.16}}}
	f.cfg.RolloutDTE = 7
	f.withOpenStrangle(0.1, 0.02) // sizes imPerLot; positions replaced below
	f.exch.summary.InitialMargin = 2.0
	day := func(d int) string {
		exp := time.Now().UTC().AddDate(0, 0, d).Truncate(24 * time.Hour).Add(8 * time.Hour)
		return strings.ToUpper(exp.Format("2Jan06"))
	}
	near, far := day(19), day(40)
	names := map[string]string{
		"nc": "BTC-" + near + "-110000-C", "np": "BTC-" + near + "-90000-P",
		"fc": "BTC-" + far + "-110000-C", "fp": "BTC-" + far + "-90000-P",
	}
	f.exch.positions = nil
	for k, n := range names {
		delta := 0.016
		if strings.HasSuffix(k, "p") {
			delta = -0.016
		}
		f.exch.positions = append(f.exch.positions,
			orders.RawPosition{InstrumentName: n, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: delta})
	}
	s30, s50 := orders.SlotRef{DTE: 30, Delta: 0.16}, orders.SlotRef{DTE: 50, Delta: 0.16}
	at := time.Now().Add(-48 * time.Hour)
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{
			openTrade(names["nc"], s30, at), openTrade(names["np"], s30, at),
			openTrade(names["fc"], s50, at), openTrade(names["fp"], s50, at),
		})
	}
	f.startRun()

	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllStrangles()) == 2 })
	for _, st := range f.state.AllStrangles() {
		want := 30
		if st.CallLeg != nil && st.CallLeg.Instrument == names["fc"] {
			want = 50
		}
		if st.TargetDTE != want {
			t.Errorf("strangle %s (%s) in slot %d, want %d", st.ID, legName(st.CallLeg), st.TargetDTE, want)
		}
	}
	rec := f.journal.events(orders.EventReconciled)
	if len(rec) != 4 {
		t.Fatalf("want 4 reconciled lines, got %d", len(rec))
	}
	for _, r := range rec {
		if !strings.Contains(r.ctx.Detail, "opened there (journal)") {
			t.Errorf("reconciled %s detail %q must say the slot came from the journal", r.instrument, r.ctx.Detail)
		}
	}
}
