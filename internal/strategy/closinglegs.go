package strategy

import (
	"log/slog"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// A leg an exit closes in pieces is "closing" until it is gone (#554). Delta
// drift and the delta exit are threshold rules and the IOC at the ask leaves
// no resting order: with |Δ| on the threshold, one tick back across it made
// the rule fall silent between two pieces, and leg balancing trimmed the other
// leg — stable ETH 2026-10-29/30, four trims, once buying back 100 calls sold
// the same second. The mark holds while the rule fires, and for
// ClosingHold after the last piece once it stops, so a tick back and forth
// does not count as an uneven strangle; after that the strangle is balanced
// as usual.

// ClosingHold is how long a partly closed leg stays marked after its last
// piece once its exit rule no longer fires.
const ClosingHold = time.Duration(config.FixedClosingHoldMinutes) * time.Minute

// ClosingHeld reports whether a leg whose exit last filled a piece at
// lastPiece still counts as closing at now: while its rule fires, and for
// hold after the last piece.
func ClosingHeld(lastPiece, now time.Time, hold time.Duration, ruleFires bool) bool {
	return ruleFires || now.Sub(lastPiece) < hold
}

// isExitTrigger reports whether a close's trigger is an exit rule of the leg
// itself (EvaluateLeg: stop-loss, DTE roll, delta exit, drift, take-profit),
// whose remainder the rule closes on a later cycle.
func isExitTrigger(reason string) bool {
	switch reason {
	case orders.TriggerStopLoss200Pct, orders.TriggerRollout19DTE, orders.TriggerDeltaExit,
		orders.TriggerRolloutDelta, orders.TriggerRolloutROI:
		return true
	}
	return false
}

// noteClosingPiece marks pos as closing after an exit filled part of it, or
// drops the mark once nothing is left (bookClose).
func (s *Strategy) noteClosingPiece(pos *orders.Position, reason string, remaining float64, now time.Time) {
	if remaining <= qtyEpsilon {
		delete(s.closing, pos.ID)
		return
	}
	if !isExitTrigger(reason) {
		return
	}
	if _, marked := s.closing[pos.ID]; !marked {
		slog.Info("exit filled in part: leg marked closing, leg balancing waits for it",
			"instrument", pos.Instrument, "reason", reason, "remaining_qty", remaining,
			"hold_minutes", config.FixedClosingHoldMinutes)
	}
	s.closing[pos.ID] = now
}

// legClosing reports whether pos is marked closing, dropping a mark whose
// hold ran out with the rule no longer firing.
func (s *Strategy) legClosing(pos *orders.Position, now time.Time) bool {
	lastPiece, marked := s.closing[pos.ID]
	if !marked {
		return false
	}
	if ClosingHeld(lastPiece, now, ClosingHold, s.evaluateLeg(pos).Action != ActionNone) {
		return true
	}
	delete(s.closing, pos.ID)
	slog.Info("closing mark cleared: the exit rule stopped firing and the hold ran out",
		"instrument", pos.Instrument, "qty", pos.Qty, "last_piece", lastPiece)
	return false
}

// pruneClosing drops the marks of positions no longer in the book (closed
// another way, or matched to the exchange).
func (s *Strategy) pruneClosing() {
	for id := range s.closing {
		if _, ok := s.state.GetPosition(id); !ok {
			delete(s.closing, id)
		}
	}
}
