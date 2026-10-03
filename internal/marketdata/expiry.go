package marketdata

import (
	"math"
	"time"
)

// Expiry selection lives here so the market-data subscriber and the strategy
// use the same rule: the expiry the strategy will trade is always one the
// manager has subscribed to.

// DaysToExpiry returns whole calendar days from now to expiry, rounded.
func DaysToExpiry(expiry, now time.Time) int {
	return int(math.Round(expiry.Sub(now).Hours() / 24))
}

// ExpiryWindow returns the inclusive DTE range an entry for targetDTE may use:
// targetDTE ± maxDeviation, but never at or below rolloutDTE — a position
// opened there would be rolled on the very next cycle.
func ExpiryWindow(targetDTE, maxDeviation, rolloutDTE int) (lo, hi int) {
	return max(targetDTE-maxDeviation, rolloutDTE+1), targetDTE + maxDeviation
}

// NearestExpiry returns the expiry with the fewest days to expiry within
// [lo, hi], ignoring expiries in skip (which may be nil).
func NearestExpiry(instruments []*Instrument, now time.Time, lo, hi int, skip map[time.Time]bool) (time.Time, bool) {
	var best time.Time
	bestDTE := math.MaxInt32
	for _, inst := range instruments {
		if skip[inst.Expiry] {
			continue
		}
		dte := DaysToExpiry(inst.Expiry, now)
		if dte < lo || dte > hi || dte >= bestDTE {
			continue
		}
		bestDTE = dte
		best = inst.Expiry
	}
	return best, !best.IsZero()
}
