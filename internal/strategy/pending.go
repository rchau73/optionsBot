package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// qtyEpsilon absorbs float noise when comparing a filled amount to the
// requested amount (both are multiples of the exchange lot size).
const qtyEpsilon = 1e-9

// slotKey uniquely identifies a (DTE, delta) strangle slot.
// DeltaX100 avoids float comparison issues (0.16 → 16, 0.18 → 18).
type slotKey struct {
	DTE       int
	DeltaX100 int
}

func makeSlotKey(dte int, delta float64) slotKey {
	return slotKey{DTE: dte, DeltaX100: int(math.Round(delta * 100))}
}

// pendingLeg is one limit sell that may still be resting on the book.
type pendingLeg struct {
	orderID    string
	instrument string
	optionType string
	qty        float64 // requested amount
	limitPrice float64
	filledQty  float64 // amount filled so far; may be partial
	fillPrice  float64 // average fill price of filledQty
	done       bool    // the order is no longer working (filled, cancelled or rejected)
}

func newPendingLeg(fill orders.Fill, instrument, optionType string, qty, limitPrice float64) *pendingLeg {
	return &pendingLeg{
		orderID:    fill.OrderID,
		instrument: instrument,
		optionType: optionType,
		qty:        qty,
		limitPrice: limitPrice,
		filledQty:  fill.Qty,
		fillPrice:  fill.FillPrice,
		done:       fill.Qty >= qty-qtyEpsilon,
	}
}

// applyState updates the leg from a private/get_order_state reply.
func (l *pendingLeg) applyState(st orders.OrderStateInfo) {
	l.filledQty = st.FilledAmount
	if st.AvgPrice > 0 {
		l.fillPrice = st.AvgPrice
	}
	switch st.State {
	case "filled", "cancelled", "rejected":
		l.done = true
	}
}

// record describes the leg's order for the journal, at limitPrice.
func (l *pendingLeg) record(trigger string, limitPrice float64) orders.PendingOrderRecord {
	return orders.PendingOrderRecord{
		OrderID: l.orderID, Instrument: l.instrument, OptionType: l.optionType,
		Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: trigger,
		Qty: l.qty, LimitPrice: limitPrice,
	}
}

func (l *pendingLeg) describe() string {
	if l == nil {
		return "skipped"
	}
	return fmt.Sprintf("%s limit=%.6f", l.orderID, l.limitPrice)
}

// pendingStrangle tracks the entry orders of one strangle until every leg is
// done. In repair mode (repairStrangleID set) the filled leg completes an
// existing strangle instead of creating a new one.
type pendingStrangle struct {
	id               string
	targetDTE        int
	entryDelta       float64
	expiry           time.Time
	underlying       string
	call             *pendingLeg // nil when the leg was not opened
	put              *pendingLeg
	submittedAt      time.Time
	adjustments      int
	repairStrangleID string
	complement       bool // sent by the rebalance to bring its slot up to size
}

// slot is the (DTE, delta) slot this entry belongs to.
func (ps *pendingStrangle) slot() *orders.SlotRef {
	return slotRef(ps.targetDTE, ps.entryDelta)
}

func (ps *pendingStrangle) legs() []*pendingLeg {
	var out []*pendingLeg
	for _, l := range []*pendingLeg{ps.call, ps.put} {
		if l != nil {
			out = append(out, l)
		}
	}
	return out
}

func (ps *pendingStrangle) allLegsDone() bool {
	for _, l := range ps.legs() {
		if !l.done {
			return false
		}
	}
	return true
}

func (s *Strategy) addPending(ps *pendingStrangle) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pendingStrangles[ps.id] = ps
}

func (s *Strategy) removePending(id string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pendingStrangles, id)
}

// complementPending reports whether a rebalance complement is still working.
func (s *Strategy) complementPending() bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for _, ps := range s.pendingStrangles {
		if ps.complement {
			return true
		}
	}
	return false
}

// trigger is the journal reason of this pending strangle's sells.
func (ps *pendingStrangle) trigger() string {
	switch {
	case ps.repairStrangleID != "":
		return orders.TriggerRepair
	case ps.complement:
		return orders.TriggerRebalanceUpsize
	}
	return orders.TriggerEntry
}

// priceFloor is how far this pending strangle's sells may step down.
func (s *Strategy) priceFloor(trigger string) string {
	if trigger == orders.TriggerRepair {
		return s.cfg.RepairPriceFloor
	}
	return s.cfg.EntryPriceFloor
}

