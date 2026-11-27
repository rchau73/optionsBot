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
	"path/filepath"
	"strings"

	"optionsbot/internal/config"

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

// LinearFee is Deribit's fee for qty units of a USDC-settled option (SOL,
// XRP, #837) traded at price with the index at spot, in USDC.
func LinearFee(spot, price, qty float64) float64 { return orders.LinearOptionFee(spot, price, qty) }

// linearLots are the contract steps of Deribit's USDC-settled options the
// replay trades (#832: whole contracts of 10 SOL, 1,000 XRP).
var linearLots = map[string]float64{"SOL": 10, "XRP": 1000}

// DefaultGEXDir is the minute GEX records a replay of booksDir reads when
// -gex is not given: market_gex beside the books, which the recorder writes
// for the BTC book only — so "none" (hourly GEX rebuilt from the coin's own
// book) for a linear coin, whose spot, regime and flip are not BTC's (#837).
func DefaultGEXDir(booksDir, underlying string) string {
	if config.IsLinear(underlying) {
		return "none"
	}
	return filepath.Join(filepath.Dir(filepath.Clean(booksDir)), "market_gex")
}

// LotFor is the replay's order step for underlying: Deribit's minimum for an
// inverse option (0.1 BTC, 1 ETH), the contract for a linear one.
func LotFor(underlying string) float64 {
	u := strings.ToUpper(underlying)
	if lot, ok := linearLots[u]; ok {
		return lot
	}
	if u == "BTC" {
		return 0.1
	}
	return 1
}

// MODEL: portfolio margin. Deribit's portfolio margin is the worst loss over
// a grid of underlying moves and volatility shocks; the scan below uses spot
// ±16 % and IV −20 % / +30 %, each leg repriced with Black–Scholes on its own
// recorded IV (inverse option: price in the coin; linear, #837: in USDC).
// MM is MMRatio of IM.
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

// scanIM is the worst loss of the short legs over the shock grid, in the
// settlement currency (the coin; USDC when linear).
func scanIM(linear bool, legs []marginLeg, spot float64) float64 {
	return scanPortfolioIM(linear, legs, 0, spot)
}

// scanPortfolioIM is scanIM with a perpetual of perpUSD (USD notional,
// negative = short) in the book (the shed study's hedge, #637): inverse, or
// the linear USDC perpetual when linear.
func scanPortfolioIM(linear bool, legs []marginLeg, perpUSD, spot float64) float64 {
	worst := 0.0
	for _, sm := range spotShocks {
		for _, vm := range volShocks {
			worst = math.Max(worst, shockLoss(linear, legs, perpUSD, spot, sm, vm))
		}
	}
	return worst
}

// shockLoss is the book's loss in the settlement currency when spot moves ×
// sm and every IV × vm.
func shockLoss(linear bool, legs []marginLeg, perpUSD, spot, sm, vm float64) float64 {
	loss := -perpPnL(linear, perpUSD, spot, spot*sm)
	for _, l := range legs {
		p0 := optionPrice(linear, l.optType, spot, l.strike, l.years, l.iv)
		p1 := optionPrice(linear, l.optType, spot*sm, l.strike, l.years, l.iv*vm)
		loss += l.qty * (p1 - p0) // a short owes more as its price rises
	}
	return loss
}

// perpPnL is what a perpetual of usd (USD notional, negative = short) earns
// when its price moves from p0 to p1: in the coin for the inverse one, in
// USDC for the linear one (usd ÷ p0 coins × the move).
func perpPnL(linear bool, usd, p0, p1 float64) float64 {
	if p0 <= 0 || p1 <= 0 {
		return 0
	}
	if linear {
		return usd / p0 * (p1 - p0)
	}
	return usd * (1/p0 - 1/p1)
}

// The study's modelled gap (#637): spot falls 20 % at once and IV rises as
// the margin scan's worst volatility shock.
const (
	GapMove       = 0.80
	GapIVMultiple = 1.3
)

// gapLoss is the held book's loss in the coin if spot gapped × move with IV ×
// ivMult (no exit fills inside the gap).
func gapLoss(linear bool, legs []marginLeg, perpUSD, spot, move, ivMult float64) float64 {
	return math.Max(shockLoss(linear, legs, perpUSD, spot, move, ivMult), 0)
}

// Widen is q with its spread m times as wide around the same mid (the
// study's cost sensitivity); a bid stays above zero (never under bid ÷ m), so
// a side that could fill still can. m ≤ 1 or a one-sided book: q as recorded.
func Widen(q recorder.Quote, m float64) recorder.Quote {
	if m <= 1 || q.Bid <= 0 || q.Ask <= 0 {
		return q
	}
	mid := (q.Bid + q.Ask) / 2
	q.Bid = math.Max(mid-m*(mid-q.Bid), q.Bid/m)
	q.Ask = mid + m*(q.Ask-mid)
	return q
}

// optionPrice is the option's model price in its settlement currency: the
// inverse price in the coin, or ×spot (the USD price) for a linear option.
func optionPrice(linear bool, optType string, s, k, t, sigma float64) float64 {
	p := inversePrice(optType, s, k, t, sigma)
	if linear {
		return p * s
	}
	return p
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
