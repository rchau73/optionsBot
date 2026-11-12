package tests

import (
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/config"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The unused-margin alert (#369): entries allowed and IM under 60 % of the
// limit for the window journals one im_under_use line per episode, with the
// cause, and shows it in /api/status. Alert only: nothing is sent.

var t0UnderUse = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

// underUseRun feeds readings every step minutes and returns the final state
// and the steps that were not UnderUseNone, by reading index.
func underUseRun(prev strategy.UnderUse, from time.Time, step time.Duration, n int, im, limit float64, known, allowed bool) (strategy.UnderUse, map[int]strategy.UnderUseStep) {
	steps := map[int]strategy.UnderUseStep{}
	for i := range n {
		var st strategy.UnderUseStep
		prev, st = strategy.IMUnderUseNext(prev, from.Add(time.Duration(i)*step), im, limit, known, allowed, 6*time.Hour)
		if st != strategy.UnderUseNone {
			steps[i] = st
		}
	}
	return prev, steps
}

// Scenario: sustained under-use with entries allowed (window).
func TestIMUnderUseNext_SustainedUnderUseOpensAfterTheWindow(t *testing.T) {
	// IM 6 % of a 20 % limit = 30 % used, a reading every 10 min for 7 h.
	st, steps := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 43, 6, 20, true, true)
	if len(steps) != 1 || steps[36] != strategy.UnderUseOpened {
		t.Fatalf("one opening at the 36th reading (6 h), got %v", steps)
	}
	if !st.Open() || !st.Since.Equal(t0UnderUse) {
		t.Errorf("episode open since the run began, got %+v", st)
	}
}

// Scenario: one line per episode.
func TestIMUnderUseNext_OneLinePerEpisode(t *testing.T) {
	st, _ := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 37, 6, 20, true, true)
	if !st.Open() {
		t.Fatal("episode should be open")
	}
	_, steps := underUseRun(st, t0UnderUse.Add(370*time.Minute), 10*time.Minute, 100, 5, 20, true, true)
	if len(steps) != 0 {
		t.Errorf("further readings under the target journal nothing, got %v", steps)
	}
}

// Scenario: data gap.
func TestIMUnderUseNext_DataGapRestartsTheWindow(t *testing.T) {
	// 5 h under the target, a 16-minute gap, 5 h more: no 6 h without a gap.
	st, steps := underUseRun(strategy.UnderUse{}, t0UnderUse, 5*time.Minute, 61, 6, 20, true, true)
	resume := t0UnderUse.Add(300*time.Minute + 16*time.Minute)
	st, steps2 := underUseRun(st, resume, 5*time.Minute, 61, 6, 20, true, true)
	if len(steps)+len(steps2) != 0 || st.Open() {
		t.Errorf("a gap over 15 min restarts the window: steps %v %v, state %+v", steps, steps2, st)
	}
	if !st.RunStart.Equal(resume) {
		t.Errorf("the run restarts at the reading after the gap, got %v", st.RunStart)
	}
	// A gap of exactly 15 min does not break the run.
	st, _ = underUseRun(strategy.UnderUse{}, t0UnderUse, 15*time.Minute, 25, 6, 20, true, true)
	if !st.Open() {
		t.Error("readings 15 min apart are a continuous run")
	}
}

// Scenario: entries not allowed (frozen, MM-blocked, GEX-stale) or margin unknown.
func TestIMUnderUseNext_EntriesNotAllowed(t *testing.T) {
	if st, steps := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 60, 6, 20, true, false); len(steps) != 0 || st.Open() {
		t.Errorf("entries not allowed: no episode, got %v %+v", steps, st)
	}
	if st, steps := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 60, 0, 20, false, false); len(steps) != 0 || st.Open() {
		t.Errorf("margin unknown: no episode, got %v %+v", steps, st)
	}
	open, _ := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 37, 6, 20, true, true)

	// Margin unknown pauses: the episode stays open, the run restarts.
	paused, st := strategy.IMUnderUseNext(open, t0UnderUse.Add(7*time.Hour), 0, 0, false, false, 6*time.Hour)
	if st != strategy.UnderUseNone || !paused.Open() || !paused.RunStart.IsZero() {
		t.Errorf("margin unknown pauses an open episode, got %v %+v", st, paused)
	}
	// Entries blocked with margin known closes it.
	closed, st := strategy.IMUnderUseNext(open, t0UnderUse.Add(7*time.Hour), 6, 20, true, false, 6*time.Hour)
	if st != strategy.UnderUseCleared || closed.Open() {
		t.Errorf("entries not allowed closes an open episode, got %v %+v", st, closed)
	}
}