// underfilled reports whether any submitted leg filled less than requested.
func (ps *pendingStrangle) underfilled() bool {
	for _, l := range ps.legs() {
		if l.filledQty < l.qty-qtyEpsilon {
			return true
		}
	}
	return false
}

func (s *Strategy) pendingSnapshot() []*pendingStrangle {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	out := make([]*pendingStrangle, 0, len(s.pendingStrangles))
	for _, ps := range s.pendingStrangles {
		out = append(out, ps)
	}
	return out
}

func (s *Strategy) pendingSlots() map[slotKey]bool {
	m := make(map[slotKey]bool)
	for _, ps := range s.pendingSnapshot() {
		if ps.repairStrangleID == "" {
			m[makeSlotKey(ps.targetDTE, ps.entryDelta)] = true
		}
	}
	return m
}

func (s *Strategy) repairPending(strangleID string) bool {
	for _, ps := range s.pendingSnapshot() {
		if ps.repairStrangleID == strangleID {
			return true
		}
	}
	return false
}

// checkPendingOrders polls every pending strangle: records fills, amends
// drifted prices and abandons orders that exceeded the fill timeout.
func (s *Strategy) checkPendingOrders(ctx context.Context) {
	snapshot := s.pendingSnapshot()
	if len(snapshot) > 0 {
		slog.Debug("checking pending orders", "count", len(snapshot))
	}
	for _, ps := range snapshot {
		s.handlePendingStrangle(ctx, ps)
	}
}

func (s *Strategy) handlePendingStrangle(ctx context.Context, ps *pendingStrangle) {
	timeout := time.Duration(s.cfg.OrderFillTimeoutSec) * time.Second
	age := time.Since(ps.submittedAt)

	for _, leg := range ps.legs() {
		if leg.done {
			continue
		}
		st, err := s.exch.GetOrderState(ctx, leg.orderID)
		if err != nil {
			slog.Warn("pending: could not get order state", "order_id", leg.orderID, "err", err)
			continue
		}
		leg.applyState(st)
		switch st.State {
		case "filled":
			slog.Info("pending: order filled",
				"pending_id", ps.id, "instrument", leg.instrument, "order_id", leg.orderID,
				"fill_price", fmt.Sprintf("%.6f", leg.fillPrice),
				"limit_price", fmt.Sprintf("%.6f", leg.limitPrice),
				"slippage", fmt.Sprintf("%.6f", leg.fillPrice-leg.limitPrice),
			)
		case "cancelled", "rejected":
			slog.Warn("pending: order cancelled/rejected by exchange",
				"pending_id", ps.id, "instrument", leg.instrument, "order_id", leg.orderID,
				"state", st.State, "filled_qty", leg.filledQty)
		default:
			slog.Debug("pending: order still open",
				"pending_id", ps.id, "instrument", leg.instrument, "order_id", leg.orderID,
				"state", st.State, "filled_qty", leg.filledQty,
				"limit_price", fmt.Sprintf("%.6f", leg.limitPrice),
				"timeout_in", (timeout - age).Round(time.Second),
			)
		}
	}

	if ps.allLegsDone() {
		s.finalizePending(ps)
		return
	}

	if age > timeout {
		slog.Info("pending: order timed out — cancelling unfilled legs",
			"pending_id", ps.id,
			"target_dte", ps.targetDTE, "entry_delta", ps.entryDelta,
			"expiry", ps.expiry.Format("2006-01-02"),
			"age", age.Round(time.Second),
			"timeout_sec", s.cfg.OrderFillTimeoutSec,
			"adjustments", ps.adjustments,
		)
		s.abandonPending(ctx, ps, "fill timeout")
		return
	}

	if s.repriceLegs(ctx, ps, age, timeout) {
		ps.adjustments++
	}
}

