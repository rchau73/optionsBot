package strategy

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// pnlBook accumulates realised P&L per slot since the journal began: it is
// restored from orders.log at startup (RestorePnL), then grows with every
// close. Written by the decision loop, read by the heartbeat goroutine.
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
func keyOf(slot *orders.SlotRef) orders.SlotRef { return orders.SlotKey(slot) }

// RestorePnL seeds realised P&L and close counts per slot from a journal
// replay, so they continue across restarts. Call before Run.
func (s *Strategy) RestorePnL(realised map[orders.SlotRef]float64, closed map[orders.SlotRef]int) {
	s.pnl.mu.Lock()
	defer s.pnl.mu.Unlock()
	for k, v := range realised {
		s.pnl.realised[keyOf(&k)] += v
	}
	for k, v := range closed {
		s.pnl.closed[keyOf(&k)] += v
	}
}

// PnLLine is one row of a P&L report: a slot, or the strategy total.
type PnLLine struct {
	Slot       *orders.SlotRef // nil = strategy total
	Retired    bool            // Slot is not in the matrix (zero SlotRef = no known slot)
	Realised   float64         // coin
	Unrealised float64         // coin
	OpenLegs   int
	ClosedLegs int
}

// ComputePnL combines realised P&L per slot (zero SlotRef = unknown slot)
// with the unrealised P&L of the open positions, marked to mid. slotOfPos maps
// a position ID to its slot. The result lists each configured slot, then one
// Retired row per other slot that holds P&L (a slot since removed from the
// matrix, or the zero SlotRef for anything without a known slot), then the
// strategy total — so the rows always add up to the total (#108).
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
	// line returns the row of slot k, adding a Retired row for a slot that is
	// not configured.
	var retired []orders.SlotRef
	line := func(k orders.SlotRef) *PnLLine {
		if l, ok := lines[k]; ok {
			return l
		}
		ref := k
		l := &PnLLine{Slot: &ref, Retired: true}
		lines[k] = l
		retired = append(retired, k)
		return l
	}
	total := &PnLLine{}

	for k, v := range realised {
		total.Realised += v
		total.ClosedLegs += closed[k]
		l := line(keyOf(&k))
		l.Realised += v
		l.ClosedLegs += closed[k]
	}
	for _, p := range positions {
		u := p.NetPnL()
		total.Unrealised += u
		total.OpenLegs++
		l := line(keyOf(slotOfPos[p.ID]))
		l.Unrealised += u
		l.OpenLegs++
	}

	sort.Slice(retired, func(i, j int) bool {
		if retired[i].DTE != retired[j].DTE {
			return retired[i].DTE < retired[j].DTE
		}
		return retired[i].Delta < retired[j].Delta
	})
	out := make([]PnLLine, 0, len(order)+len(retired)+1)
	for _, k := range append(order, retired...) {
		out = append(out, *lines[k])
	}
	return append(out, *total)
}

// PnLReport returns P&L per configured slot, then per retired slot, then the strategy total
// (realised since the journal began, unrealised marked to mid), in the
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

// historyMarginMaxAge is how old the decision loop's margin view may be and
// still be recorded with a P&L point; an older one (a stalled loop) is none.
const historyMarginMaxAge = 5 * time.Minute

// HistoryMargin is the margin view recorded with a P&L history point (#174):
// nil before the first margin read or when the view is older than
// historyMarginMaxAge; IM and MM unknown (entries not allowed) when the
// margin read failed that cycle. The margin balance and equity (#277) are
// Deribit's, in the unit the policy reads (AccountSummary.MarginUsage).
func HistoryMargin(rv RiskView, now time.Time) *history.Margin {
	if rv.AsOf.IsZero() || now.Sub(rv.AsOf) > historyMarginMaxAge {
		return nil
	}
	return &history.Margin{
		Known: rv.Error == "", IMPct: rv.IMPct, MMPct: rv.MMPct,
		IMLimitPct: rv.Status.LimitIMPct, EntriesAllowed: rv.EntriesAllowed,
		MarginBalance: rv.MarginBalance, Equity: rv.Equity, Unit: rv.Unit,
	}
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
			Retired:       l.Retired,
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
			s.history.RecordPnL(now, l.Realised, l.Unrealised, spot, HistoryMargin(s.riskView(), now))
		}
		if l.Slot == nil {
			slog.Info("pnl", "strategy_id", s.strategyID(),
				"realised", l.Realised, "unrealised", l.Unrealised, "total_usd", total*spot,
				"open_legs", l.OpenLegs, "closed_legs", l.ClosedLegs)
		}
	}
}
