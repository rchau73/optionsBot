// Package idlemargin is a research tool (#218): it splits the time each
// strategy slot spent into occupied, vacant by the journal's skip reason,
// frozen and not measured, and splits the bot's unused approved margin by
// the state of the book, so the next E1 change goes after the cause that
// costs the most. It reads the journal and the P&L history only: no Deribit
// call, no trading code.
package idlemargin

import (
	"math"
	"sort"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// Rows of a slot's table besides the skip reasons themselves.
const (
	RowOccupied       = "occupied"                  // open strangle (even one-legged) or pending entry
	RowOccupiedCapped = "occupied_liquidity_cap"    // occupied, its entry or a top-up cut by the liquidity cap
	RowFrozen         = "frozen"                    // vacant while entries were not allowed, whatever the skip
	RowNoSkip         = "no_skip_logged"            // vacant, no skip journaled since it became vacant
	RowNotMeasured    = "not_measured"              // bot down or no margin data: never under a reason
	skipLiquidityCap  = "liquidity_cap"             // strategy.SkipLiquidityCap
	qtyEpsilon        = 1e-9                        // a leg closed down to float noise is closed
	pendingMaxAge     = history.MaxSampleGap        // an entry order outlives its 90 s timeout only if the bot stopped
	bookSlotEmpty     = "slot_empty"                // some slot vacant
	bookRebalanceWait = "rebalance_waiting"         // every slot occupied, a short top-up waiting for its retry
	bookSizedSmall    = "sized_small_liquidity_cap" // every slot occupied, one sized down by the liquidity cap
	bookFull          = "all_full"                  // every slot occupied, nothing capped or waiting
)

// BookStates are the book states of Report.Book, in print order.
var BookStates = []string{bookSlotEmpty, bookRebalanceWait, bookSizedSmall, bookFull}

// Event is one journal line the attribution needs.
type Event struct {
	At         time.Time
	Event      string          // orders.Event*
	Slot       *orders.SlotRef // nil when the line has none
	Instrument string
	Qty        float64
	FilledQty  float64 // cancels: what filled before the cancel
	OrderID    string
	Trigger    string // trigger_reason
	Reason     string // skips: the reason (text before ':'); risk lines: the change
}

// SlotReport is the time one slot spent in each row, in hours.
type SlotReport struct {
	Slot  orders.SlotRef
	Hours map[string]float64
}

// MeasuredHours is the slot's time minus the not-measured row.
func (s SlotReport) MeasuredHours() float64 {
	var h float64
	for row, v := range s.Hours {
		if row != RowNotMeasured {
			h += v
		}
	}
	return h
}

// BookState is the bot's allowed time in one book state.
type BookState struct {
	Hours float64
	// MeanPct is the time-weighted IM ÷ IM limit in % (nil without time).
	MeanPct *float64
	// UnusedShare is this state's share (0–1) of Σ (1 − IM ÷ limit) × time,
	// the unused approved margin over the allowed time (nil when none).
	UnusedShare *float64
	weighted    float64 // Σ IM ÷ limit × seconds
	unused      float64 // Σ max(0, 1 − IM ÷ limit) × seconds
}

// Report is the attribution of one bot over [From, To].
type Report struct {
	From, To    time.Time
	Slots       []SlotReport // by DTE, then delta
	Total       SlotReport   // every slot summed (Slot zero)
	Utilisation history.UtilisationStats
	Book        map[string]*BookState // keyed by BookStates
}

// interval is one history point's stretch: measured, and whether entries
// were allowed, with IM ÷ limit when they were.
type interval struct {
	start, end time.Time
	allowed    bool
	ratio      float64
}

// measuredIntervals cuts [from, to] into the stretches history.Utilisation
// counts as measured, with the same rules, so the two reports agree.
func measuredIntervals(points []history.Point, from, to time.Time) []interval {
	var out []interval
	for i, p := range points {
		if p.EntriesAllowed == nil || p.T.Before(from) || !p.T.Before(to) {
			continue
		}
		end := p.T.Add(history.MaxSampleGap)
		if i+1 < len(points) && points[i+1].T.Before(end) {
			end = points[i+1].T
		}
		if end.After(to) {
			end = to
		}
		if !end.After(p.T) {
			continue
		}
		if !*p.EntriesAllowed {
			out = append(out, interval{start: p.T, end: end})
			continue
		}
		if p.IMPct == nil || p.IMLimitPct == nil || *p.IMLimitPct <= 0 {
			continue
		}
		out = append(out, interval{start: p.T, end: end, allowed: true, ratio: *p.IMPct / *p.IMLimitPct})
	}
	return out
}

// slotState is what the journal says about one slot at a moment.
type slotState struct {
	legs     map[string]float64   // instrument → short qty
	pending  map[string]time.Time // working entry order ID → submit time
	lastSkip string               // reason of the last skip since the slot became vacant
	capped   bool                 // occupied after (or with) a liquidity_cap skip
}

func (st *slotState) occupied(now time.Time) bool {
	if len(st.legs) > 0 {
		return true
	}
	for _, at := range st.pending {
		if now.Sub(at) < pendingMaxAge {
			return true
		}
	}
	return false
}

// row is the slot's row at a moment, given whether entries were allowed.
func (st *slotState) row(now time.Time, allowed bool) string {
	switch {
	case st.occupied(now) && st.capped:
		return RowOccupiedCapped
	case st.occupied(now):
		return RowOccupied
	case !allowed:
		return RowFrozen
	case st.lastSkip == "":
		return RowNoSkip
	}
	return st.lastSkip
}

// tracker replays the journal events into slot states.
type tracker struct {
	slots        map[orders.SlotRef]*slotState
	keep         map[orders.SlotRef]bool // nil = every slot seen
	rebalWaiting bool
}

func (t *tracker) slot(ref *orders.SlotRef) *slotState {
	if ref == nil || ref.DTE == 0 {
		return nil
	}
	k := orders.SlotKey(ref)
	if t.keep != nil && !t.keep[k] {
		return nil
	}
	st := t.slots[k]
	if st == nil {
		st = &slotState{legs: map[string]float64{}, pending: map[string]time.Time{}}
		t.slots[k] = st
	}
	return st
}

// apply updates the state with one event.
func (t *tracker) apply(e Event) {
	if e.Event == orders.EventRisk {
		switch e.Reason {
		case orders.RiskRebalanceRetry:
			t.rebalWaiting = true
		case orders.RiskRebalance, orders.RiskRebalanceManual, orders.RiskLimitChanged, orders.RiskRebalanceInBand:
			t.rebalWaiting = false
		}
		return
	}
	st := t.slot(e.Slot)
	if st == nil {
		return
	}
	wasOccupied := st.occupied(e.At)
	switch e.Event {
	case orders.EventSkipped:
		if e.Reason == skipLiquidityCap {
			st.capped = true
		}
		if !wasOccupied {
			st.lastSkip = e.Reason
		}
	case orders.EventSubmitted:
		if e.Trigger == orders.TriggerEntry && e.OrderID != "" {
			st.pending[e.OrderID] = e.At
		}
	case orders.EventCancelled:
		if e.FilledQty <= qtyEpsilon { // a filled part is journaled next, as a fill
			delete(st.pending, e.OrderID)
		}
	case orders.EventFilled:
		delete(st.pending, e.OrderID)
		if e.Instrument != "" && e.Qty > 0 {
			st.legs[e.Instrument] += e.Qty
		}
	case orders.EventReconciled:
		if e.Instrument != "" && e.Qty > 0 {
			st.legs[e.Instrument] = e.Qty // the exchange's size
		}
	case orders.EventClosed:
		t.close(st, e)
	}
	if !wasOccupied && st.occupied(e.At) {
		st.capped = st.lastSkip == skipLiquidityCap
	}
	if wasOccupied && !st.occupied(e.At) {
		st.lastSkip, st.capped = "", false
	}
}

// close takes a buy-back off its slot's leg; a close journaled on a slot that
// does not hold the instrument takes it off the slot that does.
func (t *tracker) close(st *slotState, e Event) {
	if _, ok := st.legs[e.Instrument]; !ok {
		for _, other := range t.slots {
			if _, held := other.legs[e.Instrument]; held {
				st = other
				break
			}
		}
	}
	if _, ok := st.legs[e.Instrument]; !ok {
		return
	}
	was := st.occupied(e.At)
	st.legs[e.Instrument] -= e.Qty
	if st.legs[e.Instrument] <= qtyEpsilon {
		delete(st.legs, e.Instrument)
	}
	if was && !st.occupied(e.At) {
		st.lastSkip, st.capped = "", false
	}
}

// expire drops legs past their settlement (an expiry the journal saw no
// close for), and ends the occupation of a slot whose pending orders aged out.
func (t *tracker) expire(now time.Time) {
	for _, st := range t.slots {
		was := len(st.legs) > 0 || len(st.pending) > 0
		for name := range st.legs {
			if _, exp, _, _, err := marketdata.ParseOptionName(name); err == nil && !now.Before(exp.Add(marketdata.SettlementHourUTC)) {
				delete(st.legs, name)
			}
		}
		for id, at := range st.pending {
			if now.Sub(at) >= pendingMaxAge {
				delete(st.pending, id)
			}
		}
		if was && len(st.legs) == 0 && len(st.pending) == 0 {
			st.lastSkip, st.capped = "", false
		}
	}
}

// Attribute returns the attribution over [from, to]. events may hold lines
// before from (they set each slot's state at from) and must be ordered by
// time; points are the P&L history ordered by time. slots lists the
// configured slots; nil takes every slot the journal names.
//
// A slot is occupied from an open (or a pending entry order) until its last
// leg is bought back or expires; vacant time takes the reason of the last
// skip since it became vacant, or frozen while entries were not allowed.
// Time history.Utilisation does not measure (bot down, no margin data) is
// not measured, for every slot.
func Attribute(events []Event, points []history.Point, slots []orders.SlotRef, from, to time.Time) Report {
	tr := &tracker{slots: map[orders.SlotRef]*slotState{}}
	if slots != nil {
		tr.keep = map[orders.SlotRef]bool{}
		for _, s := range slots {
			k := orders.SlotKey(&s)
			tr.keep[k] = true
			tr.slot(&k)
		}
	} else {
		for _, e := range events {
			tr.slot(e.Slot)
		}
	}
	hours := map[orders.SlotRef]map[string]float64{}
	for k := range tr.slots {
		hours[k] = map[string]float64{}
	}
	book := map[string]*BookState{}
	for _, s := range BookStates {
		book[s] = &BookState{}
	}

	// span attributes [a, b), on which no event changes any state.
	ivs := measuredIntervals(points, from, to)
	next := 0
	span := func(a, b time.Time) {
		if a.Before(from) {
			a = from
		}
		if b.After(to) {
			b = to
		}
		if !b.After(a) {
			return
		}
		// Not measured = the span minus the intervals inside it.
		measured := time.Duration(0)
		for next < len(ivs) && !ivs[next].end.After(a) {
			next++
		}
		for i := next; i < len(ivs) && ivs[i].start.Before(b); i++ {
			s, e := maxTime(a, ivs[i].start), minTime(b, ivs[i].end)
			if !e.After(s) {
				continue
			}
			d := e.Sub(s)
			measured += d
			anyVacant := false
			capped := false
			for k, st := range tr.slots {
				row := st.row(s, ivs[i].allowed)
				hours[k][row] += d.Hours()
				if row != RowOccupied && row != RowOccupiedCapped {
					anyVacant = true
				}
				capped = capped || row == RowOccupiedCapped
			}
			if !ivs[i].allowed {
				continue
			}
			state := bookFull
			switch {
			case anyVacant:
				state = bookSlotEmpty
			case tr.rebalWaiting:
				state = bookRebalanceWait
			case capped:
				state = bookSizedSmall
			}
			bs := book[state]
			bs.Hours += d.Hours()
			bs.weighted += ivs[i].ratio * d.Seconds()
			bs.unused += math.Max(0, 1-ivs[i].ratio) * d.Seconds()
		}
		for k := range tr.slots {
			hours[k][RowNotMeasured] += (b.Sub(a) - measured).Hours()
		}
	}

	cursor := from
	for _, e := range events {
		if e.At.After(cursor) {
			stepTo(tr, span, cursor, minTime(e.At, to))
			cursor = maxTime(cursor, e.At)
		}
		tr.expire(e.At)
		tr.apply(e)
	}
	if cursor.Before(to) {
		stepTo(tr, span, cursor, to)
	}

	out := Report{From: from, To: to, Book: book, Utilisation: history.Utilisation(points, from, to)}
	out.Total = SlotReport{Hours: map[string]float64{}}
	for k, h := range hours {
		for row, v := range h {
			if v == 0 {
				delete(h, row)
				continue
			}
			out.Total.Hours[row] += v
		}
		out.Slots = append(out.Slots, SlotReport{Slot: k, Hours: h})
	}
	sort.Slice(out.Slots, func(i, j int) bool {
		a, b := out.Slots[i].Slot, out.Slots[j].Slot
		if a.DTE != b.DTE {
			return a.DTE < b.DTE
		}
		return a.Delta < b.Delta
	})
	var unused float64
	for _, bs := range book {
		unused += bs.unused
	}
	for _, bs := range book {
		if secs := bs.Hours * 3600; secs > 0 {
			mean := 100 * bs.weighted / secs
			bs.MeanPct = &mean
		}
		if unused > 0 {
			share := bs.unused / unused
			bs.UnusedShare = &share
		}
	}
	return out
}

// stepTo attributes [a, b) piece by piece, at each moment a leg settles or a
// pending order ages out, so those changes land at their own time.
func stepTo(tr *tracker, span func(a, b time.Time), a, b time.Time) {
	tr.expire(a)
	for a.Before(b) {
		cut := minTime(b, nextChange(tr, a))
		span(a, cut)
		tr.expire(cut)
		a = cut
	}
}

// nextChange is the first moment after now a leg settles or a pending
// order ages out (far future when none).
func nextChange(tr *tracker, now time.Time) time.Time {
	next := time.Unix(1<<40, 0)
	for _, st := range tr.slots {
		for name := range st.legs {
			if _, exp, _, _, err := marketdata.ParseOptionName(name); err == nil {
				if at := exp.Add(marketdata.SettlementHourUTC); at.After(now) && at.Before(next) {
					next = at
				}
			}
		}
		for _, at := range st.pending {
			if end := at.Add(pendingMaxAge); end.After(now) && end.Before(next) {
				next = end
			}
		}
	}
	return next
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
