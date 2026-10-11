package strategy

import (
	"fmt"
	"time"

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
