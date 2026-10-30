package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/orders"
)

// buyToClose buys back up to qty of a short position and updates the book
// with what actually filled. Every exit (stop-loss, GEX close, rollout, kill
// switch, rebalance) goes through here, so a partial fill is never mistaken
// for a full close: the remainder stays tracked, with its stop-loss, and the
// rule that triggered the exit simply fires again on the next cycle.
//
// limitPrice 0 sends a market order. A positive limitPrice sends an
// immediate-or-cancel limit, which caps the price paid but still gives a final
// answer at once. The only resting close is a non-urgent exit's limit at the
// mid (restingclose.go): any close sent here for the same leg cancels it first
// and books what it filled, so the urgent exit runs at once on what is left.
//
// detail is the why, with the numbers; it is journaled with the close.
func (s *Strategy) buyToClose(ctx context.Context, pos *orders.Position, qty float64, reason string, limitPrice float64, detail string) (float64, error) {
	return s.buyToCloseNote(ctx, pos, qty, reason, limitPrice, closeNote{detail: detail})
}

// buyToCloseBy is buyToClose for a person's action: actor (non-nil) is
// journaled with the close as "actor" (#171).
func (s *Strategy) buyToCloseBy(ctx context.Context, pos *orders.Position, qty float64, reason string, limitPrice float64, detail string, actor *orders.Actor) (float64, error) {
	return s.buyToCloseNote(ctx, pos, qty, reason, limitPrice, closeNote{detail: detail, actor: actor})
}

// closeNote is what the journal records with a close besides the fill: the
// why (detail), the person behind it (actor) and a non-urgent exit's step.
type closeNote struct {
	detail string
	actor  *orders.Actor
	step   string // "mid" | "ask" for take-profit, delta drift and the DTE roll (#239)
}

func (s *Strategy) buyToCloseNote(ctx context.Context, pos *orders.Position, qty float64, reason string, limitPrice float64, note closeNote) (float64, error) {
	if rc := s.restingCloses[pos.ID]; rc != nil {
		// A non-urgent exit is resting at the mid: stop it and book its fill
		// before this close, or both could fill and leave the account long.
		booked, ok := s.endRestingClose(ctx, rc, true, "pre-empted by "+reason)
		if !ok {
			return 0, fmt.Errorf("buy to close %s: the resting close could not be stopped; resolved next cycle", pos.Instrument)
		}
		cur, still := s.state.GetPosition(pos.ID)
		if !still {
			return 0, nil // the resting close filled it all
		}
		pos, qty = cur, qty-booked
	}
	qty = math.Min(qty, pos.Qty)
	if qty <= qtyEpsilon {
		return 0, nil
	}
	if s.instrumentUnconfirmed(pos.Instrument) {
		// An earlier order may have closed it already: sending another could
		// leave the account long. Resolved next cycle (cancel by label, then
		// the book is matched to the exchange).
		return 0, fmt.Errorf("buy to close %s: an earlier order's outcome is still unknown", pos.Instrument)
	}
	if qty = s.capToExchange(pos.Instrument, qty); qty <= qtyEpsilon {
		return 0, nil // nothing short on the exchange: the position check adopts that
	}
	slot := s.slotOf(pos.ID) // before the close can remove the strangle
	order := orders.Order{
		Instrument:    pos.Instrument,
		Direction:     orders.DirectionBuy,
		OrderType:     orders.TypeMarket,
		Qty:           qty,
		TriggerReason: reason,
		Label:         s.uniqueOrderLabel(slot),
	}
	if limitPrice > 0 {
		order.OrderType = orders.TypeLimit
		order.LimitPrice = limitPrice
		order.TimeInForce = orders.TimeInForceIOC
		if inst, ok := s.md.GetInstrument(pos.Instrument); ok {
			order.TickSize = inst.EffectiveTick(limitPrice)
		}
	}

	fill, err := s.exch.Submit(ctx, order)
	if err != nil {
		if orders.MaybePlaced(err) {
			s.noteUnconfirmed(order.Label, pos.Instrument, slot, err)
		}
		return 0, fmt.Errorf("buy to close %s: %w", pos.Instrument, err)
	}
	return s.bookClose(pos, fill, reason, order.OrderType, note, slot, true), nil
}

// bookClose books a buy-back fill on pos: the journal's close line with the
// part's share of the premium and fees, the slot's realised P&L, and the book
// (the position removed, or its remainder kept tracked). churn false skips the
// churn breaker's count (a later piece of a close already counted once).
// It returns the quantity booked.
func (s *Strategy) bookClose(pos *orders.Position, fill orders.Fill, reason, orderType string, note closeNote, slot *orders.SlotRef, churn bool) float64 {
	filled := math.Min(fill.Qty, pos.Qty)
	if filled <= qtyEpsilon {
		return 0
	}
	s.spendExchangeShort(pos.Instrument, filled)
	s.boughtBack[pos.Instrument] = true
	if churn {
		s.noteChurn(slot, true, time.Now())
	}

	// Journal the part that closed, with its share of the premium, so the
	// logged P&L matches what was realised.
	closed := *pos
	closed.Qty = filled
	closed.PremiumReceived = pos.PremiumReceived * filled / pos.Qty
	closed.Fees = pos.Fees * filled / pos.Qty // its share of the opening fees
	jctx := s.instrumentContext(pos.Instrument, slot)
	jctx.Detail = note.detail
	jctx.Actor = note.actor
	jctx.ExitStep = note.step
	s.journal.LogClose(&closed, fill, reason, orderType, jctx)
	s.pnl.record(slot, orders.ClosedNetPnL(&closed, fill))

	remaining := pos.Qty - filled
	s.noteClosingPiece(pos, reason, remaining, time.Now())
	if remaining <= qtyEpsilon {
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)
		return filled
	}
	slog.Warn("partial close: remainder stays open and tracked",
		"instrument", pos.Instrument, "reason", reason,
		"filled_qty", filled, "remaining_qty", remaining)
	s.state.UpdatePositionQty(pos.ID, remaining, pos.PremiumReceived*remaining/pos.Qty)
	return filled
}

