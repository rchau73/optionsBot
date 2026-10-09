package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// Orders the bot may have placed without knowing it, and fills it did not
// book. On 2026-10-04 a repair sell went out as the connection dropped: the
// submit "failed" (connection lost) but the order had reached Deribit,
// rested untracked, and filled — next to the replacement the bot sent later.
// The strangle ended up with twice the call it should have.
//
// Two defences:
//   - an order whose submit outcome is unknown (orders.MaybePlaced) is
//     cancelled by its unique label every cycle until that succeeds; its
//     slot stays blocked meanwhile, so nothing is sent twice;
//   - every cycle the exchange's positions are compared with the book; a
//     difference seen on two consecutive cycles is adopted into the book
//     (with an ERROR log and a journal entry), so every position on the
//     exchange is managed — stop-loss included — whatever its origin.

// unconfirmedOrder is a submit whose outcome is unknown.
type unconfirmedOrder struct {
	label      string
	instrument string
	slot       slotKey
	since      time.Time
}

// noteUnconfirmed records a submit that may have reached the exchange.
func (s *Strategy) noteUnconfirmed(label, instrument string, slot *orders.SlotRef, err error) {
	key := slotKey{}
	if slot != nil {
		key = makeSlotKey(slot.DTE, slot.Delta)
	}
	s.unconfirmed[label] = unconfirmedOrder{label: label, instrument: instrument, slot: key, since: time.Now()}
	slog.Error("order submit outcome unknown: it may be on the book; cancelling it by label before anything else is sent for this slot",
		"label", label, "instrument", instrument, "err", err)
}

// resolveUnconfirmed cancels every unconfirmed order by label. Once the
// cancel succeeds the order can no longer fill; a fill that already happened
// is picked up by checkPositions.
func (s *Strategy) resolveUnconfirmed(ctx context.Context) {
	for label, u := range s.unconfirmed {
		n, err := s.exch.CancelByLabel(ctx, s.cfg.Underlying, label)
		if err != nil {
			slog.Warn("unconfirmed order: cancel by label failed, retrying next cycle",
				"label", label, "instrument", u.instrument, "err", err)
			continue
		}
		delete(s.unconfirmed, label)
		s.forceCheck[u.instrument] = true // the book must match the exchange before acting again
		slog.Warn("unconfirmed order resolved", "label", label, "instrument", u.instrument,
			"cancelled", n, "unknown_for", time.Since(u.since).Round(time.Second))
	}
}

// instrumentUnconfirmed reports whether instrument has an order with an
// unknown outcome. No other order is sent for it until that is resolved.
func (s *Strategy) instrumentUnconfirmed(instrument string) bool {
	for _, u := range s.unconfirmed {
		if u.instrument == instrument {
			return true
		}
	}
	return false
}

// slotUnconfirmed reports whether a slot has an order with an unknown outcome.
func (s *Strategy) slotUnconfirmed(key slotKey) bool {
	for _, u := range s.unconfirmed {
		if u.slot == key {
			return true
		}
	}
	return false
}

// driftConfirmations is how many consecutive cycles a position difference
// must persist before it is adopted, so a fill that lands between the order
// poll and the position read is not mistaken for drift.
const driftConfirmations = 2

// setExchangeSnapshot records this read of the exchange's positions (the cap
// for buy-backs, capToExchange) and returns its shorts by instrument.
func (s *Strategy) setExchangeSnapshot(raw []orders.RawPosition) map[string]orders.RawPosition {
	exch := map[string]orders.RawPosition{}
	snap := &exchangeSnapshot{short: map[string]float64{}, long: map[string]float64{}}
	for _, rp := range raw {
		switch {
		case rp.Size == 0:
		case rp.Direction == orders.DirectionSell:
			exch[rp.InstrumentName] = rp
			snap.short[rp.InstrumentName] = math.Abs(rp.Size)
		case rp.Direction == orders.DirectionBuy:
			snap.long[rp.InstrumentName] = math.Abs(rp.Size)
		}
	}
	s.exchSnap = snap
	return exch
}

