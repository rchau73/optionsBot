package api

import (
	"math"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The "How it works" page (#322) explains a short strangle with the bot's own
// nearest strangle: its strikes, premiums and expiry, where the position
// breaks even at expiry and where each leg's delta exit would fire. Served in
// /api/summary as `strangle` (null when no strangle holds both legs: the page
// then shows a labelled example).

// ExitLineModel names the model behind the exit-line prices.
const ExitLineModel = "model: Black–Scholes delta on the leg's IV (DVOL when missing), today, zero rate"

// HowItWorks is the nearest two-legged strangle, explained.
type HowItWorks struct {
	StrangleID string       `json:"strangle_id"`
	Expiry     time.Time    `json:"expiry"`
	Spot       float64      `json:"spot"` // USD
	Put        ExplainedLeg `json:"put"`
	Call       ExplainedLeg `json:"call"`
	// PremiumCoin is what both legs were paid, in coin: the most the
	// strangle can earn.
	PremiumCoin float64 `json:"premium_coin"`
	// BreakEvenLow/High: the coin's price at expiry where the strangle's
	// result is zero (coin-settled payoff). High is null when the call's
	// size cannot lose the whole payment (qty ≤ premium).
	BreakEvenLow  *float64 `json:"break_even_low"`
	BreakEvenHigh *float64 `json:"break_even_high"`
	// ExitLines: each leg's ExitLine is set; otherwise ExitLinesReason says why.
	ExitLines       bool   `json:"exit_lines"`
	ExitLinesReason string `json:"exit_lines_reason,omitempty"`
	ExitLinesModel  string `json:"exit_lines_model"`
}

// ExplainedLeg is one leg of the explained strangle.
type ExplainedLeg struct {
	Instrument  string  `json:"instrument"`
	Strike      float64 `json:"strike"`       // USD
	Qty         float64 `json:"qty"`          // contracts (one coin each)
	PremiumCoin float64 `json:"premium_coin"` // paid when it was sold
	// ExitLine is the coin's price at which this leg's |Δ| reaches
	// delta_exit_threshold today (null when not computable).
	ExitLine *float64 `json:"exit_line"`
}

// HowItWorksOf picks the short strangle with both legs and the nearest expiry
// (the first in view order on a tie) and explains it; nil when there is none.
func HowItWorksOf(strangles []strategy.StrangleView, spot, dvol, deltaExit float64, now time.Time) *HowItWorks {
	var best *HowItWorks
	for _, st := range strangles {
		put, call, ok := shortPair(st.Legs)
		if !ok || (best != nil && !call.Expiry.Before(best.Expiry)) {
			continue
		}
		hw := &HowItWorks{
			StrangleID: st.ID, Expiry: call.Expiry, Spot: spot, ExitLinesModel: ExitLineModel,
			Put:  ExplainedLeg{Instrument: put.Instrument, Strike: put.Strike, Qty: put.Qty, PremiumCoin: put.PremiumReceived},
			Call: ExplainedLeg{Instrument: call.Instrument, Strike: call.Strike, Qty: call.Qty, PremiumCoin: call.PremiumReceived},
		}
		hw.PremiumCoin = put.PremiumReceived + call.PremiumReceived
		hw.BreakEvenLow, hw.BreakEvenHigh = BreakEvens(put.Strike, put.Qty, call.Strike, call.Qty, hw.PremiumCoin)
		hw.setExitLines(put, call, dvol, deltaExit, now)
		best = hw
	}
	return best
}

// shortPair returns the strangle's short put and short call, both on one expiry.
func shortPair(legs []strategy.LegView) (put, call strategy.LegView, ok bool) {
	var hasPut, hasCall bool
	for _, l := range legs {
		if l.Side == orders.DirectionBuy || l.Qty <= 0 || l.Strike <= 0 {
			continue
		}
		switch l.OptionType {
		case "put":
			put, hasPut = l, true
		case "call":
			call, hasCall = l, true
		}
	}
	return put, call, hasPut && hasCall && put.Expiry.Equal(call.Expiry) && !call.Expiry.IsZero()
}

func (hw *HowItWorks) setExitLines(put, call strategy.LegView, dvol, deltaExit float64, now time.Time) {
	years := call.Expiry.Sub(now).Hours() / 24 / 365
	ivOf := func(l strategy.LegView) float64 {
		if l.Greeks.IV > 0 {
			return l.Greeks.IV
		}
		return dvol / 100
	}
	switch {
	case deltaExit <= 0 || deltaExit >= 1:
		hw.ExitLinesReason = "no delta exit threshold"
		return
	case years <= 0:
		hw.ExitLinesReason = "the strangle is at expiry"
		return
	case ivOf(put) <= 0 || ivOf(call) <= 0:
		hw.ExitLinesReason = "no implied volatility"
		return
	}
	p := ExitLinePrice("put", put.Strike, years, ivOf(put), deltaExit)
	c := ExitLinePrice("call", call.Strike, years, ivOf(call), deltaExit)
	hw.Put.ExitLine, hw.Call.ExitLine, hw.ExitLines = &p, &c, true
}

// ExitLinePrice is the coin's price at which an option's Black–Scholes |Δ|
// (zero rate) equals threshold: S = K·exp(d1·σ√t − ½σ²t), with
// d1 = Φ⁻¹(threshold) for a call and Φ⁻¹(1 − threshold) for a put.
// Callers pass 0 < threshold < 1, years > 0 and sigma > 0.
func ExitLinePrice(optType string, strike, years, sigma, threshold float64) float64 {
	p := threshold
	if optType == "put" {
		p = 1 - threshold // Δput = N(d1) − 1
	}
	d1 := math.Sqrt2 * math.Erfinv(2*p-1) // Φ⁻¹(p)
	sq := sigma * math.Sqrt(years)
	return strike * math.Exp(d1*sq-0.5*sigma*sigma*years)
}

// BreakEvens are the prices at expiry where a coin-settled short strangle
// that was paid premium (coin) neither earns nor loses: a put pays
// qty·(K − S)/S coin, a call qty·(S − K)/S. High is nil when the call can
// never cost the whole payment; low is nil without a put.
func BreakEvens(putStrike, putQty, callStrike, callQty, premium float64) (low, high *float64) {
	if putQty > 0 && premium >= 0 {
		v := putQty * putStrike / (premium + putQty)
		low = &v
	}
	if callQty > premium && premium >= 0 {
		v := callQty * callStrike / (callQty - premium)
		high = &v
	}
	return low, high
}
