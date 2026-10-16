package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/risk"
)

// ── Margin policy (internal/risk) ────────────────────────────────────────────
//
// Default bands: IV percentile ≥70 → 50 %, 30–70 → 35 %, <30 → 20 % IM;
// MM 35 %; changes confirmed after 2 consecutive daily closes.

var policyStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func policy(useRegime bool) risk.Config { return (&config.Config{}).RiskPolicy(useRegime) }

// ivDays builds consecutive daily closes with these IV percentiles and a
// known positive regime.
func ivDays(pcts ...float64) []risk.Day {
	out := make([]risk.Day, len(pcts))
	for i, p := range pcts {
		out[i] = risk.Day{Date: policyStart.AddDate(0, 0, i), IVPct: p, IVKnown: true, RegimeKnown: true}
	}
	return out
}

func liveIV(closes []risk.Day, pct float64) risk.Day {
	return risk.Day{Date: closes[len(closes)-1].Date.AddDate(0, 0, 1), IVPct: pct, IVKnown: true, RegimeKnown: true}
}

func TestRisk_BandBoundaries(t *testing.T) {
	p := policy(false)
	for pct, want := range map[float64]float64{0: 20, 29.9: 20, 30: 35, 69.9: 35, 70: 50, 100: 50} {
		if got := p.SortedBands()[p.BandIndex(pct)].MaxIMPct; got != want {
			t.Errorf("IV percentile %v → IM limit %v, want %v", pct, got, want)
		}
	}
}

func TestRisk_StableBandSetsLimit(t *testing.T) {
	closes := ivDays(80, 80, 80)
	st := risk.Evaluate(policy(false), closes, liveIV(closes, 85))
	if st.LimitIMPct != 50 || st.Frozen || !st.CanRebalance || st.MaxMMPct != 35 {
		t.Errorf("status = %+v", st)
	}
}

func TestRisk_SpikeFreezesThenUnfreezesWithoutChangingLimit(t *testing.T) {
	closes := ivDays(50, 50, 50)
	p := policy(false)

	spike := risk.Evaluate(p, closes, liveIV(closes, 10)) // intraday drop to the low band
	if !spike.Frozen || spike.LimitIMPct != 35 || !strings.Contains(spike.FreezeReason, "DVOL") {
		t.Errorf("a band change that lowers the limit must freeze new risk at once and keep the limit: %+v", spike)
	}

	closes = append(closes, ivDays(10)[0]) // one close in the low band...
	closes[3].Date = policyStart.AddDate(0, 0, 3)
	back := risk.Evaluate(p, closes, liveIV(closes, 50)) // ...then back to the active band
	if back.Frozen || back.LimitIMPct != 35 {
		t.Errorf("a reverted spike must unfreeze with the limit unchanged: %+v", back)
	}
	if len(back.Pending) != 1 || back.Pending[0].Days != 1 || back.Pending[0].Need != 2 {
		t.Errorf("the unconfirmed close is shown as pending 1/2: %+v", back.Pending)
	}
}

func TestRisk_BandChangeConfirmedAfterTwoCloses(t *testing.T) {
	p := policy(false)
	oneClose := ivDays(50, 50, 10)
	st := risk.Evaluate(p, oneClose, liveIV(oneClose, 10))
	if st.LimitIMPct != 35 || !st.Frozen {
		t.Errorf("after one low close the old limit holds, frozen: %+v", st)
	}
	twoCloses := ivDays(50, 50, 10, 10)
	st = risk.Evaluate(p, twoCloses, liveIV(twoCloses, 10))
	if st.LimitIMPct != 20 || st.Frozen || !st.CanRebalance {
		t.Errorf("two low closes confirm the 20%% limit: %+v", st)
	}
}

func TestRisk_MissingDayResetsTheStreak(t *testing.T) {
	closes := ivDays(50, 50, 10, 10)
	closes[3].Date = closes[3].Date.AddDate(0, 0, 1) // the bot was down for a day
	st := risk.Evaluate(policy(false), closes, liveIV(closes, 10))
	if st.LimitIMPct != 35 {
		t.Errorf("closes on non-consecutive days must not confirm a change: %+v", st)
	}
}

