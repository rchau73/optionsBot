package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// 2026-10-09 00:00 UTC: the negative gamma regime was confirmed, the freeze
// lifted and ETH, in a bear trend, topped up its puts (2200-P, 2100-P). Under a
// confirmed negative regime the side the trend runs toward is no longer sold.

func TestRegimeSideBlockReason(t *testing.T) {
	neg := risk.Status{RegimeUsed: true, RegimeKnown: true, RegimeNegative: true}
	pos := risk.Status{RegimeUsed: true, RegimeKnown: true}
	cases := []struct {
		typ     string
		trend   int
		st      risk.Status
		blocked bool
	}{
		{"put", -1, neg, true},
		{"call", -1, neg, false},
		{"call", 1, neg, true},
		{"put", 1, neg, false},
		{"put", 0, neg, false}, // no trend: both sides
		{"call", 0, neg, false},
		{"put", -1, pos, false},                               // regime not negative
		{"put", -1, risk.Status{RegimeNegative: true}, false}, // regime not used by the policy
	}
	for _, c := range cases {
		got := strategy.RegimeSideBlockReason(c.typ, c.trend, c.st)
		if (got != "") != c.blocked {
			t.Errorf("%s, trend %d, negative %v: blocked = %q, want %v", c.typ, c.trend, c.st.RegimeNegative, got, c.blocked)
		}
	}
}

// withBearTrendAboveFlip: a confirmed negative regime and a bear trend, with
// spot above the flip — GEX sheds nothing; only the regime rule applies.
func (f *strategyFixture) withBearTrendAboveFlip() {
	f.withPutSheddingRegime() // the bear trend from the daily closes
	f.gex = fixedGEX{&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true}}
	f.withConfirmedRegime("NEGATIVE/ACCELERATION")
}

func TestStrategy_NegativeRegimeBearTrendEntersCallOnly(t *testing.T) {
	f := newStrategyFixture(t)
	f.withBearTrendAboveFlip()
	f.startRun()

	eventually(t, 2*time.Second, "call submitted", func() bool { return len(f.exch.sells()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	for _, o := range f.exch.sells() {
		if o.Instrument != f.call {
			t.Errorf("no new puts in a confirmed negative regime with a bear trend, got %s", o.Instrument)
		}
	}
}

func TestStrategy_NegativeRegimeBearTrendHoldsPutRepair(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	f.startRun()

	eventually(t, 2*time.Second, "repair held and journaled", func() bool {
		return f.skips(strategy.SkipRepairHeld+": put confirmed negative gamma regime in a bear trend") > 0
	})
	time.Sleep(50 * time.Millisecond)
	if n := putSells(f); n != 0 {
		t.Errorf("the put must not be repaired, got %d sells", n)
	}
}

func TestStrategy_NegativeRegimeBearTrendHoldsTopUp(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 0.5 // the 20 % limit fits 4 lots: a 0.3 top-up is due
	f.withBearTrendAboveFlip()
	f.startRun()

	eventually(t, 2*time.Second, "top-up held and journaled", func() bool { return f.skips(strategy.SkipRegimeSide) > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("a top-up sells both sides or none: got %d sells", n)
	}
	for _, e := range f.journal.events("skipped") {
		if strings.HasPrefix(e.reason, strategy.SkipRegimeSide) && !strings.Contains(e.reason, "no new puts") {
			t.Errorf("reason = %q", e.reason)
		}
	}
}
