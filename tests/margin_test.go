package tests

import (
	"math"
	"testing"

	"optionsbot/internal/strategy"
)

// equity used throughout: 0.53 BTC (realistic live account size)
const testEquity = 0.53

// ── MarginGuard.AllowedMargin ─────────────────────────────────────────────────

func TestMarginGuard_AllowedMargin_Basic(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	want := testEquity * 0.35
	got := g.AllowedMargin(testEquity)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("AllowedMargin(%.2f) = %.6f, want %.6f", testEquity, got, want)
	}
}

func TestMarginGuard_AllowedMargin_ZeroEquity(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	if got := g.AllowedMargin(0); got != 0 {
		t.Errorf("AllowedMargin(0) = %.6f, want 0", got)
	}
}

// ── Budget calculation: budget = equity×cap - existingMargin ─────────────────
//
// This is the exact formula in fetchMarginState + maybeOpenStrangles.
// It ensures pre-existing positions (including those opened outside the bot)
// are already counted when the bot decides how much room it has left.

func TestMarginBudget_NoExistingPositions(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	equity := testEquity
	existingMargin := 0.0

	allowed := g.AllowedMargin(equity)
	budget := allowed - existingMargin

	wantAllowed := 0.53 * 0.35 // 0.1855 BTC
	if math.Abs(allowed-wantAllowed) > 1e-9 {
		t.Errorf("allowed = %.6f BTC, want %.6f BTC", allowed, wantAllowed)
	}
	if math.Abs(budget-wantAllowed) > 1e-9 {
		t.Errorf("budget = %.6f BTC, want %.6f BTC (full allowance)", budget, wantAllowed)
	}
}

func TestMarginBudget_PreExistingPositionsReduceBudget(t *testing.T) {
	// 10% of equity already consumed by existing positions (opened outside the bot
	// or on a previous run). Bot should only be allowed 25% more (35% - 10%).
	g := strategy.NewMarginGuard(0.35, 1.0)
	equity := testEquity
	existingMargin := equity * 0.10 // 10% used

	allowed := g.AllowedMargin(equity)
	budget := allowed - existingMargin

	wantBudget := equity * (0.35 - 0.10) // 25% of equity
	if math.Abs(budget-wantBudget) > 1e-9 {
		t.Errorf("budget = %.6f BTC, want %.6f BTC (25%% of equity)", budget, wantBudget)
	}
	if budget <= 0 {
		t.Errorf("budget should be positive when only 10%% used vs 35%% cap")
	}
}

func TestMarginBudget_AtCapStopsNewPositions(t *testing.T) {
	// Exactly at 35% — budget must be zero so no new strangles are opened.
	g := strategy.NewMarginGuard(0.35, 1.0)
	equity := testEquity
	existingMargin := equity * 0.35 // exactly at cap

	allowed := g.AllowedMargin(equity)
	budget := allowed - existingMargin

	if budget > 1e-12 {
		t.Errorf("budget = %.8f BTC, expected ≤ 0 when at cap", budget)
	}
}

func TestMarginBudget_OverCapStopsNewPositions(t *testing.T) {
	// 37.7% used (e.g. positions grew in value against us). Budget must be negative.
	g := strategy.NewMarginGuard(0.35, 1.0)
	equity := testEquity
	existingMargin := equity * 0.377

	allowed := g.AllowedMargin(equity)
	budget := allowed - existingMargin

	if budget >= 0 {
		t.Errorf("budget = %.8f BTC, expected < 0 when over cap", budget)
	}
}

func TestMarginBudget_RealWorldScenario(t *testing.T) {
	// User's live account: 0.53 BTC equity, 18% MM ≈ ~22% IM in use.
	// Cap is 35% → only 13% (≈ 0.069 BTC) of budget remains.
	g := strategy.NewMarginGuard(0.35, 1.0)
	equity := 0.53
	existingIM := equity * 0.22 // ~22% initial margin for 4-leg strangle book

	allowed := g.AllowedMargin(equity)
	budget := allowed - existingIM

	wantBudget := equity * (0.35 - 0.22)
	if math.Abs(budget-wantBudget) > 1e-9 {
		t.Errorf("budget = %.6f BTC, want %.6f BTC", budget, wantBudget)
	}
	if budget <= 0 {
		t.Errorf("budget should be positive — 22%% used vs 35%% cap still leaves room")
	}
}

// ── ComputeQtyFromIM: PM-aware position sizing ────────────────────────────────
//
// ComputeQtyFromIM scales position size to fill the per-slot margin budget using
// the incremental IM rates returned by private/get_margins for each leg.
//
// Key invariants:
//   - Result is always a whole multiple of exchMin (Deribit minimum lot)
//   - Floor of one lot even when budget < one unit's IM (never zero qty)
//   - Falls back to one lot when imPerUnit ≤ 0 (hedging reduction or zero rate)

