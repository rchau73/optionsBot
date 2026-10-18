package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The monitor shows, for every one-legged strangle, what holds the repair of
// its missing leg and the figures each release condition is measured against
// (2026-10-11: spot above the flip, but the puts were held by the regime-side
// rule and the stop records — not by the GEX anchor the 24 h release applies to).

func heldLegs(f *strategyFixture) []strategy.HeldLegView { return f.strat.View().HeldLegs }

func TestHeldLegs_RegimeSide(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip() // spot 100000, flip 90000, confirmed negative, bear
	f.startRun()

	eventually(t, 2*time.Second, "held leg published", func() bool { return len(heldLegs(f)) == 1 })
	h := heldLegs(f)[0]
	if h.Missing != "put" || h.OpenLeg != f.call || h.Rule != strategy.HoldRegimeSide {
		t.Fatalf("got missing %q open %q rule %q", h.Missing, h.OpenLeg, h.Rule)
	}
	if !strings.Contains(h.Reason, "no new puts") || !h.ConfirmedNegative || h.Trend != "bear" {
		t.Errorf("reason %q, confirmed negative %v, trend %q", h.Reason, h.ConfirmedNegative, h.Trend)
	}
	if h.Spot != 100000 || h.LiveFlip != 90000 || h.AboveFlipLevel < 90000 {
		t.Errorf("spot %v, flip %v, above-flip level %v", h.Spot, h.LiveFlip, h.AboveFlipLevel)
	}
	if h.Anchor != nil || h.StoppedAt != nil {
		t.Errorf("neither shed nor stopped: anchor %v, stopped %v", h.Anchor, h.StoppedAt)
	}
}

func TestHeldLegs_StopCooldown(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.withLoneCall(0.1)
	closed := time.Now().Add(-48 * time.Hour)
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.TriggerManualClose, Instrument: f.put, At: closed}})
	}
	f.startRun()

	eventually(t, 2*time.Second, "held leg published", func() bool { return len(heldLegs(f)) == 1 })
	h := heldLegs(f)[0]
	if h.Rule != strategy.HoldStopped || !strings.Contains(h.Reason, "cooldown") {
		t.Fatalf("rule %q reason %q", h.Rule, h.Reason)
	}
	if h.CooldownUntil == nil || !h.CooldownUntil.Equal(closed.Add(72*time.Hour)) {
		t.Errorf("cooldown until %v, want %v", h.CooldownUntil, closed.Add(72*time.Hour))
	}
	if n := putSells(f); n != 0 {
		t.Errorf("reading the view must not release the repair: %d sells", n)
	}
}

// A GEX-shed leg shows its anchor flip next to the live one.
func TestHeldLegs_ShedAnchorAgainstLiveFlip(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.GEXShedConfirmSnapshots = 2
	f.cfg.GEXRepairConfirmSnapshots = 2
	f.cfg.GEXRepairReleaseHours = 24
	f.withOpenStrangle(0.1, 0.02)
	f.withPutSheddingRegime()
	f.withConfirmedRegime("POSITIVE/PINNING") // the negative regime is unconfirmed: the shed anchor, not the regime-side rule, holds the put
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { return orders.Fill{Qty: o.Qty, FillPrice: 0.02} }
	at := time.Now()
	g := &switchGEX{}
	next := func(spot, flip float64) {
		at = at.Add(time.Minute)
		g.set(&gex.Snapshot{ComputedAt: at, Regime: "NEGATIVE/ACCELERATION", Spot: spot, GammaFlip: flip, GammaFlipFound: true})
	}
	next(100000, 120000)
	f.gex = g
	f.startRun()
	time.Sleep(150 * time.Millisecond)
	next(100000, 120000) // the signal holds on a second snapshot: shed
	eventually(t, 2*time.Second, "put shed", func() bool { return f.position(f.put) == nil })

	next(100000, 90000) // the flip jumps under spot
	eventually(t, 2*time.Second, "held by the anchor", func() bool {
		h := heldLegs(f)
		return len(h) == 1 && h[0].Rule == strategy.HoldGEXShed && h[0].LiveFlip == 90000
	})
	h := heldLegs(f)[0]
	if h.Anchor == nil || h.Anchor.Flip != 120000 || h.Anchor.Level < 120000 || h.Anchor.Need != 2 || h.Anchor.ReleaseHours != 24 {
		t.Fatalf("anchor %+v", h.Anchor)
	}
	if h.AboveFlipLevel >= h.Anchor.Level {
		t.Errorf("the live level %v is below the anchor's %v here", h.AboveFlipLevel, h.Anchor.Level)
	}
}

func TestHeldLegs_EmptyForCompleteStrangles(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "positions loaded", func() bool { return len(f.strat.View().Strangles) == 1 })
	b, err := json.Marshal(f.strat.View())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"held_legs":[]`) {
		t.Errorf("held_legs must be an empty list, not null")
	}
}
