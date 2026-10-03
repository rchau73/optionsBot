package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// ── Config.Slots() tests ──────────────────────────────────────────────────────

func TestSlots_DTEDeltaMatrix(t *testing.T) {
	cfg := &config.Config{
		DTEDeltaMatrix: []config.DTEDeltaEntry{
			{DTE: 15, Deltas: []float64{0.10}},
			{DTE: 30, Deltas: []float64{0.16, 0.18}},
			{DTE: 45, Deltas: []float64{0.16, 0.18, 0.20}},
		},
	}
	slots := cfg.Slots()
	if len(slots) != 6 {
		t.Fatalf("expected 6 slots (1+2+3), got %d", len(slots))
	}
	// Spot-check first and last
	if slots[0].TargetDTE != 15 || math.Abs(slots[0].EntryDelta-0.10) > 1e-9 {
		t.Errorf("slot[0] want {15, 0.10}, got %+v", slots[0])
	}
	if slots[5].TargetDTE != 45 || math.Abs(slots[5].EntryDelta-0.20) > 1e-9 {
		t.Errorf("slot[5] want {45, 0.20}, got %+v", slots[5])
	}
}

func TestSlots_LegacyFallback(t *testing.T) {
	cfg := &config.Config{
		TargetDTE:  []int{30, 45},
		EntryDelta: 0.16,
	}
	slots := cfg.Slots()
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots from legacy config, got %d", len(slots))
	}
	for _, s := range slots {
		if math.Abs(s.EntryDelta-0.16) > 1e-9 {
			t.Errorf("legacy fallback: expected delta 0.16, got %v", s.EntryDelta)
		}
	}
}

func TestSlots_MatrixTakesPrecedenceOverLegacy(t *testing.T) {
	cfg := &config.Config{
		DTEDeltaMatrix: []config.DTEDeltaEntry{
			{DTE: 15, Deltas: []float64{0.10}},
		},
		TargetDTE:  []int{30, 45}, // should be ignored
		EntryDelta: 0.16,          // should be ignored
	}
	slots := cfg.Slots()
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot from matrix (legacy ignored), got %d", len(slots))
	}
	if slots[0].TargetDTE != 15 {
		t.Errorf("expected DTE 15, got %d", slots[0].TargetDTE)
	}
}

func makePos(optType string, dte int, premiumReceived, currentMid, delta float64) *orders.Position {
	expiry := time.Now().AddDate(0, 0, dte)
	return &orders.Position{
		ID:              "test-pos-1",
		Instrument:      "BTC-" + expiry.Format("2Jan06") + "-50000-C",
		OptionType:      optType,
		Expiry:          expiry,
		Qty:             1.0,
		EntryPrice:      premiumReceived,
		EntryTime:       time.Now().AddDate(0, 0, -10),
		PremiumReceived: premiumReceived,
		CurrentMid:      currentMid,
		CurrentGreeks: orders.Greeks{
			Delta: delta,
			Gamma: 0.001,
		},
		MarkLive: true,
	}
}

