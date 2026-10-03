package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"

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
// answer at once, so there is never a resting close order to track.
func (s *Strategy) buyToClose(ctx context.Context, pos *orders.Position, qty float64, reason string, limitPrice float64) (float64, error) {
	qty = math.Min(qty, pos.Qty)
	order := orders.Order{
		Instrument:    pos.Instrument,
		Direction:     orders.DirectionBuy,
		OrderType:     orders.TypeMarket,
		Qty:           qty,
		TriggerReason: reason,
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
		return 0, fmt.Errorf("buy to close %s: %w", pos.Instrument, err)
	}
	filled := math.Min(fill.Qty, pos.Qty)
	if filled <= qtyEpsilon {
		return 0, nil
	}

	// Journal the part that closed, with its share of the premium, so the
	// logged P&L matches what was realised.
	closed := *pos
	closed.Qty = filled
	closed.PremiumReceived = pos.PremiumReceived * filled / pos.Qty
	s.journal.LogClose(&closed, fill, s.md.IVPercentile(), reason, s.marketContext(), s.gexContext())

	remaining := pos.Qty - filled
	if remaining <= qtyEpsilon {
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)
		return filled, nil
	}
	slog.Warn("partial close: remainder stays open and tracked",
		"instrument", pos.Instrument, "reason", reason,
		"filled_qty", filled, "remaining_qty", remaining)
	s.state.UpdatePositionQty(pos.ID, remaining, pos.PremiumReceived*remaining/pos.Qty)
	return filled, nil
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
		"unrealised_pnl", fmt.Sprintf("%.4f", pos.MtMPnL()),
	)

	switch d.Action {
	case ActionStopLoss:
		// Market order: getting out matters more than the price.
		startQty := pos.Qty
		filled, err := s.buyToClose(ctx, pos, pos.Qty, orders.TriggerStopLoss200Pct, 0)
		if err != nil {
			slog.Error("stop loss close failed", "err", err, "instrument", pos.Instrument)
			return
		}
		slog.Warn("stop loss triggered",
			"instrument", pos.Instrument,
			"closed_qty", filled, "position_qty", startQty,
			"loss_pct", fmt.Sprintf("%.2f", pos.LossPct()),
		)

	case ActionRollNextMonth, ActionRollSameLeg:
		// Pay at most the current ask: a marketable limit that fills like a
		// market order in a normal book but cannot sweep a thin one.
		inst, ok := s.md.GetInstrument(pos.Instrument)
		if !ok || inst.Ask <= 0 {
			slog.Warn("rollout close skipped: no ask quote, retrying next cycle",
				"instrument", pos.Instrument, "reason", d.Reason)
			return
		}
		filled, err := s.buyToClose(ctx, pos, pos.Qty, d.Reason, inst.Ask)
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
// risk (puts in a confirmed down-move, calls in a confirmed up-move).
func (s *Strategy) handleGammaAction(ctx context.Context, dec GammaDecision) {
	for _, pos := range s.state.AllPositions() {
		shouldClose := (dec.Action == GammaActionClosePuts && pos.OptionType == "put") ||
			(dec.Action == GammaActionCloseCalls && pos.OptionType == "call")
		if !shouldClose {
			continue
		}
		if _, err := s.buyToClose(ctx, pos, pos.Qty, orders.TriggerGammaClose, 0); err != nil {
			slog.Error("gamma close failed", "err", err, "instrument", pos.Instrument)
		}
	}
}
