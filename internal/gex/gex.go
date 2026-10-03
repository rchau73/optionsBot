// Package gex computes the Gamma Exposure (GEX) profile across the full options
// chain. Unlike the simple net-portfolio-gamma check, GEX uses market-wide open
// interest to determine whether market makers are net long or net short gamma.
//
// Positive GEX (MMs long gamma): they sell rallies and buy dips → mean-reverting
// environment → strangle selling is safe.
//
// Negative GEX (MMs short gamma): they chase the trend → accelerating moves →
// strangle sellers should shed the leg in the direction of the trend.
package gex

import (
	"math"
	"sort"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

// InstrumentGEXInput is all the data needed to compute one instrument's GEX
// contribution. Sourced from public/get_book_summary_by_currency + instrument chain.
type InstrumentGEXInput struct {
	Instrument   string
	Strike       float64
	Expiry       time.Time
	OptionType   string // "call" or "put"
	Spot         float64
	OpenInterest float64
	MarkIV       float64 // annualised, fractional (e.g. 0.75 for 75%)
}

// StrikeGEX holds the aggregated GEX contribution for one strike price.
type StrikeGEX struct {
	Strike    float64
	CallOI    float64
	PutOI     float64
	GEXCall   float64 // Σ BSGamma × OI × spot² for calls at this strike
	GEXPut    float64 // same for puts
	GEXSigned float64 // GEXCall − GEXPut (positive = call-dominant)
}

// ExpiryProfile is the per-expiry intermediate result before consolidation.
type ExpiryProfile struct {
	Expiry      time.Time
	Label       string
	TotalOI     float64
	BaseWeight  float64 // OI fraction of total chain OI
	TimeWeight  float64 // weekday / month-end / quarter-end multiplier
	FinalWeight float64 // BaseWeight × TimeWeight
	Strikes     []StrikeGEX
}

// Snapshot is the fully consolidated GEX result the strategy queries.
type Snapshot struct {
	ComputedAt     time.Time
	Spot           float64
	RegimeScore    float64 // Σ weighted_signed_gex — positive=pinning, negative=acceleration
	Regime         string  // "POSITIVE/PINNING" | "NEGATIVE/ACCELERATION" | "NEUTRAL"
	GammaFlip      float64 // strike where weighted_signed_gex crosses zero; 0 if not found
	GammaFlipFound bool
	// Weighted per-strike profile (sorted ascending by Strike)
	Strikes []WeightedStrike
	// Derived key levels
	AggCallWall float64 // strike with highest weighted call dominance
	AggPutWall  float64 // strike with highest weighted put dominance
}

// WeightedStrike is one row in the consolidated, cross-expiry GEX profile.
type WeightedStrike struct {
	Strike         float64
	WeightedCallOI float64
	WeightedPutOI  float64
	WeightedGEX    float64 // consolidated signed GEX for this strike
}

// ── Black-Scholes Gamma ───────────────────────────────────────────────────────

func normPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

// BSGamma computes the Black-Scholes gamma for a European option.
// Returns 0 for degenerate inputs (expired, zero vol, etc.).
func BSGamma(spot, strike, tYears, vol float64) float64 {
	if spot <= 0 || strike <= 0 || tYears <= 0 || vol <= 0 {
		return 0
	}
	sigmaSqrtT := vol * math.Sqrt(tYears)
	d1 := (math.Log(spot/strike) + 0.5*vol*vol*tYears) / sigmaSqrtT
	return normPDF(d1) / (spot * sigmaSqrtT)
}

// ── Per-expiry GEX computation ────────────────────────────────────────────────

// ComputeExpiryGEX calculates the signed GEX profile for one expiry's instruments.
// It mirrors Python's compute_gex_proxy: gamma × OI × spot².
func ComputeExpiryGEX(instruments []InstrumentGEXInput) []StrikeGEX {
	now := time.Now()

	// Accumulate per strike
	type acc struct {
		callOI, putOI, gexCall, gexPut float64
	}
	byStrike := make(map[float64]*acc)

	for _, inst := range instruments {
		tYears := inst.Expiry.Sub(now).Hours() / 24 / 365
		if tYears < 1e-9 {
			tYears = 1e-9
		}
		gamma := BSGamma(inst.Spot, inst.Strike, tYears, inst.MarkIV)
		gex := gamma * inst.OpenInterest * inst.Spot * inst.Spot

		a, ok := byStrike[inst.Strike]
		if !ok {
			a = &acc{}
			byStrike[inst.Strike] = a
		}
		if inst.OptionType == "call" {
			a.callOI += inst.OpenInterest
			a.gexCall += gex
		} else {
			a.putOI += inst.OpenInterest
			a.gexPut += gex
		}
	}

	strikes := make([]StrikeGEX, 0, len(byStrike))
	for k, a := range byStrike {
		strikes = append(strikes, StrikeGEX{
			Strike:    k,
			CallOI:    a.callOI,
			PutOI:     a.putOI,
			GEXCall:   a.gexCall,
			GEXPut:    a.gexPut,
			GEXSigned: a.gexCall - a.gexPut,
		})
	}
	sort.Slice(strikes, func(i, j int) bool { return strikes[i].Strike < strikes[j].Strike })
	return strikes
}

// ── Expiry weighting (mirrors Python's get_expiration_weight) ─────────────────

// ExpiryWeight returns a 0–1 multiplier for how much to weight an expiry.
// Month-end and quarter-end always return 1.0 (max weight).
// Weekday weights: Mon=0.5, Tue=0.6, Wed=0.7, Thu=0.8, Fri=1.0, Sat=0.9, Sun=0.85
func ExpiryWeight(expiry time.Time) float64 {
	next := expiry.AddDate(0, 0, 1)
	isMonthEnd := next.Month() != expiry.Month()
	isQuarterEnd := isMonthEnd && (expiry.Month() == 3 || expiry.Month() == 6 ||
		expiry.Month() == 9 || expiry.Month() == 12)
	if isMonthEnd || isQuarterEnd {
		return 1.0
	}
	// Go: Sunday=0, Monday=1, ..., Saturday=6
	// Python: Monday=0 … Sunday=6 → [0.5, 0.6, 0.7, 0.8, 1.0, 0.9, 0.85]
	byDow := [7]float64{0.85, 0.5, 0.6, 0.7, 0.8, 1.0, 0.9} // indexed by Go Weekday
	return byDow[expiry.Weekday()]
}

// ── Cross-expiry consolidation ────────────────────────────────────────────────

// ConsolidateProfiles aggregates per-expiry GEX profiles into a single
// cross-expiry view weighted by OI proportion × expiry time-weight.
// Mirrors Python's consolidate_levels.
func ConsolidateProfiles(profiles []ExpiryProfile) []WeightedStrike {
	// Compute final weights
	totalOI := 0.0
	for i := range profiles {
		totalOI += profiles[i].TotalOI
	}
	for i := range profiles {
		if totalOI > 0 {
			profiles[i].BaseWeight = profiles[i].TotalOI / totalOI
		}
		profiles[i].FinalWeight = profiles[i].BaseWeight * profiles[i].TimeWeight
	}

	// Accumulate weighted values per strike
	type acc struct {
		callOI, putOI, gex float64
	}
	byStrike := make(map[float64]*acc)

	for _, p := range profiles {
		w := p.FinalWeight
		for _, s := range p.Strikes {
			a, ok := byStrike[s.Strike]
			if !ok {
				a = &acc{}
				byStrike[s.Strike] = a
			}
			a.callOI += s.CallOI * w
			a.putOI += s.PutOI * w
			a.gex += s.GEXSigned * w
		}
	}

	out := make([]WeightedStrike, 0, len(byStrike))
	for k, a := range byStrike {
		out = append(out, WeightedStrike{
			Strike:         k,
			WeightedCallOI: a.callOI,
			WeightedPutOI:  a.putOI,
			WeightedGEX:    a.gex,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Strike < out[j].Strike })
	return out
}

// ── Gamma flip (mirrors Python's find_gamma_flip) ─────────────────────────────

// FindGammaFlip finds the strike price where weighted_signed_gex crosses zero,
// interpolated linearly between the two bracketing strikes, returning the
// crossing nearest to spot. The full options chain can have multiple crossings
// (e.g. one at 30k and one at 64k); the one closest to current price is the
// operationally meaningful gamma flip level.
// Returns (0, false) if no crossing is found.
func FindGammaFlip(strikes []WeightedStrike, spot float64) (float64, bool) {
	if len(strikes) < 2 {
		return 0, false
	}
	bestFlip := 0.0
	bestDist := math.Inf(1)
	found := false

	for i := 1; i < len(strikes); i++ {
		prev := strikes[i-1]
		curr := strikes[i]

		var flip float64
		var hasCrossing bool
		switch {
		case prev.WeightedGEX == 0:
			flip, hasCrossing = prev.Strike, true
		case curr.WeightedGEX == 0:
			flip, hasCrossing = curr.Strike, true
		case (prev.WeightedGEX < 0 && curr.WeightedGEX > 0) ||
			(prev.WeightedGEX > 0 && curr.WeightedGEX < 0):
			ratio := math.Abs(prev.WeightedGEX) / (math.Abs(prev.WeightedGEX) + math.Abs(curr.WeightedGEX))
			flip = prev.Strike + (curr.Strike-prev.Strike)*ratio
			hasCrossing = true
		}

		if hasCrossing {
			dist := math.Abs(flip - spot)
			if !found || dist < bestDist {
				bestFlip = flip
				bestDist = dist
				found = true
			}
		}
	}
	return bestFlip, found
}

// ── Regime and key levels ─────────────────────────────────────────────────────

// FilterByStrikeRange returns only the instruments whose strike falls within
// rangePct of spot on either side. rangePct=0 or spot≤0 disables the filter.
//
// Example: spot=64000, rangePct=0.25 → keep strikes in [48000, 80000].
//
// Note: BSGamma already decays naturally for deep-OTM strikes, but very large
// OI at far-OTM puts can still bias the aggregate signed GEX score. This filter
// removes that structural contamination before computing the gamma flip.
func FilterByStrikeRange(instruments []InstrumentGEXInput, spot, rangePct float64) []InstrumentGEXInput {
	if rangePct <= 0 || spot <= 0 {
		return instruments
	}
	lower := spot * (1 - rangePct)
	upper := spot * (1 + rangePct)
	out := make([]InstrumentGEXInput, 0, len(instruments))
	for _, inst := range instruments {
		if inst.Strike >= lower && inst.Strike <= upper {
			out = append(out, inst)
		}
	}
	return out
}

// ApplyRegimeHysteresis prevents rapid regime flipping when spot oscillates
// within a tolerance band around the gamma flip level. It implements Schmitt-
// trigger hysteresis: once in a regime, spot must break clearly past the flip
// by bandPct before the regime switches.
//
//   - From POSITIVE/PINNING: switches to NEGATIVE only when spot < flip*(1-bandPct)
//   - From NEGATIVE/ACCELERATION: switches to POSITIVE only when spot > flip*(1+bandPct)
//
// Returns (adjusted regime, true if hysteresis overrode the raw regime).
// Passthrough (no override) when: lastRegime is empty (first call), bandPct≤0,
// or flip≤0 (no gamma flip found).
func ApplyRegimeHysteresis(rawRegime, lastRegime string, spot, flip, bandPct float64) (string, bool) {
	if lastRegime == "" || bandPct <= 0 || flip <= 0 {
		return rawRegime, false
	}
	var adjusted string
	switch lastRegime {
	case "POSITIVE/PINNING":
		if spot < flip*(1-bandPct) {
			adjusted = "NEGATIVE/ACCELERATION"
		} else {
			adjusted = "POSITIVE/PINNING"
		}
	case "NEGATIVE/ACCELERATION":
		if spot > flip*(1+bandPct) {
			adjusted = "POSITIVE/PINNING"
		} else {
			adjusted = "NEGATIVE/ACCELERATION"
		}
	default:
		return rawRegime, false
	}
	return adjusted, adjusted != rawRegime
}

// RegimeLabel classifies the gamma regime based on the sign of the total signed GEX.
func RegimeLabel(score float64) string {
	switch {
	case score > 0:
		return "POSITIVE/PINNING"
	case score < 0:
		return "NEGATIVE/ACCELERATION"
	default:
		return "NEUTRAL"
	}
}

// BuildSnapshot assembles a complete GEX snapshot from the consolidated
// strike profile. This is what the strategy queries.
func BuildSnapshot(consolidated []WeightedStrike, spot float64) Snapshot {
	score := 0.0
	for _, s := range consolidated {
		score += s.WeightedGEX
	}

	flip, flipFound := FindGammaFlip(consolidated, spot)

	// When the gamma flip is found, use spot vs flip to classify the regime —
	// this reflects the local GEX sign near current price. The aggregate score
	// is kept for logging/magnitude but is structurally biased negative in BTC
	// due to persistent put-skew across the full chain.
	regime := RegimeLabel(score)
	if flipFound {
		if spot > flip {
			regime = "POSITIVE/PINNING"
		} else {
			regime = "NEGATIVE/ACCELERATION"
		}
	}

	// Aggregate call wall: highest call dominance (call_oi - put_oi) above spot
	// Aggregate put wall: highest put dominance below spot
	aggCallWall, aggPutWall := 0.0, 0.0
	bestCallDom, bestPutDom := math.Inf(-1), math.Inf(-1)
	for _, s := range consolidated {
		dom := s.WeightedCallOI - s.WeightedPutOI
		if dom > bestCallDom {
			bestCallDom = dom
			aggCallWall = s.Strike
		}
		putDom := s.WeightedPutOI - s.WeightedCallOI
		if putDom > bestPutDom {
			bestPutDom = putDom
			aggPutWall = s.Strike
		}
	}

	return Snapshot{
		ComputedAt:     time.Now(),
		Spot:           spot,
		RegimeScore:    score,
		Regime:         regime,
		GammaFlip:      flip,
		GammaFlipFound: flipFound,
		Strikes:        consolidated,
		AggCallWall:    aggCallWall,
		AggPutWall:     aggPutWall,
	}
}
