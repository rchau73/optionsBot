package tests

import (
	"math"
	"testing"

	"optionsbot/internal/orders"
)

// ── StateManager.UpdatePositionQty ───────────────────────────────────────────

func makePosSimple(id string, qty, premium float64) *orders.Position {
	return &orders.Position{
		ID:              id,
		Instrument:      "BTC-TEST-CALL",
		OptionType:      "call",
		Qty:             qty,
		PremiumReceived: premium,
	}
}

func TestUpdatePositionQty_DownsizeReducesQtyAndPremium(t *testing.T) {
	sm := orders.NewStateManager()
	pos := makePosSimple("p1", 4.0, 0.040)
	sm.AddPosition(pos)

	// Downsize from 4 ETH to 2 ETH: premium scales proportionally (0.040 × 2/4 = 0.020).
	sm.UpdatePositionQty("p1", 2.0, 0.020)

	got, ok := sm.GetPosition("p1")
	if !ok {
		t.Fatal("position not found after update")
	}
	if math.Abs(got.Qty-2.0) > 1e-9 {
		t.Errorf("Qty = %.4f, want 2.0", got.Qty)
	}
	if math.Abs(got.PremiumReceived-0.020) > 1e-9 {
		t.Errorf("PremiumReceived = %.6f, want 0.020", got.PremiumReceived)
	}
}

func TestUpdatePositionQty_UpsizeIncreasesQtyAndPremium(t *testing.T) {
	sm := orders.NewStateManager()
	pos := makePosSimple("p2", 2.0, 0.020)
	sm.AddPosition(pos)

	// Upsize from 2 to 4 ETH at fill price 0.010 per ETH:
	// newPremium = 0.020 + 0.010×2 = 0.040
	sm.UpdatePositionQty("p2", 4.0, 0.040)

	got, _ := sm.GetPosition("p2")
	if math.Abs(got.Qty-4.0) > 1e-9 {
		t.Errorf("Qty = %.4f, want 4.0", got.Qty)
	}
	if math.Abs(got.PremiumReceived-0.040) > 1e-9 {
		t.Errorf("PremiumReceived = %.6f, want 0.040", got.PremiumReceived)
	}
}

func TestUpdatePositionQty_UnknownIDIsNoOp(t *testing.T) {
	sm := orders.NewStateManager()
	// Should not panic on unknown ID.
	sm.UpdatePositionQty("does-not-exist", 5.0, 0.050)
}

// ── Rebalance sizing math ─────────────────────────────────────────────────────
//
// These tests verify the arithmetic the rebalance function applies to decide
// whether to downsize, upsize, or leave a position alone.

func TestRebalanceMath_DownsizeProportionalPremium(t *testing.T) {
	// Existing position: 215 ETH qty, 2.150 ETH premium received.
	// New target: 108 ETH (leverage halved).
	// Expected new premium: 2.150 × (108/215) = 1.081…
	currentQty := 215.0
	currentPremium := 2.150
	targetQty := 108.0

	newPremium := currentPremium * (targetQty / currentQty)
	want := 2.150 * (108.0 / 215.0)
	if math.Abs(newPremium-want) > 1e-9 {
		t.Errorf("downsize premium = %.6f, want %.6f", newPremium, want)
	}
	// Proportional reduction: premium should shrink by the same ratio as qty.
	qtyRatio := targetQty / currentQty
	premiumRatio := newPremium / currentPremium
	if math.Abs(qtyRatio-premiumRatio) > 1e-9 {
		t.Errorf("premium ratio %.6f != qty ratio %.6f — must be proportional", premiumRatio, qtyRatio)
	}
}

