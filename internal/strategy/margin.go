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

// SlotShare is the initial margin one slot may use: the IM limit (as % of
// margin balance) split equally between all configured slots. Entries and
// the rebalance both size a strangle to it, so a strangle opened into an
// otherwise full book is not later cut back.
func SlotShare(limitIMPct, marginBalance float64, slots int) float64 {
	if slots <= 0 {
		return 0
	}
	return limitIMPct / 100 * marginBalance / float64(slots)
}

// EntryShare is the IM a vacant slot may use now: its slot share, but never
// more than an equal part of the remaining headroom. Without the cap, one
// vacant slot in a full book would take all the headroom (e.g. 12 % of
// balance instead of 20 % ÷ 3 slots ≈ 6.7 %).
func EntryShare(headroom, slotShare float64, vacant int) float64 {
	if vacant <= 0 || headroom <= 0 {
		return 0
	}
	return min(headroom/float64(vacant), slotShare)
}

// CapLots limits lots to multiple × normalLots, the size the slot would get
// if the strangle stood alone. Portfolio margin can price a new leg almost
// free when it offsets the book (a short call against short puts after a
// drop), and dividing the slot's share by that tiny cost gives a huge size.
// The offset holds only while the rest of the book does: once the puts are
// closed the calls stand alone. Returns the lots and whether the cap bound.
// normalLots ≤ 0 (unknown) or multiple ≤ 0 means no cap.
func CapLots(lots, normalLots int, multiple float64) (int, bool) {
	if normalLots <= 0 || multiple <= 0 {
		return lots, false
	}
	maxLots := max(1, int(math.Floor(multiple*float64(normalLots)+1e-9)))
	if lots > maxLots {
		return maxLots, true
	}
	return lots, false
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
