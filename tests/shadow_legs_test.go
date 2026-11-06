package tests

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/shadow"
	"optionsbot/internal/strategy"
)

// withShadowLegs records the fixture's held legs at a temp file (#676).
func (f *strategyFixture) withShadowLegs(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shadow_legs.jsonl")
	b, err := shadow.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	f.shadow = b
	return path
}

func shadowLines(t *testing.T, path, event string) []shadow.Record {
	t.Helper()
	recs, skipped, err := shadow.Read(path)
	if err != nil || skipped != 0 {
		t.Fatalf("read: %v, %d skipped", err, skipped)
	}
	return shadowOfEvent(recs, event)
}

func shadowOfEvent(recs []shadow.Record, event string) []shadow.Record {
	var out []shadow.Record
	for _, r := range recs {
		if r.Event == event {
			out = append(out, r)
		}
	}
	return out
}

func waitShadow(t *testing.T, path, event string) shadow.Record {
	t.Helper()
	eventually(t, 2*time.Second, event+" line written", func() bool { return len(shadowLines(t, path, event)) > 0 })
	return shadowLines(t, path, event)[0]
}

// Scenario: a held put becomes a shadow leg.
func TestShadow_HeldPutBecomesShadowLeg(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	f.startRun()

	r := waitShadow(t, path, shadow.EventOpen)
	time.Sleep(60 * time.Millisecond) // several 10 ms cycles: still one leg
	if n := len(shadowLines(t, path, shadow.EventOpen)); n != 1 {
		t.Fatalf("%d open lines, want one per held leg", n)
	}
	if r.Rule != strategy.HoldRegimeSide || !strings.Contains(r.Reason, "no new puts") {
		t.Errorf("rule %q reason %q", r.Rule, r.Reason)
	}
	if r.Instrument != f.put || r.OptionType != "put" || r.OpenLeg != f.call || r.Qty != 0.1 {
		t.Errorf("leg %+v", r.Leg)
	}
	if r.Premium != 0.019 || r.Bid != 0.019 || r.Ask != 0.021 || r.Mark != 0.02 {
		t.Errorf("modelled fill: premium %v bid %v ask %v mark %v", r.Premium, r.Bid, r.Ask, r.Mark)
	}
	if r.Greeks.Delta != -0.16 || r.DTE < 44 || r.StrangleID == "" || r.Slot.DTE != 45 {
		t.Errorf("greeks %+v dte %d strangle %q slot %+v", r.Greeks, r.DTE, r.StrangleID, r.Slot)
	}
	if r.Market == nil || r.Market.Spot != 100000 || r.Market.Strike != 90000 || r.Portfolio == nil || r.Portfolio.Trend != "bear" {
		t.Errorf("market %+v portfolio %+v", r.Market, r.Portfolio)
	}
	if r.Book == nil || r.Book.Net != r.Book.Options+r.Book.Perp || r.Book.Options == 0 {
		t.Errorf("book delta %+v", r.Book)
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("a shadow leg sends no order: %d sells", n)
	}
}

// A one-sided entry leaves a strangle with a missing leg: it is shadowed too.
func TestShadow_OneSidedEntry(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { return orders.Fill{Qty: o.Qty, FillPrice: 0.02} }
	f.withBearTrendAboveFlip()
	f.startRun()

	r := waitShadow(t, path, shadow.EventOpen)
	if r.Instrument != f.put || r.Rule != strategy.HoldRegimeSide || r.Qty <= 0 {
		t.Errorf("leg %+v", r.Leg)
	}
	if n := putSells(f); n != 0 {
		t.Errorf("%d put sells", n)
	}
}

