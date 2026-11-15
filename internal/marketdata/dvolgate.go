package marketdata

import (
	"fmt"
	"math"
	"time"
)

// The DVOL sanity gate (#796). The bot reads DVOL from its trading
// connection; on testnet that index swung 40 ↔ 125 within minutes while spot
// was flat and the option marks read about 40 (2026-11-14/15), and every
// reading moved the shed buffer and the IV band. Each live reading now passes
// CheckDVOL before it reaches the DVOLTracker; a rejected one leaves the last
// good value in use, so the daily closes the band confirms on are the
// validated series too.
//
// Two references, the first that applies decides:
//  1. the option marks: with a fresh ATM mark IV, a reading within
//     ×/÷ DVOLMarkRatioMax of it is accepted and one beyond is rejected
//     — a real vol spike moves the marks with it;
//  2. without marks, the jump: a reading more than DVOLJumpMaxPct from the
//     last accepted one while spot moved less than DVOLSpotFlatPct is
//     rejected, unless the readings have been rejected for DVOLHoldMax
//     (the new level stays: the gate must not hold a stale value for ever).
const (
	DVOLMarkRatioMax = 1.5              // reading ÷ ATM mark IV (or its inverse) accepted up to this
	DVOLJumpMaxPct   = 25.0             // % move from the last accepted reading counted as a jump
	DVOLSpotFlatPct  = 1.0              // % spot move under which a jump has no market behind it
	DVOLHoldMax      = 30 * time.Minute // longest a jump without marks is held off
	// DVOLMarkMaxAge is how old an option's last ticker may be to count as a mark.
	DVOLMarkMaxAge = 10 * time.Minute
	// DVOLATMTargetDTE is the expiry DVOL tracks (30-day implied volatility).
	DVOLATMTargetDTE = 30
	dvolATMMinDTE    = 7 // nearer expiries' IV is not the 30-day vol
)

// DVOLReading is one live DVOL value with what the gate checks it against.
type DVOLReading struct {
	At    time.Time
	Value float64 // DVOL, %
	Spot  float64 // index price, 0 if unknown
	ATMIV float64 // ATM option mark IV, % (ATMMarkIV), 0 if unknown
}

// DVOLGate is the gate's state: the last accepted reading and the current
// episode of rejections. The zero value accepts its first reading.
type DVOLGate struct {
	last       DVOLReading // last accepted; zero before the first
	rejectFrom time.Time   // first rejection of the current episode; zero when none
	reported   time.Time   // when the episode was last reported
}

// DVOLRejection is one episode of rejected readings, as first seen.
type DVOLRejection struct {
	At     time.Time
	Value  float64 // the rejected reading
	Kept   float64 // the value still in use
	Spot   float64
	ATMIV  float64 // 0 when the marks were unknown
	Reason string  // marks | jump
	Detail string  // both values and the numbers behind the call
}

// CheckDVOL decides whether r is used; inUse is the value that stays in use
// if it is not (the tracker's, which may come from the startup history). It
// returns a rejection for the first rejected reading of an episode and then
// at most once per DVOLHoldMax while it lasts, so the journal is not flooded
// by the once-a-second feed. Pure: the caller passes the time.
func (g *DVOLGate) CheckDVOL(r DVOLReading, inUse float64) (accept bool, rej *DVOLRejection) {
	reason, detail := g.rejectReason(r, inUse)
	if reason == "" {
		g.last, g.rejectFrom = r, time.Time{}
		return true, nil
	}
	if g.rejectFrom.IsZero() {
		g.rejectFrom, g.reported = r.At, r.At
	} else if r.At.Sub(g.reported) >= DVOLHoldMax {
		// A long episode (the marks keep disagreeing) is reported again, so
		// the journal shows the value in use is still held.
		g.reported = r.At
		detail += fmt.Sprintf(" (rejecting since %s)", r.At.Sub(g.rejectFrom).Round(time.Minute))
	} else {
		return false, nil
	}
	return false, &DVOLRejection{At: r.At, Value: r.Value, Kept: inUse, Spot: r.Spot,
		ATMIV: r.ATMIV, Reason: reason, Detail: detail}
}

// rejectReason is why r is not used, or "".
func (g *DVOLGate) rejectReason(r DVOLReading, inUse float64) (reason, detail string) {
	if r.ATMIV > 0 {
		if ratio := r.Value / r.ATMIV; ratio > DVOLMarkRatioMax || ratio < 1/DVOLMarkRatioMax {
			return "marks", fmt.Sprintf("DVOL %.1f rejected, %.1f kept: the option marks' ATM IV is %.1f (%.2f×, limit ×/÷ %.1f)",
				r.Value, inUse, r.ATMIV, ratio, DVOLMarkRatioMax)
		}
		return "", ""
	}
	if g.last.Value <= 0 {
		return "", "" // nothing to compare with
	}
	if !g.rejectFrom.IsZero() && r.At.Sub(g.rejectFrom) >= DVOLHoldMax {
		return "", "" // the new level held: take it
	}
	jump := (r.Value/g.last.Value - 1) * 100
	if math.Abs(jump) <= DVOLJumpMaxPct {
		return "", ""
	}
	spotMove := 0.0 // spot unknown on either side: no move seen
	if r.Spot > 0 && g.last.Spot > 0 {
		spotMove = (r.Spot/g.last.Spot - 1) * 100
	}
	if math.Abs(spotMove) >= DVOLSpotFlatPct {
		return "", ""
	}
	return "jump", fmt.Sprintf("DVOL %.1f rejected, %.1f kept: %+.0f %% in %s with spot %+.2f %% (limits %.0f %% / %.0f %%), no option marks to check",
		r.Value, inUse, jump, r.At.Sub(g.last.At).Round(time.Second), spotMove, DVOLJumpMaxPct, DVOLSpotFlatPct)
}

// ATMMarkIV is the option marks' at-the-money IV (%) for the expiry nearest
// DVOLATMTargetDTE days (at least dvolATMMinDTE out): the mean mark IV of the
// quoted options on the strike nearest spot, counting only tickers at most
// DVOLMarkMaxAge old. 0 when none qualifies. Pure: the caller passes now.
func ATMMarkIV(insts []*Instrument, spot float64, now time.Time) float64 {
	if spot <= 0 {
		return 0
	}
	fresh := func(i *Instrument) bool {
		return i.HasQuote() && i.Greeks.IV > 0 && now.Sub(i.UpdatedAt) <= DVOLMarkMaxAge
	}
	var expiry time.Time
	bestGap := math.MaxInt
	for _, i := range insts {
		dte := DaysToExpiry(i.Expiry, now)
		if dte < dvolATMMinDTE || !fresh(i) {
			continue
		}
		gap := dte - DVOLATMTargetDTE
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap || (gap == bestGap && i.Expiry.Before(expiry)) {
			bestGap, expiry = gap, i.Expiry
		}
	}
	if expiry.IsZero() {
		return 0
	}
	strike, bestDist := 0.0, math.Inf(1)
	for _, i := range insts {
		if i.Expiry.Equal(expiry) && fresh(i) {
			if d := math.Abs(i.Strike - spot); d < bestDist || (d == bestDist && i.Strike < strike) {
				strike, bestDist = i.Strike, d
			}
		}
	}
	sum, n := 0.0, 0
	for _, i := range insts {
		if i.Expiry.Equal(expiry) && i.Strike == strike && fresh(i) {
			sum += i.Greeks.IV * 100
			n++
		}
	}
	return sum / float64(n)
}