// bookByInstrument groups the book's positions by instrument. An instrument
// can back several positions (a filled rebalance complement, or two slots on
// the same expiry and strike); the exchange reports their sum, so the book is
// compared by its sum too.
func (s *Strategy) bookByInstrument() map[string][]*orders.Position {
	book := map[string][]*orders.Position{}
	for _, p := range s.state.AllPositions() {
		book[p.Instrument] = append(book[p.Instrument], p)
	}
	return book
}

// syncBookToExchange makes the book hold exactly the exchange's shorts, at
// once (no drift confirmation): the kill switch must flatten what the
// exchange holds, not what the book believes.
func (s *Strategy) syncBookToExchange(ctx context.Context) error {
	raw, err := s.exch.GetPositions(ctx, s.cfg.Underlying)
	if err != nil {
		return err
	}
	exch := s.setExchangeSnapshot(raw)
	book := s.bookByInstrument()
	names := map[string]bool{}
	for n := range exch {
		names[n] = true
	}
	for n := range book {
		names[n] = true
	}
	for name := range names {
		if have := math.Abs(exch[name].Size); math.Abs(have-totalQty(book[name])) >= qtyEpsilon {
			s.adoptPosition(name, book[name], exch[name], have)
		}
	}
	return nil
}

// checkPositions compares the exchange's short positions with the book
// (plus fills of entry orders still being tracked) and adopts persistent
// differences.
func (s *Strategy) checkPositions(ctx context.Context) {
	raw, err := s.exch.GetPositions(ctx, s.cfg.Underlying)
	if err != nil {
		s.exchSnap = nil // unknown this cycle: buy-backs are not capped
		slog.Debug("position check skipped", "err", err)
		return
	}
	exch := s.setExchangeSnapshot(raw)
	book := s.bookByInstrument()
	inflight := map[string]float64{}
	for _, ps := range s.pendingSnapshot() {
		for _, l := range ps.legs() {
			inflight[l.instrument] += l.filledQty
		}
	}

	seen := map[string]bool{}
	names := make([]string, 0, len(exch)+len(book))
	for n := range exch {
		names, seen[n] = append(names, n), true
	}
	for n := range book {
		if !seen[n] {
			names = append(names, n)
		}
	}
	defer clear(s.forceCheck) // a forced check applies to this cycle only
	for _, name := range names {
		have := math.Abs(exch[name].Size)
		want := inflight[name] + totalQty(book[name])
		if math.Abs(have-want) < qtyEpsilon {
			delete(s.drift, name)
			continue
		}
		s.drift[name]++
		if s.drift[name] < driftConfirmations && !s.forceCheck[name] {
			continue
		}
		delete(s.drift, name)
		delete(s.forceCheck, name)
		s.adoptPosition(name, book[name], exch[name], have-inflight[name])
	}
}