// Scenario: the shadow leg follows the real exit rules. The exit is what the
// real one pays, the ask, and a leg an exit ended is not opened again while
// its hold lasts.
func TestShadow_ExitRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		move   func(f *strategyFixture)
		reason string
		exit   float64
	}{
		{"mark at 2× the premium: stop_loss", func(f *strategyFixture) { f.market.setQuote(f.put, 0.039, 0.041) }, shadow.ReasonStopLoss, 0.041},
		{"|Δ| at the threshold: delta_exit", func(f *strategyFixture) { f.market.setDelta(f.put, -0.31) }, shadow.ReasonDelta, 0.021},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			path := f.withShadowLegs(t)
			f.cfg.StopLossMultiplier = 1 // a loss of 1× the premium: a mark of 2×
			f.cfg.DeltaExitThreshold = 0.30
			f.withLoneCall(0.1)
			f.withBearTrendAboveFlip()
			f.startRun()
			waitShadow(t, path, shadow.EventOpen)

			tc.move(f)
			r := waitShadow(t, path, shadow.EventClose)
			if r.ExitReason != tc.reason || r.ExitPrice != tc.exit || r.ExitDetail == "" || r.ClosedAt == nil {
				t.Fatalf("closed %q at %v (%q)", r.ExitReason, r.ExitPrice, r.ExitDetail)
			}
			want := (0.019 - tc.exit) * 0.1
			if math.Abs(r.PnLCoin-want) > 1e-12 || math.Abs(r.PnLUSD-want*100000) > 1e-6 {
				t.Errorf("P&L %v coin, %v USD, want %v coin", r.PnLCoin, r.PnLUSD, want)
			}
			if r.NetPnLCoin != r.PnLCoin+r.PerpPnLCoin-r.PerpFeesCoin || r.Market == nil || r.Book == nil {
				t.Errorf("net %v, market %v, book %v", r.NetPnLCoin, r.Market, r.Book)
			}
			time.Sleep(60 * time.Millisecond)
			if n := len(shadowLines(t, path, shadow.EventOpen)); n != 1 {
				t.Errorf("%d opens: an ended leg must not reopen while its hold lasts", n)
			}
			if n := len(f.exch.sells()); n != 0 {
				t.Errorf("%d sells", n)
			}
		})
	}
}

