package strategy

import (
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// A leg closed by a stop-loss or a delta exit (or cut to zero by the MM
// limit) was lost to a move against it. Re-selling it at once, as repair does for a rolled leg,
// sells into the same move: in a 2-week stress simulation that loop
// (stop → re-sell further out → stop again) was about half of all option
// losses. So such a leg is re-sold only once the market has calmed.

// RepairBlockReason says why a stopped-out leg may not be re-sold yet, or ""
// when it may: entries must not be frozen, the confirmed gamma regime must
// not be negative, and cooldown must have passed since the stop-loss.
func RepairBlockReason(stoppedAt, now time.Time, cooldown time.Duration, st risk.Status) string {
	switch {
	case st.Frozen:
		return "entries frozen: " + st.FreezeReason
	case st.RegimeUsed && st.RegimeNegative:
		return "confirmed negative gamma regime"
	case now.Sub(stoppedAt) < cooldown:
		return fmt.Sprintf("cooldown after stop-out until %s", stoppedAt.Add(cooldown).UTC().Format("Jan 2 15:04 UTC"))
	}
	return ""
}

func stopKey(strangleID, optType string) string { return strangleID + ":" + optType }

// noteStopped remembers that pos's leg of its strangle was stopped out.
// Call before closing (the strangle is found through its legs).
func (s *Strategy) noteStopped(pos *orders.Position, at time.Time) {
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == pos.ID) || (st.PutLeg != nil && st.PutLeg.ID == pos.ID) {
			s.stopped[stopKey(st.ID, pos.OptionType)] = at
			return
		}
	}
}

// slotStopHold says why optType may not be added to slot key now, or "": a
// leg of that type of a strangle in the slot was stopped out (or closed by
// hand) and RepairBlockReason still holds its repair. Used by the rebalance
// top-up, which sells into the slot without going through repair.
func (s *Strategy) slotStopHold(key slotKey, optType string, st risk.Status) string {
	now := time.Now()
	for _, str := range s.state.AllStrangles() {
		if makeSlotKey(str.TargetDTE, str.EntryDelta) != key {
			continue
		}
		at, ok := s.stopped[stopKey(str.ID, optType)]
		if !ok {
			continue
		}
		if why := RepairBlockReason(at, now, time.Duration(s.cfg.RepairCooldownHours)*time.Hour, st); why != "" {
			return optType + " stopped out, " + why
		}
	}
	return ""
}

// Restoring stop records after a restart. The records live in memory, so a
// restart used to forget them: a hand-closed or stopped-out side was re-sold
// by repair at the first moment nothing else held it (2026-10-11 BTC: the
// deploy of the top-up fix dropped the records of both hand-closed puts).
// The journal holds every close, so the records are rebuilt from it.

// StopSide is one expiry's call or put side. Expiry is the expiry's UTC
// date (midnight): option names carry only the date.
type StopSide struct {
	Expiry     time.Time
	OptionType string
}

func expiryDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// stopReasons are the closes that hold a side's repair (noteStopped).
var stopReasons = map[string]bool{
	orders.TriggerStopLoss200Pct: true,
	orders.TriggerDeltaExit:      true,
	orders.TriggerManualClose:    true,
	orders.TriggerMarginMM:       true, // held only when it closed the leg out; a partial cut leaves the leg, so nothing is missing
}

// partialReasons trim a leg that stays open: they say nothing about holds.
var partialReasons = map[string]bool{
	orders.TriggerRebalanceLegs:     true,
	orders.TriggerRebalanceDownsize: true,
}

// StopsFromTrades returns, for each expiry side, when it was last stopped
// out, as the in-memory record would hold it: the side's latest close was a
// stop-class close (stop-loss, delta exit, manual close, MM cut) and no
// repair re-sold that side afterwards. Any other later close (a roll, a GEX
// shed) replaces it. trades are oldest first, as the journal replay gives
// them; names that do not parse are ignored.
func StopsFromTrades(trades []orders.Trade) map[StopSide]time.Time {
	out := make(map[StopSide]time.Time)
	for _, t := range trades {
		_, expiry, _, typ, err := marketdata.ParseOptionName(t.Instrument)
		if err != nil {
			continue
		}
		side := StopSide{expiryDay(expiry), typ}
		switch {
		case t.Kind == "close" && stopReasons[t.Reason]:
			out[side] = t.At
		case t.Kind == "close" && !partialReasons[t.Reason]:
			delete(out, side)
		case t.Kind == "open" && t.Reason == orders.TriggerRepair:
			delete(out, side)
		}
	}
	return out
}

// RestoreStops hands the journal's trades to the strategy; the stop records
// are rebuilt after the startup reconcile. Call before Run.
func (s *Strategy) RestoreStops(trades []orders.Trade) {
	s.journalStops = StopsFromTrades(trades)
}

// applyJournalStops records a stop for every reconciled strangle missing a
// side the journal says was stopped on its expiry, with the original time,
// so the cooldown runs on from the stop.
func (s *Strategy) applyJournalStops() {
	for _, st := range s.state.AllStrangles() {
		present, missing := s.liveLegs(st)
		if present == nil || missing == "" {
			continue
		}
		at, ok := s.journalStops[StopSide{expiryDay(present.Expiry), missing}]
		if !ok {
			continue
		}
		s.stopped[stopKey(st.ID, missing)] = at
		slog.Warn("stop record restored from the journal: repair held as after the stop",
			"strangle_id", st.ID, "present", present.Instrument, "missing", missing, "stopped_at", at.UTC().Format(time.RFC3339))
	}
	s.journalStops = nil
}