func TestComputeQtyFromIM_FloorIsOneExchMin(t *testing.T) {
	// Budget is smaller than one unit's IM — must still open one lot.
	qty := strategy.ComputeQtyFromIM(0.1, 0.005, 0.015, 0.015)
	if math.Abs(qty-0.1) > 1e-9 {
		t.Errorf("expected floor of 1 lot = 0.1 BTC, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ExactlyOneLot(t *testing.T) {
	// Budget equals exactly one unit's IM.
	qty := strategy.ComputeQtyFromIM(0.1, 0.030, 0.015, 0.015)
	if math.Abs(qty-0.1) > 1e-9 {
		t.Errorf("expected 1 lot = 0.1 BTC, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ScalesUpToFillBudget(t *testing.T) {
	// Budget fits 4 lots exactly.
	// exchMin=0.1, callIM=0.015, putIM=0.015 → imPerUnit=0.030
	// floor(0.120 / 0.030) = 4 → qty = 0.4 BTC
	qty := strategy.ComputeQtyFromIM(0.1, 0.120, 0.015, 0.015)
	if math.Abs(qty-0.4) > 1e-9 {
		t.Errorf("expected 4 lots = 0.4 BTC, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_PartialLotIsFloored(t *testing.T) {
	// Budget fits 3.67 lots — must floor to 3.
	// floor(0.110 / 0.030) = 3 → qty = 0.3 BTC
	qty := strategy.ComputeQtyFromIM(0.1, 0.110, 0.015, 0.015)
	if math.Abs(qty-0.3) > 1e-9 {
		t.Errorf("expected 3 lots = 0.3 BTC (floored), got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ZeroIMUsesBudgetAsNotional(t *testing.T) {
	// Both legs show zero IM — PM data unavailable (e.g. testnet ETH).
	// Should size by floor(targetMargin / exchMin), not fall back to exchMin.
	// floor(0.5 / 0.1) = 5 → qty = 0.5
	qty := strategy.ComputeQtyFromIM(0.1, 0.5, 0.0, 0.0)
	if math.Abs(qty-0.5) > 1e-9 {
		t.Errorf("zero IM: expected budget-based qty 0.5, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ZeroIMBudgetFloor(t *testing.T) {
	// Budget smaller than one lot — floor clamps to exchMin (1 lot), not zero.
	qty := strategy.ComputeQtyFromIM(1.0, 0.5, 0.0, 0.0)
	if math.Abs(qty-1.0) > 1e-9 {
		t.Errorf("zero IM budget floor: expected 1.0 (floor), got %.6f", qty)
	}
}

func TestComputeQtyFromIM_NegativeTotalIMFallsBackToExchMin(t *testing.T) {
	// Net IM is negative (new strangle reduces portfolio risk under PM netting).
	// Avoid unbounded sizing — open one lot at minimum exposure.
	qty := strategy.ComputeQtyFromIM(0.1, 0.5, -0.02, 0.01)
	if math.Abs(qty-0.1) > 1e-9 {
		t.Errorf("expected fallback to exchMin when imPerUnit < 0, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ETHTestnetScenario(t *testing.T) {
	// Mirrors the ETH testnet case: equity=924 ETH, max_margin_pct=35%, leverage=2×,
	// 3 slots, no existing positions → budget=647 ETH, targetPerSlot=215.75 ETH.
	// Testnet returns zero IM for ETH options → budget-as-notional fallback.
	// exchMin=1 ETH → floor(215.75/1) = 215 → qty = 215 ETH.
	targetPerSlot := 924.0 * 0.35 * 2.0 / 3.0 // 215.75 ETH
	qty := strategy.ComputeQtyFromIM(1.0, targetPerSlot, 0.0, 0.0)
	if math.Abs(qty-215.0) > 1e-9 {
		t.Errorf("ETH testnet: expected 215 ETH (budget-based), got %.6f", qty)
	}
}

func TestComputeQtyFromIM_AsymmetricLegs(t *testing.T) {
	// Call and put have different IM rates — they are summed, not averaged.
	// callIM=0.010, putIM=0.020 → imPerUnit=0.030
	// floor(0.095 / 0.030) = 3 → qty = 0.3 BTC
	qty := strategy.ComputeQtyFromIM(0.1, 0.095, 0.010, 0.020)
	if math.Abs(qty-0.3) > 1e-9 {
		t.Errorf("expected 3 lots = 0.3 BTC, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_ProductionScenario(t *testing.T) {
	// Validates the leverage math against the user's live account numbers:
	//   equity=0.53 BTC, max_margin_pct=35%, 4 slots, no existing positions.
	//   target IM = 0.53 × 0.35 = 0.1855 BTC → per slot = 0.046375 BTC.
	//
	// If Deribit reports callIM=0.023, putIM=0.023 per exchMin (0.1 BTC) lot:
	//   imPerUnit = 0.046, units = floor(0.046375/0.046) = 1 → 0.1 BTC per leg
	//
	// With larger IM rates (tighter strikes / higher vol):
	//   callIM=0.011, putIM=0.012 → imPerUnit=0.023
	//   units = floor(0.046375/0.023) = 2 → 0.2 BTC per leg
	equity := 0.53
	maxMarginPct := 0.35
	numSlots := 4
	existingIM := 0.0
	budget := equity*maxMarginPct - existingIM
	targetPerSlot := budget / float64(numSlots) // 0.046375

	// IM rates that yield 2 lots per leg
	qty := strategy.ComputeQtyFromIM(0.1, targetPerSlot, 0.011, 0.012)
	if math.Abs(qty-0.2) > 1e-9 {
		t.Errorf("production scenario: expected 2 lots = 0.2 BTC, got %.6f", qty)
	}
}

func TestComputeQtyFromIM_TestnetScenario(t *testing.T) {
	// Validates the needsOpen divisor fix: if 2 of 4 slots are already filled,
	// each vacant slot gets budget/2, not budget/4, producing 2× the qty.
	equity := 2.15
	maxMarginPct := 0.35
	existingIM := 0.44
	budget := equity*maxMarginPct - existingIM // ~0.3125 BTC

	// All 4 slots vacant (startup): target = 0.3125/4 = 0.078125 BTC per slot
	allVacant := budget / 4.0
	qty4 := strategy.ComputeQtyFromIM(0.1, allVacant, 0.018, 0.018) // imPerUnit=0.036

	// Only 2 slots vacant (mid-session): target = 0.3125/2 = 0.15625 BTC per slot
	twoVacant := budget / 2.0
	qty2 := strategy.ComputeQtyFromIM(0.1, twoVacant, 0.018, 0.018)

	// With 2 vacant slots the per-slot budget is 2× larger → 2× the qty
	if qty2 <= qty4 {
		t.Errorf("fewer vacant slots should produce larger per-slot qty: qty4=%.4f qty2=%.4f", qty4, qty2)
	}
	// floor(0.078125/0.036)=2 → 0.2 BTC; floor(0.15625/0.036)=4 → 0.4 BTC
	if math.Abs(qty4-0.2) > 1e-9 {
		t.Errorf("4-vacant: expected 0.2 BTC, got %.6f", qty4)
	}
	if math.Abs(qty2-0.4) > 1e-9 {
		t.Errorf("2-vacant: expected 0.4 BTC, got %.6f", qty2)
	}
}

// ── MarginGuard.WithinLimit ───────────────────────────────────────────────────

func TestWithinLimit_FitsUnderCap(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	// 10% used, adding 5% → 15%, well under 35%.
	if !g.WithinLimit(testEquity*0.10, testEquity*0.05, testEquity) {
		t.Error("15% combined should be within 35% cap")
	}
}

func TestWithinLimit_ExactlyAtCap(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	// 10% used, adding 25% → exactly 35%.
	if !g.WithinLimit(testEquity*0.10, testEquity*0.25, testEquity) {
		t.Error("35% combined should be at (not over) 35% cap")
	}
}

func TestWithinLimit_ExceedsCap(t *testing.T) {
	g := strategy.NewMarginGuard(0.35, 1.0)
	// 30% used, adding 10% → 40%, over cap.
	if g.WithinLimit(testEquity*0.30, testEquity*0.10, testEquity) {
		t.Error("40% combined should exceed 35% cap")
	}
}

// ── Leverage ──────────────────────────────────────────────────────────────────

func TestLeverage_DoublesAllowedMargin(t *testing.T) {
	base := strategy.NewMarginGuard(0.35, 1.0)
	lev2 := strategy.NewMarginGuard(0.35, 2.0)

	want := base.AllowedMargin(testEquity) * 2
	got := lev2.AllowedMargin(testEquity)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("2× leverage: got %.6f BTC, want %.6f BTC", got, want)
	}
}

func TestLeverage_ExactBudgetCalculation(t *testing.T) {
	// equity=1 BTC, max_margin_pct=0.35, leverage=2 → allowed=0.70 BTC
	g := strategy.NewMarginGuard(0.35, 2.0)
	want := 0.70
	got := g.AllowedMargin(1.0)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("AllowedMargin = %.6f BTC, want %.6f BTC", got, want)
	}
}

func TestLeverage_OneIsIdentityFunction(t *testing.T) {
	// leverage=1 must be identical to the pre-leverage baseline.
	g1 := strategy.NewMarginGuard(0.35, 1.0)
	gBase := strategy.NewMarginGuard(0.35, 1.0)
	if g1.AllowedMargin(testEquity) != gBase.AllowedMargin(testEquity) {
		t.Error("leverage=1 should be identical to no-leverage baseline")
	}
}

func TestLeverage_WithinLimit_Respects2x(t *testing.T) {
	// Without leverage: 30% used + 10% new = 40% > 35% cap → denied.
	// With 2× leverage: cap is 70% → the same position should be allowed.
	g1x := strategy.NewMarginGuard(0.35, 1.0)
	g2x := strategy.NewMarginGuard(0.35, 2.0)

	current := testEquity * 0.30
	newCost := testEquity * 0.10

	if g1x.WithinLimit(current, newCost, testEquity) {
		t.Error("1× leverage: 40% should exceed 35% cap")
	}
	if !g2x.WithinLimit(current, newCost, testEquity) {
		t.Error("2× leverage: 40% should be within 70% cap")
	}
}