// The DTE roll closes a leg restored from the file, at its mark.
func TestShadow_DTERoll(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	path := filepath.Join(t.TempDir(), "shadow_legs.jsonl")
	key := shadow.LegKey(f.call, "put")
	seed, _ := json.Marshal(shadow.Record{Event: shadow.EventOpen, At: time.Now(), Leg: shadow.Leg{
		ID: key + "@1", Key: key, Rule: strategy.HoldRegimeSide, HeldBy: strategy.HoldRegimeSide, OpenLeg: f.call, OptionType: "put",
		Instrument: "BTC-OLD-80000-P", Expiry: time.Now().Add(5 * 24 * time.Hour), Qty: 0.1, Premium: 0.02, Mark: 0.015,
		OpenedAt: time.Now().Add(-48 * time.Hour)}})
	if err := os.WriteFile(path, append(seed, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := shadow.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	f.cfg.RolloutDTE = 19
	f.shadow = b
	f.startRun()

	r := waitShadow(t, path, shadow.EventClose)
	if r.ExitReason != shadow.ReasonDTERoll || r.ExitPrice != 0.015 {
		t.Fatalf("closed %q at %v", r.ExitReason, r.ExitPrice)
	}
	if r.DaysHeld < 1.9 || math.Abs(r.PnLCoin-0.0005) > 1e-12 {
		t.Errorf("days %v, P&L %v", r.DaysHeld, r.PnLCoin)
	}
}

// Scenario: the shadow leg ends when the rule releases it.
func TestShadow_ReleasedWhenRepairSells(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.withLoneCall(0.1)
	f.withPutSheddingRegime()
	f.withConfirmedRegime("POSITIVE/PINNING")
	g := &switchGEX{}
	g.set(&gex.Snapshot{ComputedAt: time.Now(), Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true})
	f.gex = g
	f.startRun()
	if open := waitShadow(t, path, shadow.EventOpen); open.Rule != strategy.HoldGEXShedding {
		t.Fatalf("held by %q", open.Rule)
	}

	g.set(&gex.Snapshot{ComputedAt: time.Now().Add(time.Minute), Regime: "POSITIVE/PINNING", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true})
	r := waitShadow(t, path, shadow.EventClose)
	if r.ExitReason != shadow.ReasonReleased || r.ExitPrice != r.Mark {
		t.Fatalf("closed %q at %v (mark %v)", r.ExitReason, r.ExitPrice, r.Mark)
	}
	eventually(t, 2*time.Second, "the real put sold", func() bool { return putSells(f) == 1 })
}

// The open leg closing ends the hold: the study can tell it from a release.
func TestShadow_OpenLegClosedEndsTheHold(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	f.startRun()
	waitShadow(t, path, shadow.EventOpen)

	f.market.setQuote(f.call, 0.079, 0.081) // the real stop-loss closes the call
	if r := waitShadow(t, path, shadow.EventClose); r.ExitReason != shadow.ReasonStrangleClosed {
		t.Fatalf("closed %q", r.ExitReason)
	}
}

// A hold with no strike to follow is one no_leg line, not a shadow leg.
func TestShadow_NoStrikeIsCounted(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	f.market.mu.Lock()
	delete(f.market.instruments, f.put)
	f.market.mu.Unlock()
	f.startRun()

	r := waitShadow(t, path, shadow.EventNoLeg)
	time.Sleep(60 * time.Millisecond)
	if n := len(shadowLines(t, path, shadow.EventNoLeg)); n != 1 {
		t.Errorf("%d no_leg lines, want one", n)
	}
	if r.HeldBy != strategy.HoldRegimeSide || r.Instrument != "" || r.OpenLeg != f.call {
		t.Errorf("no_leg %+v", r.Leg)
	}
	if n := len(shadowLines(t, path, shadow.EventOpen)); n != 0 {
		t.Errorf("%d opens", n)
	}
}

// Repair's own two checks hold a leg its gate let through.
func TestShadow_RepairOwnChecks(t *testing.T) {
	t.Run("premium floor: followed", func(t *testing.T) {
		f := newStrategyFixture(t)
		path := f.withShadowLegs(t)
		f.cfg.MinPremiumBTC = 0.05
		f.withLoneCall(0.1)
		f.startRun()
		r := waitShadow(t, path, shadow.EventOpen)
		if r.Rule != "premium_floor" || !strings.Contains(r.Reason, "below floor") {
			t.Errorf("rule %q reason %q", r.Rule, r.Reason)
		}
		if n := len(f.exch.sells()); n != 0 {
			t.Errorf("%d sells", n)
		}
	})
	t.Run("no strike: counted", func(t *testing.T) {
		f := newStrategyFixture(t)
		path := f.withShadowLegs(t)
		f.withLoneCall(0.1)
		f.market.mu.Lock()
		delete(f.market.instruments, f.put)
		f.market.mu.Unlock()
		f.startRun()
		if r := waitShadow(t, path, shadow.EventNoLeg); r.HeldBy != strategy.SkipNoStrike {
			t.Errorf("rule %q", r.HeldBy)
		}
	})
}

// A repair free to sell is not a held leg: nothing is recorded.
func TestShadow_NothingHeldNothingRecorded(t *testing.T) {
	f := newStrategyFixture(t)
	path := f.withShadowLegs(t)
	f.withLoneCall(0.1)
	f.startRun()
	eventually(t, 2*time.Second, "repair sent", func() bool { return putSells(f) == 1 })
	time.Sleep(40 * time.Millisecond)
	if recs, _, _ := shadow.Read(path); len(recs) != 0 {
		t.Errorf("lines %+v", recs)
	}
}

func TestShadowBook_RestoresAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "shadow_legs.jsonl")
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	leg := shadow.Leg{ID: "a@1", Key: "a", Rule: "r", Instrument: "ETH-X-1-P", Expiry: now.AddDate(0, 0, 30), Qty: 1, Premium: 0.02, OpenedAt: now}
	b, err := shadow.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b.Start(shadow.Record{At: now, Leg: leg})
	stopped := leg
	stopped.ID, stopped.Key = "b@1", "b"
	b.Start(shadow.Record{At: now, Leg: stopped})
	released := shadow.Leg{Key: "c", Instrument: "ETH-X-2-P"}
	b.Start(shadow.Record{At: now, Leg: released})
	stopped.Finish(shadow.ReasonStopLoss, "stop", 0.06, 3000, 0.0005, now.Add(time.Hour))
	b.End(shadow.Record{At: now.Add(time.Hour), Leg: stopped})
	released.ExitReason = shadow.ReasonReleased
	b.End(shadow.Record{At: now.Add(time.Hour), Leg: released})
	b.NoteNoLeg(shadow.Leg{Key: "d", HeldBy: "regime_side"}, now)
	b.Close()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{torn\n")
	f.Close()

	again, err := shadow.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if legs := again.Legs(); len(legs) != 1 || legs[0].Key != "a" {
		t.Fatalf("open legs %+v", legs)
	}
	if !again.Following("a") || !again.Following("b") {
		t.Error("an open leg and one an exit ended are both still followed")
	}
	if again.Following("c") {
		t.Error("a released leg may open again")
	}
	again.NoteNoLeg(shadow.Leg{Key: "d", HeldBy: "regime_side"}, now)
	recs, skipped, _ := shadow.Read(path)
	if len(shadowOfEvent(recs, shadow.EventNoLeg)) != 1 || skipped != 1 {
		t.Errorf("no_leg must stay one line across a restart; skipped %d", skipped)
	}
	again.Forget("b")
	if again.Following("b") {
		t.Error("a hold that ended frees its leg")
	}
}

func TestShadowBook_UpdatesAreSpaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shadow_legs.jsonl")
	b, err := shadow.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	l := shadow.Leg{Key: "a", Instrument: "x"}
	b.Start(shadow.Record{At: now, Leg: l})
	b.Save(&l, now.Add(time.Second), false) // too soon
	b.Save(&l, now.Add(time.Minute), true)  // the hold changed
	b.Save(&l, now.Add(time.Minute+shadow.UpdateEvery), false)
	if got := len(shadowLines(t, path, shadow.EventUpdate)); got != 2 {
		t.Errorf("%d updates, want 2", got)
	}
}

