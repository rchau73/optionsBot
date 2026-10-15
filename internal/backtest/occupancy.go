package backtest

import (
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// SlotOccupancy is how many simulated days one (DTE, delta) slot held at
// least one live leg.
type SlotOccupancy struct {
	TargetDTE    int
	EntryDelta   float64
	OccupiedDays int
	Days         int
}

// Pct is the share of days the slot was occupied, in percent (0 with no days).
func (s SlotOccupancy) Pct() float64 {
	if s.Days == 0 {
		return 0
	}
	return float64(s.OccupiedDays) / float64(s.Days) * 100
}

type slotKey struct {
	DTE       int
	DeltaX100 int
}

func keyOf(dte int, delta float64) slotKey {
	return slotKey{dte, int(math.Round(delta * 100))}
}

// occupancy counts, at the end of each simulated day, which slots hold a live
// leg, the days on which two slots held the same expiry (the one-expiry-per-
// slot rule says never) and the premium held, the backtest's stand-in for
// margin.
type occupancy struct {
	slots      []SlotOccupancy
	sharedDays int
	premiumSum float64
	days       int
}

func newOccupancy(slots []config.StrangleSlot) *occupancy {
	o := &occupancy{}
	for _, s := range slots {
		o.slots = append(o.slots, SlotOccupancy{TargetDTE: s.TargetDTE, EntryDelta: s.EntryDelta})
	}
	return o
}

func (o *occupancy) record(date time.Time, state *orders.StateManager) {
	live := map[string]*orders.Position{}
	for _, p := range state.AllPositions() {
		live[p.ID] = p
		o.premiumSum += p.PremiumReceived
	}
	o.days++

	occupied := map[slotKey]bool{}
	owners := map[time.Time]slotKey{}
	shared := false
	for _, st := range state.AllStrangles() {
		key := keyOf(st.TargetDTE, st.EntryDelta)
		for _, leg := range []*orders.Position{st.CallLeg, st.PutLeg} {
			if leg == nil || live[leg.ID] == nil {
				continue
			}
			occupied[key] = true
			if other, ok := owners[leg.Expiry]; ok && other != key {
				shared = true
				slog.Warn("backtest: two slots hold the same expiry",
					"date", date.Format("2006-01-02"), "expiry", leg.Expiry.Format("2006-01-02"),
					"slot_dte", key.DTE, "other_slot_dte", other.DTE)
			}
			owners[leg.Expiry] = key
		}
	}
	if shared {
		o.sharedDays++
	}
	for i := range o.slots {
		o.slots[i].Days++
		if occupied[keyOf(o.slots[i].TargetDTE, o.slots[i].EntryDelta)] {
			o.slots[i].OccupiedDays++
		}
	}
}

// SetMonthlyOnly hides every expiry that is not a monthly (IsMonthlyExpiry)
// from the strategy, to compare a ladder with and without weeklies. Call it
// before Run.
func (e *Engine) SetMonthlyOnly(on bool) { e.monthlyOnly = on }

// Occupancy returns each slot's occupied days, in the order of cfg.Slots().
func (e *Engine) Occupancy() []SlotOccupancy {
	return append([]SlotOccupancy(nil), e.occ.slots...)
}

// SharedExpiryDays is the number of days on which two slots held a live leg
// on the same expiry. The one-expiry-per-slot rule keeps it at 0.
func (e *Engine) SharedExpiryDays() int { return e.occ.sharedDays }

// AvgPremiumHeld is the mean premium of the open legs over the simulated
// days — the margin stand-in the backtest's entry check also uses.
func (e *Engine) AvgPremiumHeld() float64 {
	if e.occ.days == 0 {
		return 0
	}
	return e.occ.premiumSum / float64(e.occ.days)
}
