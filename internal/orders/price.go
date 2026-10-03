package orders

import "math"

// Deribit prices and quantities live on fixed grids (tick size, minimum trade
// amount). Plain float math leaves residue such as 107*0.0001 = 0.010700000000000001,
// which the exchange rejects. These helpers snap a value onto its grid and then
// trim the residue at 8 decimals — finer than any Deribit tick or lot size.

const floatResidueScale = 1e8

// stepEpsilon absorbs division error before Ceil/Floor: 0.3/0.1 evaluates to
// 2.9999999999999996, which would otherwise floor to 2 lots instead of 3.
const stepEpsilon = 1e-9

func trimResidue(v float64) float64 {
	return math.Round(v*floatResidueScale) / floatResidueScale
}

// RoundToStep rounds v to the nearest multiple of step.
func RoundToStep(v, step float64) float64 {
	return trimResidue(math.Round(v/step) * step)
}

// CeilToStep rounds v up to the next multiple of step.
func CeilToStep(v, step float64) float64 {
	return trimResidue(math.Ceil(v/step-stepEpsilon) * step)
}

// FloorToStep rounds v down to the previous multiple of step. Use it for
// quantities, where rounding up would exceed the intended size.
func FloorToStep(v, step float64) float64 {
	return trimResidue(math.Floor(v/step+stepEpsilon) * step)
}
