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

// Non-urgent exits work a cheaper price first (#239, the product owner's
// option B). A take-profit, a delta drift or a DTE roll is not a race: the
// leg has earned its premium or is simply old. Instead of an IOC at the ask,
// which gives away about half the spread, the buy-back rests at the mid
// (rounded down a tick) for one fill window (order_fill_timeout_sec, the
// entries' window); whatever is left then is bought with an IOC at the ask of
// that moment, as before.
//
// While it rests the leg stays in the book with every other exit live: a
// stop-loss, a delta exit, the MM cut, a GEX shed, a manual close or the kill
// switch goes through buyToClose, which cancels the resting order and books
// its fill first (buyToCloseNote), so the urgent exit runs at once on what is
// left. The order has a unique label (a lost submit is cancelled by label like
// any order, orphans.go) and the startup reconcile cancels every order.

// Exit steps, journaled as "exit_step" on a non-urgent exit's close.
const (
	ExitStepMid = "mid" // its resting limit at the mid filled
	ExitStepAsk = "ask" // bought with an IOC at the ask
)

// RestsAtMid reports whether an exit decision first rests at the mid: the
// take-profit and the delta drift (ActionRollSameLeg) and the DTE roll.
// Stop-loss and delta exit cross at once — getting out matters more there.
func RestsAtMid(a RolloutAction) bool {
	return a == ActionRollSameLeg || a == ActionRollNextMonth
}

// RestingClosePrice is the buy limit a non-urgent exit rests at: the mid of
// bid and ask rounded down to tick, strictly under the ask. ok is false when
// there is no two-sided quote or no tick of room under the ask; the exit then
// crosses at the ask at once.
func RestingClosePrice(bid, ask, tick float64) (price float64, ok bool) {
	if bid <= 0 || ask <= bid || tick <= 0 {
		return 0, false
	}
	price = orders.FloorToStep((bid+ask)/2, tick)
	if price <= 0 || price > ask-tick/2 {
		return 0, false
	}
	return price, true
}

// exitQuote is the book when a non-urgent exit triggered, for the journal.
type exitQuote struct{ bid, mid, ask float64 }

func (q exitQuote) String() string {
	return fmt.Sprintf("bid %.4f / mid %.4f / ask %.4f at the trigger", q.bid, q.mid, q.ask)
}

// restingClose is one non-urgent exit's buy limit resting at the mid.
type restingClose struct {
	posID      string
	instrument string
	optionType string
	orderID    string
	label      string
	slot       *orders.SlotRef
	reason     string // the exit's trigger (rollout_roi, rollout_delta, rollout_19dte)
	detail     string
	qty        float64
	limitPrice float64
	quote      exitQuote
	since      time.Time
	// What is already booked: filled amount, its cost and the fee read so far.
	filledQty  float64
	filledCost float64
	feeBooked  float64
}

func (rc *restingClose) record() orders.PendingOrderRecord {
	return orders.PendingOrderRecord{
		OrderID: rc.orderID, Instrument: rc.instrument, OptionType: rc.optionType,
		Direction: orders.DirectionBuy, OrderType: orders.TypeLimit, TriggerReason: rc.reason,
		Qty: rc.qty, LimitPrice: rc.limitPrice, FilledQty: rc.filledQty,
	}
}

// restingOn reports whether a resting close works on instrument.
func (s *Strategy) restingOn(instrument string) bool {
	for _, rc := range s.restingCloses {
		if rc.instrument == instrument {
			return true
		}
	}
	return false
}

