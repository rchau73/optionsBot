package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// The margin policy in the decision loop. internal/risk decides the limits;
// this file feeds it (DVOL and gamma regime at daily closes, Deribit's margin)
// and acts on its answer:
//
//   - every cycle: work out the policy status and journal any change;
//   - MM above max_mm_pct: reduce positions at once (frozen or not);
//   - a confirmed IM limit change (or startup, unless IM is already within
//     rebalance_startup_band_pct of the limit): resize the book toward it;
//   - IM over the limit with no rebalance due (a transfer out, a price move):
//     journal im_over_limit once per episode, send nothing (imalert.go);
//   - entries: blocked while frozen, sized with private/simulate_portfolio so
//     Deribit's post-trade IM and MM stay inside the limits.

// marginState is one cycle's view of the account: the policy status and
// Deribit's margin, or why the margin is unknown.
type marginState struct {
	status risk.Status
	usage  risk.Usage
	err    error // margin unknown: no new risk this cycle
	// gexWait is why new risk waits for the GEX data (GEXWaitReason), or "".
	gexWait string
}

// mmBreached reports whether maintenance margin is at or above its limit.
func (m marginState) mmBreached() bool {
	return m.err == nil && m.usage.MMPct() >= m.status.MaxMMPct
}

// entriesAllowed reports whether this cycle's margin view lets entries in:
// the same blocks maybeOpenStrangles checks first (margin unknown, frozen,
// GEX not ready, a stop waiting for the spread, MM breached). Recorded in
// the P&L history so margin utilisation counts only that time (#174).
func (m marginState) entriesAllowed(stopsDeferred bool) bool {
	return m.err == nil && !m.status.Frozen && m.gexWait == "" && !stopsDeferred && !m.mmBreached()
}

// riskStatus evaluates the policy for now and journals what changed.
func (s *Strategy) riskStatus(now time.Time, gammaDec GammaDecision) risk.Status {
	regimeKnown := gammaDec.Regime != "" && gammaDec.Regime != "UNKNOWN"
	seeded := false
	if s.riskCfg.UseRegime && regimeKnown {
		// A brand-new bot has no closes to confirm a regime against: it starts
		// from the live one. The confirmation is for changes seen after a start
		// and across restarts (#147: mvp sat frozen for a day on a fresh account).
		seeded = s.regimes.Seed(now, gammaDec.Regime, s.riskCfg.ConfirmDays)
		s.regimes.Record(now, gammaDec.Regime)
	}
	closes, live := s.policyDays(now, gammaDec.Regime, regimeKnown)
	st := risk.Evaluate(s.riskCfg, closes, live)
	if seeded {
		detail := fmt.Sprintf("no regime history: brand-new start takes the live regime %s as its last %d daily closes", gammaDec.Regime, s.riskCfg.ConfirmDays)
		slog.Warn("regime history seeded from the live regime", "regime", gammaDec.Regime, "days", s.riskCfg.ConfirmDays)
		s.logRisk(orders.RiskRegimeSeeded, detail, st, risk.Usage{}, gammaDec.Regime)
	}
	s.noteRiskChange(st, gammaDec.Regime)
	return st
}

// policyDays merges DVOL and regime daily closes by UTC day (today excluded)
// and builds the live observation.
func (s *Strategy) policyDays(now time.Time, regime string, regimeKnown bool) ([]risk.Day, risk.Day) {
	today := now.UTC().Truncate(24 * time.Hour)
	byDay := map[time.Time]*risk.Day{}
	day := func(t time.Time) *risk.Day {
		d, ok := byDay[t]
		if !ok {
			d = &risk.Day{Date: t}
			byDay[t] = d
		}
		return d
	}
	ivCloses, ivToday := s.md.DVOLDaily()
	for _, c := range ivCloses {
		if c.Day.Before(today) {
			d := day(c.Day)
			d.IVPct, d.IVKnown = c.Percentile, c.Known
		}
	}
	if s.riskCfg.UseRegime {
		for _, r := range s.regimes.Daily() {
			if r.Day.Before(today) && r.Regime != "" && r.Regime != "UNKNOWN" {
				d := day(r.Day)
				d.Negative, d.RegimeKnown = isNegativeRegime(r.Regime), true
			}
		}
	}
	closes := make([]risk.Day, 0, len(byDay))
	for _, d := range byDay {
		closes = append(closes, *d)
	}
	sort.Slice(closes, func(i, j int) bool { return closes[i].Date.Before(closes[j].Date) })

	live := risk.Day{Date: today, RegimeKnown: regimeKnown, Negative: isNegativeRegime(regime)}
	if ivToday.Day.Equal(today) {
		live.IVPct, live.IVKnown = ivToday.Percentile, ivToday.Known
	}
	return closes, live
}