// Scenario: back above the target.
func TestIMUnderUseNext_BackAboveTheTargetClearsOnce(t *testing.T) {
	open, _ := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 37, 6, 20, true, true)
	st, steps := underUseRun(open, t0UnderUse.Add(7*time.Hour), 10*time.Minute, 5, 12, 20, true, true) // exactly 60 %
	if len(steps) != 1 || steps[0] != strategy.UnderUseCleared || st.Open() {
		t.Errorf("one clear at 60 %%, got %v %+v", steps, st)
	}
	if _, st := strategy.IMUnderUseNext(strategy.UnderUse{}, t0UnderUse, 11.99, 20, true, true, 6*time.Hour); st != strategy.UnderUseNone {
		t.Errorf("just under the target starts a run, not an episode: %v", st)
	}
}

func TestIMUnderUseNext_NoLimitAndWindowOff(t *testing.T) {
	if st, steps := underUseRun(strategy.UnderUse{}, t0UnderUse, 10*time.Minute, 60, 6, 0, true, true); len(steps) != 0 || st.Open() {
		t.Errorf("no limit: nothing, got %v %+v", steps, st)
	}
	st := strategy.UnderUse{}
	for i := range 60 {
		var step strategy.UnderUseStep
		st, step = strategy.IMUnderUseNext(st, t0UnderUse.Add(time.Duration(i)*10*time.Minute), 6, 20, true, true, 0)
		if step != strategy.UnderUseNone {
			t.Fatalf("window 0 never opens, got %v", step)
		}
	}
}

// Cause choice.
func TestUnderUseCause(t *testing.T) {
	vac := func(skip string) strategy.UnderUseSlot { return strategy.UnderUseSlot{Vacant: true, LastSkip: skip} }
	full := strategy.UnderUseSlot{}
	capped := strategy.UnderUseSlot{Capped: true}
	cases := []struct {
		name    string
		slots   []strategy.UnderUseSlot
		waiting bool
		want    string
	}{
		{"no slots", nil, false, orders.IdleNotMeasured},
		{"vacant slot names its skip", []strategy.UnderUseSlot{full, vac(strategy.SkipNoFreeExpiry)}, true, strategy.SkipNoFreeExpiry},
		{"first vacant slot with a skip", []strategy.UnderUseSlot{vac(""), vac(strategy.SkipRegimeSide), vac(strategy.SkipGEXWait)}, false, strategy.SkipRegimeSide},
		{"vacant with no skip", []strategy.UnderUseSlot{full, vac("")}, true, orders.IdleNoSkip},
		{"rebalance waiting before capped", []strategy.UnderUseSlot{capped, full}, true, orders.IdleRebalanceWaiting},
		{"liquidity-capped slot", []strategy.UnderUseSlot{full, capped}, false, orders.IdleSizedSmall},
		{"a vacant slot's stale cap is not a sized-small book", []strategy.UnderUseSlot{full, {Vacant: true, Capped: true, LastSkip: strategy.SkipLiquidityCap}}, false, strategy.SkipLiquidityCap},
		{"all full", []strategy.UnderUseSlot{full, full}, false, orders.IdleAllFull},
	}
	for _, c := range cases {
		if got := strategy.UnderUseCause(c.slots, c.waiting); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// The alert and cmd/idlemargin share one vocabulary.
func TestUnderUseCause_SharesIdleMarginVocabulary(t *testing.T) {
	book := map[string]bool{}
	for _, s := range idlemargin.BookStates {
		book[s] = true
	}
	for _, c := range []string{orders.IdleSlotEmpty, orders.IdleRebalanceWaiting, orders.IdleSizedSmall, orders.IdleAllFull} {
		if !book[c] {
			t.Errorf("cause %q is not an idlemargin book state %v", c, idlemargin.BookStates)
		}
	}
	if idlemargin.RowNoSkip != orders.IdleNoSkip || idlemargin.RowNotMeasured != orders.IdleNotMeasured {
		t.Error("idlemargin's rows must be the shared orders.Idle* values")
	}
}

func TestConfig_IMUnderUseWindowIsFixed(t *testing.T) {
	var c config.Config
	c.ApplyFixed()
	if c.IMUnderUseWindowMinutes != 360 {
		t.Errorf("window = %v min, want 6 h", c.IMUnderUseWindowMinutes)
	}
}

// underUseFixture starts a bot whose one slot holds a strangle at IM 19 % of
// its 20 % limit (in the startup band, so nothing is resized), with a short
// alert window so the test sees it pass.
func underUseFixture(t *testing.T) *strategyFixture {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.cfg.IMUnderUseWindowMinutes = 0.3 / 60 // 300 ms
	f.withOpenStrangle(0.5, 0.02)
	f.exch.setMargin(1.9, 0.5)
	f.startRun()
	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })
	return f
}

