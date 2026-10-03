package strategy

import (
	"math"

	"optionsbot/internal/orders"
)

// Sizing helpers for the margin policy (see limits.go). Every margin figure
// they take comes from Deribit: the account summary or private/simulate_portfolio.

// EntryLots is how many whole lots of a strangle fit in headroom when one lot
// adds imPerLot of initial margin. A lot that adds no margin (portfolio
// netting) is sized at one lot, never scaled up without bound.
func EntryLots(headroom, imPerLot float64) int {
	if imPerLot <= 0 {
		return 1
	}
	return int(math.Floor(headroom/imPerLot + 1e-9))
}

// TargetLots is the size a held strangle should have so its initial margin
// fits share, when each lot uses imPerLot. The IM limit never closes a
// strangle completely (at least one lot stays); the MM limit can.
// It returns 0 when imPerLot is unknown (≤ 0) and no target can be set.
func TargetLots(share, imPerLot float64) int {
	if imPerLot <= 0 {
		return 0
	}
	return max(1, int(math.Floor(share/imPerLot+1e-9)))
}

// MMKeepQty is the size to keep of a short leg when maintenance margin is at
// mmPct of margin balance and must come down to maxMMPct: positions shrink in
// proportion, with 5 % slack, and always by at least one lot.
func MMKeepQty(qty, lot, mmPct, maxMMPct float64) float64 {
	if mmPct <= maxMMPct || lot <= 0 {
		return qty
	}
	keep := orders.FloorToStep(qty*maxMMPct/mmPct*0.95, lot)
	if keep > qty-lot {
		keep = qty - lot
	}
	return math.Max(0, keep)
}
