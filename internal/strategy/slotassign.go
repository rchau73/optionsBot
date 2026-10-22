package strategy

import (
	"fmt"
	"math"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// OpenSlots is, per instrument, the slots (orders.SlotKey) the journal says
// opened it and have not closed it since. The startup reconcile prefers
// them: a strangle keeps the slot it was opened under, so its P&L is booked
// there after a restart (2026-10-17 ETH: 27NOV at 40 DTE scored the same
// against slots 30 and 50 and took slot 30, swapping it with 6NOV). The
// exchange stays the only source of positions; this is a preference.
type OpenSlots map[string][]orders.SlotRef

// OpenSlotsFromTrades builds OpenSlots from the journal's trades (oldest
// first, as the replay gives them): an open adds its slot to the
// instrument, a close of that slot removes it — except the partial trims
// (leg balancing, downsize) that leave the leg open.
func OpenSlotsFromTrades(trades []orders.Trade) OpenSlots {
	out := OpenSlots{}
	for _, t := range trades {
		if t.Slot == nil || t.Slot.DTE == 0 {
			continue
		}
		key := orders.SlotKey(t.Slot)
		refs := out[t.Instrument]
		switch {
		case t.Kind == "open" && !hasSlot(refs, key):
			out[t.Instrument] = append(refs, key)
		case t.Kind == "close" && !partialReasons[t.Reason]:
			kept := refs[:0:0]
			for _, r := range refs {
				if r != key {
					kept = append(kept, r)
				}
			}
			if len(kept) == 0 {
				delete(out, t.Instrument)
			} else {
				out[t.Instrument] = kept
			}
		}
	}
	return out
}

func hasSlot(refs []orders.SlotRef, key orders.SlotRef) bool {
	for _, r := range refs {
		if r == key {
			return true
		}
	}
	return false
}

// The costs of an assignment, strongest first. Distances (slotScore) are a
// few hundred at most, so each rule outweighs every distance.
const (
	// journalMismatchCost, per leg the journal says another slot opened: the
	// journal decides whenever it has a record — even when two strangles
	// then share their slot.
	journalMismatchCost = 10000
	// outOfWindowCost: a strangle further out than the slot's window
	// (target + max_dte_deviation) that the journal does not put there. The
	// slot could not have opened it, so a vacant slot stays vacant rather
	// than take another slot's strangle (2026-10-21 BTC, #242: the empty
	// 25-DTE slot was handed a 36-DTE strangle of the 50-DTE slot).
	outOfWindowCost = 5000
	// sharedExpiryCost, per extra slot on one expiry: one expiry per slot.
	sharedExpiryCost = 5000
	// sharedSlotCost, per extra expiry in one slot: the book's expiries
	// spread over the slots before one slot takes two (#156: ETH's 19- and
	// 40-DTE strangles in slots 30 and 50, not both in 30).
	sharedSlotCost = 1000
)

// maxExhaustiveStrangles bounds the search over every assignment (slots^n);
// a larger book (never seen live) is assigned greedily, strangle by strangle.
const maxExhaustiveStrangles = 8

// assignSlots gives each strangle a slot with the lowest total cost over the
// whole book: slotScore plus the costs above. A slot may stay vacant: it is
// never handed a strangle beyond its window that the journal does not put
// there, nor an expiry another slot holds. Strangles on one expiry share a
// slot unless the journal says otherwise.
func assignSlots(out []ReconciledStrangle, now time.Time, slots []config.StrangleSlot, maxDeviation int, opened OpenSlots) {
	n, k := len(out), len(slots)
	if n == 0 || k == 0 {
		return
	}
	cost := make([][]float64, n)
	fromJournal := make([][]bool, n)
	expiry := make([]int, n) // index of each strangle's expiry
	expIdx := map[time.Time]int{}
	for i, st := range out {
		if _, ok := expIdx[st.Expiry]; !ok {
			expIdx[st.Expiry] = len(expIdx)
		}
		expiry[i] = expIdx[st.Expiry]
		cost[i] = make([]float64, k)
		fromJournal[i] = make([]bool, k)
		for j, sl := range slots {
			key := orders.SlotKey(slotRef(sl.TargetDTE, sl.EntryDelta))
			recorded, mismatched := 0, 0
			for _, leg := range []*orders.Position{st.Call, st.Put} {
				if leg == nil || len(opened[leg.Instrument]) == 0 {
					continue
				}
				recorded++
				if !hasSlot(opened[leg.Instrument], key) {
					mismatched++
				}
			}
			fromJournal[i][j] = recorded > 0 && mismatched == 0
			cost[i][j] = slotScore(st.Call, st.Put, st.Expiry, now, sl) + float64(mismatched)*journalMismatchCost
			if !fromJournal[i][j] && !inSlotWindow(st.Expiry, now, sl, maxDeviation) {
				cost[i][j] += outOfWindowCost
			}
		}
	}

	var pick []int
	if n <= maxExhaustiveStrangles {
		pick = lowestTotal(cost, expiry, len(expIdx), k)
	} else {
		pick = greedy(cost, expiry, len(expIdx), k)
	}
	for i, j := range pick {
		out[i].Slot, out[i].SlotFromJournal = slots[j], fromJournal[i][j]
	}
}

// inSlotWindow: the slot could have opened a strangle on expiry — its DTE is
// at most the top of the slot's window. A strangle ages, so any DTE below is
// in (a 50-DTE slot's strangle is at 20 DTE a month later). The stretch
// beyond the window is used only while another slot holds the slot's own
// expiry, and such an entry is in the journal.
func inSlotWindow(expiry, now time.Time, sl config.StrangleSlot, maxDeviation int) bool {
	_, hi := marketdata.ExpiryWindow(sl.TargetDTE, maxDeviation, 0)
	return marketdata.DaysToExpiry(expiry, now) <= hi
}

// sharing counts, during a search, the strangles of each expiry in each
// slot, and prices one more (sharedExpiryCost, sharedSlotCost).
type sharing struct {
	count      [][]int // [expiry][slot]
	slotsOfExp []int   // distinct slots per expiry
	expsOfSlot []int   // distinct expiries per slot
}

func newSharing(exps, k int) *sharing {
	sh := &sharing{count: make([][]int, exps), slotsOfExp: make([]int, exps), expsOfSlot: make([]int, k)}
	for e := range sh.count {
		sh.count[e] = make([]int, k)
	}
	return sh
}

// extra is the sharing cost of one more strangle of expiry e in slot j.
func (sh *sharing) extra(e, j int) float64 {
	if sh.count[e][j] > 0 {
		return 0 // the slot already holds this expiry
	}
	c := 0.0
	if sh.slotsOfExp[e] > 0 {
		c += sharedExpiryCost
	}
	if sh.expsOfSlot[j] > 0 {
		c += sharedSlotCost
	}
	return c
}

// add places (d = 1) or removes (d = −1) one strangle of expiry e in slot j.
func (sh *sharing) add(e, j, d int) {
	before := sh.count[e][j]
	sh.count[e][j] += d
	if before == 0 || sh.count[e][j] == 0 { // the (expiry, slot) pair appeared or went
		sh.slotsOfExp[e] += d
		sh.expsOfSlot[j] += d
	}
}

// lowestTotal searches every assignment of the strangles to k slots and
// returns the cheapest (the first found on a tie: strangles by expiry, slots
// in config order).
func lowestTotal(cost [][]float64, expiry []int, exps, k int) []int {
	n := len(cost)
	sh := newSharing(exps, k)
	best := math.Inf(1)
	var bestPick []int
	cur := make([]int, n)
	var search func(i int, total float64)
	search = func(i int, total float64) {
		if total >= best {
			return // costs are never negative: this branch cannot win
		}
		if i == n {
			best, bestPick = total, append([]int(nil), cur...)
			return
		}
		for j := 0; j < k; j++ {
			step := cost[i][j] + sh.extra(expiry[i], j)
			sh.add(expiry[i], j, 1)
			cur[i] = j
			search(i+1, total+step)
			sh.add(expiry[i], j, -1)
		}
	}
	search(0, 0)
	return bestPick
}

// greedy gives each strangle in turn its cheapest slot, sharing included.
func greedy(cost [][]float64, expiry []int, exps, k int) []int {
	sh := newSharing(exps, k)
	pick := make([]int, len(cost))
	for i := range cost {
		bj, best := 0, math.Inf(1)
		for j := 0; j < k; j++ {
			if c := cost[i][j] + sh.extra(expiry[i], j); c < best {
				bj, best = j, c
			}
		}
		pick[i] = bj
		sh.add(expiry[i], bj, 1)
	}
	return pick
}

// VacantSlot is a configured slot the startup reconcile gave no strangle,
// and why. It then enters like any vacant slot: an expiry in its own window
// (or the stretch when that one is held), else it waits (no_expiry).
type VacantSlot struct {
	Slot   config.StrangleSlot
	Reason string
}

// VacantSlots lists the slots no reconciled strangle was given, with the
// reason: no strangle within the slot's window, or the ones within it were
// opened by another slot (journal) or are on an expiry another slot holds.
func VacantSlots(got []ReconciledStrangle, now time.Time, slots []config.StrangleSlot, maxDeviation int) []VacantSlot {
	used := map[config.StrangleSlot]bool{}
	for _, g := range got {
		used[g.Slot] = true
	}
	var out []VacantSlot
	for _, sl := range slots {
		if used[sl] {
			continue
		}
		_, hi := marketdata.ExpiryWindow(sl.TargetDTE, maxDeviation, 0)
		reason := fmt.Sprintf("no strangle within its window (DTE <= %d)", hi)
		for _, g := range got {
			if inSlotWindow(g.Expiry, now, sl, maxDeviation) {
				reason = fmt.Sprintf("the strangles within its window (DTE <= %d) were opened by another slot (journal) or are on an expiry another slot holds", hi)
				break
			}
		}
		out = append(out, VacantSlot{Slot: sl, Reason: reason})
	}
	return out
}