func TestEvaluateLeg_StopLoss(t *testing.T) {
	// Loss of 200%: position cost 3x the premium received
	pos := makePos("call", 30, 100, 300, 0.50) // mid=300, premium=100 → loss = 200%
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionStopLoss {
		t.Errorf("expected ActionStopLoss, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerStopLoss200Pct {
		t.Errorf("expected trigger %q, got %q", orders.TriggerStopLoss200Pct, dec.Reason)
	}
}

func TestEvaluateLeg_19DTE(t *testing.T) {
	pos := makePos("call", 15, 100, 60, 0.20) // DTE=15, loss=40%
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionRollNextMonth {
		t.Errorf("expected ActionRollNextMonth, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerRollout19DTE {
		t.Errorf("expected trigger %q, got %q", orders.TriggerRollout19DTE, dec.Reason)
	}
}

func TestEvaluateLeg_DeltaDrift(t *testing.T) {
	// Delta drifted below 0.10, DTE >= 25
	pos := makePos("put", 30, 100, 50, 0.05)
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionRollSameLeg {
		t.Errorf("expected ActionRollSameLeg for delta drift, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerRolloutDelta {
		t.Errorf("expected trigger %q, got %q", orders.TriggerRolloutDelta, dec.Reason)
	}
}

func TestEvaluateLeg_ROITakeProfit(t *testing.T) {
	// ROI = (100 - 40) / 100 = 60% >= 50%, DTE >= 25
	pos := makePos("call", 30, 100, 40, 0.15)
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionRollSameLeg {
		t.Errorf("expected ActionRollSameLeg for ROI take-profit, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerRolloutROI {
		t.Errorf("expected trigger %q, got %q", orders.TriggerRolloutROI, dec.Reason)
	}
}

// Without a live quote, delta and mid are only the last known values (or
// zero): delta drift and take-profit must wait, while the stop-loss and the
// time roll still act on what is known.
func TestEvaluateLeg_WithoutLiveQuote(t *testing.T) {
	drift := makePos("put", 30, 100, 0, 0) // mark and delta zeroed: what a missing quote looks like
	drift.MarkLive = false
	if dec := strategy.EvaluateLeg(drift, time.Now(), 19, 0.10, 0.50, 2.0); dec.Action != strategy.ActionNone {
		t.Errorf("no delta-drift or take-profit close without a live quote, got %v (%s)", dec.Action, dec.Reason)
	}
	stop := makePos("put", 30, 100, 300, 0)
	stop.MarkLive = false
	if dec := strategy.EvaluateLeg(stop, time.Now(), 19, 0.10, 0.50, 2.0); dec.Action != strategy.ActionStopLoss {
		t.Errorf("the stop-loss must still fire on the last known mark, got %v", dec.Action)
	}
	roll := makePos("put", 15, 100, 60, 0)
	roll.MarkLive = false
	if dec := strategy.EvaluateLeg(roll, time.Now(), 19, 0.10, 0.50, 2.0); dec.Action != strategy.ActionRollNextMonth {
		t.Errorf("the time roll must still apply, got %v", dec.Action)
	}
}

func TestEvaluateLeg_NoAction(t *testing.T) {
	// Healthy position: 40 DTE, delta=0.16, ROI=30%, no loss
	pos := makePos("call", 40, 100, 70, 0.16)
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionNone {
		t.Errorf("expected ActionNone, got %v", dec.Action)
	}
}

func TestEvaluateLeg_PriorityOrder(t *testing.T) {
	// Stop loss takes priority over 19 DTE
	pos := makePos("call", 10, 100, 350, 0.50)
	dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionStopLoss {
		t.Errorf("stop loss should take priority over 19 DTE, got %v", dec.Action)
	}
}

// Entry and repair GEX gating are exercised end to end in strategy_run_test.go
// (TestStrategy_GEXSheddingPutsOpensCallOnly, TestStrategy_RepairRespectsGEXGate).

// ── ResolveGammaAction tests ──────────────────────────────────────────────────
//
// ResolveGammaAction is the pure function that maps (regime, flipFound, spot,
// flip, trend) → GammaAction. These tests cover the three logical zones:
//
//   Zone 1 — full strangle: spot ≥ flip (raw), or regime held POSITIVE by
//             hysteresis, or no flip found
//   Zone 2 — transition band: spot in (flip*(1-band), flip) with lastRegime=POSITIVE
//             → handled by "regime == POSITIVE/PINNING" arm
//   Zone 3 — confirmed NEGATIVE: spot < flip AND regime == NEGATIVE/ACCELERATION
//             → single leg based on trend

// TestResolveGammaAction_NoFlipFound verifies that when no gamma flip is available
// (flipFound=false) the action is always None regardless of regime or trend.
func TestResolveGammaAction_NoFlipFound(t *testing.T) {
	for _, trend := range []int{-1, 0, 1} {
		got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", false, 60000, 64500, trend)
		if got != strategy.GammaActionNone {
			t.Errorf("no flip, trend=%d: expected ActionNone, got %v", trend, got)
		}
	}
}

// TestResolveGammaAction_SpotAboveFlip_BullTrend is the key bug-fix case:
// hysteresis may have held the regime as NEGATIVE even though spot is above the
// flip. The raw spot ≥ flip check must override → ActionNone, not CloseCalls.
func TestResolveGammaAction_SpotAboveFlip_BullTrend(t *testing.T) {
	// spot=64600 > flip=64500, regime still NEGATIVE (within 2% hysteresis band)
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 64600, 64500, 1)
	if got != strategy.GammaActionNone {
		t.Errorf("spot above flip with bull trend: expected ActionNone (both legs), got %v", got)
	}
}

// TestResolveGammaAction_SpotAboveFlip_BearTrend verifies the symmetric case:
// spot above flip + bear trend must also give ActionNone (not ClosePuts).
func TestResolveGammaAction_SpotAboveFlip_BearTrend(t *testing.T) {
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 64600, 64500, -1)
	if got != strategy.GammaActionNone {
		t.Errorf("spot above flip with bear trend: expected ActionNone (both legs), got %v", got)
	}
}

// TestResolveGammaAction_SpotAtFlipBoundary verifies that spot exactly equal to
// the flip (≥ boundary) is treated as full-strangle territory.
func TestResolveGammaAction_SpotAtFlipBoundary(t *testing.T) {
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 64500, 64500, -1)
	if got != strategy.GammaActionNone {
		t.Errorf("spot == flip: expected ActionNone, got %v", got)
	}
}

// TestResolveGammaAction_HysteresisHeldPositive verifies the transition-band case
// where spot has dipped below the flip but hysteresis holds regime as POSITIVE.
// Both legs must remain open to avoid premature closes.
func TestResolveGammaAction_HysteresisHeldPositive(t *testing.T) {
	// spot=64200 < flip=64500, but lastRegime=POSITIVE held by hysteresis (within band)
	for _, trend := range []int{-1, 0, 1} {
		got := strategy.ResolveGammaAction("POSITIVE/PINNING", true, 64200, 64500, trend)
		if got != strategy.GammaActionNone {
			t.Errorf("hysteresis-held POSITIVE, trend=%d: expected ActionNone, got %v", trend, got)
		}
	}
}

// TestResolveGammaAction_ConfirmedNegative_BearTrend verifies that when spot is
// clearly below the flip AND regime is NEGATIVE, a bear trend closes puts.
func TestResolveGammaAction_ConfirmedNegative_BearTrend(t *testing.T) {
	// spot=63000, flip=64500, regime=NEGATIVE — genuinely in negative territory
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 63000, 64500, -1)
	if got != strategy.GammaActionClosePuts {
		t.Errorf("confirmed negative + bear: expected GammaActionClosePuts, got %v", got)
	}
}

// TestResolveGammaAction_ConfirmedNegative_BullTrend verifies that a bull trend
// below the flip closes calls (price rising toward OTM calls).
func TestResolveGammaAction_ConfirmedNegative_BullTrend(t *testing.T) {
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 63000, 64500, 1)
	if got != strategy.GammaActionCloseCalls {
		t.Errorf("confirmed negative + bull: expected GammaActionCloseCalls, got %v", got)
	}
}

// TestResolveGammaAction_ConfirmedNegative_NeutralTrend verifies that when the
// trend is neutral (no confirmation), no leg is closed even in NEGATIVE regime.
func TestResolveGammaAction_ConfirmedNegative_NeutralTrend(t *testing.T) {
	got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 63000, 64500, 0)
	if got != strategy.GammaActionNone {
		t.Errorf("confirmed negative + neutral trend: expected ActionNone (wait), got %v", got)
	}
}
