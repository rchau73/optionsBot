package marketdata

import (
	"math"
	"sort"
	"time"
)

// ChainIVSource names the volatility reading of a coin Deribit publishes no
// volatility index for (SOL, XRP: #834). It stands in for DVOL wherever DVOL
// feeds a rule — the flip buffer, the worst-case model, the IV percentile of
// the margin bands and their daily closes — and is journaled under this name.
const ChainIVSource = "chain ATM IV 30d"

// DVOLSource names Deribit's volatility index, BTC's and ETH's reading.
const DVOLSource = "DVOL"

// ChainIVTargetDays is the maturity the chain IV is interpolated to, as DVOL's.
const ChainIVTargetDays = 30

// HasVolIndex reports whether Deribit publishes a volatility index for
// underlying: BTC and ETH have DVOL, the linear coins (SOL, XRP) have none.
func HasVolIndex(underlying string) bool { return !IsLinear(underlying) }

// VolSource is the name of underlying's volatility reading, chosen once by
// the coin: DVOLSource or ChainIVSource.
func VolSource(underlying string) string {
	if HasVolIndex(underlying) {
		return DVOLSource
	}
	return ChainIVSource
}

// atmPoint is one expiry's at-the-money implied volatility.
type atmPoint struct {
	years float64 // time to settlement
	iv    float64 // fraction
}

// ChainATMIV30 is the chain's 30-day at-the-money implied volatility, in
// percent like DVOL: for each expiry the mean mark IV of the call and put of
// the strike nearest spot (both quoted), then interpolated in total variance
// (σ² × T) to 30 days between the expiries on either side of it. With
// expiries on one side only it is the nearest one's own ATM IV: nothing is
// extrapolated. ok is false without spot or without one expiry to read.
// Pure: now decides each expiry's time left.
func ChainATMIV30(chain []*Instrument, spot float64, now time.Time) (iv float64, ok bool) {
	if spot <= 0 {
		return 0, false
	}
	points := atmPoints(chain, spot, now)
	if len(points) == 0 {
		return 0, false
	}
	target := float64(ChainIVTargetDays) / 365
	below, above := -1, -1
	for i, p := range points { // sorted by time left
		if p.years <= target {
			below = i
		} else if above < 0 {
			above = i
		}
	}
	switch {
	case below < 0:
		return points[above].iv * 100, true
	case above < 0 || points[below].years == target:
		return points[below].iv * 100, true
	}
	lo, hi := points[below], points[above]
	wLo, wHi := lo.iv*lo.iv*lo.years, hi.iv*hi.iv*hi.years
	w := wLo + (wHi-wLo)*(target-lo.years)/(hi.years-lo.years)
	if w <= 0 {
		return 0, false
	}
	return math.Sqrt(w/target) * 100, true
}

// atmPoints is each unexpired expiry's ATM IV (the strike nearest spot with
// both a call and a put quoted with a mark IV), sorted by time left.
func atmPoints(chain []*Instrument, spot float64, now time.Time) []atmPoint {
	type pair struct{ call, put float64 }
	byExpiry := map[time.Time]map[float64]*pair{}
	for _, inst := range chain {
		if inst == nil || !inst.HasQuote() || inst.Greeks.IV <= 0 || !inst.Expiry.After(now) {
			continue
		}
		strikes := byExpiry[inst.Expiry]
		if strikes == nil {
			strikes = map[float64]*pair{}
			byExpiry[inst.Expiry] = strikes
		}
		p := strikes[inst.Strike]
		if p == nil {
			p = &pair{}
			strikes[inst.Strike] = p
		}
		switch inst.OptionType {
		case "call":
			p.call = inst.Greeks.IV
		case "put":
			p.put = inst.Greeks.IV
		}
	}
	var out []atmPoint
	for expiry, strikes := range byExpiry {
		best, bestDist := 0.0, math.Inf(1)
		for k, p := range strikes {
			if p.call <= 0 || p.put <= 0 {
				continue
			}
			// The lower strike wins a tie, so the pick never depends on map order.
			if d := math.Abs(k - spot); d < bestDist || (d == bestDist && k < best) {
				best, bestDist = k, d
			}
		}
		if math.IsInf(bestDist, 1) {
			continue
		}
		p := strikes[best]
		out = append(out, atmPoint{years: expiry.Sub(now).Hours() / 24 / 365, iv: (p.call + p.put) / 2})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].years < out[j].years })
	return out
}
