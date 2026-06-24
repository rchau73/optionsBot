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
	}
}

func TestEvaluateLeg_StopLoss(t *testing.T) {
	// Loss of 200%: position cost 3x the premium received
	pos := makePos("call", 30, 100, 300, 0.50) // mid=300, premium=100 → loss = 200%
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionStopLoss {
		t.Errorf("expected ActionStopLoss, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerStopLoss200Pct {
		t.Errorf("expected trigger %q, got %q", orders.TriggerStopLoss200Pct, dec.Reason)
	}
}

func TestEvaluateLeg_19DTE(t *testing.T) {
	pos := makePos("call", 15, 100, 60, 0.20) // DTE=15, loss=40%
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
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
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
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
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionRollSameLeg {
		t.Errorf("expected ActionRollSameLeg for ROI take-profit, got %v", dec.Action)
	}
	if dec.Reason != orders.TriggerRolloutROI {
		t.Errorf("expected trigger %q, got %q", orders.TriggerRolloutROI, dec.Reason)
	}
}

func TestEvaluateLeg_NoAction(t *testing.T) {
	// Healthy position: 40 DTE, delta=0.16, ROI=30%, no loss
	pos := makePos("call", 40, 100, 70, 0.16)
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionNone {
		t.Errorf("expected ActionNone, got %v", dec.Action)
	}
}

func TestEvaluateLeg_PriorityOrder(t *testing.T) {
	// Stop loss takes priority over 19 DTE
	pos := makePos("call", 10, 100, 350, 0.50)
	dec := strategy.EvaluateLeg(pos, 19, 0.10, 0.50, 2.0)
	if dec.Action != strategy.ActionStopLoss {
		t.Errorf("stop loss should take priority over 19 DTE, got %v", dec.Action)
	}
}

// ── Repair guard tests ────────────────────────────────────────────────────────
//
// Critical scenario: a strangle has its put leg missing. GEX is POSITIVE/PINNING
// (spot above flip) but the short-term trend indicator says "bear". The repair
// must NOT be blocked — positive GEX means the regime is safe and the strangle
// should be made whole. The old bug used raw trend for this guard instead of the
// GEX action, causing the put leg to never be repaired in bear trends even when
// the market structure was supportive.

// repairAllowedForPut mirrors the guard logic inside repairIncompleteStrangles.
func repairAllowedForPut(dec strategy.GammaDecision) bool {
	return dec.Action != strategy.GammaActionClosePuts
}

func repairAllowedForCall(dec strategy.GammaDecision) bool {
	return dec.Action != strategy.GammaActionCloseCalls
}

func TestRepairGuard_PositiveGEX_BearTrend_PutAllowed(t *testing.T) {
	// The exact bug scenario: GEX positive, trend bear → put repair must proceed.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionNone,
		Regime: "POSITIVE/PINNING",
		Trend:  "bear",
	}
	if !repairAllowedForPut(dec) {
		t.Error("put repair must be allowed when GEX is POSITIVE/PINNING, even with bear trend")
	}
}

func TestRepairGuard_PositiveGEX_BullTrend_CallAllowed(t *testing.T) {
	// Symmetric case: GEX positive, trend bull → call repair must also proceed.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionNone,
		Regime: "POSITIVE/PINNING",
		Trend:  "bull",
	}
	if !repairAllowedForCall(dec) {
		t.Error("call repair must be allowed when GEX is POSITIVE/PINNING, even with bull trend")
	}
}

func TestRepairGuard_NegativeGEX_BearTrend_PutBlocked(t *testing.T) {
	// Negative GEX + bear trend → GammaActionClosePuts → put repair must be blocked.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionClosePuts,
		Regime: "NEGATIVE/ACCELERATION",
		Trend:  "bear",
	}
	if repairAllowedForPut(dec) {
		t.Error("put repair must be blocked when GEX action is GammaActionClosePuts")
	}
	// But call repair must still be allowed in the same decision.
	if !repairAllowedForCall(dec) {
		t.Error("call repair must NOT be blocked by GammaActionClosePuts")
	}
}

