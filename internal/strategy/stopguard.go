package strategy

import (
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
)

// A stop-loss buys back at market. In a flash wick (2025-10-10: the ETH index
// fell ~18 % and came back within minutes) market makers pull their quotes,
// and the first price the bot sees can be the bottom of an empty book: in
// stress runs the ask there was 35× the premium and today's stop paid it on
// the whole leg, a minute before the price came back. The spread guard waits
// while the book is empty — the ask more than stop_spread_guard_pct above the
// mid — and re-checks every cycle: once the spread is normal the stop fires
// if it still applies, or the leg is kept if the price came back under it.
// It waits stop_spread_max_wait_minutes at most, then buys at market. The MM
// cut never waits. Stress-tested (2026-10-08): better in every index-size
// flash case, identical on all 38 ordinary paths (spreads never got that wide).

// StopDeferReason says why a triggered stop-loss waits, or "" when it fires
// now. since is when this stop was first deferred (zero = not yet).
func StopDeferReason(bid, ask float64, since, now time.Time, guardPct float64, maxWait time.Duration) string {
	if guardPct <= 0 || ask <= 0 {
		return "" // off, or nothing to read: at market as before
	}
	if !since.IsZero() && now.Sub(since) >= maxWait {
		return ""
	}
	if bid <= 0 {
		return fmt.Sprintf("no bid (ask %.4f): the book is empty", ask)
	}
	mid := (bid + ask) / 2
	if over := (ask/mid - 1) * 100; over > guardPct {
		return fmt.Sprintf("ask %.4f is %.0f%% above mid %.4f (guard %.0f%%): the book is empty", ask, over, mid, guardPct)
	}
	return ""
}

// deferStop reports whether pos's triggered stop-loss waits this cycle,
// journaling the first wait of each episode.
func (s *Strategy) deferStop(pos *orders.Position, now time.Time) bool {
	inst, ok := s.md.GetInstrument(pos.Instrument)
	if !ok {
		return false
	}
	since := s.stopDefer[pos.ID]
	maxWait := time.Duration(s.cfg.StopSpreadMaxWaitMin) * time.Minute
	reason := StopDeferReason(inst.Bid, inst.Ask, since, now, s.cfg.StopSpreadGuardPct, maxWait)
	if reason == "" {
		if !since.IsZero() {
			slog.Warn("stop loss: spread guard over, firing",
				"instrument", pos.Instrument, "waited", now.Sub(since).Round(time.Second))
		}
		delete(s.stopDefer, pos.ID)
		return false
	}
	if since.IsZero() {
		s.stopDefer[pos.ID] = now
		detail := fmt.Sprintf("%s %s: %s — waiting up to %d min, re-checked each cycle",
			SkipStopDeferred, pos.Instrument, reason, s.cfg.StopSpreadMaxWaitMin)
		slog.Warn("stop loss deferred: spread guard", "instrument", pos.Instrument, "reason", reason)
		s.journal.LogSkipped(detail, s.instrumentContext(pos.Instrument, s.slotOf(pos.ID)))
	}
	return true
}
