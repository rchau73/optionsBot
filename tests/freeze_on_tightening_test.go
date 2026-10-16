package tests

import (
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
)

// New risk is frozen only while a pending change would lower the IM limit
// (#105). 2026-10-16 ETH: the live gamma regime flapped around zero under a
// confirmed negative regime; each turn to positive froze new risk although
// confirming it could only lift the limit from 20 % to the 35 % band — about
// 8 h a day frozen, working entries and top-ups cancelled at every flap.

var (
	positiveSnap = &gex.Snapshot{Regime: "POSITIVE/PINNING", Spot: 100000, GammaFlip: 95000, GammaFlipFound: true}
	negativeSnap = &gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true}
)

// frozenRecords counts the journal's risk_frozen lines.
func (f *strategyFixture) frozenRecords() int { return len(f.journal.riskChanges(orders.RiskFrozen)) }

// Scenario: a pending regime change that would only raise the limit does not
// freeze — the entry rests and is not cancelled.
func TestFreeze_PendingRegimeRiseKeepsTheEntryWorking(t *testing.T) {
	f := newStrategyFixture(t)
	f.withConfirmedRegime("NEGATIVE/ACCELERATION")   // limit 20 %
	f.market.setIVHistory([]float64{50, 50, 50}, 50) // the confirmed DVOL band would allow 35 %
	f.gex = fixedGEX{positiveSnap}                   // live regime turned positive, not yet confirmed
	f.cfg.OrderFillTimeoutSec = 60                   // the entry rests
	f.startRun()

	eventually(t, 3*time.Second, "entry sent", func() bool { return len(f.exch.sells()) == 2 })
	time.Sleep(150 * time.Millisecond) // ~15 cycles
	if n := f.cancelsWith(orders.TriggerRiskFrozen); n != 0 {
		t.Errorf("a pending limit rise must not cancel the working entry, got %d cancels", n)
	}
	if n := f.skips("risk_frozen"); n != 0 {
		t.Errorf("no slot may be skipped as frozen, got %d", n)
	}
	if n := f.frozenRecords(); n != 0 {
		t.Errorf("no risk_frozen line, got %d", n)
	}
}

// Scenario: a pending regime change that would only raise the limit does not
// freeze — a working top-up keeps working when the live regime turns positive.
func TestFreeze_PendingRegimeRiseKeepsTheTopUpWorking(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot /= 2 // two lots fit the 20 % limit: the startup pass tops up one lot
	f.withConfirmedRegime("NEGATIVE/ACCELERATION")
	f.market.setIVHistory([]float64{50, 50, 50}, 50)
	g := &switchGEX{snap: negativeSnap}
	f.gex = g
	f.cfg.OrderFillTimeoutSec = 60 // the complement rests
	f.startRun()

	eventually(t, 3*time.Second, "complement resting", func() bool { return f.sellsOn(f.call, f.put) == 2 })
	g.set(positiveSnap) // live regime turns positive, not yet confirmed
	time.Sleep(150 * time.Millisecond)
	if n := f.cancelsWith(orders.TriggerRiskFrozen); n != 0 {
		t.Errorf("a pending limit rise must not cancel the working top-up, got %d cancels", n)
	}
	if n := f.frozenRecords(); n != 0 {
		t.Errorf("no risk_frozen line, got %d", n)
	}
}

// Scenario: a change that would lower the limit still freezes — confirmed
// positive at the 35 % band, the live regime turns negative: the working
// entry is cancelled.
func TestFreeze_PendingRegimeDropCancelsTheEntry(t *testing.T) {
	f := newStrategyFixture(t)
	f.withConfirmedRegime("POSITIVE/PINNING")        // limit 35 %
	f.market.setIVHistory([]float64{50, 50, 50}, 50) // middle band
	g := &switchGEX{snap: positiveSnap}
	f.gex = g
	f.cfg.OrderFillTimeoutSec = 60
	f.startRun()

	eventually(t, 3*time.Second, "entry resting", func() bool { return len(f.exch.sells()) == 2 })
	g.set(negativeSnap) // no trend in the fixture: nothing is shed, only the freeze acts
	eventually(t, 3*time.Second, "freeze cancels the entry", func() bool { return f.cancelsWith(orders.TriggerRiskFrozen) > 0 })
	if f.frozenRecords() == 0 {
		t.Error("the freeze is journaled as risk_frozen")
	}
}

// Scenario: a change that would lower the limit still freezes — and cancels
// a working top-up.
func TestFreeze_PendingRegimeDropCancelsTheTopUp(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()                      // 50 % band confirmed: a one-lot complement is sent
	f.withConfirmedRegime("POSITIVE/PINNING") // top-ups allowed
	g := &switchGEX{snap: positiveSnap}
	f.gex = g
	f.cfg.OrderFillTimeoutSec = 60
	f.startRun()

	eventually(t, 3*time.Second, "complement resting", func() bool { return len(f.exch.sells()) == 2 })
	g.set(negativeSnap)
	eventually(t, 3*time.Second, "freeze cancels the complement", func() bool { return f.cancelsWith(orders.TriggerRiskFrozen) > 0 })
}