// startRestingClose sends a non-urgent exit's buy limit at the mid. It
// returns false when the exit must cross at the ask now instead: its mid rest
// already ran out (askStep), the quote leaves no room under the ask, or
// buyToClose must answer (an unknown earlier order). True means this cycle's
// exit is handled (resting, filled at once, or the submit failed and is
// retried next cycle).
func (s *Strategy) startRestingClose(ctx context.Context, pos *orders.Position, d RolloutDecision, inst *marketdata.Instrument) bool {
	if _, done := s.askStep[pos.ID]; done || !inst.HasQuote() || s.instrumentUnconfirmed(pos.Instrument) {
		return false
	}
	price, ok := RestingClosePrice(inst.Bid, inst.Ask, inst.EffectiveTick(inst.Mid))
	if !ok {
		return false
	}
	qty := s.capToExchange(pos.Instrument, pos.Qty)
	if qty <= qtyEpsilon {
		return true // nothing short on the exchange: the position check adopts that
	}
	slot := s.slotOf(pos.ID)
	order := orders.Order{
		Instrument:    pos.Instrument,
		Direction:     orders.DirectionBuy,
		OrderType:     orders.TypeLimit,
		Qty:           qty,
		LimitPrice:    price,
		TickSize:      inst.EffectiveTick(price),
		TriggerReason: d.Reason,
		Label:         s.uniqueOrderLabel(slot),
	}
	fill, err := s.exch.Submit(ctx, order)
	if err != nil {
		if orders.MaybePlaced(err) {
			s.noteUnconfirmed(order.Label, pos.Instrument, slot, err)
		}
		slog.Warn("resting close failed, retrying next cycle", "instrument", pos.Instrument, "reason", d.Reason, "err", err)
		return true
	}
	rc := &restingClose{
		posID: pos.ID, instrument: pos.Instrument, optionType: pos.OptionType,
		orderID: fill.OrderID, label: order.Label, slot: slot,
		reason: d.Reason, detail: d.Detail, qty: qty, limitPrice: price,
		quote: exitQuote{bid: inst.Bid, mid: (inst.Bid + inst.Ask) / 2, ask: inst.Ask},
		since: time.Now(),
	}
	s.restingCloses[pos.ID] = rc
	s.journal.LogSubmit(rc.record(), s.eventContext(inst, slot))
	slog.Info("rollout close resting at the mid; crosses at the ask after the fill window",
		"instrument", pos.Instrument, "reason", d.Reason, "order_id", rc.orderID, "label", rc.label,
		"qty", qty, "limit_price", fmt.Sprintf("%.6f", price), "bid", inst.Bid, "ask", inst.Ask,
		"window_sec", s.cfg.OrderFillTimeoutSec)
	state := "open"
	if fill.Qty >= qty-qtyEpsilon {
		state = "filled"
	}
	s.applyRestingState(ctx, rc, orders.OrderStateInfo{OrderID: fill.OrderID, State: state, FilledAmount: fill.Qty, AvgPrice: fill.FillPrice})
	return true
}

// checkRestingCloses follows every resting close: books what filled, drops a
// finished one and, once the fill window is over, cancels it — the leg's rule
// is evaluated again this cycle and, if it still applies, crosses at the ask.
func (s *Strategy) checkRestingCloses(ctx context.Context) {
	for id := range s.askStep {
		if _, ok := s.state.GetPosition(id); !ok {
			delete(s.askStep, id)
		}
	}
	window := time.Duration(s.cfg.OrderFillTimeoutSec) * time.Second
	for _, rc := range s.restingCloses {
		if _, ok := s.state.GetPosition(rc.posID); !ok {
			// The leg left the book another way (the position check): the
			// order must not fill into nothing.
			s.endRestingClose(ctx, rc, true, "position no longer in the book")
			continue
		}
		if st, err := s.exch.GetOrderState(ctx, rc.orderID); err != nil {
			slog.Warn("resting close: could not get order state", "order_id", rc.orderID, "err", err)
		} else if s.applyRestingState(ctx, rc, st) {
			continue
		}
		if time.Since(rc.since) > window {
			if _, ok := s.endRestingClose(ctx, rc, true, "fill window over"); ok {
				s.askStep[rc.posID] = rc.quote
			}
		}
	}
}

// applyRestingState books a resting close's new fills from its order state
// and reports whether the order is finished (then dropped from tracking). An
// order the exchange ended unfilled leaves the rest to the ask step.
func (s *Strategy) applyRestingState(ctx context.Context, rc *restingClose, st orders.OrderStateInfo) bool {
	s.bookRestingFill(ctx, rc, st)
	switch st.State {
	case "filled", "cancelled", "rejected":
	default:
		return false
	}
	delete(s.restingCloses, rc.posID)
	if st.State != "filled" {
		s.askStep[rc.posID] = rc.quote
		s.journal.LogCancelled(rc.record(), s.instrumentContext(rc.instrument, rc.slot))
		slog.Warn("resting close ended by the exchange; the rest crosses at the ask",
			"instrument", rc.instrument, "order_id", rc.orderID, "state", st.State, "filled_qty", rc.filledQty)
		return true
	}
	slog.Info("rollout leg closed at the mid; replacement opens via repair/entry",
		"instrument", rc.instrument, "reason", rc.reason, "closed_qty", rc.filledQty,
		"price", fmt.Sprintf("%.6f", rc.limitPrice))
	return true
}