// The modelled perpetual holds the short leg's coin delta at zero: it earns
// when the leg loses to the move, and pays its taker fee on what it trades.
func TestShadowLeg_PerpetualModel(t *testing.T) {
	const fee = 0.0005
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	l := shadow.Leg{Qty: 10, Premium: 0.02, OpenedAt: at}
	l.Follow(0.02, 0.022, 0.021, orders.Greeks{Delta: -0.20}, 1000, fee) // short put: coin delta +2.21, perpetual −2.21
	if want := -2.21; math.Abs(l.PerpDelta-want) > 1e-9 {
		t.Fatalf("perp delta %v, want %v", l.PerpDelta, want)
	}
	if math.Abs(l.PerpFeesCoin-2.21*fee) > 1e-9 || l.PerpPnLCoin != 0 || l.Spot != 1000 {
		t.Errorf("fees %v, perp P&L %v, spot %v", l.PerpFeesCoin, l.PerpPnLCoin, l.Spot)
	}
	l.Follow(0.04, 0.042, 0.041, orders.Greeks{Delta: -0.40}, 900, fee) // spot falls: the short perpetual earns
	wantPnL := -2.21 * 1000 * (1.0/1000 - 1.0/900)
	if math.Abs(l.PerpPnLCoin-wantPnL) > 1e-9 || l.PerpPnLCoin <= 0 {
		t.Errorf("perp P&L %v, want %v", l.PerpPnLCoin, wantPnL)
	}
	if math.Abs(l.PerpDelta+4.41) > 1e-9 || math.Abs(l.PerpDeltaMax-4.41) > 1e-9 || math.Abs(l.PerpDeltaAvg-3.31) > 1e-9 || l.Cycles != 2 {
		t.Errorf("delta %v max %v avg %v cycles %d", l.PerpDelta, l.PerpDeltaMax, l.PerpDeltaAvg, l.Cycles)
	}
	before := l.PerpFeesCoin
	l.Finish(shadow.ReasonDelta, "delta exit", 0.042, 900, fee, at.Add(36*time.Hour))
	if math.Abs(l.PerpFeesCoin-before-4.41*fee) > 1e-9 {
		t.Errorf("closing the perpetual costs its fee: %v", l.PerpFeesCoin-before)
	}
	if math.Abs(l.PnLCoin+0.22) > 1e-9 || math.Abs(l.PnLUSD+198) > 1e-6 || l.DaysHeld != 1.5 {
		t.Errorf("P&L %v coin %v USD, %v days", l.PnLCoin, l.PnLUSD, l.DaysHeld)
	}
	if math.Abs(l.NetPnLCoin-(l.PnLCoin+l.PerpPnLCoin-l.PerpFeesCoin)) > 1e-12 {
		t.Errorf("net %v", l.NetPnLCoin)
	}
}

