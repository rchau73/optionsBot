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
//   - a confirmed IM limit change (or startup): resize the book toward it;
//   - entries: blocked while frozen, sized with private/simulate_portfolio so
//     Deribit's post-trade IM and MM stay inside the limits.

// marginState is one cycle's view of the account: the policy status and
// Deribit's margin, or why the margin is unknown.
type marginState struct {
	status risk.Status
	usage  risk.Usage
	err    error // margin unknown: no new risk this cycle
}

// mmBreached reports whether maintenance margin is at or above its limit.
func (m marginState) mmBreached() bool {
	return m.err == nil && m.usage.MMPct() >= m.status.MaxMMPct
}

// riskStatus evaluates the policy for now and journals what changed.
func (s *Strategy) riskStatus(now time.Time, gammaDec GammaDecision) risk.Status {
	regimeKnown := gammaDec.Regime != "" && gammaDec.Regime != "UNKNOWN"
	if s.riskCfg.UseRegime && regimeKnown {
		s.regimes.Record(now, gammaDec.Regime)
	}
	closes, live := s.policyDays(now, gammaDec.Regime, regimeKnown)
	st := risk.Evaluate(s.riskCfg, closes, live)
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
	s.journal.LogRisk(orders.RiskRecord{
		Timestamp: time.Now().UTC(), StrategyID: s.strategyID(), Change: change, Detail: detail,
		LimitIMPct: st.LimitIMPct, BandLimitPct: st.BandLimitPct, MaxMMPct: st.MaxMMPct,
		IMPct: u.IMPct(), MMPct: u.MMPct(), Unit: u.Unit,
		DVOL: s.md.DVOL(), IVPercentile: s.md.IVPercentile(), Regime: regime, Frozen: st.Frozen,
	})
}

// marginNow reads Deribit's current margin and evaluates the policy.
func (s *Strategy) marginNow(ctx context.Context, gammaDec GammaDecision) marginState {
	m := marginState{status: s.riskStatus(time.Now(), gammaDec)}
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
	if m.mmBreached() {
		s.reduceForMM(ctx, m)
		return
	}
	st := m.status
	if st.Frozen || !st.CanRebalance || st.LimitIMPct == s.appliedLimit {
		return
	}
	if s.rebalancePositions(ctx, m) {
		s.appliedLimit = st.LimitIMPct
	}
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
			if _, err := s.buyToClose(ctx, pos, excess, orders.TriggerMarginMM, 0); err != nil {
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

// sizeEntry picks the largest strangle size whose post-trade margin, as
// Deribit simulates it, stays inside both limits and within share of the IM
// headroom. One simulation prices a lot; a second confirms the final size.
func (s *Strategy) sizeEntry(ctx context.Context, call, put string, lot, share float64, m marginState) (float64, error) {
	limit, maxMM := m.status.LimitIMPct, m.status.MaxMMPct
	oneLot, err := s.simulate(ctx, map[string]float64{call: -lot, put: -lot}, m.usage.Unit)
	if err != nil {
		return 0, err
	}
	imPerLot := oneLot.IM - m.usage.IM
	lots := EntryLots(share, imPerLot)
	if lots < 1 {
		return 0, fmt.Errorf("one lot adds %.6f %s of IM, more than this slot's headroom %.6f", imPerLot, m.usage.Unit, share)
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
				"im_pct_before", fmt.Sprintf("%.2f", m.usage.IMPct()), "im_pct_after", fmt.Sprintf("%.2f", post.IMPct()),
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
