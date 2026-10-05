package marketdata

import (
	"math"
	"time"
)

// Expiry selection lives here so the market-data subscriber and the strategy
// use the same rule: the expiry the strategy will trade is always one the
// manager has subscribed to.

// DaysToExpiry returns the whole days left until expiry, rounded down (0
// once expired). It is the single definition of DTE: entry uses it to pick an
// expiry and the exit rules (Position.DTEAt) to decide when to roll. With two
// different roundings an expiry 15.5 days out counted as 16 at entry (allowed)
// and 15 at exit (roll now), so a strangle could be opened and rolled on
// consecutive cycles, over and over.
func DaysToExpiry(expiry, now time.Time) int {
	d := expiry.Sub(now).Hours() / 24
	if d < 0 {
		return 0
	}
	return int(math.Floor(d))
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

// StretchHi is the farthest DTE a slot may use when its own expiry is held
// by another slot: targetDTE × stretch, never below the window's top.
func StretchHi(targetDTE, maxDeviation int, stretch float64) int {
	_, hi := ExpiryWindow(targetDTE, maxDeviation, 0)
	return max(hi, int(math.Floor(float64(targetDTE)*stretch)))
}

// ExpiryPick says how PickExpiry chose.
type ExpiryPick int

const (
	PickNone      ExpiryPick = iota // no listed expiry in the slot's window
	PickWindow                      // the slot's own expiry (nearest in its window)
	PickStretched                   // its own was held by another slot: the nearest free one, possibly beyond the window
	PickAllHeld                     // its own was held and no free expiry up to StretchHi: wait
)

// PickExpiry chooses a slot's expiry so slots stay on separate dates.
// Deribit lists weeklies only a few weeks out, then month- and quarter-ends,
// so the ±maxDeviation windows of nearby slots (45 and 60 days) often pick
// the same month-end. held lists expiries other slots already use (open or
// pending). When the slot's own expiry is held, the nearest free expiry up
// to StretchHi is used; when none is free the slot waits rather than stack.
func PickExpiry(instruments []*Instrument, now time.Time, targetDTE, maxDeviation, rolloutDTE int, stretch float64, held map[time.Time]bool) (time.Time, ExpiryPick) {
	lo, hi := ExpiryWindow(targetDTE, maxDeviation, rolloutDTE)
	own, ok := NearestExpiry(instruments, now, lo, hi, nil)
	switch {
	case !ok:
		return time.Time{}, PickNone
	case !held[own]:
		return own, PickWindow
	}
	if free, ok := NearestExpiry(instruments, now, lo, StretchHi(targetDTE, maxDeviation, stretch), held); ok {
		return free, PickStretched
	}
	return time.Time{}, PickAllHeld
}
