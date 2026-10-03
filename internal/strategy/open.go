package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// maybeOpenStrangles fills every vacant (DTE, delta) slot, sharing the free
// margin budget equally between the slots that still need a strangle.
// A slot is vacant when no open strangle and no pending entry occupies it.
func (s *Strategy) maybeOpenStrangles(ctx context.Context, gammaDec GammaDecision) error {
	occupied := s.occupiedSlots()
	slots := s.cfg.Slots()
	needsOpen := 0
	for _, slot := range slots {
		if !occupied[makeSlotKey(slot.TargetDTE, slot.EntryDelta)] {
			needsOpen++
		}
	}
	if needsOpen == 0 {
		return nil
	}

	equity, initialMarginUsed, err := s.fetchMarginState(ctx)
	if err != nil {
		return err
	}
	allowed := s.marginGuard.AllowedMargin(equity)
	budget := allowed - initialMarginUsed

	unit := strings.ToLower(s.cfg.Underlying)
	if budget <= 0 {
		slog.Debug("skip entry: margin limit",
			"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
			"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
			"slots_needed", needsOpen,
		)
		for _, slot := range slots {
			if !occupied[makeSlotKey(slot.TargetDTE, slot.EntryDelta)] {
				s.noteSkip(slot.TargetDTE, slot.EntryDelta, SkipMarginLimit,
					fmt.Sprintf("allowed %.6f, used %.6f", allowed, initialMarginUsed))
			}
		}
		return nil
	}
	targetMarginPerSlot := budget / float64(needsOpen)
	instruments := s.md.AllInstruments()

	for _, slot := range slots {
		if occupied[makeSlotKey(slot.TargetDTE, slot.EntryDelta)] {
			continue
		}

		expiry, ok := s.slotExpiry(instruments, slot.TargetDTE, slot.EntryDelta)
		if !ok {
			slog.Info("skip slot: no suitable expiry available",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			s.noteSkip(slot.TargetDTE, slot.EntryDelta, SkipNoExpiry, "")
			continue
		}

		call, err := SelectStrike(instruments, expiry, "call", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: call strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			s.noteSkip(slot.TargetDTE, slot.EntryDelta, SkipNoStrike, err.Error())
			continue
		}
		put, err := SelectStrike(instruments, expiry, "put", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: put strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			s.noteSkip(slot.TargetDTE, slot.EntryDelta, SkipNoStrike, err.Error())
			continue
		}

		qty := s.resolveQty(ctx, call, put, targetMarginPerSlot)
		slog.Debug("entry margin check",
			"target_dte", slot.TargetDTE,
			"entry_delta", slot.EntryDelta,
			"expiry", expiry.Format("2006-01-02"),
			"call", call.Name, "call_delta", fmt.Sprintf("%.4f", call.Greeks.Delta), "call_mid", fmt.Sprintf("%.6f", call.Mid),
			"put", put.Name, "put_delta", fmt.Sprintf("%.4f", put.Greeks.Delta), "put_mid", fmt.Sprintf("%.6f", put.Mid),
			"qty_"+unit, fmt.Sprintf("%.6f", qty),
			"target_margin_per_slot_"+unit, fmt.Sprintf("%.6f", targetMarginPerSlot),
		)
		if err := s.openStrangle(ctx, call, put, slot.TargetDTE, slot.EntryDelta, qty, gammaDec); err != nil {
			slog.Warn("open strangle failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta, "err", err)
			s.noteSkip(slot.TargetDTE, slot.EntryDelta, SkipEntryRejected, err.Error())
			continue
		}
		delete(s.lastSkip, makeSlotKey(slot.TargetDTE, slot.EntryDelta))
	}
	return nil
}

// Skip reasons journaled when a vacant slot is not entered.
const (
	SkipMarginLimit   = "margin_limit"
	SkipNoExpiry      = "no_expiry"
	SkipNoStrike      = "no_strike"
	SkipEntryRejected = "entry_rejected" // premium floor, lot size or order error
)

// noteSkip journals why a slot stayed empty, with the market at that moment.
// The same reason is written once until it changes or the slot is filled,
// so a slot waiting for an expiry does not write a line every cycle.
func (s *Strategy) noteSkip(dte int, delta float64, reason, detail string) {
	key := makeSlotKey(dte, delta)
	if s.lastSkip[key] == reason {
		return
	}
	s.lastSkip[key] = reason
	text := reason
	if detail != "" {
		text += ": " + detail
	}
	s.journal.LogSkipped(text, s.eventContext(nil, slotRef(dte, delta)))
}

// occupiedSlots returns the slots taken by open strangles or pending entries.
// A one-legged strangle still occupies its slot: repair completes it.
func (s *Strategy) occupiedSlots() map[slotKey]bool {
	occupied := s.pendingSlots()
	for _, st := range s.state.AllStrangles() {
		occupied[makeSlotKey(st.TargetDTE, st.EntryDelta)] = true
	}
	return occupied
}

// slotExpiry picks the expiry for a slot. When the primary expiry already holds
// a strangle at the same delta (another slot landed there), it falls back to
// the next suitable expiry so two slots never stack on one expiry.
func (s *Strategy) slotExpiry(instruments []*marketdata.Instrument, targetDTE int, delta float64) (time.Time, bool) {
	expiry, ok := SelectExpiry(instruments, time.Now(), targetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE)
	if !ok {
		return time.Time{}, false
	}
	occupied := s.occupiedExpiriesForDelta(delta)
	if !occupied[expiry] {
		return expiry, true
	}
	expiry, ok = SelectExpiryFallback(instruments, time.Now(), targetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE, occupied)
	if ok {
		slog.Debug("slot: using fallback expiry",
			"target_dte", targetDTE, "entry_delta", delta,
			"fallback_expiry", expiry.Format("2006-01-02"))
	}
	return expiry, ok
}

// occupiedExpiriesForDelta returns the expiries that already hold a strangle
// opened at delta (compared in hundredths, so 0.16 == 0.160000001).
func (s *Strategy) occupiedExpiriesForDelta(delta float64) map[time.Time]bool {
	dKey := makeSlotKey(0, delta).DeltaX100
	occupied := make(map[time.Time]bool)
	for _, st := range s.state.AllStrangles() {
		if makeSlotKey(0, st.EntryDelta).DeltaX100 != dKey {
			continue
		}
		if st.CallLeg != nil {
			occupied[st.CallLeg.Expiry] = true
		}
		if st.PutLeg != nil {
			occupied[st.PutLeg.Expiry] = true
		}
	}
	return occupied
}

// openStrangle submits limit sells for both legs (or one leg when GEX is
// shedding the other) and tracks them as a pending strangle until they fill.
func (s *Strategy) openStrangle(ctx context.Context, call, put *marketdata.Instrument, targetDTE int, entryDelta, qty float64, gammaDec GammaDecision) error {
	// Deribit enforces per-instrument minimums (e.g. 0.1 BTC); an amount off
	// that grid is rejected, so snap qty down onto it.
	if exchStep := math.Max(call.MinTradeAmount, put.MinTradeAmount); exchStep > 0 {
		qty = orders.FloorToStep(qty, exchStep)
		if qty < exchStep {
			return fmt.Errorf("qty %.6f below exchange minimum %.6f (call=%s put=%s)",
				qty, exchStep, call.Name, put.Name)
		}
	}

	// Skip a leg only when GEX is actively shedding that leg type.
	openCall := gammaDec.Action != GammaActionCloseCalls
	openPut := gammaDec.Action != GammaActionClosePuts
	if !openCall || !openPut {
		skipped := "put"
		if !openCall {
			skipped = "call"
		}
		slog.Info("single-leg entry due to GEX regime",
			"regime", gammaDec.Regime, "action", actionLabel(gammaDec.Action),
			"skipping", skipped, "target_dte", targetDTE)
	}

	// Check the premium floor for every leg before submitting any order, so a
	// cheap put can never leave a lone call behind.
	if openCall {
		if err := s.checkPremiumFloor(call); err != nil {
			return err
		}
	}
	if openPut {
		if err := s.checkPremiumFloor(put); err != nil {
			return err
		}
	}

	ps := &pendingStrangle{
		id:          s.state.NextID("ps"),
		targetDTE:   targetDTE,
		entryDelta:  entryDelta,
		expiry:      call.Expiry,
		underlying:  call.Underlying,
		submittedAt: time.Now(),
	}

	if openCall {
		leg, err := s.submitEntryLeg(ctx, call, qty, slotRef(targetDTE, entryDelta))
		if err != nil {
			return fmt.Errorf("sell call: %w", err)
		}
		ps.call = leg
	}
	if openPut {
		leg, err := s.submitEntryLeg(ctx, put, qty, slotRef(targetDTE, entryDelta))
		if err != nil {
			// The call may already be resting or (partly) filled. Cancel what is
			// left and keep whatever filled, so no short leg goes untracked.
			if ps.call != nil {
				s.abandonPending(ctx, ps, "put leg failed")
			}
			return fmt.Errorf("sell put: %w", err)
		}
		ps.put = leg
	}

	if ps.allLegsDone() {
		s.finalizePending(ps)
		return nil
	}
	s.addPending(ps)

	slog.Info("strangle orders submitted, awaiting fill",
		"pending_id", ps.id,
		"target_dte", targetDTE,
		"entry_delta", entryDelta,
		"expiry", call.Expiry.Format("2006-01-02"),
		"gex_regime", gammaDec.Regime,
		"call", ps.call.describe(),
		"put", ps.put.describe(),
		"qty", fmt.Sprintf("%.6f", qty),
		"fill_timeout_sec", s.cfg.OrderFillTimeoutSec,
	)
	return nil
}

// entryLimitPrice is the price for a short entry: max(mid, ask). Using the ask
// when the bid is zero avoids Deribit's price_too_low rejection.
func entryLimitPrice(inst *marketdata.Instrument) float64 {
	return math.Max(inst.Mid, inst.Ask)
}

func (s *Strategy) checkPremiumFloor(inst *marketdata.Instrument) error {
	price := entryLimitPrice(inst)
	if s.cfg.MinPremiumBTC > 0 && price < s.cfg.MinPremiumBTC {
		return fmt.Errorf("%s premium %.6f below floor %.6f — skipping (%s)",
			inst.OptionType, price, s.cfg.MinPremiumBTC, inst.Name)
	}
	return nil
}

// submitEntryLeg places one limit sell for slot and journals the submission
// with the market snapshot at that moment.
func (s *Strategy) submitEntryLeg(ctx context.Context, inst *marketdata.Instrument, qty float64, slot *orders.SlotRef) (*pendingLeg, error) {
	price := entryLimitPrice(inst)
	fill, err := s.exch.Submit(ctx, orders.Order{
		Instrument:    inst.Name,
		Direction:     orders.DirectionSell,
		OrderType:     orders.TypeLimit,
		Qty:           qty,
		LimitPrice:    price,
		TickSize:      inst.EffectiveTick(price),
		TriggerReason: orders.TriggerEntry,
		Label:         s.orderLabel(slot),
	})
	if err != nil {
		return nil, err
	}
	s.journal.LogSubmit(orders.PendingOrderRecord{
		OrderID: fill.OrderID, Instrument: inst.Name, OptionType: inst.OptionType,
		Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: orders.TriggerEntry,
		Qty: qty, LimitPrice: price, Greeks: toOrderGreeks(inst),
	}, s.eventContext(inst, slot))
	return newPendingLeg(fill, inst.Name, inst.OptionType, qty, price), nil
}

// resolveQty sizes a strangle from the Portfolio Margin budget. It asks
// private/get_margins for the initial margin of one minimum lot per leg and
// fits as many whole lots as targetMarginPerSlot allows (see ComputeQtyFromIM).
// Any error falls back to one minimum lot — the smallest, safest size.
func (s *Strategy) resolveQty(ctx context.Context, call, put *marketdata.Instrument, targetMarginPerSlot float64) float64 {
	exchMin := math.Max(call.MinTradeAmount, put.MinTradeAmount)
	if exchMin <= 0 {
		exchMin = s.cfg.MinTradeAmount
	}

	callInfo, err := s.exch.GetMargins(ctx, call.Name, exchMin, entryLimitPrice(call))
	if err != nil {
		slog.Warn("resolveQty: GetMargins failed for call, using exchange minimum",
			"instrument", call.Name, "err", err)
		return exchMin
	}
	putInfo, err := s.exch.GetMargins(ctx, put.Name, exchMin, entryLimitPrice(put))
	if err != nil {
		slog.Warn("resolveQty: GetMargins failed for put, using exchange minimum",
			"instrument", put.Name, "err", err)
		return exchMin
	}

	qty := ComputeQtyFromIM(exchMin, targetMarginPerSlot, callInfo.InitialMargin, putInfo.InitialMargin)
	unit := strings.ToLower(s.cfg.Underlying)
	imTotal := callInfo.InitialMargin + putInfo.InitialMargin
	sizingMethod := "pm_aware"
	if imTotal <= 0 || qty == exchMin {
		sizingMethod = "exchange_minimum"
	}
	slog.Info("resolveQty",
		"call", call.Name, "put", put.Name,
		"sizing_method", sizingMethod,
		"exch_min_"+unit, fmt.Sprintf("%.4f", exchMin),
		"call_im_per_lot", fmt.Sprintf("%.6f", callInfo.InitialMargin),
		"put_im_per_lot", fmt.Sprintf("%.6f", putInfo.InitialMargin),
		"im_per_lot_total", fmt.Sprintf("%.6f", imTotal),
		"target_margin_per_slot_"+unit, fmt.Sprintf("%.6f", targetMarginPerSlot),
		"qty_"+unit, fmt.Sprintf("%.4f", qty),
	)
	return qty
}

// toOrderGreeks copies an instrument's greeks into the orders package type.
func toOrderGreeks(inst *marketdata.Instrument) orders.Greeks {
	return orders.Greeks{
		Delta: inst.Greeks.Delta,
		Gamma: inst.Greeks.Gamma,
		Theta: inst.Greeks.Theta,
		Vega:  inst.Greeks.Vega,
		Rho:   inst.Greeks.Rho,
		IV:    inst.Greeks.IV,
	}
}
