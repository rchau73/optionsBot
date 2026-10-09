package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// 2026-10-09 restart: both bots sold full strangles, puts included, seconds
// after the start — before the first GEX snapshot (a minute later) said the
// regime was negative with a bear trend. Then the kill switch cancelled the
// entries but dropped them unread: 100 of 703 lots had filled and stayed short.

func TestGEXWaitReason(t *testing.T) {
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	fresh := strategy.GammaDecision{Regime: "NEGATIVE/ACCELERATION", SnapshotAt: now.Add(-time.Minute)}
	if r := strategy.GEXWaitReason(false, strategy.GammaDecision{Regime: "UNKNOWN"}, now); r != "" {
		t.Errorf("GEX not wired: never wait, got %q", r)
	}
	if r := strategy.GEXWaitReason(true, strategy.GammaDecision{Regime: "UNKNOWN"}, now); !strings.Contains(r, "no GEX snapshot") {
		t.Errorf("before the first snapshot: wait, got %q", r)
	}
	if r := strategy.GEXWaitReason(true, fresh, now); r != "" {
		t.Errorf("a fresh snapshot: no wait, got %q", r)
	}
	stale := fresh
	stale.SnapshotAt = now.Add(-strategy.GEXStaleAfter - time.Second)
	if r := strategy.GEXWaitReason(true, stale, now); !strings.Contains(r, "old") {
		t.Errorf("a stale snapshot: wait, got %q", r)
	}
}

func TestStrategy_NoEntryBeforeTheFirstGEXSnapshot(t *testing.T) {
	f := newStrategyFixture(t)
	f.withConfirmedRegime("POSITIVE")
	g := &switchGEX{} // wired, no snapshot yet
	f.gex = g
	f.startRun()

	eventually(t, 2*time.Second, "wait journaled", func() bool { return f.skips(strategy.SkipGEXWait) > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Fatalf("no entry before the first GEX snapshot, got %d sells", n)
	}

	g.set(&gex.Snapshot{ComputedAt: time.Now(), Regime: "POSITIVE", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true})
	eventually(t, 2*time.Second, "entry once the snapshot arrives", func() bool { return len(f.exch.sells()) == 2 })
}

func TestStrategy_NoEntryOnAStaleGEXSnapshot(t *testing.T) {
	f := newStrategyFixture(t)
	f.withConfirmedRegime("POSITIVE")
	f.gex = fixedGEX{&gex.Snapshot{ComputedAt: time.Now().Add(-10 * time.Minute), Regime: "POSITIVE", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true}}
	f.startRun()

	eventually(t, 2*time.Second, "wait journaled", func() bool { return f.skips(strategy.SkipGEXWait+": GEX snapshot is") > 0 })
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no entry on a stale GEX snapshot, got %d sells", n)
	}
}

func (f *fakeExchange) shortQty() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := 0.0
	for _, p := range f.positions {
		if p.Size < 0 {
			sum += -p.Size
		}
	}
	return sum
}

func TestStrategy_KillSwitchBooksAndClosesAPartlyFilledEntry(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()
	eventually(t, 2*time.Second, "entry resting", func() bool { return len(f.exch.sells()) == 2 })

	f.exch.mu.Lock()
	var callID string
	for id, o := range f.exch.byID {
		if o.Instrument == f.call && o.Direction == orders.DirectionSell {
			callID = id
		}
	}
	f.exch.mu.Unlock()
	f.exch.partial(callID, 0.1, 0.02) // part of the entry fills just before the kill

	f.strat.KillSwitch()
	eventually(t, 2*time.Second, "exchange flat", func() bool { return f.exch.shortQty() < 1e-9 })
	var bought float64
	for _, b := range f.exch.buys() {
		if b.Instrument == f.call && b.TriggerReason == orders.TriggerKillSwitch {
			bought += b.Qty
		}
	}
	if math.Abs(bought-0.1) > 1e-9 {
		t.Errorf("the filled 0.1 must be bought back by the kill switch, bought %v", bought)
	}
}

func TestStrategy_KillSwitchClosesAShortTheBookMissed(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })

	// A short appears that the book does not know yet (adopted only after
	// two cycles of drift).
	f.exch.mu.Lock()
	for i := range f.exch.positions {
		if f.exch.positions[i].InstrumentName == f.put {
			f.exch.positions[i].Size -= 0.3
		}
	}
	f.exch.mu.Unlock()

	f.strat.KillSwitch()
	eventually(t, 2*time.Second, "exchange flat", func() bool { return f.exch.shortQty() < 1e-9 })
}
