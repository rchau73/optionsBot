package strategy

import (
	"fmt"
	"time"

	"optionsbot/internal/orders"
)

// A leg shed by GEX is re-sold against the flip it was shed at, not the live
// one. The script's flip (lowest crossing) jumps several percent when open
// interest shifts: on 2026-10-06 23:02 the ETH flip fell 2697 → 2440 in one
// minute, the live signal stopped shedding and repair re-sold the puts the
// minute after shedding them. Anchored, the leg waits for spot to come back
// above the shed-time flip plus its buffer — positive-gamma territory where
// the shed was about — on consecutive snapshots, or for the live regime to
// stay non-negative long enough that the shed no longer applies. In stress
// runs with modelled flip jumps this beat re-selling on the live signal by
// about 9 % (ETH) and 10 % (BTC) against holding, and cost ~nothing without
// jumps (2026-10-08).

// ShedAnchor is the flip and buffer a leg was shed at, and how many
// consecutive snapshots spot has since been back above them.
type ShedAnchor struct {
	At        time.Time
	Flip      float64
	BufferPct float64
	Above     int       // consecutive snapshots spot has been above Level()
	lastSnap  time.Time // last snapshot counted
}

// Level is the spot above which the shed leg may be re-sold.
func (a *ShedAnchor) Level() float64 { return a.Flip * (1 + a.BufferPct/100) }

// Observe counts one GEX snapshot (spot at snapAt); a snapshot already
// counted is ignored, so it can run every cycle.
func (a *ShedAnchor) Observe(spot float64, snapAt time.Time) {
	if snapAt.IsZero() || !snapAt.After(a.lastSnap) {
		return
	}
	a.lastSnap = snapAt
	if spot > a.Level() {
		a.Above++
	} else {
		a.Above = 0
	}
}

// ShedRepairBlockReason says why a GEX-shed leg may not be re-sold yet, or
// "" when it may: spot above the anchor for confirm snapshots in a row, or
// the live regime non-negative since nonNegSince for release (0 = never).
func ShedRepairBlockReason(a *ShedAnchor, now, nonNegSince time.Time, confirm int, release time.Duration) string {
	if a.Above >= confirm {
		return ""
	}
	if release > 0 && !nonNegSince.IsZero() && now.Sub(nonNegSince) >= release {
		return ""
	}
	return fmt.Sprintf("shed by GEX at flip %.0f: waiting for spot above %.0f (flip + %.1f%%) on %d snapshots in a row, or a non-negative regime for %.0f h",
		a.Flip, a.Level(), a.BufferPct, confirm, release.Hours())
}

// noteShed anchors pos's leg of its strangle at the decision's flip and
// buffer. Call before closing (the strangle is found through its legs).
func (s *Strategy) noteShed(pos *orders.Position, dec GammaDecision, at time.Time) {
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == pos.ID) || (st.PutLeg != nil && st.PutLeg.ID == pos.ID) {
			s.shedAnchors[stopKey(st.ID, pos.OptionType)] = &ShedAnchor{At: at, Flip: dec.GammaFlip, BufferPct: dec.FlipBufferPct}
			return
		}
	}
}

// dropStaleAnchors forgets anchors of strangles no longer in the book.
func (s *Strategy) dropStaleAnchors() {
	if len(s.shedAnchors) == 0 {
		return
	}
	live := map[string]bool{}
	for _, st := range s.state.AllStrangles() {
		live[stopKey(st.ID, "call")], live[stopKey(st.ID, "put")] = true, true
	}
	for k := range s.shedAnchors {
		if !live[k] {
			delete(s.shedAnchors, k)
		}
	}
}
