package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Regression (2026-10-05): reconcile built one strangle per expiry and kept
// only the last call and put of it. With two strangles on Nov 27 (the 45- and
// 60-day slots) two legs were loaded but in no strangle, so the startup
// rebalance sized the "strangle" from half the book and sent top-ups (BTC
// +1.2 and +0.5, ETH +113), and leg balancing bought back 100 ETH calls it
// saw as excess.

func pos(name, typ string, strike float64, exp time.Time, qty, delta float64) *orders.Position {
	return &orders.Position{ID: name, Instrument: name, OptionType: typ, Strike: strike, Expiry: exp,
		Side: orders.DirectionSell, Qty: qty, CurrentGreeks: orders.Greeks{Delta: delta}}
}

var (
	reconNow  = time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	oct23     = time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	nov27     = time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	liveSlots = []config.StrangleSlot{{TargetDTE: 25, EntryDelta: 0.16}, {TargetDTE: 45, EntryDelta: 0.16}, {TargetDTE: 60, EntryDelta: 0.18}}
	legName   = func(p *orders.Position) string {
		if p == nil {
			return "-"
		}
		return p.Instrument
	}
)

// everyPositionOnce fails unless each position is in exactly one strangle.
func everyPositionOnce(t *testing.T, ps []*orders.Position, got []strategy.ReconciledStrangle) {
	t.Helper()
	seen := map[string]int{}
	for _, g := range got {
		for _, l := range []*orders.Position{g.Call, g.Put} {
			if l != nil {
				seen[l.ID]++
				if !l.Expiry.Equal(g.Expiry) {
					t.Errorf("%s grouped under another expiry", l.Instrument)
				}
			}
		}
	}
	for _, p := range ps {
		if seen[p.ID] != 1 {
			t.Errorf("%s is in %d strangles, want exactly 1", p.Instrument, seen[p.ID])
		}
	}
}

func TestGroupPositions_TodaysBTCBook(t *testing.T) {
	ps := []*orders.Position{
		pos("BTC-23OCT26-92000-C", "call", 92000, oct23, 1.2, 0.24),
		pos("BTC-23OCT26-78000-P", "put", 78000, oct23, 1.2, -0.10),
		pos("BTC-27NOV26-98000-C", "call", 98000, nov27, 0.8, 0.22),
		pos("BTC-27NOV26-100000-C", "call", 100000, nov27, 0.8, 0.19),
		pos("BTC-27NOV26-77000-P", "put", 77000, nov27, 0.8, -0.14),
		pos("BTC-27NOV26-76000-P", "put", 76000, nov27, 0.8, -0.13),
	}
	got := strategy.GroupPositions(ps, reconNow, liveSlots)
	everyPositionOnce(t, ps, got)
	if len(got) != 3 {
		t.Fatalf("want 3 strangles (Oct 23 + two on Nov 27), got %d", len(got))
	}
	pairs := map[string]string{}
	slots := map[int]bool{}
	for _, g := range got {
		pairs[legName(g.Call)] = legName(g.Put)
		slots[g.Slot.TargetDTE] = true
	}
	// Equal sizes: nearest-the-money call with nearest-the-money put.
	if pairs["BTC-27NOV26-98000-C"] != "BTC-27NOV26-77000-P" || pairs["BTC-27NOV26-100000-C"] != "BTC-27NOV26-76000-P" {
		t.Errorf("Nov 27 pairs: %v", pairs)
	}
	if len(slots) != 3 {
		t.Errorf("each strangle gets its own slot: %v", slots)
	}
}

func TestGroupPositions_TodaysETHBookPairsBySize(t *testing.T) {
	ps := []*orders.Position{
		pos("ETH-23OCT26-2500-P", "put", 2500, oct23, 627, -0.15),
		pos("ETH-27NOV26-3300-C", "call", 3300, nov27, 344, 0.20),
		pos("ETH-27NOV26-2250-P", "put", 2250, nov27, 444, -0.14),
		pos("ETH-27NOV26-2300-P", "put", 2300, nov27, 200, -0.16),
	}
	got := strategy.GroupPositions(ps, reconNow, liveSlots)
	everyPositionOnce(t, ps, got)
	if len(got) != 3 {
		t.Fatalf("want 3 strangles, got %d", len(got))
	}
	var paired, single int
	for _, g := range got {
		switch {
		case g.Call != nil && g.Put != nil:
			paired++
			// 344 pairs with 444 (its original put), not the nearer 200.
			if g.Call.Instrument != "ETH-27NOV26-3300-C" || g.Put.Instrument != "ETH-27NOV26-2250-P" {
				t.Errorf("pair %s / %s, want 3300-C / 2250-P (closest sizes)", g.Call.Instrument, g.Put.Instrument)
			}
		default:
			single++
		}
	}
	if paired != 1 || single != 2 {
		t.Errorf("want 1 pair and 2 one-legged strangles, got %d / %d", paired, single)
	}
}

func TestGroupPositions_MoreStranglesThanSlotsShare(t *testing.T) {
	ps := []*orders.Position{
		pos("A-C", "call", 1, nov27, 1, 0.2), pos("A-P", "put", 1, nov27, 1, -0.2),
		pos("B-C", "call", 2, nov27, 2, 0.1), pos("B-P", "put", 2, nov27, 2, -0.1),
	}
	ps = append(ps, pos("C-C", "call", 3, nov27, 5, 0.05)) // a leftover call
	got := strategy.GroupPositions(ps, reconNow, liveSlots[:1])
	everyPositionOnce(t, ps, got)
	if len(got) != 3 {
		t.Fatalf("two pairs and a one-legged call, got %d", len(got))
	}
	for _, g := range got {
		if g.Slot != liveSlots[0] {
			t.Errorf("with one slot every strangle shares it: %+v", g.Slot)
		}
	}
}

// Run level: a restart onto two strangles sharing one expiry rebuilds both,
// at their size, and sends no order.
func TestStrategy_RestartWithTwoStranglesOnOneExpirySendsNothing(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ExpiryStretch = 1.5
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.withOpenStrangle(0.1, 0.02) // strangle 1, at its slot share
	call2 := f.call[:len(f.call)-len("110000-C")] + "115000-C"
	put2 := f.put[:len(f.put)-len("90000-P")] + "85000-P"
	f.market.mu.Lock() // listed, quoted, same expiry as the first strangle
	for name, src := range map[string]string{call2: f.call, put2: f.put} {
		cp := *f.market.instruments[src]
		cp.Name = name
		f.market.instruments[name] = &cp
	}
	f.market.mu.Unlock()
	f.exch.positions = append(f.exch.positions,
		orders.RawPosition{InstrumentName: call2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.012},
		orders.RawPosition{InstrumentName: put2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.012},
	)
	f.startRun()

	eventually(t, 2*time.Second, "both strangles rebuilt", func() bool { return len(f.state.AllStrangles()) == 2 })
	time.Sleep(150 * time.Millisecond) // many cycles: rebalance, balance, repair, entries
	if b, s := len(f.exch.buys()), len(f.exch.sells()); b != 0 || s != 0 {
		t.Errorf("a correct book needs no orders at startup: %d buys, %d sells", b, s)
	}
	for _, st := range f.state.AllStrangles() {
		if st.CallLeg == nil || st.PutLeg == nil || math.Abs(st.CallLeg.Qty-st.PutLeg.Qty) > 1e-9 {
			t.Errorf("strangle %s rebuilt uneven or one-legged", st.ID)
		}
	}
}