func TestRisk_UnknownDVOLUsesLowestBandWithoutRebalance(t *testing.T) {
	st := risk.Evaluate(policy(false), nil, risk.Day{Date: policyStart})
	if st.LimitIMPct != 20 || st.CanRebalance || st.Frozen {
		t.Errorf("no DVOL history → lowest band, no rebalancing: %+v", st)
	}
}

func TestRisk_ConfirmedNegativeGammaForcesLowestBand(t *testing.T) {
	closes := ivDays(90, 90, 90)
	closes[1].Negative, closes[2].Negative = true, true
	live := liveIV(closes, 90)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if st.LimitIMPct != 20 || st.BandLimitPct != 50 || !st.RegimeNegative || st.Frozen {
		t.Errorf("confirmed negative gamma must override a high DVOL band: %+v", st)
	}
	if !strings.Contains(st.Reason, "negative gamma") || !strings.Contains(st.Reason, "50%") {
		t.Errorf("reason must explain the override: %q", st.Reason)
	}
}

func TestRisk_UnconfirmedNegativeGammaOnlyFreezes(t *testing.T) {
	closes := ivDays(90, 90, 90)
	closes[2].Negative = true // one negative close
	live := liveIV(closes, 90)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if st.LimitIMPct != 50 || !st.Frozen || !strings.Contains(st.FreezeReason, "gamma regime") {
		t.Errorf("unconfirmed negative gamma: freeze, keep the limit: %+v", st)
	}
}

// negDays: consecutive closes at these IV percentiles under a confirmed
// negative regime (every close negative).
func negDays(pcts ...float64) []risk.Day {
	closes := ivDays(pcts...)
	for i := range closes {
		closes[i].Negative = true
	}
	return closes
}

// 2026-10-15 ETH: confirmed negative regime pins the limit to 20 %; DVOL
// crossing into the 30–70 band (35 %) cannot move it, so it must not freeze.
func TestRisk_DVOLChangeUnderNegativeRegimeDoesNotFreeze(t *testing.T) {
	closes := negDays(20, 20, 20)
	live := liveIV(closes, 34)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if st.Frozen || st.LimitIMPct != 20 || !st.CanRebalance {
		t.Errorf("a DVOL change that cannot move the pinned limit must not freeze: %+v", st)
	}
}

func TestRisk_DVOLChangeUnderNegativeRegimeStillListsPending(t *testing.T) {
	closes := negDays(20, 20, 34) // one close in the new band
	live := liveIV(closes, 34)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if st.Frozen || len(st.Pending) != 1 || st.Pending[0].Rule != "dvol_band" {
		t.Errorf("the pending band stays visible without freezing: %+v", st)
	}
}

func TestRisk_DVOLDropUnderPositiveRegimeStillFreezes(t *testing.T) {
	closes := ivDays(34, 34, 34)
	st := risk.Evaluate(policy(true), closes, liveIV(closes, 20))
	if !st.Frozen || st.LimitIMPct != 35 || !strings.Contains(st.FreezeReason, "DVOL") {
		t.Errorf("a DVOL change that lowers the limit (35 → 20) must freeze: %+v", st)
	}
}

// #105: a pending rise waits for confirmation under the current, stricter
// limit, so freezing would protect nothing.
func TestRisk_DVOLRiseDoesNotFreeze(t *testing.T) {
	for _, c := range []struct {
		name     string
		from, to float64
		limit    float64
	}{
		{"low to middle band", 20, 34, 20},
		{"middle to high band", 50, 90, 35},
		{"low to high band", 10, 90, 20},
	} {
		closes := ivDays(c.from, c.from, c.from)
		st := risk.Evaluate(policy(true), closes, liveIV(closes, c.to))
		if st.Frozen || st.LimitIMPct != c.limit {
			t.Errorf("%s: a DVOL rise must not freeze and must keep the limit %v: %+v", c.name, c.limit, st)
		}
		// One close in the new band: the rise is listed as pending, still no freeze.
		closes = append(closes, ivDays(c.to)[0])
		closes[3].Date = policyStart.AddDate(0, 0, 3)
		st = risk.Evaluate(policy(true), closes, liveIV(closes, c.to))
		if st.Frozen || st.LimitIMPct != c.limit || len(st.Pending) != 1 || st.Pending[0].Rule != "dvol_band" {
			t.Errorf("%s: a pending rise is listed without freezing: %+v", c.name, st)
		}
	}
}