// repriceLegs moves resting sells to StepDownPrice: the ask, then mid, then
// the trigger's price floor as the fill timeout runs, following the ask when
// it moves more than order_slippage_pct. Moves down are never capped (two
// planned steps, plus any fall of the market toward the order); a re-price up
// to a higher ask counts toward order_max_adjustments. It returns true when a
// leg was re-priced up. The price never goes below min_premium_btc.
func (s *Strategy) repriceLegs(ctx context.Context, ps *pendingStrangle, age, timeout time.Duration) bool {
	floor := s.priceFloor(ps.trigger())
	repricedUp := false
	for _, leg := range ps.legs() {
		if leg.done || leg.limitPrice <= 0 {
			continue
		}
		inst, ok := s.md.GetInstrument(leg.instrument)
		if !ok || !inst.HasQuote() {
			continue
		}
		tick := inst.EffectiveTick(inst.Ask)
		newPrice := StepDownPrice(inst.Bid, inst.Ask, tick, age, timeout, floor)
		if s.cfg.MinPremiumBTC > 0 {
			newPrice = math.Max(newPrice, orders.CeilToStep(s.cfg.MinPremiumBTC, tick))
		}
		newPrice = orders.RoundToStep(newPrice, inst.EffectiveTick(newPrice))
		if newPrice <= 0 {
			continue
		}
		down := newPrice < leg.limitPrice-tick/2
		drift := math.Abs(newPrice-leg.limitPrice) / leg.limitPrice
		if !down && drift <= s.cfg.OrderSlippagePct {
			continue
		}
		if !down && ps.adjustments >= s.cfg.OrderMaxAdjustments {
			continue
		}
		if err := s.exch.AmendOrder(ctx, leg.orderID, leg.qty, newPrice); err != nil {
			slog.Warn("pending: amend failed", "order_id", leg.orderID, "err", err)
			continue
		}
		s.journal.LogAmend(leg.record(ps.trigger(), newPrice), leg.limitPrice,
			s.eventContext(inst, ps.slot()))
		reason := "ask drift"
		if down {
			reason = "step down"
		}
		slog.Info("pending: order re-priced",
			"pending_id", ps.id,
			"instrument", leg.instrument,
			"reason", reason,
			"price_floor", floor,
			"old_price", fmt.Sprintf("%.6f", leg.limitPrice),
			"new_price", fmt.Sprintf("%.6f", newPrice),
			"bid", inst.Bid, "ask", inst.Ask,
			"age", age.Round(time.Second),
			"adjustments", ps.adjustments,
			"max_adjustments", s.cfg.OrderMaxAdjustments,
		)
		leg.limitPrice = newPrice
		repricedUp = repricedUp || !down
	}
	return repricedUp
}

// abandonPending cancels every leg still working, reads back what filled
// before the cancel landed, and finalizes with the filled part. A leg that
// filled is therefore always tracked, even when its partner never did.
func (s *Strategy) abandonPending(ctx context.Context, ps *pendingStrangle, reason string) {
	for _, leg := range ps.legs() {
		s.cancelLeg(ctx, ps, leg, orders.TriggerTimeout, reason)
	}
	s.finalizePending(ps)
}

// cancelLeg cancels one working leg and reads back what filled before the
// cancel landed, so a late fill is still booked when the strangle finalizes.
func (s *Strategy) cancelLeg(ctx context.Context, ps *pendingStrangle, leg *pendingLeg, trigger, reason string) {
	if leg.done || leg.orderID == "" {
		return
	}
	if err := s.exch.Cancel(ctx, leg.orderID); err != nil {
		slog.Error("pending: cancel failed — order may still be working",
			"pending_id", ps.id, "order_id", leg.orderID, "instrument", leg.instrument, "err", err)
	}
	// The order may have (partly) filled between our last poll and the cancel.
	if st, err := s.exch.GetOrderState(ctx, leg.orderID); err == nil {
		leg.applyState(st)
	} else {
		slog.Warn("pending: could not confirm final fill after cancel; startup reconcile will catch any fill",
			"order_id", leg.orderID, "err", err)
	}
	leg.done = true

	s.journal.LogCancelled(leg.record(trigger, leg.limitPrice),
		s.instrumentContext(leg.instrument, ps.slot()))
	slog.Info("pending: leg abandoned",
		"pending_id", ps.id, "instrument", leg.instrument, "order_id", leg.orderID,
		"reason", reason, "filled_qty", leg.filledQty, "requested_qty", leg.qty)
}

// cancelShedSide cancels every pending entry or repair with a working sell
// of the option type a GEX shed is closing. Left working, such an order could
// fill during the shed and be bought straight back at market — paying the
// spread for nothing. The whole entry is cancelled (its other leg too) and
// finalized at once, so whatever filled before the cancel is booked now and
// shed in this same cycle, never left waiting for a partner order; the other
// side is re-entered on its own by the next entry or repair, which the shed
// does not block.
func (s *Strategy) cancelShedSide(ctx context.Context, optType string) {
	for _, ps := range s.pendingSnapshot() {
		hit := false
		for _, leg := range ps.legs() {
			hit = hit || (leg.optionType == optType && !leg.done)
		}
		if !hit {
			continue
		}
		for _, leg := range ps.legs() {
			s.cancelLeg(ctx, ps, leg, orders.TriggerGammaClose, "GEX shed of "+optType+"s")
		}
		s.finalizePending(ps)
	}
}