func shadowSample() []shadow.Record {
	t0 := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) // ISO week 45
	leg := func(key, rule string) shadow.Leg {
		return shadow.Leg{ID: key + "@1", Key: key, Rule: rule, HeldBy: rule, Instrument: key + "-P", Qty: 2, Premium: 0.01, Mark: 0.012, PnLCoin: -0.004, PerpDelta: 0.5}
	}
	closed := leg("a", "regime_side")
	closed.ExitReason, closed.PnLCoin, closed.PnLUSD, closed.PerpPnLCoin, closed.PerpFeesCoin, closed.NetPnLCoin = shadow.ReasonStopLoss, -0.02, -60, 0.01, 0.001, -0.011
	return []shadow.Record{
		{Event: shadow.EventOpen, At: t0, Leg: leg("a", "regime_side")},
		{Event: shadow.EventOpen, At: t0.AddDate(0, 0, 7), Leg: leg("b", "regime_side")}, // week 46
		{Event: shadow.EventOpen, At: t0, Leg: leg("c", "stopped")},
		{Event: shadow.EventUpdate, At: t0, Leg: leg("c", "stopped")},
		{Event: shadow.EventClose, At: t0, Leg: closed},
		{Event: shadow.EventNoLeg, At: t0, Leg: shadow.Leg{Key: "d", HeldBy: "gex_wait"}},
		{Event: shadow.EventNoLeg, At: t0, Leg: shadow.Leg{Key: "e", HeldBy: "gex_wait"}},
		{Event: shadow.EventOpen, At: t0, Leg: leg("e", "gex_wait")}, // a strike came back: followed after all
	}
}

func TestShadowSummary_ByRuleWeekAndCoverage(t *testing.T) {
	s := shadow.Summarize(shadowSample(), 3)
	if s.Opened != 4 || s.Closed != 1 || s.OpenNow != 3 || s.NoLeg != 1 || s.Skipped != 3 || s.Lines != 8 {
		t.Fatalf("%+v", s)
	}
	if s.CoveragePct == nil || *s.CoveragePct != 80 {
		t.Errorf("coverage %v", s.CoveragePct)
	}
	if s.Result.PnLCoin != -0.02 || s.Result.NetPnLCoin != -0.011 || s.Result.PremiumCoin != 0.02 {
		t.Errorf("result %+v", s.Result)
	}
	if len(s.ByRule) != 3 || s.ByRule[1].Rule != "regime_side" || s.ByRule[1].Opened != 2 || s.ByRule[1].ByReason[shadow.ReasonStopLoss] != 1 {
		t.Errorf("by rule %+v", s.ByRule)
	}
	if len(s.Weeks) != 2 || s.Weeks[0].Week != "2026-W45" || s.Weeks[0].Opened["stopped"] != 1 || s.Weeks[1].Opened["regime_side"] != 1 {
		t.Errorf("weeks %+v", s.Weeks)
	}
	if math.Abs(s.ExtraPerpNow-1.5) > 1e-9 || math.Abs(s.OpenPnLCoin+0.012) > 1e-9 || s.Open[0].Key != "b" {
		t.Errorf("open %+v", s)
	}
}

func shadowAPI(path string) http.Handler {
	opts := []api.Option{}
	if path != "" {
		opts = append(opts, api.WithShadowLegs(path))
	}
	return api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), opts...).Handler()
}

func TestAPI_ShadowLegs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shadow_legs.jsonl")

	t.Run("no record yet: empty lists and a null coverage", func(t *testing.T) {
		code, body := getJSON(t, shadowAPI(path), "/api/shadow-legs")
		if code != http.StatusOK || body["coverage_pct"] != nil || body["since"] != nil {
			t.Fatalf("%d %v", code, body)
		}
		if l, ok := body["by_rule"].([]any); !ok || len(l) != 0 {
			t.Errorf("by_rule %v", body["by_rule"])
		}
		if l, ok := body["open"].([]any); !ok || len(l) != 0 {
			t.Errorf("open %v", body["open"])
		}
	})
	t.Run("the summary of the file", func(t *testing.T) {
		var lines []string
		for _, r := range shadowSample() {
			b, _ := json.Marshal(r)
			lines = append(lines, string(b))
		}
		lines = append(lines, "{torn")
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, body := getJSON(t, shadowAPI(path), "/api/shadow-legs")
		if code != http.StatusOK || body["opened"] != 4.0 || body["no_leg"] != 1.0 || body["skipped_lines"] != 1.0 {
			t.Fatalf("%d %v", code, body)
		}
	})
	t.Run("an unreadable record is an error, not an empty answer", func(t *testing.T) {
		code, _ := getJSON(t, shadowAPI(dir), "/api/shadow-legs") // a directory
		if code != http.StatusInternalServerError {
			t.Fatalf("code %d", code)
		}
	})
	t.Run("not wired: no route", func(t *testing.T) {
		rec := httptest.NewRecorder()
		shadowAPI("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shadow-legs", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code %d", rec.Code)
		}
	})
}