// adoptPosition makes the book hold qty of name in total, as the exchange
// does. A shortfall is taken from the newest positions first (the likeliest
// to be unbooked or double-booked); an excess is added to the newest one at
// the exchange's average price, or adopted as a new position.
func (s *Strategy) adoptPosition(name string, held []*orders.Position, rp orders.RawPosition, qty float64) {
	before := totalQty(held)
	slog.Error("position drift: the exchange holds a different size than the book — adopting the exchange's",
		"instrument", name, "exchange_qty", qty, "book_qty", before, "book_positions", len(held))

	sort.Slice(held, func(i, j int) bool { return held[i].EntryTime.After(held[j].EntryTime) })
	diff := qty - before
	switch {
	case diff < 0:
		for _, pos := range held {
			if diff > -qtyEpsilon {
				break
			}
			cut := math.Min(pos.Qty, -diff)
			diff += cut
			left := pos.Qty - cut
			if left <= qtyEpsilon { // closed outside the bot
				s.state.RemovePosition(pos.ID)
				s.state.RemoveStrangleContaining(pos.ID)
				continue
			}
			s.state.UpdatePositionQty(pos.ID, left, pos.PremiumReceived*left/pos.Qty)
			s.logAdopted(name, pos.ID)
		}
	case len(held) > 0:
		pos := held[0]
		s.state.UpdatePositionQty(pos.ID, pos.Qty+diff, pos.PremiumReceived+rp.AveragePrice*diff)
		s.logAdopted(name, pos.ID)
	default:
		p, err := s.positionFromRaw(rp, time.Now())
		if err != nil {
			slog.Error("position drift: cannot adopt unknown instrument", "instrument", name, "err", err)
			return
		}
		p.Qty, p.PremiumReceived = qty, rp.AveragePrice*qty
		s.state.AddPosition(p)
		slot := s.attachToStrangle(p)
		s.journal.LogReconciled(p, s.instrumentContext(name, slot))
	}
}

// logAdopted journals a book position whose size was set from the exchange.
func (s *Strategy) logAdopted(name, id string) {
	if p, ok := s.state.GetPosition(id); ok {
		s.journal.LogReconciled(p, s.instrumentContext(name, s.slotOf(id)))
	}
}

// totalQty sums the sizes of positions.
func totalQty(ps []*orders.Position) float64 {
	sum := 0.0
	for _, p := range ps {
		sum += p.Qty
	}
	return sum
}

// attachToStrangle puts an adopted leg in the strangle of its expiry that
// misses that leg type, or in a new strangle matched to the closest slot.
func (s *Strategy) attachToStrangle(p *orders.Position) *orders.SlotRef {
	for _, st := range s.state.AllStrangles() {
		present, missing := s.liveLegs(st)
		if present != nil && missing == p.OptionType && present.Expiry.Equal(p.Expiry) {
			s.state.SetStrangleLeg(st.ID, p.OptionType, p)
			return slotRef(st.TargetDTE, st.EntryDelta)
		}
	}
	var call, put *orders.Position
	if p.OptionType == "call" {
		call = p
	} else {
		put = p
	}
	now := time.Now()
	best := MatchSlotToPosition(call, put, p.Expiry, now, s.cfg.Slots())
	s.state.AddStrangle(&orders.Strangle{
		ID: s.state.NextID("st"), TargetDTE: best.TargetDTE, EntryDelta: best.EntryDelta,
		CallLeg: call, PutLeg: put, OpenedAt: now,
	})
	return slotRef(best.TargetDTE, best.EntryDelta)
}

// positionFromRaw builds a book position from a private/get_positions row.
func (s *Strategy) positionFromRaw(rp orders.RawPosition, now time.Time) (*orders.Position, error) {
	var strike float64
	var expiry time.Time
	var optType, underlying string
	if inst, ok := s.md.GetInstrument(rp.InstrumentName); ok {
		strike, expiry, optType, underlying = inst.Strike, inst.Expiry, inst.OptionType, inst.Underlying
	} else {
		var err error
		underlying, expiry, strike, optType, err = marketdata.ParseOptionName(rp.InstrumentName)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rp.InstrumentName, err)
		}
	}
	qty := math.Abs(rp.Size)
	return &orders.Position{
		ID:              s.state.NextID("pos"),
		Instrument:      rp.InstrumentName,
		Underlying:      underlying,
		Strike:          strike,
		Expiry:          expiry,
		OptionType:      optType,
		Side:            rp.Direction,
		Qty:             qty,
		EntryPrice:      rp.AveragePrice,
		UnderlyingPrice: rp.IndexPrice,
		EntryTime:       now,
		PremiumReceived: rp.AveragePrice * qty,
		CurrentMid:      rp.MarkPrice,
		CurrentGreeks:   PerOptionGreeks(rp),
	}, nil
}
