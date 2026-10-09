package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// 2026-10-06 23:02 (ETH): GEX shed the puts with spot under the flip 2697;
// one minute later the script's flip (lowest crossing) jumped to 2440, the
// live signal stopped shedding and repair re-sold the puts at once — the
// spread paid twice for a signal that lasted a minute. Now a shed waits for
// the signal to hold on a second snapshot, and the shed leg is re-sold only
// once spot is back above the flip it was shed at (plus the buffer) on
// consecutive snapshots.

func TestShedAnchor_RepairRule(t *testing.T) {
	shed := time.Date(2026, 10, 6, 23, 2, 0, 0, time.UTC)
	snap := func(min int) time.Time { return shed.Add(time.Duration(min) * time.Minute) }
	a := &strategy.ShedAnchor{At: shed, Flip: 2697, BufferPct: 2.6}
	if lvl := a.Level(); lvl < 2767 || lvl > 2768 {
		t.Fatalf("level = flip × (1 + buffer) = 2767.1, got %.1f", lvl)
	}
	reason := func(now time.Time, nonNeg time.Time) string {
		return strategy.ShedRepairBlockReason(a, now, nonNeg, 2, 24*time.Hour)
	}

	// The flip jumps to 2440: spot 2560 is far above the live flip but still
	// below the anchor — held, whatever the live signal says.
	a.Observe(2560, snap(1))
	if r := reason(snap(1), snap(1)); !strings.Contains(r, "waiting for spot above 2767") {
		t.Errorf("held below the anchor, got %q", r)
	}
	// Above once, then back below: the count restarts.
	a.Observe(2780, snap(2))
	a.Observe(2700, snap(3))
	if a.Above != 0 {
		t.Errorf("a snapshot below the anchor resets the count, got %d", a.Above)
	}
	// The same snapshot seen again (several cycles a minute) counts once.
	a.Observe(2780, snap(4))
	a.Observe(2780, snap(4))
	if r := reason(snap(4), time.Time{}); r == "" || a.Above != 1 {
		t.Errorf("one snapshot above is not enough (above=%d, reason %q)", a.Above, r)
	}
	a.Observe(2785, snap(5))
	if r := reason(snap(5), time.Time{}); r != "" {
		t.Errorf("two snapshots above the anchor release the leg, got %q", r)
	}

	// Release valve: spot never comes back, but the regime has been
	// non-negative for 24 h — the shed no longer applies.
	b := &strategy.ShedAnchor{At: shed, Flip: 2697, BufferPct: 2.6}
	b.Observe(2500, snap(1))
	if r := strategy.ShedRepairBlockReason(b, shed.Add(23*time.Hour), shed, 2, 24*time.Hour); r == "" {
		t.Error("23 h of non-negative regime is not enough")
	}
	if r := strategy.ShedRepairBlockReason(b, shed.Add(24*time.Hour), shed, 2, 24*time.Hour); r != "" {
		t.Errorf("24 h of non-negative regime releases the leg, got %q", r)
	}
	if r := strategy.ShedRepairBlockReason(b, shed.Add(48*time.Hour), time.Time{}, 2, 24*time.Hour); r == "" {
		t.Error("a regime still negative never releases by time")
	}
}

// The Oct 6 sequence end to end, on the fixture's BTC numbers: flip 120k,
// spot 100k, then a flip jump to 90k.
func TestStrategy_ShedHoldsThroughAFlipJumpUntilSpotRecovers(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.GEXShedConfirmSnapshots = 2
	f.cfg.GEXRepairConfirmSnapshots = 2
	f.cfg.GEXRepairReleaseHours = 24
	f.withOpenStrangle(0.1, 0.02)
	f.withPutSheddingRegime() // bear trend from the daily closes
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		return orders.Fill{Qty: o.Qty, FillPrice: 0.02} // buy-backs and repairs fill
	}
	at := time.Now() // a minute apart from now: fresh, never stale (GEXStaleAfter)
	g := &switchGEX{}
	next := func(spot, flip float64) {
		at = at.Add(time.Minute)
		g.set(&gex.Snapshot{ComputedAt: at, Regime: "NEGATIVE/ACCELERATION", Spot: spot, GammaFlip: flip, GammaFlipFound: true})
	}
	next(100000, 120000) // first snapshot under the flip: pending, not shed
	f.gex = g
	f.startRun()

	time.Sleep(150 * time.Millisecond) // many cycles on the same snapshot
	if n := len(f.exch.buys()); n != 0 {
		t.Fatalf("one snapshot under the flip must not shed, got %d buys", n)
	}

	next(100000, 120000) // the signal holds: shed
	eventually(t, 2*time.Second, "put shed on the second snapshot", func() bool { return f.position(f.put) == nil })

	next(100000, 90000) // the flip jumps under spot: the live signal clears
	eventually(t, 2*time.Second, "repair held and journaled", func() bool {
		for _, e := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(e.reason, strategy.SkipRepairHeld) && strings.Contains(e.reason, "shed by GEX at flip 120000") {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	if n := putSells(f); n != 0 {
		t.Fatalf("the put must not be re-sold on the flip jump, got %d sells", n)
	}

	next(121000, 90000) // back above the anchored flip: once is not enough
	time.Sleep(100 * time.Millisecond)
	if n := putSells(f); n != 0 {
		t.Fatalf("one snapshot above the anchor must not re-sell, got %d sells", n)
	}
	next(121500, 90000)
	eventually(t, 2*time.Second, "put repaired after two snapshots above the anchor", func() bool { return putSells(f) == 1 })
	for _, o := range f.exch.sells() {
		if o.Instrument == f.put && o.TriggerReason != orders.TriggerRepair {
			t.Errorf("the put is re-sold by repair, got %s", o.TriggerReason)
		}
	}
}

func putSells(f *strategyFixture) int {
	n := 0
	for _, o := range f.exch.sells() {
		if o.Instrument == f.put {
			n++
		}
	}
	return n
}