func isNegativeRegime(regime string) bool { return strings.HasPrefix(regime, "NEGATIVE") }

// noteRiskChange journals a freeze, unfreeze or limit change once, when it happens.
func (s *Strategy) noteRiskChange(st risk.Status, regime string) {
	prev, first := s.lastRisk, s.lastRisk == nil
	s.lastRisk = &st
	if !first && prev.LimitIMPct != st.LimitIMPct {
		s.resetRebalanceBackoff()
		if b := s.lastUsage().MarginBalance; b > 0 {
			s.refBalance = b // the balance the new limit is first measured against (imalert.go)
		}
		s.startupPass = false // a confirmed limit change is met exactly, band or not
		s.limitChanged = true // ... and so are its downsizes (DownsizeDue)
	}
	if !first && prev.Frozen == st.Frozen && prev.LimitIMPct == st.LimitIMPct && prev.Reason == st.Reason {
		return
	}
	change, detail := orders.RiskLimitChanged, st.Reason
	switch {
	case first:
		detail = "startup: " + st.Reason
	case st.Frozen && !prev.Frozen:
		change, detail = orders.RiskFrozen, st.FreezeReason
	case !st.Frozen && prev.Frozen && prev.LimitIMPct == st.LimitIMPct && prev.Reason == st.Reason:
		change, detail = orders.RiskUnfrozen, "change reverted before confirmation"
	}
	if st.Frozen && change != orders.RiskFrozen {
		detail += " · new risk frozen: " + st.FreezeReason
	}
	slog.Info("margin policy", "change", change, "limit_im_pct", st.LimitIMPct, "max_mm_pct", st.MaxMMPct,
		"frozen", st.Frozen, "detail", detail)
	s.logRisk(change, detail, st, s.lastUsage(), regime)
}

func (s *Strategy) logRisk(change, detail string, st risk.Status, u risk.Usage, regime string) {
	s.journal.LogRisk(s.riskRecord(change, detail, st, u, regime))
}

func (s *Strategy) riskRecord(change, detail string, st risk.Status, u risk.Usage, regime string) orders.RiskRecord {
	return orders.RiskRecord{
		Timestamp: time.Now().UTC(), StrategyID: s.strategyID(), Change: change, Detail: detail,
		LimitIMPct: st.LimitIMPct, BandLimitPct: st.BandLimitPct, MaxMMPct: st.MaxMMPct,
		IMPct: u.IMPct(), MMPct: u.MMPct(), Unit: u.Unit, MarginBalance: u.MarginBalance,
		DVOL: s.md.DVOL(), IVPercentile: s.md.IVPercentile(), Regime: regime, Frozen: st.Frozen,
	}
}

// marginNow reads Deribit's current margin and evaluates the policy.
func (s *Strategy) marginNow(ctx context.Context, gammaDec GammaDecision) marginState {
	m := marginState{status: s.riskStatus(time.Now(), gammaDec),
		gexWait: GEXWaitReason(s.riskCfg.UseRegime, gammaDec, time.Now())}
	sum, err := s.fetchAccount(ctx)
	if err != nil {
		m.err = err
	} else {
		m.usage = sum.MarginUsage()
	}
	s.recordRisk(m)
	return m
}

// applyMarginPolicy enforces the limits on the book. It runs every cycle,
// after exits and before repairs and entries.
func (s *Strategy) applyMarginPolicy(ctx context.Context, m marginState) {
	if m.err != nil {
		return // no margin data: hold, exits still run
	}
	if rebalanced := s.enforceMarginPolicy(ctx, m); !rebalanced {
		s.noteIMOverLimit(m, time.Now()) // alert only; a pass that just ran made this IM reading stale
	}
}