func TestRisk_RegimeTurningPositiveAtLowestBandDoesNotFreeze(t *testing.T) {
	closes := negDays(20, 20, 20)
	st := risk.Evaluate(policy(true), closes, liveIV(closes, 20)) // live regime positive
	if st.Frozen || st.LimitIMPct != 20 {
		t.Errorf("at the lowest band the regime cannot move the limit: %+v", st)
	}
}

// Scenario: a pending regime change that would only raise the limit does not
// freeze (#105, 2026-10-16 ETH: frozen ~8 h a day while the regime hovered at zero).
func TestRisk_PendingRegimeChangeThatWouldOnlyRaiseTheLimitDoesNotFreeze(t *testing.T) {
	closes := negDays(50, 50, 50) // confirmed NEGATIVE: limit 20 %; the DVOL band would allow 35 %
	live := liveIV(closes, 50)    // live regime positive, not yet confirmed
	st := risk.Evaluate(policy(true), closes, live)
	if st.Frozen || st.LimitIMPct != 20 || st.BandLimitPct != 35 {
		t.Errorf("a regime change that can only raise the limit must not freeze: %+v", st)
	}

	positiveClose := ivDays(50)[0] // one positive close: the change is pending 1/2
	positiveClose.Date = policyStart.AddDate(0, 0, 3)
	closes = append(closes, positiveClose)
	st = risk.Evaluate(policy(true), closes, liveIV(closes, 50))
	if st.Frozen || st.LimitIMPct != 20 {
		t.Errorf("pending 1/2: still no freeze, limit unchanged: %+v", st)
	}
	if len(st.Pending) != 1 || st.Pending[0].Rule != "gamma_regime" || st.Pending[0].To != "positive/neutral" || st.Pending[0].Days != 1 {
		t.Errorf("the change is listed under pending: %+v", st.Pending)
	}
}

// Scenario: a change that would lower the limit still freezes.
func TestRisk_RegimeTurningNegativeFromThe35PctBandFreezes(t *testing.T) {
	closes := ivDays(50, 50, 50) // confirmed positive: limit 35 %
	live := liveIV(closes, 50)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if !st.Frozen || st.LimitIMPct != 35 || !strings.Contains(st.FreezeReason, "turned negative") {
		t.Errorf("a regime change that lowers the limit (35 → 20) must freeze: %+v", st)
	}
}

func TestRisk_RegimeTurningNegativeAtLowestBandStillFreezes(t *testing.T) {
	closes := ivDays(20, 20, 20)
	live := liveIV(closes, 20)
	live.Negative = true
	st := risk.Evaluate(policy(true), closes, live)
	if !st.Frozen || !strings.Contains(st.FreezeReason, "negative") {
		t.Errorf("turning negative is a danger signal: freeze even when the limit stays: %+v", st)
	}
}

func TestRisk_NoRegimeHistoryFreezesWithoutRebalance(t *testing.T) {
	closes := ivDays(50, 50, 50)
	for i := range closes {
		closes[i].RegimeKnown = false // e.g. first days after deployment
	}
	st := risk.Evaluate(policy(true), closes, liveIV(closes, 50))
	if !st.Frozen || st.CanRebalance || !strings.Contains(st.FreezeReason, "not confirmed yet") {
		t.Errorf("with no confirmed regime: frozen, no rebalance: %+v", st)
	}
}

func TestRisk_UsageAndFits(t *testing.T) {
	u := risk.Usage{IM: 30, MM: 21, MarginBalance: 150}
	if !near(u.IMPct(), 20, 1e-9) || !near(u.MMPct(), 14, 1e-9) || !near(u.Headroom(35), 22.5, 1e-9) {
		t.Errorf("usage = IM %v%% MM %v%% headroom %v", u.IMPct(), u.MMPct(), u.Headroom(35))
	}
	if (risk.Usage{IM: 1}).IMPct() != 100 {
		t.Error("margin with no collateral counts as fully used")
	}
	if !risk.Fits(u, 20, 35) || risk.Fits(u, 19, 35) || risk.Fits(u, 50, 10) {
		t.Error("Fits must check both IM and MM limits")
	}
}
