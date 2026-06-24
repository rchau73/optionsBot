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
// max_margin_pct × leverage × total account equity. The IV percentile is accepted
// for call-site compatibility but no longer overrides the configured cap — Deribit's
// Portfolio Margin model already incorporates volatility into its requirements.
func (g *MarginGuard) AllowedMargin(equity, _ float64) float64 {
	return equity * g.maxMarginPct * g.leverage
}

// WithinLimit returns true if adding newMarginCost to currentMargin stays within
// the cap. equity is total account equity (balance + unrealized PnL).
func (g *MarginGuard) WithinLimit(currentMargin, newMarginCost, equity, _ float64) bool {
	return currentMargin+newMarginCost <= g.AllowedMargin(equity, 0)
}

// ComputeQtyFromIM derives the position size from a PM margin target using leverage.
//
// callIM and putIM are the incremental initial margins Deribit estimates for one
// exchMin-sized lot of the call and put respectively (from private/get_margins,
// estimated separately per leg). The function scales up to as many whole lots as
// fit within targetMargin, with a floor of one lot.
//
// Falls back to one lot (exchMin) when:
//   - exchMin ≤ 0 (degenerate instrument data)
//   - imPerUnit ≤ 0 (the strangle reduces or neutralises portfolio PM — avoid
//     unbounded sizing; open one lot at minimum exposure)
func ComputeQtyFromIM(exchMin, targetMargin, callIM, putIM float64) float64 {
	if exchMin <= 0 {
		return exchMin
	}
	imPerUnit := callIM + putIM
	if imPerUnit <= 0 {
		return exchMin
	}
	units := math.Floor(targetMargin / imPerUnit)
	if units < 1 {
		units = 1
	}
	return units * exchMin
}