// handleRollout executes one rollout decision. Rollouts only close: the
// replacement is opened by the regular, fill-tracked paths — repair reopens a
// single rolled leg at the strangle's expiry and entry delta, and a strangle
// whose legs are both rolled frees its slot for a fresh entry. This keeps one
// way to open a leg, so a rollout can never open a duplicate.
func (s *Strategy) handleRollout(ctx context.Context, d RolloutDecision) {
	pos, ok := s.state.GetPosition(d.LegID)
	if !ok {
		return
	}

	slog.Info("rollout triggered",
		"instrument", pos.Instrument,
		"action", d.Action,
		"reason", d.Reason,
		"dte", pos.DTE(),
		"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
		"current_mid", fmt.Sprintf("%.4f", pos.CurrentMid),
		"premium_received", fmt.Sprintf("%.4f", pos.PremiumReceived),
		"unrealised_pnl", fmt.Sprintf("%.4f", pos.NetPnL()),
	)

	switch d.Action {
	case ActionStopLoss:
		// Market order: getting out matters more than the price.
		startQty := pos.Qty
		s.noteStopped(pos, time.Now())
		filled, err := s.buyToClose(ctx, pos, pos.Qty, orders.TriggerStopLoss200Pct, 0, d.Detail)
		if err != nil {
			slog.Error("stop loss close failed", "err", err, "instrument", pos.Instrument)
			return
		}
		slog.Warn("stop loss triggered",
			"instrument", pos.Instrument,
			"closed_qty", filled, "position_qty", startQty,
			"loss_pct", fmt.Sprintf("%.2f", pos.LossPct()),
		)

	case ActionRollNextMonth, ActionRollSameLeg, ActionDeltaExit:
		// Pay at most the current ask: a marketable limit that fills like a
		// market order in a normal book but cannot sweep a thin one.
		inst, ok := s.md.GetInstrument(pos.Instrument)
		if !ok || inst.Ask <= 0 {
			slog.Warn("rollout close skipped: no ask quote, retrying next cycle",
				"instrument", pos.Instrument, "reason", d.Reason)
			return
		}
		note := closeNote{detail: d.Detail}
		if d.Action == ActionDeltaExit {
			// Held like a stop-loss: repair re-sells it only once calm.
			s.noteStopped(pos, time.Now())
		} else {
			// Not urgent (take-profit, drift, the DTE roll): rest at the mid
			// for one fill window first, then cross at the ask (#239).
			if s.restingCloses[pos.ID] != nil || s.startRestingClose(ctx, pos, d, inst) {
				return
			}
			note = s.askStepNote(pos.ID, d.Detail, inst)
		}
		filled, err := s.buyToCloseNote(ctx, pos, pos.Qty, d.Reason, inst.Ask, note)
		if err != nil {
			slog.Warn("rollout close failed", "err", err, "instrument", pos.Instrument)
			return
		}
		if filled == 0 {
			slog.Info("rollout close did not fill at the ask, retrying next cycle",
				"instrument", pos.Instrument, "ask", inst.Ask)
			return
		}
		slog.Info("rollout leg closed; replacement opens via repair/entry",
			"instrument", pos.Instrument, "reason", d.Reason,
			"closed_qty", filled, "price", fmt.Sprintf("%.6f", inst.Ask))
	}
}

// handleGammaAction sheds every leg of the type the GEX regime says is at
// risk (puts in a confirmed down-move, calls in a confirmed up-move) once the
// signal has held (dec.Shed), anchoring each at the flip it was shed at.
func (s *Strategy) handleGammaAction(ctx context.Context, dec GammaDecision) {
	// First stop working orders on the shed side from filling into the shed;
	// anything that filled before the cancel is booked and closed below.
	switch dec.Shed {
	case GammaActionClosePuts:
		s.cancelShedSide(ctx, "put")
	case GammaActionCloseCalls:
		s.cancelShedSide(ctx, "call")
	}
	for _, pos := range s.state.AllPositions() {
		shouldClose := (dec.Shed == GammaActionClosePuts && pos.OptionType == "put") ||
			(dec.Shed == GammaActionCloseCalls && pos.OptionType == "call")
		if !shouldClose {
			continue
		}
		s.noteShed(pos, dec, time.Now())
		detail := shedDetail(dec, s.md.UnderlyingPrice(), pos.OptionType)
		if _, err := s.buyToClose(ctx, pos, pos.Qty, orders.TriggerGammaClose, 0, detail); err != nil {
			slog.Error("gamma close failed", "err", err, "instrument", pos.Instrument)
		}
	}
}

// shedDetail is the journal's reason for a GEX shed, in the terms of the
// direction source that shed it.
func shedDetail(dec GammaDecision, spot float64, optType string) string {
	if dec.Direction == DirectionBias {
		side := "above"
		if dec.BiasDir < 0 {
			side = "below"
		}
		return fmt.Sprintf("GEX shed (bias %s): regime %s, spot %.0f is more than the %.1f%% buffer %s the flip %.0f (median %.0f) — shedding %ss",
			dec.Bias, dec.Regime, spot, dec.FlipBufferPct, side, dec.GammaFlip, dec.BiasFlip, optType)
	}
	return fmt.Sprintf("GEX shed: regime %s, spot %.0f is more than the %.1f%% buffer below the flip %.0f, trend %s — shedding %ss",
		dec.Regime, spot, dec.FlipBufferPct, dec.GammaFlip, dec.Trend, optType)
}
