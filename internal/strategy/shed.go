package strategy

import (
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/marketdata"
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
	// Clear counts consecutive snapshots the GEX bias has not pointed at the
	// shed side (gex_direction: bias).
	Clear         int
	lastClearSnap time.Time
}

// ObserveBias counts one GEX snapshot for the bias release: clear when the
// bias does not point at the shed side. A snapshot already counted is ignored.
func (a *ShedAnchor) ObserveBias(clear bool, snapAt time.Time) {
	if snapAt.IsZero() || !snapAt.After(a.lastClearSnap) {
		return
	}
	a.lastClearSnap = snapAt
	if clear {
		a.Clear++
	} else {
		a.Clear = 0
	}
}

// BiasRepairBlockReason says why a leg shed under gex_direction: bias may
// not be re-sold yet, or "" when it may: cooldown has passed since the shed
// (a spike through the flip is not re-sold into at once) and the bias has
// left the shed side on confirm snapshots in a row — the move has stalled.
func BiasRepairBlockReason(a *ShedAnchor, optType string, now time.Time, cooldown time.Duration, confirm int) string {
	side := "down"
	if optType == "call" {
		side = "up"
	}
	if until := a.At.Add(cooldown); now.Before(until) {
		return fmt.Sprintf("shed by GEX (bias %s): cooldown until %s", side, until.UTC().Format("Jan 2 15:04 UTC"))
	}
	if a.Clear < confirm {
		return fmt.Sprintf("shed by GEX (bias %s): waiting for the bias to leave %s on %d snapshots in a row (%d so far)", side, side, confirm, a.Clear)
	}
	return ""
}

// biasPointsAt reports whether bias runs toward the side optType loses on.
func biasPointsAt(bias int, optType string) bool {
	return (optType == "put" && bias < 0) || (optType == "call" && bias > 0)
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

// Restoring shed anchors after a restart. Anchors live in memory, so a
// restart forgot them: on 2026-10-14 the bias shed BTC's put at 11:45, a
// deploy restarted the bot at 12:05, and repair re-sold the put at 12:14 —
// 24 h early, paying the spread twice. The journal holds every close with
// the flip (and, since 2026-10-14, the buffer), so anchors are rebuilt from
// it the way stop records are.

// JournalShed is a side's last GEX shed as the journal recorded it.
type JournalShed struct {
	At        time.Time
	Flip      float64
	BufferPct float64 // 0 in records journaled before 2026-10-14: see DVOL
	DVOL      float64 // at the shed, to rebuild a missing buffer
}

// shedReasons are a GEX shed's trigger and its close_reason label.
var shedReasons = closeReasons(orders.TriggerGammaClose)

// ShedsFromTrades returns, for each expiry side, its last GEX shed when the
// side's latest close was that shed and no repair re-sold the side since —
// as the in-memory anchor would hold it. Any other later close (a stop, a
// roll) replaces it; partial trims do not. trades are oldest first.
func ShedsFromTrades(trades []orders.Trade) map[StopSide]JournalShed {
	out := make(map[StopSide]JournalShed)
	for _, t := range trades {
		_, expiry, _, typ, err := marketdata.ParseOptionName(t.Instrument)
		if err != nil {
			continue
		}
		side := StopSide{expiryDay(expiry), typ}
		switch {
		case t.Kind == "close" && shedReasons[t.Reason]:
			out[side] = JournalShed{At: t.At, Flip: t.GammaFlip, BufferPct: t.FlipBufferPct, DVOL: t.DVOL}
		case t.Kind == "close" && !partialReasons[t.Reason]:
			delete(out, side)
		case t.Kind == "open" && t.Reason == orders.TriggerRepair:
			delete(out, side)
		}
	}
	return out
}

// applyJournalSheds anchors every reconciled strangle missing a side the
// journal says GEX shed on its expiry, at the shed's time, flip and buffer,
// so the repair waits as it would have without the restart. A record
// without a buffer takes today's buffer setting at the shed's DVOL.
func (s *Strategy) applyJournalSheds() {
	for _, st := range s.state.AllStrangles() {
		present, missing := s.liveLegs(st)
		if present == nil || missing == "" {
			continue
		}
		j, ok := s.journalSheds[StopSide{expiryDay(present.Expiry), missing}]
		if !ok {
			continue
		}
		key := stopKey(st.ID, missing)
		if _, held := s.shedAnchors[key]; held {
			continue
		}
		buf := j.BufferPct
		if buf <= 0 {
			buf = FlipBufferPct(s.cfg.GammaFlipBufferSD, j.DVOL)
		}
		s.shedAnchors[key] = &ShedAnchor{At: j.At, Flip: j.Flip, BufferPct: buf}
		slog.Warn("shed anchor restored from the journal: repair held as after the shed",
			"strangle_id", st.ID, "present", present.Instrument, "missing", missing,
			"shed_at", j.At.UTC().Format(time.RFC3339), "flip", j.Flip, "buffer_pct", buf)
	}
	s.journalSheds = nil
}
