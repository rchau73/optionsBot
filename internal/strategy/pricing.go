package strategy

import (
	"math"
	"time"

	"optionsbot/internal/orders"
)

// Price floors: the lowest price a resting short sell may step down to
// before order_fill_timeout_sec cancels it (config entry_price_floor and
// repair_price_floor).
const (
	PriceFloorAsk = "ask" // never below the ask: wait for a buyer
	PriceFloorMid = "mid" // down to mid, rounded up to a tick
	PriceFloorBid = "bid" // down to the bid: crosses the spread, fills at once
)

// StepDownPrice is the limit for a resting short sell that has waited age of
// its timeout. The first third it offers at the ask (nothing given up if a
// buyer comes); the second third at mid, rounded up to a tick; the last third
// at the floor. It never goes below the floor.
func StepDownPrice(bid, ask, tick float64, age, timeout time.Duration, floor string) float64 {
	switch {
	case age < timeout/3:
		return FloorPrice(bid, ask, tick, PriceFloorAsk)
	case age < timeout*2/3 && floor == PriceFloorBid:
		return FloorPrice(bid, ask, tick, PriceFloorMid)
	}
	return FloorPrice(bid, ask, tick, floor)
}

// FloorPrice is the lowest price a sell steps down to for this quote: the
// premium floor is checked against it, not the ask.
//
// Without a bid (testnet often quotes none) there is no market to step into —
// mid would be half the ask — so the price stays at the ask. Returns 0 when
// there is no ask.
func FloorPrice(bid, ask, tick float64, floor string) float64 {
	if ask <= 0 {
		return 0
	}
	if bid <= 0 || bid >= ask {
		return ask
	}
	switch floor {
	case PriceFloorBid:
		return bid
	case PriceFloorMid:
		return math.Min(orders.CeilToStep((bid+ask)/2, tick), ask)
	}
	return ask
}