// Scenarios: sustained under-use, one line per episode, back above the
// target, no trading effect — on Strategy.Run against the fake exchange.
func TestIMUnderUse_RunJournalsOnceShowsAndClears(t *testing.T) {
	f := underUseFixture(t)
	h := api.New(f.strat, orders.NewWriterLogger(io.Discard, 0.05)).Handler()
	sells, buys := len(f.exch.sells()), len(f.exch.buys())
	time.Sleep(500 * time.Millisecond)
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUse)); n != 0 {
		t.Fatalf("IM 95 %% of the limit is no under-use, got %d lines", n)
	}

	f.exch.setMarginBalance(1.9 / 0.06) // a deposit: IM 6 % = 30 % of the 20 % limit
	eventually(t, 3*time.Second, "im_under_use journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMUnderUse)) == 1 })
	r := f.journal.riskChanges(orders.RiskIMUnderUse)[0]
	if math.Abs(r.IMPct-6) > 1e-9 || r.LimitIMPct != 20 || math.Abs(r.UtilisationPct-30) > 1e-9 {
		t.Errorf("record IM %v limit %v utilisation %v, want 6, 20, 30", r.IMPct, r.LimitIMPct, r.UtilisationPct)
	}
	if r.Minutes < 0.3/60*0.99 || r.Cause != orders.IdleAllFull {
		t.Errorf("record minutes %v cause %q, want ≥ the window and all_full", r.Minutes, r.Cause)
	}
	if !strings.Contains(r.Detail, "no order sent") {
		t.Errorf("detail says it is an alert only: %q", r.Detail)
	}
	_, status := getJSON(t, h, "/api/status")
	rv, _ := status["risk"].(map[string]any)
	if rv["im_under_use"] != true || rv["im_under_use_since"] == nil {
		t.Errorf("/api/status risk = %v, want im_under_use true with its start", rv)
	}

	time.Sleep(300 * time.Millisecond) // many cycles still under the target
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUse)); n != 1 {
		t.Errorf("one im_under_use per episode, got %d", n)
	}

	f.exch.setMarginBalance(1.9 / 0.19) // IM back at 95 % of the limit
	eventually(t, 2*time.Second, "im_under_use_cleared journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMUnderUseDone)) == 1 })
	if rv := f.strat.View().Risk; rv.IMUnderUse || rv.IMUnderUseSince != nil {
		t.Errorf("cleared episode still in the view: %+v", rv)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUseDone)); n != 1 {
		t.Errorf("cleared once, got %d", n)
	}
	// Scenario: no trading effect.
	if len(f.exch.sells()) != sells || len(f.exch.buys()) != buys {
		t.Errorf("the alert sends nothing: sells %d → %d, buys %d → %d", sells, len(f.exch.sells()), buys, len(f.exch.buys()))
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalance)); n != 0 {
		t.Errorf("no rebalance from the alert, got %d", n)
	}
}

// #768: a no_expiry skip names the slot's window and every listed expiry.
func TestNoExpiryDetail_ListsTheExchangesExpiries(t *testing.T) {
	now := time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)
	at := func(d int) *marketdata.Instrument {
		return &marketdata.Instrument{Expiry: time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC).AddDate(0, 0, d)}
	}
	// Two instruments on 25DEC count once; listed in date order.
	got := strategy.NoExpiryDetail([]*marketdata.Instrument{at(43), at(15), at(43), at(1)}, now, 25, 10, 15)
	want := "window 16–35 DTE; the exchange lists 2026-11-13 (0 DTE), 2026-11-27 (14 DTE), 2026-12-25 (42 DTE)"
	if got != want {
		t.Errorf("detail\n got %q\nwant %q", got, want)
	}
	if got := strategy.NoExpiryDetail(nil, now, 25, 10, 15); got != "window 16–35 DTE; the exchange lists no expiry" {
		t.Errorf("empty chain: %q", got)
	}
}