// finalizePending turns the filled part of a finished pending strangle into
// positions. Normal mode creates a strangle (one-legged if only one leg
// filled — repair then completes it); repair mode fills the missing leg of an
// existing strangle. Nothing filled → the slot is simply released.
func (s *Strategy) finalizePending(ps *pendingStrangle) {
	s.removePending(ps.id)
	if ps.complement && ps.underfilled() {
		s.rearmRebalance(fmt.Sprintf("complement %s filled short of its size", ps.id))
	}

	now := time.Now()
	mkt := s.marketContext()
	ivPct := s.md.IVPercentile()

	build := func(leg *pendingLeg) *orders.Position {
		if leg == nil || leg.filledQty <= qtyEpsilon {
			return nil
		}
		pos := s.newPosition(leg.instrument, leg.optionType, ps.expiry, leg.filledQty, leg.fillPrice, now)
		s.state.AddPosition(pos)
		s.journal.LogOpen(pos,
			orders.Fill{OrderID: leg.orderID, FillPrice: leg.fillPrice, Qty: leg.filledQty, Timestamp: now},
			ps.trigger(), s.instrumentContext(leg.instrument, ps.slot()))
		return pos
	}
	callPos, putPos := build(ps.call), build(ps.put)

	if callPos == nil && putPos == nil {
		slog.Info("pending: nothing filled, slot released for a fresh order next cycle",
			"pending_id", ps.id, "target_dte", ps.targetDTE, "entry_delta", ps.entryDelta)
		return
	}

	if ps.repairStrangleID != "" {
		for _, pos := range []*orders.Position{callPos, putPos} {
			if pos != nil {
				s.state.SetStrangleLeg(ps.repairStrangleID, pos.OptionType, pos)
				slog.Info("strangle repaired: missing leg filled",
					"strangle_id", ps.repairStrangleID, "leg_type", pos.OptionType,
					"instrument", pos.Instrument, "qty", pos.Qty,
					"fill_price", fmt.Sprintf("%.6f", pos.EntryPrice))
			}
		}
		return
	}

	stID := s.state.NextID("st")
	s.state.AddStrangle(&orders.Strangle{
		ID: stID, TargetDTE: ps.targetDTE, EntryDelta: ps.entryDelta,
		CallLeg: callPos, PutLeg: putPos, OpenedAt: now,
	})
	slog.Info("strangle filled and active",
		"strangle_id", stID, "pending_id", ps.id,
		"underlying", ps.underlying, "target_dte", ps.targetDTE,
		"expiry", ps.expiry.Format("2006-01-02"),
		"market_trend", mkt.Trend,
		"call", describePos(callPos), "put", describePos(putPos),
		"adjustments", ps.adjustments,
		"iv_percentile", fmt.Sprintf("%.1f", ivPct),
		"port_net_delta", fmt.Sprintf("%.4f", mkt.NetDelta),
		"port_net_gamma", fmt.Sprintf("%.6f", mkt.NetGamma),
	)
}

func describePos(p *orders.Position) string {
	if p == nil {
		return "none"
	}
	return fmt.Sprintf("%s qty=%.4f fill=%.6f", p.Instrument, p.Qty, p.EntryPrice)
}

// newPosition builds a short position, enriched with the latest market data
// when the instrument is known. A missing instrument never drops the position:
// an untracked short leg would have no stop-loss.
func (s *Strategy) newPosition(instrument, optionType string, expiry time.Time, qty, fillPrice float64, now time.Time) *orders.Position {
	pos := &orders.Position{
		ID:              s.state.NextID("pos"),
		Instrument:      instrument,
		Underlying:      s.cfg.Underlying,
		Expiry:          expiry,
		OptionType:      optionType,
		Side:            orders.DirectionSell, // entries and repairs sell
		Qty:             qty,
		EntryPrice:      fillPrice,
		EntryTime:       now,
		PremiumReceived: fillPrice * qty,
		CurrentMid:      fillPrice,
	}
	if inst, ok := s.md.GetInstrument(instrument); ok {
		pos.Underlying = inst.Underlying
		pos.Strike = inst.Strike
		pos.Expiry = inst.Expiry
		pos.UnderlyingPrice = inst.UnderlyingPrice
		pos.CurrentMid = inst.Mid
		pos.CurrentGreeks = toOrderGreeks(inst)
	} else if _, exp, strike, _, err := marketdata.ParseOptionName(instrument); err == nil {
		pos.Strike = strike
		pos.Expiry = exp
	}
	return pos
}
