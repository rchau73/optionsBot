package strategy

import (
	"math"
	"time"

	"optionsbot/internal/config"
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

// journalMismatchCost is added to a (strangle, slot) score for each leg the
// journal says another slot opened: far above any distance (a few hundred
// at most), so the journal decides whenever it has a record.
const journalMismatchCost = 10000

// maxExhaustiveStrangles bounds the search over every assignment (slots^n);
// a larger book (never seen live) falls back to closest pairs first.
const maxExhaustiveStrangles = 8

// assignSlots gives each strangle a slot with the lowest total cost over the
// whole book — slotScore plus journalMismatchCost per leg the journal says
// another slot opened. Every slot is used once before any is shared: with
// no more strangles than slots each gets its own. Taking the closest pair
// first instead handed the other strangle a far slot on a tie (#156).
func assignSlots(out []ReconciledStrangle, now time.Time, slots []config.StrangleSlot, opened OpenSlots) {
	n, k := len(out), len(slots)
	if n == 0 || k == 0 {
		return
	}
	cost := make([][]float64, n)
	fromJournal := make([][]bool, n)
	for i, st := range out {
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
			cost[i][j] = slotScore(st.Call, st.Put, st.Expiry, now, sl) + float64(mismatched)*journalMismatchCost
			fromJournal[i][j] = recorded > 0 && mismatched == 0
		}
	}

	var pick []int
	if n <= maxExhaustiveStrangles {
		pick = lowestTotal(cost, n, k)
	} else {
		pick = closestFirst(cost, n, k)
	}
	for i, j := range pick {
		out[i].Slot, out[i].SlotFromJournal = slots[j], fromJournal[i][j]
	}
}

// lowestTotal searches every assignment of n strangles to k slots in which
// min(n, k) distinct slots are used, and returns the cheapest (the first
// found on a tie: strangles by expiry, slots in config order).
func lowestTotal(cost [][]float64, n, k int) []int {
	need := min(n, k)
	best := math.Inf(1)
	var bestPick []int
	cur := make([]int, n)
	uses := make([]int, k)
	distinct := 0
	var search func(i int, total float64)
	search = func(i int, total float64) {
		if total >= best {
			return // costs are never negative: this branch cannot win
		}
		if i == n {
			if distinct == need {
				best, bestPick = total, append([]int(nil), cur...)
			}
			return
		}
		for j := 0; j < k; j++ {
			if uses[j] > 0 && n <= k {
				continue
			}
			next := distinct
			if uses[j] == 0 {
				next++
			}
			if need-next > n-i-1 {
				continue // the strangles left could not cover every slot
			}
			uses[j]++
			prev := distinct
			distinct, cur[i] = next, j
			search(i+1, total+cost[i][j])
			uses[j]--
			distinct = prev
		}
	}
	search(0, 0)
	return bestPick
}

// closestFirst repeatedly takes the cheapest (strangle, free slot) pair; a
// slot is shared only once every slot is taken.
func closestFirst(cost [][]float64, n, k int) []int {
	pick := make([]int, n)
	taken := map[int]bool{}
	assigned := make([]bool, n)
	for range n {
		bi, bs, best := -1, -1, math.Inf(1)
		for i := 0; i < n; i++ {
			if assigned[i] {
				continue
			}
			for j := 0; j < k; j++ {
				if taken[j] && len(taken) < k {
					continue
				}
				if cost[i][j] < best {
					bi, bs, best = i, j, cost[i][j]
				}
			}
		}
		pick[bi], assigned[bi], taken[bs] = bs, true, true
	}
	return pick
}
