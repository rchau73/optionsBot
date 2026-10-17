// Package replay runs the bot's pure rules over the recorded mainnet option
// book (internal/recorder, data/market/) hour by hour, so two rule sets can
// be compared on real history: cmd/stress -replay. Prices, spreads, greeks,
// open interest (for GEX) and DVOL are the recording's; what cannot be
// recorded is modelled and labelled MODEL — margin (a scenario scan, not
// Deribit's simulator) and whether a resting order fills (it fills at its
// price floor when the book has that side). Research only: it never connects
// to Deribit and places no orders.
package replay

import (
	"math"

	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
)

// FeePerContract is Deribit's option fee per contract, in the coin (0.03 % of
// the underlying), capped by orders.OptionFee at 12.5 % of the price.
const FeePerContract = 0.0003

// Fill is the price an order gets on a recorded quote; ok is false when the
// side it needs is empty (the order would not fill this hour).
type Fill struct {
	Price float64
	OK    bool
}

// SellFill is a short sale resting until the bot's price floor: "bid" (a
// repair crosses to the bid) or "mid" (entries and top-ups step down from the
// ask to the mid). Nobody buys from an empty bid side, so no bid → no fill.
func SellFill(q recorder.Quote, floor string) Fill {
	if q.Bid <= 0 {
		return Fill{}
	}
	if floor == "bid" || q.Ask <= 0 {
		return Fill{Price: q.Bid, OK: true}
	}
	return Fill{Price: (q.Bid + q.Ask) / 2, OK: true}
}

// BuyFill is a buy-back at market or IOC at the ask (stops, delta exits, GEX
// sheds, rolls, the MM cut): the ask, or no fill when nobody is offering.
func BuyFill(q recorder.Quote) Fill {
	if q.Ask <= 0 {
		return Fill{}
	}
	return Fill{Price: q.Ask, OK: true}
}

// Fee is Deribit's fee for qty contracts traded at price.
func Fee(price, qty float64) float64 { return orders.OptionFee(FeePerContract, price, qty) }

// MODEL: portfolio margin. Deribit's portfolio margin is the worst loss over
// a grid of underlying moves and volatility shocks; the scan below uses spot
// ±16 % and IV −20 % / +30 %, each leg repriced with Black–Scholes on its own
// recorded IV (inverse option: price in the coin). MM is MMRatio of IM.
var (
	spotShocks = []float64{0.84, 0.88, 0.92, 0.96, 1, 1.04, 1.08, 1.12, 1.16}
	volShocks  = []float64{0.8, 1, 1.3}
)

// MMRatio is the maintenance margin as a share of the initial margin (MODEL).
const MMRatio = 0.8

// marginLeg is what the margin scan needs of a short leg.
type marginLeg struct {
	optType     string
	strike, qty float64
	years, iv   float64
}

// scanIM is the worst loss of the short legs over the shock grid, in the coin.
func scanIM(legs []marginLeg, spot float64) float64 {
	worst := 0.0
	for _, sm := range spotShocks {
		for _, vm := range volShocks {
			loss := 0.0
			for _, l := range legs {
				p0 := inversePrice(l.optType, spot, l.strike, l.years, l.iv)
				p1 := inversePrice(l.optType, spot*sm, l.strike, l.years, l.iv*vm)
				loss += l.qty * (p1 - p0) // a short owes more coin as its price rises

			}
			worst = math.Max(worst, loss)
		}
	}
	return worst
}

// inversePrice is the Black–Scholes price of an option in the coin (USD
// price ÷ spot), zero rate; at or past expiry, the intrinsic value.
func inversePrice(optType string, s, k, t, sigma float64) float64 {
	if s <= 0 || k <= 0 {
		return 0
	}
	if t <= 0 || sigma <= 0 {
		if optType == "call" {
			return math.Max(s-k, 0) / s
		}
		return math.Max(k-s, 0) / s
	}
	sq := sigma * math.Sqrt(t)
	d1 := (math.Log(s/k) + 0.5*sigma*sigma*t) / sq
	d2 := d1 - sq
	n := func(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }
	if optType == "call" {
		return (s*n(d1) - k*n(d2)) / s
	}
	return (k*n(-d2) - s*n(-d1)) / s
}
