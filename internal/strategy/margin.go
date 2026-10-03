package strategy

import "math"

// MarginGuard enforces IV-risk-model margin limits.
type MarginGuard struct {
	maxMarginPct float64
	leverage     float64
}

func NewMarginGuard(maxMarginPct, leverage float64) *MarginGuard {
	return &MarginGuard{maxMarginPct: maxMarginPct, leverage: leverage}
}

// AllowedMargin returns the maximum PM initial margin allowed, computed as
// max_margin_pct × leverage × total account equity. Volatility is deliberately
// not an input: Deribit's Portfolio Margin model already prices it in.
func (g *MarginGuard) AllowedMargin(equity float64) float64 {
	return equity * g.maxMarginPct * g.leverage
}

// WithinLimit returns true if adding newMarginCost to currentMargin stays within
// the cap. equity is total account equity (balance + unrealized PnL).
func (g *MarginGuard) WithinLimit(currentMargin, newMarginCost, equity float64) bool {
	return currentMargin+newMarginCost <= g.AllowedMargin(equity)
}

// ComputeQtyFromIM derives the position size from a PM margin target.
//
// callIM and putIM are the incremental initial margins Deribit estimates for one
// exchMin-sized lot of the call and put respectively (from private/get_margins).
// The function scales up to as many whole lots as fit within targetMargin, with
// a floor of one lot.
//
// Three cases for imPerUnit = callIM + putIM:
//   - imPerUnit > 0: normal PM data — size by floor(targetMargin / imPerUnit)
//   - imPerUnit == 0: PM data unavailable (e.g. testnet ETH) — size by
//     floor(targetMargin / exchMin), treating the budget as direct notional
//   - imPerUnit < 0: strangle reduces portfolio PM (netting benefit) — cap at
//     exchMin to avoid unbounded sizing
func ComputeQtyFromIM(exchMin, targetMargin, callIM, putIM float64) float64 {
	if exchMin <= 0 {
		return exchMin
	}
	imPerUnit := callIM + putIM
	if imPerUnit < 0 {
		// Netting benefit: adding more lots would reduce PM indefinitely — cap at minimum.
		return exchMin
	}
	var units float64
	if imPerUnit == 0 {
		// No PM data from exchange: use budget as direct notional (budget ÷ lot size).
		units = math.Floor(targetMargin / exchMin)
	} else {
		units = math.Floor(targetMargin / imPerUnit)
	}
	if units < 1 {
		units = 1
	}
	return units * exchMin
}