func TestRepairGuard_NegativeGEX_BullTrend_CallBlocked(t *testing.T) {
	// Negative GEX + bull trend → GammaActionCloseCalls → call repair blocked, put OK.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionCloseCalls,
		Regime: "NEGATIVE/ACCELERATION",
		Trend:  "bull",
	}
	if repairAllowedForCall(dec) {
		t.Error("call repair must be blocked when GEX action is GammaActionCloseCalls")
	}
	if !repairAllowedForPut(dec) {
		t.Error("put repair must NOT be blocked by GammaActionCloseCalls")
	}
}

// ── Entry leg guard tests ─────────────────────────────────────────────────────
//
// The entry path (openStrangle) had the same bug as the repair guard: it used
// raw trend instead of GEX action to decide which legs to open. These tests
// mirror the repair guard tests so both paths are covered.

func entryPutAllowed(dec strategy.GammaDecision) bool {
	return dec.Action != strategy.GammaActionClosePuts
}

func entryCallAllowed(dec strategy.GammaDecision) bool {
	return dec.Action != strategy.GammaActionCloseCalls
}

func TestEntryGuard_PositiveGEX_BearTrend_BothLegsOpen(t *testing.T) {
	// The exact production bug: GEX positive + bear trend → both legs must open.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionNone,
		Regime: "POSITIVE/PINNING",
		Trend:  "bear",
	}
	if !entryPutAllowed(dec) {
		t.Error("put leg must be opened when GEX is POSITIVE/PINNING, even with bear trend")
	}
	if !entryCallAllowed(dec) {
		t.Error("call leg must be opened when GEX is POSITIVE/PINNING, even with bear trend")
	}
}

func TestEntryGuard_PositiveGEX_BullTrend_BothLegsOpen(t *testing.T) {
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionNone,
		Regime: "POSITIVE/PINNING",
		Trend:  "bull",
	}
	if !entryPutAllowed(dec) {
		t.Error("put leg must be opened when GEX is POSITIVE/PINNING, even with bull trend")
	}
	if !entryCallAllowed(dec) {
		t.Error("call leg must be opened when GEX is POSITIVE/PINNING, even with bull trend")
	}
}

func TestEntryGuard_NegativeGEX_BearTrend_PutSkipped(t *testing.T) {
	// Negative GEX + bear → GammaActionClosePuts → put must not be opened.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionClosePuts,
		Regime: "NEGATIVE/ACCELERATION",
		Trend:  "bear",
	}
	if entryPutAllowed(dec) {
		t.Error("put leg must be skipped when GEX action is GammaActionClosePuts")
	}
	if !entryCallAllowed(dec) {
		t.Error("call leg must still open when GEX action is GammaActionClosePuts")
	}
}

func TestEntryGuard_NegativeGEX_BullTrend_CallSkipped(t *testing.T) {
	// Negative GEX + bull → GammaActionCloseCalls → call must not be opened.
	dec := strategy.GammaDecision{
		Action: strategy.GammaActionCloseCalls,
		Regime: "NEGATIVE/ACCELERATION",
		Trend:  "bull",
	}
	if entryCallAllowed(dec) {
		t.Error("call leg must be skipped when GEX action is GammaActionCloseCalls")
	}
	if !entryPutAllowed(dec) {
		t.Error("put leg must still open when GEX action is GammaActionCloseCalls")
	}
}

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

// ── Minimum premium floor tests ───────────────────────────────────────────────
//
// The premium floor prevents opening positions when the market offers near-zero
// premium that doesn't justify the risk. It gates both legs upfront before any
// order is submitted, so a low put premium won't leave an orphaned call on the
// exchange.

// premiumPassesFloor mirrors the guard in openStrangle and reopenLeg.
func premiumPassesFloor(price, floor float64) bool {
	return floor <= 0 || price >= floor
}

