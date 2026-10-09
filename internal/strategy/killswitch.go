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
//  2. Book what working entries filled before the cancel (2026-10-09: an
//     entry had filled 100 of 703 lots; the pending was dropped unread and
//     the 100 stayed short after "all positions closed").
//  3. Buy back every position at market, the book first matched to the
//     exchange's shorts on each attempt, retrying partial fills.
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
		for _, leg := range ps.legs() {
			s.settleLeg(kctx, ps, leg, orders.TriggerKillSwitch, "kill switch")
		}
		s.finalizePending(kctx, ps)
	}

	for attempt := 1; attempt <= killSwitchAttempts; attempt++ {
		if err := s.syncBookToExchange(kctx); err != nil {
			slog.Error("kill switch: could not read the exchange's positions — flattening the book as it is", "attempt", attempt, "err", err)
		}
		open := s.state.AllPositions()
		if len(open) == 0 {
			break
		}
		for _, pos := range open {
			if _, err := s.buyToClose(kctx, pos, pos.Qty, orders.TriggerKillSwitch, 0, "kill switch: flattening every position at market"); err != nil {
				slog.Error("kill switch close failed", "attempt", attempt, "instrument", pos.Instrument, "err", err)
			}
		}
	}

	if err := s.syncBookToExchange(kctx); err != nil {
		slog.Error("kill switch: could not confirm the exchange is flat — check it manually", "err", err)
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
