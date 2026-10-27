package strategy

import (
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The unused-margin alert (#369), the other direction of imalert.go. E1's
// outcome is IM at 60 % or more of the limit while entries are allowed;
// history.Utilisation and cmd/idlemargin measure it after the fact. This
// rule tells the same day: one im_under_use journal line and WARN per
// episode when entries have been allowed and IM has stayed under 60 % of the
// limit for IMUnderUseWindowMinutes, naming the cause; the episode shown in
// /api/status; im_under_use_cleared when it ends. It never sends an order
// and changes no trading decision.
//
// The episode lives in memory like im_over_limit: a restart in the middle of
// one starts a fresh window, and journals it again once the window passes.

// UnderUseTargetPct is E1's margin-use target: IM ÷ limit, in %.
const UnderUseTargetPct = 60.0

// UnderUse is the alert's state: the current run of readings under the
// target with entries allowed, and the open episode.
type UnderUse struct {
	RunStart time.Time // first reading of the run (zero: no run)
	Last     time.Time // the run's last reading
	Since    time.Time // the episode is open since the run began (zero: none)
}

// Open reports whether an episode is open.
func (u UnderUse) Open() bool { return !u.Since.IsZero() }

// UnderUseStep is what a reading did to the episode.
type UnderUseStep int

const (
	UnderUseNone    UnderUseStep = iota // nothing to journal
	UnderUseOpened                      // the window passed: journal im_under_use
	UnderUseCleared                     // utilisation back at the target, or entries no longer allowed: journal im_under_use_cleared
)

// IMUnderUseNext folds one reading into the state. The window is time-
// weighted like history.Utilisation: the run restarts after a gap between
// readings over history.MaxSampleGap, so a gap alone never opens an episode.
//
//   - margin unknown (or no limit): pause — the run restarts, an open episode
//     stays open, nothing is journaled;
//   - IM ÷ limit at or over the target: the run ends and an open episode clears;
//   - entries not allowed: the run ends and an open episode clears — margin
//     the bot may not use is not unused;
//   - otherwise the run goes on, and opens the episode once it spans window.
//
// Pure.
func IMUnderUseNext(prev UnderUse, now time.Time, imPct, limitPct float64, marginKnown, entriesAllowed bool, window time.Duration) (UnderUse, UnderUseStep) {
	if !marginKnown || limitPct <= 0 {
		return UnderUse{Since: prev.Since}, UnderUseNone
	}
	if imPct/limitPct*100 >= UnderUseTargetPct || !entriesAllowed {
		if prev.Open() {
			return UnderUse{}, UnderUseCleared
		}
		return UnderUse{}, UnderUseNone
	}
	next := prev
	if next.RunStart.IsZero() || now.Sub(next.Last) > history.MaxSampleGap || now.Before(next.Last) {
		next.RunStart = now
	}
	next.Last = now
	if !next.Open() && window > 0 && now.Sub(next.RunStart) >= window {
		next.Since = next.RunStart
		return next, UnderUseOpened
	}
	return next, UnderUseNone
}

// UnderUseSlot is one configured slot as the cause sees it.
type UnderUseSlot struct {
	Vacant   bool
	LastSkip string // the last skip reason journaled for it (a Skip* constant), "" = none
	Capped   bool   // its last entry or top-up was cut by the liquidity cap
}

// UnderUseCause names why approved margin is unused, in the vocabulary
// cmd/idlemargin attributes idle time to (orders.Idle*): a vacant slot by
// its last skip reason (first vacant slot in config order that has one),
// else orders.IdleNoSkip; with every slot occupied, a top-up waiting for its
// retry, then a slot sized down by the liquidity cap, else all full.
// No slots → not measured. Pure.
func UnderUseCause(slots []UnderUseSlot, rebalanceWaiting bool) string {
	if len(slots) == 0 {
		return orders.IdleNotMeasured
	}
	vacant, capped := false, false
	for _, sl := range slots {
		if sl.Vacant && sl.LastSkip != "" {
			return sl.LastSkip
		}
		vacant = vacant || sl.Vacant
		capped = capped || (!sl.Vacant && sl.Capped)
	}
	switch {
	case vacant:
		return orders.IdleNoSkip
	case rebalanceWaiting:
		return orders.IdleRebalanceWaiting
	case capped:
		return orders.IdleSizedSmall
	}
	return orders.IdleAllFull
}

// underUseCause reads the cause from the loop's own state: occupied slots,
// each slot's last skip and liquidity cap, and the rebalance retry.
func (s *Strategy) underUseCause(now time.Time) string {
	occupied := s.occupiedSlots()
	var slots []UnderUseSlot
	for _, sl := range s.cfg.Slots() {
		k := makeSlotKey(sl.TargetDTE, sl.EntryDelta)
		slots = append(slots, UnderUseSlot{Vacant: !occupied[k], LastSkip: s.lastSkip[k], Capped: s.liqCapped[k]})
	}
	return UnderUseCause(slots, now.Before(s.rebalanceRetryAt))
}

// entriesBlockedWhy names what blocks entries this cycle (marginState.entriesAllowed's checks).
func (s *Strategy) entriesBlockedWhy(m marginState) string {
	switch {
	case m.status.Frozen:
		return "new risk frozen: " + m.status.FreezeReason
	case m.gexWait != "":
		return "waiting for GEX: " + m.gexWait
	case s.stopsDeferred:
		return "a stop-loss is waiting for the spread"
	case m.mmBreached():
		return "MM limit reached"
	}
	return "entries not allowed"
}

// noteIMUnderUse folds this cycle's margin into the episode and journals a
// change. Decision loop only; IMUnderUseWindowMinutes ≤ 0 turns it off.
func (s *Strategy) noteIMUnderUse(m marginState, now time.Time) {
	window := time.Duration(s.cfg.IMUnderUseWindowMinutes * float64(time.Minute))
	if window <= 0 {
		return
	}
	im, limit := m.usage.IMPct(), m.status.LimitIMPct
	prev := s.underUse
	next, step := IMUnderUseNext(prev, now, im, limit, m.err == nil, m.entriesAllowed(s.stopsDeferred), window)
	s.underUse = next
	switch step {
	case UnderUseOpened:
		cause := s.underUseCause(now)
		util := im / limit * 100
		minutes := now.Sub(next.Since).Minutes()
		detail := fmt.Sprintf("IM %.1f%% of limit %.0f%% = %.0f%% used, under the %.0f%% target for %.0f min with entries allowed · cause: %s · no order sent",
			im, limit, util, UnderUseTargetPct, minutes, cause)
		slog.Warn("approved margin unused with entries allowed",
			"im_pct", fmt.Sprintf("%.2f", im), "limit_im_pct", limit, "utilisation_pct", fmt.Sprintf("%.1f", util),
			"minutes", fmt.Sprintf("%.0f", minutes), "cause", cause, "since", next.Since)
		rec := s.riskRecord(orders.RiskIMUnderUse, detail, m.status, m.usage, "")
		rec.UtilisationPct, rec.Minutes, rec.Cause = util, minutes, cause
		s.journal.LogRisk(rec)
	case UnderUseCleared:
		why := fmt.Sprintf("IM %.1f%% of limit %.0f%% is at or over the %.0f%% target", im, limit, UnderUseTargetPct)
		if !m.entriesAllowed(s.stopsDeferred) && im/limit*100 < UnderUseTargetPct {
			why = s.entriesBlockedWhy(m)
		}
		detail := fmt.Sprintf("%s (under-used since %s)", why, prev.Since.UTC().Format("2006-01-02 15:04"))
		slog.Warn("approved margin unused: episode ended", "reason", why, "since", prev.Since)
		s.logRisk(orders.RiskIMUnderUseDone, detail, m.status, m.usage, "")
	default:
		return
	}
	s.pub.mu.Lock()
	s.pub.risk.setIMUnderUse(s.underUse.Since)
	s.pub.mu.Unlock()
}