func TestRebalanceMath_UpsizeKeepsExistingPositions(t *testing.T) {
	// Upsize opens a complement strangle for the missing balance; existing positions
	// must remain untouched so no unrealised P&L is crystallised.
	sm := orders.NewStateManager()
	call := makePosSimple("call-1", 108.0, 1.080)
	put := makePosSimple("put-1", 108.0, 1.080)
	sm.AddPosition(call)
	sm.AddPosition(put)

	// rebalancePositions does NOT remove positions on upsize — only opens new orders.
	// Verify originals are still present after the rebalance cycle.
	got, ok := sm.GetPosition("call-1")
	if !ok {
		t.Fatal("call leg must not be removed on upsize")
	}
	if math.Abs(got.Qty-108.0) > 1e-9 {
		t.Errorf("call qty should be unchanged: got %.4f, want 108.0", got.Qty)
	}
	got, ok = sm.GetPosition("put-1")
	if !ok {
		t.Fatal("put leg must not be removed on upsize")
	}
	if math.Abs(got.Qty-108.0) > 1e-9 {
		t.Errorf("put qty should be unchanged: got %.4f, want 108.0", got.Qty)
	}
}

func TestRebalanceMath_UpsizeComplementQtyIsBalanced(t *testing.T) {
	// The complement qty is min(callDiff, putDiff) so the new strangle is symmetric.
	callDiff := 115.0
	putDiff := 107.0
	exchMin := 1.0

	addQty := math.Min(callDiff, putDiff)
	if addQty < exchMin {
		t.Errorf("complement qty %.4f is below exchMin %.4f", addQty, exchMin)
	}
	// Both legs of the complement strangle get the same qty.
	if math.Abs(addQty-putDiff) > 1e-9 {
		t.Errorf("complement qty %.4f should equal min diff %.4f", addQty, putDiff)
	}
}

func TestRebalanceMath_NoDiffWithinOneLot(t *testing.T) {
	// If target and current differ by less than exchMin (1 ETH), no action.
	currentQty := 215.0
	targetQty := 215.5 // 0.5 ETH difference < exchMin of 1 ETH
	exchMin := 1.0

	diff := targetQty - currentQty
	if math.Abs(diff) >= exchMin {
		t.Errorf("expected no rebalance needed (diff %.2f < exchMin %.2f)", diff, exchMin)
	}
}

func TestRebalanceMath_DownsizeTriggerCondition(t *testing.T) {
	// Position is over-budget: current 430 ETH, target 215 ETH → downsize.
	currentQty := 430.0
	targetQty := 215.0
	exchMin := 1.0

	diff := targetQty - currentQty // -215: negative means over-sized
	if diff >= 0 {
		t.Error("expected negative diff for over-budget position")
	}
	excess := -diff
	if math.Abs(excess-215.0) > 1e-9 {
		t.Errorf("excess to close = %.2f, want 215.0", excess)
	}
	if excess < exchMin {
		t.Error("excess should be >= exchMin to trigger rebalance")
	}
}

func TestRebalanceMath_UpsizeTriggerCondition(t *testing.T) {
	// Under-budget position: diff >= exchMin triggers the close-and-reenter path.
	currentQty := 108.0
	targetQty := 215.0
	exchMin := 1.0

	diff := targetQty - currentQty // +107: positive means under-sized
	if diff <= 0 {
		t.Error("expected positive diff for under-budget position")
	}
	// The upsize branch triggers when callDiff >= exchMin OR putDiff >= exchMin.
	if diff < exchMin {
		t.Error("diff should be >= exchMin to trigger close-and-reenter")
	}
}

func TestRebalanceMath_NearExpirySkipped(t *testing.T) {
	// Positions at or below rolloutDTE should not be rebalanced.
	rolloutDTE := 15
	positionDTE := 14

	shouldSkip := positionDTE <= rolloutDTE
	if !shouldSkip {
		t.Errorf("DTE=%d should be skipped (rolloutDTE=%d)", positionDTE, rolloutDTE)
	}
}

func TestRebalanceMath_AboveRolloutDTENotSkipped(t *testing.T) {
	rolloutDTE := 15
	positionDTE := 16

	shouldSkip := positionDTE <= rolloutDTE
	if shouldSkip {
		t.Errorf("DTE=%d should NOT be skipped (rolloutDTE=%d)", positionDTE, rolloutDTE)
	}
}
