package strategy

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"optionsbot/internal/orders"
)

// pnlBook accumulates realised P&L per slot since the process started.
// It is written by the decision loop and read by the heartbeat goroutine.
type pnlBook struct {
	mu       sync.Mutex
	realised map[orders.SlotRef]float64
	closed   map[orders.SlotRef]int
}

func newPnLBook() *pnlBook {
	return &pnlBook{realised: map[orders.SlotRef]float64{}, closed: map[orders.SlotRef]int{}}
}

// record adds a realised close. Positions without a known slot are booked
// under the zero SlotRef, so the strategy total stays complete.
func (b *pnlBook) record(slot *orders.SlotRef, pnl float64) {
	k := keyOf(slot)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.realised[k] += pnl
	b.closed[k]++
}

func (b *pnlBook) snapshot() (map[orders.SlotRef]float64, map[orders.SlotRef]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := make(map[orders.SlotRef]float64, len(b.realised))
	c := make(map[orders.SlotRef]int, len(b.closed))
	for k, v := range b.realised {
		r[k] = v
	}
	for k, v := range b.closed {
		c[k] = v
	}
	return r, c
}

// keyOf normalises a slot for map lookups (delta compared in hundredths).
func keyOf(slot *orders.SlotRef) orders.SlotRef {
	if slot == nil {
		return orders.SlotRef{}
	}
	return orders.SlotRef{DTE: slot.DTE, Delta: math.Round(slot.Delta*100) / 100}
}

// PnLLine is one row of a P&L report: a slot, or the strategy total.
type PnLLine struct {
	Slot       *orders.SlotRef // nil = strategy total
	Realised   float64         // coin
	Unrealised float64         // coin
	OpenLegs   int
	ClosedLegs int
}

// ComputePnL combines realised P&L per slot (zero SlotRef = unknown slot)
// with the unrealised P&L of the open positions, marked to mid. slotOfPos maps
// a position ID to its slot. The result lists each configured slot, then the
// strategy total, which also includes anything without a known slot.
func ComputePnL(slots []orders.SlotRef, positions []*orders.Position, slotOfPos map[string]*orders.SlotRef,
	realised map[orders.SlotRef]float64, closed map[orders.SlotRef]int) []PnLLine {

	lines := make(map[orders.SlotRef]*PnLLine, len(slots))
	order := make([]orders.SlotRef, 0, len(slots))
	for _, sl := range slots {
		k := keyOf(&sl)
		if _, dup := lines[k]; dup {
			continue
		}
		ref := sl
		lines[k] = &PnLLine{Slot: &ref}
		order = append(order, k)
	}
	total := &PnLLine{}

	for k, v := range realised {
		total.Realised += v
		total.ClosedLegs += closed[k]
		if l, ok := lines[keyOf(&k)]; ok {
			l.Realised += v
			l.ClosedLegs += closed[k]
		}
	}
	for _, p := range positions {
		u := p.MtMPnL()
		total.Unrealised += u
		total.OpenLegs++
		if l, ok := lines[keyOf(slotOfPos[p.ID])]; ok {
			l.Unrealised += u
			l.OpenLegs++
		}
	}

	out := make([]PnLLine, 0, len(order)+1)
	for _, k := range order {
		out = append(out, *lines[k])
	}
	return append(out, *total)
}

// PnLReport returns P&L per configured slot followed by the strategy total
// (realised since the process started, unrealised marked to mid), in the
// underlying coin. Safe to call from any goroutine.
func (s *Strategy) PnLReport() []PnLLine {
	return s.pnlWithMarks(nil)
}

// pnlWithMarks is PnLReport with positions re-marked from marks (position ID
// → price) where given, e.g. live ticker prices for the monitor.
func (s *Strategy) pnlWithMarks(marks map[string]float64) []PnLLine {
	slotOfPos := map[string]*orders.SlotRef{}
	for _, st := range s.state.AllStrangles() {
		ref := slotRef(st.TargetDTE, st.EntryDelta)
		for _, leg := range []*orders.Position{st.CallLeg, st.PutLeg} {
			if leg != nil {
				slotOfPos[leg.ID] = ref
			}
		}
	}
	var slots []orders.SlotRef
	for _, sl := range s.cfg.Slots() {
		slots = append(slots, orders.SlotRef{DTE: sl.TargetDTE, Delta: sl.EntryDelta})
	}
	realised, closed := s.pnl.snapshot()
	positions := s.state.AllPositions()
	for _, p := range positions {
		if m, ok := marks[p.ID]; ok {
			p.CurrentMid = m // positions are snapshots, safe to modify
		}
	}
	return ComputePnL(slots, positions, slotOfPos, realised, closed)
}

// logPnL writes one P&L line per slot plus the strategy total to the journal.
func (s *Strategy) logPnL() {
	spot := s.md.UnderlyingPrice()
	now := time.Now()
	for _, l := range s.PnLReport() {
		total := l.Realised + l.Unrealised
		s.journal.LogPnL(orders.PnLRecord{
			Timestamp:     now,
			StrategyID:    s.strategyID(),
			Slot:          l.Slot,
			Realised:      l.Realised,
			Unrealised:    l.Unrealised,
			Total:         total,
			RealisedUSD:   l.Realised * spot,
			UnrealisedUSD: l.Unrealised * spot,
			TotalUSD:      total * spot,
			Spot:          spot,
			OpenLegs:      l.OpenLegs,
			ClosedLegs:    l.ClosedLegs,
		})
		if l.Slot == nil && s.history != nil {
			s.history.RecordPnL(now, l.Realised, l.Unrealised, spot)
		}
		if l.Slot == nil {
			slog.Info("pnl", "strategy_id", s.strategyID(),
				"realised", l.Realised, "unrealised", l.Unrealised, "total_usd", total*spot,
				"open_legs", l.OpenLegs, "closed_legs", l.ClosedLegs)
		}
	}
}