// enforceMarginPolicy runs the MM cut or the rebalance when one is due and
// reports whether a rebalance pass ran.
func (s *Strategy) enforceMarginPolicy(ctx context.Context, m marginState) bool {
	if m.mmBreached() {
		s.cancelNewRisk(ctx, "MM limit reached")
		s.reduceForMM(ctx, m)
		return false
	}
	st := m.status
	if st.Frozen {
		s.cancelNewRisk(ctx, "new risk frozen: "+st.FreezeReason)
	}
	if st.LimitIMPct == s.appliedLimit || time.Now().Before(s.rebalanceRetryAt) {
		return false // nothing to resize, or a complement fell short recently
	}
	why := s.rebalanceBlockReason(m)
	if why == rebalanceWaitEntry {
		s.noteRebalanceWaitEntry(m)
	} else {
		s.waitEntryNoted = false
	}
	if why != "" {
		return false
	}
	if s.startupPass && RebalanceInBand(m.usage.IMPct(), st.LimitIMPct, s.cfg.RebalanceStartupBandPct) {
		s.skipStartupRebalance(m)
		return false
	}
	s.runRebalance(ctx, m)
	return true
}

// RebalanceInBand reports whether IM is within bandPct percentage points of
// the limit, on either side (band ≤ 0: never). Only the first rebalance after
// a startup asks it: between restarts IM drifts with the market and nothing
// acts on it, so without the band each deploy cut or topped up the book to
// the exact target — 2026-10-17 BTC: IM 20.9 % of 20 % cut, 38 min later
// 19.0 % re-sold, for no change in risk (#123). Pure.
func RebalanceInBand(imPct, limitPct, bandPct float64) bool {
	return bandPct > 0 && math.Abs(imPct-limitPct) <= bandPct
}

