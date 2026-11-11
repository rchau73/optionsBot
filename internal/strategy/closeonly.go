package strategy

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// Close-only exits (#753). Under config.FixedCloseOnlyBelowDTE (25) a delta
// drift or a take-profit buys the leg back (EvaluateLeg's ActionClose) instead
// of rolling it: re-selling on that expiry would be time-rolled within days.
// Two records follow such a close, both until the expiry passes:
//   - the strangle's side is not repaired (repairGate: HoldCloseOnly);
//   - the expiry is held for the slot (ClosedExpiries), so once the strangle
//     has no leg left the slot re-enters on another expiry — its window
//     (16–35 DTE for the 25-DTE slot) still holds the expiry just closed.
//
// Both live in memory and are rebuilt at startup from the journal's close
// lines, whose detail carries CloseOnlyMarker (CloseOnlyFromTrades).

// ClosedExpiries are, per slot, the expiry days (UTC midnight) a close-only
// exit left. Shared with the backtest and the replay, which keep their own.
type ClosedExpiries map[slotKey]map[time.Time]bool

// Hold records expiry as closed for the slot (dte, delta).
func (c ClosedExpiries) Hold(dte int, delta float64, expiry time.Time) {
	k := makeSlotKey(dte, delta)
	if c[k] == nil {
		c[k] = make(map[time.Time]bool)
	}
	c[k][expiryDay(expiry)] = true
}

// Held reports whether expiry is closed for the slot (dte, delta).
func (c ClosedExpiries) Held(dte int, delta float64, expiry time.Time) bool {
	return c[makeSlotKey(dte, delta)][expiryDay(expiry)]
}

// HasAny reports whether the slot (dte, delta) holds any closed expiry.
func (c ClosedExpiries) HasAny(dte int, delta float64) bool {
	return len(c[makeSlotKey(dte, delta)]) > 0
}

// Prune drops the expiries already past at now.
func (c ClosedExpiries) Prune(now time.Time) {
	today := expiryDay(now)
	for k, days := range c {
		for d := range days {
			if d.Before(today) {
				delete(days, d)
			}
		}
		if len(days) == 0 {
			delete(c, k)
		}
	}
}

// WithHeld returns held plus every expiry of instruments closed for the slot
// (dte, delta): the set SelectSlotExpiry avoids for that slot. held is not
// changed.
func (c ClosedExpiries) WithHeld(held map[time.Time]bool, instruments []*marketdata.Instrument, dte int, delta float64) map[time.Time]bool {
	days := c[makeSlotKey(dte, delta)]
	if len(days) == 0 {
		return held
	}
	out := make(map[time.Time]bool, len(held)+len(days))
	for t, v := range held {
		out[t] = v
	}
	for _, in := range instruments {
		if days[expiryDay(in.Expiry)] {
			out[in.Expiry] = true
		}
	}
	return out
}

// IsCloseOnlyDetail reports whether a close's journaled detail is a
// close-only exit's.
func IsCloseOnlyDetail(detail string) bool {
	return strings.Contains(detail, CloseOnlyMarker)
}

// JournalCloseOnly is what the journal says close-only exits left: the sides
// (expiry, type) whose latest close was one, with no repair since, and the
// expiries held per slot.
type JournalCloseOnly struct {
	Sides map[StopSide]bool
	Slots ClosedExpiries
}

// CloseOnlyFromTrades rebuilds the close-only records from the journal's
// trades (oldest first, as the replay gives them): a side whose latest close
// on an expiry was a close-only exit, with no repair re-sale since, and every
// slot's close-only expiries not yet past at now.
func CloseOnlyFromTrades(trades []orders.Trade, now time.Time) JournalCloseOnly {
	out := JournalCloseOnly{Sides: map[StopSide]bool{}, Slots: ClosedExpiries{}}
	for _, t := range trades {
		_, expiry, _, typ, err := marketdata.ParseOptionName(t.Instrument)
		if err != nil {
			continue
		}
		side := StopSide{expiryDay(expiry), typ}
		switch {
		case t.Kind == "close" && IsCloseOnlyDetail(t.Detail):
			out.Sides[side] = true
			if t.Slot != nil {
				out.Slots.Hold(t.Slot.DTE, t.Slot.Delta, expiry)
			}
		case t.Kind == "close" && !partialReasons[t.Reason]:
			delete(out.Sides, side)
		case t.Kind == "open" && t.Reason == orders.TriggerRepair:
			delete(out.Sides, side)
		}
	}
	out.Slots.Prune(now)
	return out
}

// noteCloseOnly records pos's close-only exit (#753): its side of the
// strangle is not repaired and its expiry is held for the slot. Call before
// closing (the strangle is found through its legs).
func (s *Strategy) noteCloseOnly(pos *orders.Position) {
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == pos.ID) || (st.PutLeg != nil && st.PutLeg.ID == pos.ID) {
			s.closeOnlySides[stopKey(st.ID, pos.OptionType)] = pos.Expiry
			s.closedExpiries.Hold(st.TargetDTE, st.EntryDelta, pos.Expiry)
			return
		}
	}
}

// closeOnlyHold says why st's missing leg may not be repaired because a
// close-only exit closed it, or "".
func (s *Strategy) closeOnlyHold(st *orders.Strangle, missing string) string {
	if _, ok := s.closeOnlySides[stopKey(st.ID, missing)]; !ok {
		return ""
	}
	return fmt.Sprintf("closed only under %d DTE: not re-sold on this expiry", s.closeOnlyFloor())
}

// closeOnlyFloor is the DTE under which drift and take-profit close only.
func (s *Strategy) closeOnlyFloor() int {
	if s.cfg.CloseOnlyBelowDTE > 0 {
		return s.cfg.CloseOnlyBelowDTE
	}
	return SameLegRollMinDTE
}

// pruneCloseOnly drops the records of expiries already past and of strangles
// no longer in the book.
func (s *Strategy) pruneCloseOnly(now time.Time) {
	s.closedExpiries.Prune(now)
	if len(s.closeOnlySides) == 0 {
		return
	}
	live := map[string]bool{}
	for _, st := range s.state.AllStrangles() {
		live[stopKey(st.ID, "call")], live[stopKey(st.ID, "put")] = true, true
	}
	for k, exp := range s.closeOnlySides {
		if !live[k] || expiryDay(exp).Before(expiryDay(now)) {
			delete(s.closeOnlySides, k)
		}
	}
}

// applyJournalCloseOnly restores the close-only records the journal holds
// (RestoreStops) onto the reconciled strangles.
func (s *Strategy) applyJournalCloseOnly() {
	j := s.journalCloseOnly
	s.journalCloseOnly = JournalCloseOnly{}
	for k, days := range j.Slots {
		for d := range days {
			s.closedExpiries.Hold(k.DTE, float64(k.DeltaX100)/100, d)
		}
	}
	for _, st := range s.state.AllStrangles() {
		present, missing := s.liveLegs(st)
		if present == nil || missing == "" || !j.Sides[StopSide{expiryDay(present.Expiry), missing}] {
			continue
		}
		s.closeOnlySides[stopKey(st.ID, missing)] = present.Expiry
		slog.Warn("close-only record restored from the journal: repair held on this expiry",
			"strangle_id", st.ID, "present", present.Instrument, "missing", missing)
	}
}
