package strategy

import (
	"context"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
)

const (
	// killSwitchTimeout bounds the whole flatten, independent of shutdown.
	killSwitchTimeout = 2 * time.Minute
	// killSwitchAttempts is how often a partially filled close is retried.
	killSwitchAttempts = 3
)

// killSwitch flattens the book and then halts trading:
//
//  1. Cancel every resting order first, so no entry can fill after the flatten.
//  2. Buy back every position at market, retrying partial fills.
//  3. Stay idle until the process is stopped. Returning would let the
//     supervisor (Docker restart: unless-stopped) restart the bot, which would
//     immediately open new positions — the opposite of a kill switch.
//
// It runs on a context detached from shutdown, so a SIGTERM that arrives at
// the same moment cannot abort the flatten half-way.
func (s *Strategy) killSwitch(ctx context.Context) error {
	slog.Warn("kill switch: cancelling orders and flattening all positions at market")
	s.setHalted()
	kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), killSwitchTimeout)
	defer cancel()

	if err := s.exch.CancelAllOrders(kctx, s.cfg.Underlying); err != nil {
		slog.Error("kill switch: cancel all orders failed", "err", err)
	} else {
		clear(s.unconfirmed) // every order is cancelled: nothing unknown can still fill
	}
	for _, ps := range s.pendingSnapshot() {
		s.removePending(ps.id)
	}

	for attempt := 1; attempt <= killSwitchAttempts; attempt++ {
		open := s.state.AllPositions()
		if len(open) == 0 {
			break
		}
		for _, pos := range open {
			if _, err := s.buyToClose(kctx, pos, pos.Qty, orders.TriggerKillSwitch, 0); err != nil {
				slog.Error("kill switch close failed", "attempt", attempt, "instrument", pos.Instrument, "err", err)
			}
		}
	}

	if left := s.state.AllPositions(); len(left) > 0 {
		for _, pos := range left {
			slog.Error("kill switch: position still open — close it manually",
				"instrument", pos.Instrument, "qty", pos.Qty)
		}
	} else {
		slog.Warn("kill switch: all positions closed")
	}

	s.publish()
	slog.Warn("kill switch complete — trading halted; restart the bot to resume")
	<-ctx.Done()
	return ErrKillSwitch
}
