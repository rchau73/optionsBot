package replay

import (
	"math"
	"sort"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
)

// The journal (orders.log) is what the bot actually did. BookAt rebuilds its
// short book at a time, to start a replay where the bot was; Actual is the
// bot's own P&L over a window, to check a replay of the live settings
// against reality.

// BookAt is the journal's open short legs at t (opens and closes before it),
// one per slot and instrument, at their average premium.
func BookAt(trades []orders.Trade, t time.Time) []StartLeg {
	type k struct {
		slot config.StrangleSlot
		inst string
	}
	open := map[k]*StartLeg{}
	var order []k
	for _, tr := range trades {
		if !tr.At.Before(t) || tr.Qty <= 0 {
			continue
		}
		slot := config.StrangleSlot{}
		if tr.Slot != nil {
			ref := orders.SlotKey(tr.Slot)
			slot = config.StrangleSlot{TargetDTE: ref.DTE, EntryDelta: ref.Delta}
		}
		id := k{slot, tr.Instrument}
		l := open[id]
		switch tr.Kind {
		case "open":
			if l == nil {
				l = &StartLeg{Slot: slot, Instrument: tr.Instrument}
				open[id] = l
				order = append(order, id)
			}
			l.Price = (l.Price*l.Qty + tr.Price*tr.Qty) / (l.Qty + tr.Qty)
			l.Qty += tr.Qty
		case "close":
			if l != nil {
				l.Qty = math.Max(0, l.Qty-tr.Qty)
			}
		}
	}
	var out []StartLeg
	for _, id := range order {
		if l := open[id]; l.Qty > 1e-9 {
			out = append(out, *l)
		}
	}
	return out
}

// Actual is the bot's P&L over [From, To] from its journal, marked with the
// recording: the realised P&L of its closes (net of fees, as journaled) plus
// the change in the unrealised P&L of its open book, each end at the mid of
// the nearest snapshot. The opening fees of legs still open at To are not in
// it (the journal books them on the close).
type Actual struct {
	Realised      float64
	Unrealised0   float64 // open book at From: premium − value at the mid
	Unrealised1   float64 // at To
	Net           float64 // Realised + Unrealised1 − Unrealised0
	Opens, Closes int
	Unmarked      int // open legs at either end without a recorded quote (valued at their premium)
}

// ActualPnL is Actual over the replayed window: from the first to the last
// of snaps (oldest first).
func ActualPnL(trades []orders.Trade, snaps []recorder.Snapshot, from, to time.Time) Actual {
	var a Actual
	if len(snaps) == 0 {
		return a
	}
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].At.Before(snaps[j].At) })
	first, last := nearest(snaps, from), nearest(snaps, to)
	for _, tr := range trades {
		if tr.At.Before(first.At) || tr.At.After(last.At) {
			continue
		}
		switch tr.Kind {
		case "open":
			a.Opens++
		case "close":
			a.Closes++
			a.Realised += tr.PnL
		}
	}
	unrealised := func(s recorder.Snapshot) float64 {
		quotes := s.ByName()
		u := 0.0
		for _, l := range BookAt(trades, s.At) {
			mid := l.Price
			if q, ok := quotes[l.Instrument]; ok {
				mid = q.LiveMid()
			} else {
				a.Unmarked++
			}
			u += (l.Price - mid) * l.Qty
		}
		return u
	}
	a.Unrealised0, a.Unrealised1 = unrealised(first), unrealised(last)
	a.Net = a.Realised + a.Unrealised1 - a.Unrealised0
	return a
}

// nearest is the snapshot closest to t (the first or the last outside them).
func nearest(snaps []recorder.Snapshot, t time.Time) recorder.Snapshot {
	best := snaps[0]
	for _, s := range snaps {
		if math.Abs(float64(s.At.Sub(t))) < math.Abs(float64(best.At.Sub(t))) {
			best = s
		}
	}
	return best
}