func TestUnderUseCauseChanged(t *testing.T) {
	open := strategy.UnderUse{RunStart: t0UnderUse, Last: t0UnderUse, Since: t0UnderUse, Cause: orders.IdleAllFull}
	cases := []struct {
		name  string
		u     strategy.UnderUse
		cause string
		want  bool
	}{
		{"open, new cause", open, strategy.SkipNoExpiry, true},
		{"open, same cause", open, orders.IdleAllFull, false},
		{"no episode", strategy.UnderUse{RunStart: t0UnderUse, Last: t0UnderUse}, strategy.SkipNoExpiry, false},
		{"paused (margin unknown)", strategy.UnderUse{Since: t0UnderUse, Cause: orders.IdleAllFull}, strategy.SkipNoExpiry, false},
	}
	for _, c := range cases {
		if got := strategy.UnderUseCauseChanged(c.u, c.cause); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// Margin unknown keeps the journaled cause with the episode.
	paused, _ := strategy.IMUnderUseNext(open, t0UnderUse.Add(time.Minute), 0, 0, false, false, 6*time.Hour)
	if paused.Cause != orders.IdleAllFull || !paused.Open() {
		t.Errorf("a pause keeps the episode and its cause, got %+v", paused)
	}
}

// Scenario: the cause is recorded (#768). A 25-DTE slot next to the
// fixture's 45-DTE strangle has no listed expiry in its window: it waits
// with no_expiry naming the listed expiries, and the alert's cause is
// no_expiry with the same list. When an expiry is listed and the slot
// enters, the episode is journaled again with its new cause.
func TestIMUnderUse_CauseIsRecordedForANoExpirySlot(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16}}, {DTE: 25, Deltas: []float64{0.16}}}
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.cfg.IMUnderUseWindowMinutes = 0.3 / 60 // 300 ms
	f.withOpenStrangle(0.5, 0.02)
	f.exch.setMargin(1.9, 0.5)
	f.startRun()
	eventually(t, 2*time.Second, "slot 25 waits with no_expiry", func() bool { return f.skips(strategy.SkipNoExpiry) == 1 })
	listed := fmt.Sprintf("%s (%d DTE)", f.expiry.Format("2006-01-02"), marketdata.DaysToExpiry(f.expiry, time.Now()))
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, strategy.SkipNoExpiry) && !strings.Contains(e.reason, "window 20–35 DTE; the exchange lists "+listed) {
			t.Errorf("no_expiry skip should list the window and the expiries, got %q", e.reason)
		}
	}

	f.exch.setMarginBalance(1.9 / 0.06) // IM 6 % = 30 % of the 20 % limit
	eventually(t, 3*time.Second, "im_under_use journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMUnderUse)) == 1 })
	r := f.journal.riskChanges(orders.RiskIMUnderUse)[0]
	if r.Cause != strategy.SkipNoExpiry {
		t.Errorf("cause %q, want no_expiry", r.Cause)
	}
	if !strings.Contains(r.Detail, "cause: no_expiry (window 20–35 DTE; the exchange lists "+listed+")") {
		t.Errorf("alert detail should list the expiries: %q", r.Detail)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUse)); n != 1 {
		t.Fatalf("an unchanged cause is journaled once, got %d", n)
	}

	// An expiry in the window is listed: the slot enters and the cause moves on.
	exp := time.Now().UTC().AddDate(0, 0, 25).Truncate(24 * time.Hour).Add(8 * time.Hour)
	label := strings.ToUpper(exp.Format("2Jan06"))
	f.market.mu.Lock()
	f.market.instruments[fmt.Sprintf(testCall, label)] = fixtureInstrument(fmt.Sprintf(testCall, label), "call", exp, 110000, 0.16)
	f.market.instruments[fmt.Sprintf(testPut, label)] = fixtureInstrument(fmt.Sprintf(testPut, label), "put", exp, 90000, -0.16)
	f.market.mu.Unlock()
	eventually(t, 3*time.Second, "cause change journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMUnderUse)) == 2 })
	r2 := f.journal.riskChanges(orders.RiskIMUnderUse)[1]
	if r2.Cause == strategy.SkipNoExpiry || !strings.Contains(r2.Detail, "cause: no_expiry → "+r2.Cause) {
		t.Errorf("second line cause %q detail %q, want the change from no_expiry", r2.Cause, r2.Detail)
	}
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUseDone)); n != 0 {
		t.Errorf("a cause change is no clear, got %d", n)
	}
}

// A Config built in code without ApplyFixed has window 0: the alert is off,
// so the other Run tests' fixtures journal nothing new.
func TestIMUnderUse_OffWithoutAWindow(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.5, 0.02)
	f.exch.setMargin(0.6, 0.5)
	f.cfg.RebalanceStartupBandPct = 100 // leave the book alone
	f.startRun()
	time.Sleep(200 * time.Millisecond)
	if n := len(f.journal.riskChanges(orders.RiskIMUnderUse)); n != 0 {
		t.Errorf("window 0 turns the alert off, got %d", n)
	}
}