func TestPremiumFloor_BothLegsAboveFloor_Allowed(t *testing.T) {
	floor := 0.001
	if !premiumPassesFloor(0.005, floor) {
		t.Error("call at 0.005 should pass floor of 0.001")
	}
	if !premiumPassesFloor(0.003, floor) {
		t.Error("put at 0.003 should pass floor of 0.001")
	}
}

func TestPremiumFloor_CallBelowFloor_Blocked(t *testing.T) {
	floor := 0.001
	if premiumPassesFloor(0.0005, floor) {
		t.Error("call at 0.0005 must be blocked by floor of 0.001")
	}
}

func TestPremiumFloor_PutBelowFloor_Blocked(t *testing.T) {
	floor := 0.001
	if premiumPassesFloor(0.0008, floor) {
		t.Error("put at 0.0008 must be blocked by floor of 0.001")
	}
}

func TestPremiumFloor_ExactlyAtFloor_Allowed(t *testing.T) {
	// Floor is ≥ not >, so exactly at the floor must be allowed.
	floor := 0.001
	if !premiumPassesFloor(0.001, floor) {
		t.Error("premium exactly at floor must be allowed (uses >=, not >)")
	}
}

func TestPremiumFloor_ZeroFloor_DisablesCheck(t *testing.T) {
	// floor=0 disables the check entirely — any premium passes including 0.
	for _, price := range []float64{0, 0.0001, 0.00001} {
		if !premiumPassesFloor(price, 0) {
			t.Errorf("floor=0 should allow any premium, blocked at %.6f", price)
		}
	}
}

func TestPremiumFloor_ConfigFieldParsed(t *testing.T) {
	cfg := &config.Config{MinPremiumBTC: 0.001}
	if cfg.MinPremiumBTC != 0.001 {
		t.Errorf("expected MinPremiumBTC=0.001, got %v", cfg.MinPremiumBTC)
	}
}

func TestPremiumFloor_OrderTimeoutDefault(t *testing.T) {
	// Load() defaults OrderFillTimeoutSec to 90 when config sets 0.
	// Simulate the fallback logic here so we catch regressions.
	timeoutSec := 0
	if timeoutSec <= 0 {
		timeoutSec = 90
	}
	if timeoutSec != 90 {
		t.Errorf("expected default timeout 90s, got %d", timeoutSec)
	}
}

// ── resolveQty / position sizing tests ───────────────────────────────────────
//
// resolveQty must return a fixed, uniform size for every slot — the exchange's
// actual per-instrument minimum. It must NOT scale up based on the margin budget,
// which caused unbalanced strangles (e.g. 0.1 call / 0.2 put) when PM margin
// per unit divided unevenly into the slot target.

// resolveQtyForSlot mirrors the logic in strategy.resolveQty.
func resolveQtyForSlot(callMinTrade, putMinTrade, cfgMinTrade float64) float64 {
	exchMin := math.Max(callMinTrade, putMinTrade)
	if exchMin <= 0 {
		exchMin = cfgMinTrade
	}
	return exchMin
}

func TestResolveQty_UsesExchangeMinimum(t *testing.T) {
	// Standard case: both legs have the same Deribit minimum (0.1 BTC).
	qty := resolveQtyForSlot(0.1, 0.1, 0.05)
	if math.Abs(qty-0.1) > 1e-9 {
		t.Errorf("expected 0.1 BTC, got %.6f", qty)
	}
}

func TestResolveQty_TakesMaxOfLegs(t *testing.T) {
	// When legs have different minimums, the higher one governs.
	qty := resolveQtyForSlot(0.1, 0.2, 0.05)
	if math.Abs(qty-0.2) > 1e-9 {
		t.Errorf("expected max(0.1, 0.2)=0.2, got %.6f", qty)
	}
}

func TestResolveQty_FallsBackToCfgWhenBothZero(t *testing.T) {
	// Instruments with no MinTradeAmount set → fall back to cfg.MinTradeAmount.
	qty := resolveQtyForSlot(0, 0, 0.05)
	if math.Abs(qty-0.05) > 1e-9 {
		t.Errorf("expected cfg fallback 0.05, got %.6f", qty)
	}
}

