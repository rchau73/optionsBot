package strategy

import (
	"math"
	"time"

	"optionsbot/internal/marketdata"
)

// Why a strike is not picked although its delta fits (#553).
const (
	StrikeNoQuote  = "no two-sided quote"
	StrikeOffSmile = "mark IV off the smile"
)

// StrikeQualityReason says why inst may not be sold ("" = it may): it needs
// a two-sided quote (bid and ask), and, when both IVs are known (> 0) and
// ivMaxRatio > 0, a mark IV within [atmIV ÷ ivMaxRatio, atmIV × ivMaxRatio].
// A strike without a market has only a mark, and a mark far off the
// at-the-money IV gives it a delta it does not have
// (2026-10-30 BTC: 118000-C, bid 0 ask 0, mark IV 88 % against 38 % ATM,
// Δ 0.152 — a repair sent there 66 times without a fill).
func StrikeQualityReason(inst *marketdata.Instrument, atmIV, ivMaxRatio float64) string {
	if inst.Bid <= 0 || inst.Ask <= 0 {
		return StrikeNoQuote
	}
	iv := inst.Greeks.IV
	if ivMaxRatio > 0 && atmIV > 0 && iv > 0 && (iv > atmIV*ivMaxRatio || iv < atmIV/ivMaxRatio) {
		return StrikeOffSmile
	}
	return ""
}

// ATMIV is the mark IV of expiry's option whose |delta| is nearest 0.5, either
// type (0 when no option of that expiry has an IV).
func ATMIV(instruments []*marketdata.Instrument, expiry time.Time) float64 {
	iv, best := 0.0, math.Inf(1)
	for _, inst := range instruments {
		if !inst.Expiry.Equal(expiry) || inst.Greeks.IV <= 0 {
			continue
		}
		if d := math.Abs(math.Abs(inst.Greeks.Delta) - 0.5); d < best {
			iv, best = inst.Greeks.IV, d
		}
	}
	return iv
}