// skipStartupRebalance marks the limit as applied without trading and
// journals why. The next pass runs on a confirmed limit change (exactly, no
// band); the MM cut never waits on any of this.
func (s *Strategy) skipStartupRebalance(m marginState) {
	s.startupPass = false
	s.appliedLimit = m.status.LimitIMPct
	s.refBalance = m.usage.MarginBalance
	band := s.cfg.RebalanceStartupBandPct
	detail := fmt.Sprintf("startup: IM %.1f%% within ±%g pp of limit %.0f%% — book not resized", m.usage.IMPct(), band, m.status.LimitIMPct)
	slog.Info("rebalance: startup IM within the band, book not resized",
		"im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()), "limit_im_pct", m.status.LimitIMPct, "band_pp", band)
	s.logRisk(orders.RiskRebalanceInBand, detail, m.status, m.usage, "")
}

// noteRebalanceWaitEntry journals, once per wait, that a due rebalance waits
// for a working entry (#97): sized together, each against the book without
// the other, an entry and a top-up overshot BTC's IM limit (25.7 % of 20 %).
func (s *Strategy) noteRebalanceWaitEntry(m marginState) {
	if s.waitEntryNoted {
		return
	}
	s.waitEntryNoted = true
	detail := fmt.Sprintf("%s: %s · IM %.1f%% vs limit %.0f%%", SkipRebalanceWaitEntry, rebalanceWaitEntry,
		m.usage.IMPct(), m.status.LimitIMPct)
	slog.Info("rebalance waits for a working entry", "im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()),
		"limit_im_pct", m.status.LimitIMPct)
	s.journal.LogSkipped(detail, s.eventContext(nil, nil))
}

// runRebalance runs a real rebalance pass toward the current limit.
// Marked applied first: a complement that fails during the pass re-arms the
// rebalance (rearmRebalance), and that must not be overwritten.
func (s *Strategy) runRebalance(ctx context.Context, m marginState) RebalanceReport {
	s.appliedLimit = m.status.LimitIMPct
	s.refBalance = m.usage.MarginBalance
	s.startupPass = false // retries of this pass (rearmRebalance) go to the exact target
	rep := s.rebalancePositions(ctx, m, false)
	if !rep.Done {
		s.appliedLimit = math.NaN() // something failed: run again next cycle
	} else {
		s.limitChanged = false // the change is met: later passes leave drift in the band alone
	}
	return rep
}

// DownsizeDue reports whether a rebalance pass buys legs back: IM above the
// limit after a confirmed limit change (met exactly, as ever), otherwise only
// above the limit + bandPct points. A retry, startup or manual pass is driven
// by the market moving, not by the policy: 2026-10-29 stable BTC topped up to
// the 20 % limit, and the retry pass 15 min later read 20.4 % and bought back
// 0.2 of four legs at a loss (#555). The MM cut (max_mm_pct) never waits on
// this. Pure.
func DownsizeDue(imPct, limitPct, bandPct float64, limitChanged bool) bool {
	if limitChanged {
		return imPct > limitPct
	}
	return imPct > limitPct+math.Max(bandPct, 0)
}

// rebalanceBlockReason is RebalanceBlockReason for the current state. The
// automatic rebalance (applyMarginPolicy) and the manual one share it, so the
// two can never drift apart.
func (s *Strategy) rebalanceBlockReason(m marginState) string {
	return RebalanceBlockReason(m.status, m.usage, m.err, m.gexWait, s.complementPending(), s.entryPending())
}

// rebalanceWaitEntry is RebalanceBlockReason's answer while an entry works.
const rebalanceWaitEntry = "an entry is still working: its margin is not in the book yet"

// RebalanceBlockReason says why the book may not be resized now, or "" when
// it may. Pure: the inputs are the margin policy's status, Deribit's usage,
// a margin read error, the GEX wait reason and whether a complement or an
// entry is still working — one new-risk sizer at a time (#97): each sizes
// against the book without the other's orders, and together they overshoot.
func RebalanceBlockReason(st risk.Status, u risk.Usage, marginErr error, gexWait string, complementPending, entryPending bool) string {
	switch {
	case marginErr != nil:
		return "margin data unavailable: " + marginErr.Error()
	case u.MMPct() >= st.MaxMMPct:
		return fmt.Sprintf("maintenance margin %.1f%% at its %.0f%% limit: the MM cut runs instead", u.MMPct(), st.MaxMMPct)
	case st.Frozen:
		return "new risk frozen: " + st.FreezeReason
	case !st.CanRebalance:
		return "the IM limit does not rest on confirmed data yet"
	case gexWait != "":
		return "waiting for GEX: " + gexWait
	case complementPending:
		return "a rebalance complement is still working"
	case entryPending:
		return rebalanceWaitEntry
	}
	return ""
}

// reduceForMM buys back part of every position, at market, so maintenance
// margin falls under max_mm_pct. It repeats each cycle until it does.
func (s *Strategy) reduceForMM(ctx context.Context, m marginState) {
	mmPct := m.usage.MMPct()
	s.logRisk(orders.RiskMMBreach,
		fmt.Sprintf("maintenance margin %.1f%% ≥ limit %.0f%%: reducing positions at market", mmPct, m.status.MaxMMPct),
		m.status, m.usage, "")
	slog.Warn("maintenance margin above limit: reducing positions",
		"mm_pct", fmt.Sprintf("%.1f", mmPct), "max_mm_pct", m.status.MaxMMPct, "unit", m.usage.Unit)
	for _, pos := range s.state.AllPositions() {
		inst, ok := s.md.GetInstrument(pos.Instrument)
		lot := s.cfg.MinTradeAmount
		if ok && inst.MinTradeAmount > 0 {
			lot = inst.MinTradeAmount
		}
		keep := MMKeepQty(pos.Qty, lot, mmPct, m.status.MaxMMPct)
		if keep <= qtyEpsilon {
			s.noteStopped(pos, time.Now()) // closed out entirely: same as a stop-loss
		}
		if excess := pos.Qty - keep; excess > qtyEpsilon {
			detail := fmt.Sprintf("MM limit: maintenance margin %.1f%% ≥ %.0f%% — cutting %s from %g to %g at market", mmPct, m.status.MaxMMPct, pos.Instrument, pos.Qty, keep)
			if _, err := s.buyToClose(ctx, pos, excess, orders.TriggerMarginMM, 0, detail); err != nil {
				slog.Error("MM reduction failed", "instrument", pos.Instrument, "qty", excess, "err", err)
			}
		}
	}
}

// errMarginUnknown marks sizing that cannot trust the margin data.
var errMarginUnknown = errors.New("margin data unavailable")

// simulate returns Deribit's margin with positions added to the book. A
// result in a different unit than the live account (e.g. cross-collateral
// totals missing from one of them) cannot be compared and is an error.
func (s *Strategy) simulate(ctx context.Context, positions map[string]float64, unit string) (risk.Usage, error) {
	sum, err := s.exch.SimulatePortfolio(ctx, s.cfg.Underlying, positions)
	if err != nil {
		return risk.Usage{}, fmt.Errorf("%w: simulate_portfolio: %w", errMarginUnknown, err)
	}
	u := sum.MarginUsage()
	if u.Unit != unit {
		return risk.Usage{}, fmt.Errorf("%w: simulate_portfolio unit %s differs from account unit %s", errMarginUnknown, u.Unit, unit)
	}
	return u, nil
}

// capToNormalSize limits lots to max_leg_size_multiple × the slot's normal
// size: its slot share ÷ the strangle's margin per lot on its own (Deribit,
// add_positions=false). See CapLots.
func (s *Strategy) capToNormalSize(ctx context.Context, call, put string, lot float64, lots int, slotShare float64, unit string) (int, error) {
	alone, err := s.simulateAlone(ctx, map[string]float64{call: -lot, put: -lot}, unit)
	if err != nil {
		return 0, err
	}
	normal := TargetLots(slotShare, alone.IM)
	capped, bound := CapLots(lots, normal, s.cfg.MaxLegSizeMultiple)
	if bound {
		slog.Warn("size capped at a multiple of the slot's normal size: the book's offsets made each lot look almost free",
			"call", call, "put", put, "margin_allowed_lots", lots, "normal_lots", normal,
			"max_leg_size_multiple", s.cfg.MaxLegSizeMultiple, "lots", capped)
	}
	return capped, nil
}

// simulateAlone is simulate for positions on their own (no book offsets).
func (s *Strategy) simulateAlone(ctx context.Context, positions map[string]float64, unit string) (risk.Usage, error) {
	sum, err := s.exch.SimulateAlone(ctx, s.cfg.Underlying, positions)
	if err != nil {
		return risk.Usage{}, fmt.Errorf("%w: simulate_portfolio (alone): %w", errMarginUnknown, err)
	}
	u := sum.MarginUsage()
	if u.Unit != unit {
		return risk.Usage{}, fmt.Errorf("%w: standalone simulation unit %s differs from account unit %s", errMarginUnknown, u.Unit, unit)
	}
	return u, nil
}

// sizeEntry picks the largest strangle size whose post-trade margin, as
// Deribit simulates it, stays inside both limits and within share of the IM
// headroom. One simulation prices a lot; a second confirms the final size.
func (s *Strategy) sizeEntry(ctx context.Context, call, put string, lot, share, slotShare float64, m marginState) (float64, error) {
	limit, maxMM := m.status.LimitIMPct, m.status.MaxMMPct
	// The cost of a lot is measured between two simulations of the same
	// moment: the book now (orders placed earlier this cycle included, as
	// Deribit counts resting orders) and the book plus one lot. Against the
	// summary read at the start of the cycle, a top-up sent a second earlier
	// made one ETH lot look like 25 ETH of margin.
	base, err := s.simulate(ctx, map[string]float64{}, m.usage.Unit)
	if err != nil {
		return 0, err
	}
	oneLot, err := s.simulate(ctx, map[string]float64{call: -lot, put: -lot}, m.usage.Unit)
	if err != nil {
		return 0, err
	}
	imPerLot := oneLot.IM - base.IM
	lots := EntryLots(share, imPerLot)
	if lots < 1 {
		return 0, fmt.Errorf("one lot adds %.6f %s of IM, more than this slot's headroom %.6f", imPerLot, m.usage.Unit, share)
	}
	if lots, err = s.capToNormalSize(ctx, call, put, lot, lots, slotShare, m.usage.Unit); err != nil {
		return 0, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		qty := float64(lots) * lot
		post := oneLot
		if lots > 1 {
			if post, err = s.simulate(ctx, map[string]float64{call: -qty, put: -qty}, m.usage.Unit); err != nil {
				return 0, err
			}
		}
		if risk.Fits(post, limit, maxMM) {
			slog.Info("entry sized with simulate_portfolio",
				"call", call, "put", put, "qty", qty, "im_per_lot", imPerLot, "unit", m.usage.Unit,
				"im_pct_before", fmt.Sprintf("%.2f", base.IMPct()), "im_pct_after", fmt.Sprintf("%.2f", post.IMPct()),
				"mm_pct_after", fmt.Sprintf("%.2f", post.MMPct()), "limit_im_pct", limit, "max_mm_pct", maxMM)
			return qty, nil
		}
		// Margin is not linear in size: shrink in proportion to the overshoot.
		over := math.Max(post.IMPct()/limit, post.MMPct()/maxMM)
		next := min(lots-1, int(math.Floor(float64(lots)/over)))
		if next < 1 {
			return 0, fmt.Errorf("post-trade IM %.1f%% / MM %.1f%% exceed limits %.0f%% / %.0f%% even at one lot",
				post.IMPct(), post.MMPct(), limit, maxMM)
		}
		lots = next
	}
	return 0, fmt.Errorf("no size within limits after 3 simulations")
}