// endRestingClose stops a resting close (cancel; false when the exchange
// already cancelled it, as the kill switch's cancel-all does), reads back its
// final fill and books it. It returns the quantity booked now and whether the
// order is known to be over; when it is not (the cancel or the read-back
// failed), the order is handed to the unknown-outcome path: cancelled by label
// next cycle and the book matched to the exchange before the instrument trades
// again (orphans.go).
func (s *Strategy) endRestingClose(ctx context.Context, rc *restingClose, cancel bool, why string) (float64, bool) {
	delete(s.restingCloses, rc.posID)
	var cancelErr error
	if cancel {
		cancelErr = s.exch.Cancel(ctx, rc.orderID) // fails too when it just filled: read back below
	}
	st, err := s.exch.GetOrderState(ctx, rc.orderID)
	if err != nil {
		s.noteUnconfirmed(rc.label, rc.instrument, rc.slot, fmt.Errorf("read back after cancel: %w", err))
		return 0, false
	}
	booked := s.bookRestingFill(ctx, rc, st)
	if cancelErr != nil && st.State == "open" {
		s.noteUnconfirmed(rc.label, rc.instrument, rc.slot, cancelErr)
		return booked, false
	}
	if st.State == "filled" { // it filled whole before the cancel reached it
		slog.Info("resting close filled before its cancel", "instrument", rc.instrument, "order_id", rc.orderID,
			"reason", why, "filled_qty", rc.filledQty, "price", fmt.Sprintf("%.6f", rc.limitPrice))
		return booked, true
	}
	s.journal.LogCancelled(rc.record(), s.instrumentContext(rc.instrument, rc.slot))
	slog.Info("resting close cancelled", "instrument", rc.instrument, "order_id", rc.orderID,
		"reason", why, "filled_qty", rc.filledQty, "requested_qty", rc.qty)
	return booked, true
}

// endAllRestingCloses stops every resting close (the kill switch, shutdown).
func (s *Strategy) endAllRestingCloses(ctx context.Context, cancel bool, why string) {
	for _, rc := range s.restingCloses {
		s.endRestingClose(ctx, rc, cancel, why)
	}
}

// restingSettleTimeout bounds the shutdown settle: well inside Docker's 10 s
// stop grace period.
const restingSettleTimeout = 5 * time.Second

// settleRestingCloses cancels every resting close when Run stops (a deploy,
// SIGTERM) and books what it filled since the last poll: the journal's closes
// are the one source of realised P&L, and the startup reconcile only cancels
// orders — it cannot book a fill that came before. It runs on a detached,
// short context, as the kill switch does; an order it cannot confirm is
// cancelled by the next startup's cancel-all and its position adopted by the
// reconcile.
func (s *Strategy) settleRestingCloses(ctx context.Context) {
	if len(s.restingCloses) == 0 {
		return
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restingSettleTimeout)
	defer cancel()
	slog.Info("shutdown: settling resting closes", "count", len(s.restingCloses))
	s.endAllRestingCloses(sctx, true, "shutdown")
}

// bookRestingFill books what the order filled since the last booking, at the
// average price of that part, and returns the quantity booked.
func (s *Strategy) bookRestingFill(ctx context.Context, rc *restingClose, st orders.OrderStateInfo) float64 {
	delta := st.FilledAmount - rc.filledQty
	if delta <= qtyEpsilon {
		return 0
	}
	avg := st.AvgPrice
	if avg <= 0 {
		avg = rc.limitPrice
	}
	price := (avg*st.FilledAmount - rc.filledCost) / delta
	fee := math.Max(s.orderFee(ctx, rc.orderID)-rc.feeBooked, 0)
	first := rc.filledQty <= qtyEpsilon
	rc.filledQty, rc.filledCost, rc.feeBooked = st.FilledAmount, avg*st.FilledAmount, rc.feeBooked+fee

	pos, ok := s.state.GetPosition(rc.posID)
	if !ok {
		slog.Error("resting close filled but its position left the book; the position check adopts the exchange's",
			"instrument", rc.instrument, "order_id", rc.orderID, "filled_qty", delta)
		return 0
	}
	fill := orders.Fill{OrderID: rc.orderID, FillPrice: price, Qty: delta, Fee: fee, Timestamp: time.Now(), TargetQty: rc.qty}
	note := closeNote{
		detail: fmt.Sprintf("%s — exit step mid: rested at %.4f (%s)", rc.detail, rc.limitPrice, rc.quote),
		step:   ExitStepMid,
	}
	return s.bookClose(pos, fill, rc.reason, orders.TypeLimit, note, rc.slot, first)
}

// askStepNote is the journal note of a non-urgent exit crossing at the ask:
// after its mid rest ran out, or at once when the book left no room for one.
func (s *Strategy) askStepNote(posID, detail string, inst *marketdata.Instrument) closeNote {
	if q, ok := s.askStep[posID]; ok {
		return closeNote{detail: fmt.Sprintf("%s — exit step ask after the mid rest: IOC at %.4f (%s)", detail, inst.Ask, q), step: ExitStepAsk}
	}
	return closeNote{detail: fmt.Sprintf("%s — exit step ask: no room for a mid rest (bid %.4f / ask %.4f)", detail, inst.Bid, inst.Ask), step: ExitStepAsk}
}
