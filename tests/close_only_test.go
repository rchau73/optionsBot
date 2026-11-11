package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #753: under 25 DTE, take-profit and delta drift buy the leg back (close
// only) instead of doing nothing; the leg is not repaired on that expiry and
// the slot re-enters on another one.

const closeOnlyFloor = 25

// closeOnlyLeg is a live short call sold at 0.02 with dte days to expiry.
func closeOnlyLeg(now time.Time, dte int, mid, delta float64) *orders.Position {
	return &orders.Position{ID: "p", Qty: 1, EntryPrice: 0.02, PremiumReceived: 0.02, CurrentMid: mid, OptionType: "call",
		CurrentGreeks: orders.Greeks{Delta: delta}, MarkLive: true, Expiry: now.AddDate(0, 0, dte).Add(time.Hour)}
}

func TestEvaluateLeg_CloseOnlyBelowFloor(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		pos    *orders.Position
		floor  int
		action strategy.RolloutAction
		reason string
		whole  bool
	}{
		// Scenario: take-profit below 25 DTE closes only (16 DTE, ROI 80 %).
		{"take-profit at 16 DTE", closeOnlyLeg(now, 16, 0.004, 0.16), closeOnlyFloor, strategy.ActionClose, orders.TriggerRolloutROI, false},
		// Scenario: drift below 25 DTE closes only (20 DTE, |Δ| 0.04, ROI 40 %).
		{"drift at 20 DTE", closeOnlyLeg(now, 20, 0.012, 0.04), closeOnlyFloor, strategy.ActionClose, orders.TriggerRolloutDelta, false},
		{"take-profit at 24 DTE, one under", closeOnlyLeg(now, 24, 0.004, 0.16), closeOnlyFloor, strategy.ActionClose, orders.TriggerRolloutROI, false},
		// Scenario: unchanged at 25 DTE or more (30 DTE, ROI 55 %).
		{"take-profit at 30 DTE rolls", closeOnlyLeg(now, 30, 0.009, 0.16), closeOnlyFloor, strategy.ActionRollSameLeg, orders.TriggerRolloutROI, false},
		{"take-profit at 25 DTE rolls", closeOnlyLeg(now, 25, 0.004, 0.16), closeOnlyFloor, strategy.ActionRollSameLeg, orders.TriggerRolloutROI, false},
		// Scenario: the time roll still wins (15 DTE).
		{"time roll at 15 DTE", closeOnlyLeg(now, 15, 0.004, 0.04), closeOnlyFloor, strategy.ActionRollNextMonth, orders.TriggerRollout19DTE, true},
		{"nothing to do at 16 DTE", closeOnlyLeg(now, 16, 0.018, 0.16), closeOnlyFloor, strategy.ActionNone, "", false},
		// The floor varied (cmd/stress -closeonly): 20 rolls from 20 DTE.
		{"floor 20: 22 DTE rolls", closeOnlyLeg(now, 22, 0.004, 0.16), 20, strategy.ActionRollSameLeg, orders.TriggerRolloutROI, false},
		{"floor 20: 18 DTE closes only", closeOnlyLeg(now, 18, 0.004, 0.16), 20, strategy.ActionClose, orders.TriggerRolloutROI, false},
		// 0 = the rule before #753: the roll from 25 DTE, nothing under it.
		{"off: 16 DTE does nothing", closeOnlyLeg(now, 16, 0.004, 0.04), 0, strategy.ActionNone, "", false},
		{"off: 30 DTE rolls", closeOnlyLeg(now, 30, 0.004, 0.16), 0, strategy.ActionRollSameLeg, orders.TriggerRolloutROI, false},
	}
	for _, c := range cases {
		d := strategy.EvaluateLeg(c.pos, now, 15, 0.10, 0.50, 2.0, 0.30, c.floor)
		if d.Action != c.action || d.Reason != c.reason || d.WholeStrangle != c.whole {
			t.Errorf("%s: got %v %q whole=%v (%s), want %v %q whole=%v", c.name, d.Action, d.Reason, d.WholeStrangle, d.Detail, c.action, c.reason, c.whole)
		}
		closeOnly := strings.Contains(d.Detail, fmt.Sprintf("close only: < %d DTE", c.floor))
		if closeOnly != (c.action == strategy.ActionClose) {
			t.Errorf("%s: the detail says close only = %v, want %v: %q", c.name, closeOnly, c.action == strategy.ActionClose, d.Detail)
		}
		if strategy.IsCloseOnlyDetail(d.Detail) != (c.action == strategy.ActionClose) {
			t.Errorf("%s: IsCloseOnlyDetail(%q) disagrees", c.name, d.Detail)
		}
	}
}

func TestRestsAtMid_CloseOnly(t *testing.T) {
	if !strategy.RestsAtMid(strategy.ActionClose) {
		t.Error("a close-only exit is the same resting close as the take-profit")
	}
}

func TestClosedExpiries(t *testing.T) {
	now := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	nov27 := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	dec25 := time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)
	c := strategy.ClosedExpiries{}
	c.Hold(25, 0.16, nov27)
	if !c.Held(25, 0.16, nov27) || !c.Held(25, 0.161, nov27.Add(-8*time.Hour)) {
		t.Error("the slot's closed expiry is held, by its day")
	}
	if c.Held(50, 0.16, nov27) || c.Held(25, 0.16, dec25) {
		t.Error("only for that slot and that expiry")
	}
	insts := []*marketdata.Instrument{{Name: "BTC-27NOV26-90000-C", Expiry: nov27}, {Name: "BTC-25DEC26-90000-C", Expiry: dec25}}
	base := map[time.Time]bool{}
	held := c.WithHeld(base, insts, 25, 0.16)
	if !held[nov27] || held[dec25] || len(base) != 0 {
		t.Errorf("WithHeld adds the slot's closed expiry only, leaving the input alone: %v (input %v)", held, base)
	}
	if got := c.WithHeld(base, insts, 50, 0.16); len(got) != 0 {
		t.Errorf("another slot sees nothing held: %v", got)
	}
	c.Prune(now)
	if !c.Held(25, 0.16, nov27) {
		t.Error("held until the expiry passes")
	}
	c.Prune(nov27.AddDate(0, 0, 1))
	if c.Held(25, 0.16, nov27) {
		t.Error("an expired expiry is dropped")
	}
}

func TestCloseOnlyFromTrades(t *testing.T) {
	now := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	closeOnly := "take-profit: 80% of the premium earned ≥ 50% — close only: < 25 DTE (16 days left): bought back"
	nov27 := strategy.StopSide{Expiry: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), OptionType: "call"}
	cases := []struct {
		name   string
		trades []orders.Trade
		side   bool
		slot   bool
	}{
		{"close-only close", []orders.Trade{
			{Kind: "close", Reason: "roi_target", Detail: closeOnly, Instrument: "BTC-27NOV26-86000-C", Slot: slot, At: now}}, true, true},
		{"a repair after it releases the side, not the slot", []orders.Trade{
			{Kind: "close", Reason: "roi_target", Detail: closeOnly, Instrument: "BTC-27NOV26-86000-C", Slot: slot, At: now},
			{Kind: "open", Reason: orders.TriggerRepair, Instrument: "BTC-27NOV26-87000-C", At: now}}, false, true},
		{"an ordinary take-profit", []orders.Trade{
			{Kind: "close", Reason: "roi_target", Detail: "take-profit: 60% of the premium earned ≥ 50%", Instrument: "BTC-27NOV26-86000-C", Slot: slot, At: now}}, false, false},
		{"an expired expiry is not held", []orders.Trade{
			{Kind: "close", Reason: "roi_target", Detail: closeOnly, Instrument: "BTC-6NOV26-86000-C", Slot: slot, At: now}}, false, false},
	}
	for _, c := range cases {
		j := strategy.CloseOnlyFromTrades(c.trades, now)
		if j.Sides[nov27] != c.side {
			t.Errorf("%s: side held = %v, want %v", c.name, j.Sides[nov27], c.side)
		}
		if got := j.Slots.Held(25, 0.16, nov27.Expiry); got != c.slot {
			t.Errorf("%s: slot's expiry held = %v, want %v", c.name, got, c.slot)
		}
	}
}

// newShortDatedFixture is the strategy fixture with one 25-DTE slot, the roll
// at 15 and its strangle's expiry dte days away, plus a second listed expiry
// 30 days away (BTC-…-110000-C / -90000-P).
func newShortDatedFixture(t *testing.T, dte int) (f *strategyFixture, laterCall, laterPut string) {
	t.Helper()
	f = newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix[0].DTE = closeOnlyFloor
	f.cfg.RolloutDTE = 15
	f.cfg.DeltaDriftThreshold = 0.10
	f.cfg.CloseOnlyBelowDTE = closeOnlyFloor
	f.cfg.ExpiryStretch = 1.5
	move := func(days int, oldCall, oldPut string) (string, string, time.Time) {
		exp := time.Now().UTC().AddDate(0, 0, days).Truncate(24 * time.Hour).Add(8 * time.Hour)
		label := strings.ToUpper(exp.Format("2Jan06"))
		call, put := fmt.Sprintf(testCall, label), fmt.Sprintf(testPut, label)
		f.market.mu.Lock()
		defer f.market.mu.Unlock()
		for name, src := range map[string]string{call: oldCall, put: oldPut} {
			cp := *f.market.instruments[src]
			cp.Name, cp.Expiry, cp.UpdatedAt = name, exp, time.Now()
			f.market.instruments[name] = &cp
		}
		return call, put, exp
	}
	laterCall, laterPut, _ = move(30, f.call, f.put)
	call, put, exp := move(dte, f.call, f.put)
	f.market.mu.Lock()
	delete(f.market.instruments, f.call)
	delete(f.market.instruments, f.put)
	f.market.mu.Unlock()
	f.call, f.put, f.expiry = call, put, exp
	return f, laterCall, laterPut
}

// setDelta sets an instrument's quoted delta.
func (f *strategyFixture) setDelta(name string, delta float64) {
	f.market.mu.Lock()
	defer f.market.mu.Unlock()
	f.market.instruments[name].Greeks.Delta = delta
}

// Scenario: take-profit below 25 DTE closes only.
// Scenario: drift below 25 DTE closes only.
func TestStrategy_CloseOnlyBelow25DTEBuysBackAndDoesNotRepair(t *testing.T) {
	cases := []struct {
		name     string
		dte      int
		bid, ask float64
		delta    float64
		trigger  string
	}{
		{"take-profit at 16 DTE, ROI 80 %", 17, 0.0035, 0.0045, 0.16, orders.TriggerRolloutROI},
		{"drift at 20 DTE, |Δ| 0.04, ROI 40 %", 21, 0.0115, 0.0125, 0.04, orders.TriggerRolloutDelta},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, _, _ := newShortDatedFixture(t, c.dte)
			f.withOpenStrangle(0.3, 0.02)
			f.market.setQuote(f.call, c.bid, c.ask)
			f.setDelta(f.call, c.delta)
			f.restMidBuys()
			f.startRun()

			eventually(t, 2*time.Second, "resting close sent", func() bool { return len(f.callBuys()) == 1 })
			rest := f.callBuys()[0]
			if rest.OrderType != orders.TypeLimit || rest.TimeInForce != "" || rest.TriggerReason != c.trigger {
				t.Fatalf("the leg is bought back with a resting close (%s), got %+v", c.trigger, rest)
			}
			f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.3, rest.LimitPrice)
			eventually(t, 2*time.Second, "call closed", func() bool { return f.position(f.call) == nil })
			time.Sleep(150 * time.Millisecond) // many cycles

			if n := f.callSells(); n != 0 {
				t.Errorf("no repair re-sells that expiry: %d call sells", n)
			}
			if n := len(f.exch.sells()); n != 0 {
				t.Errorf("the put still holds the slot: nothing is sold, got %+v", f.exch.sells())
			}
			closes := f.callCloses()
			if len(closes) != 1 || !strings.Contains(closes[0].ctx.Detail, "close only: < 25 DTE") {
				t.Fatalf("the journal line says \"close only: < 25 DTE\", got %+v", closes)
			}
			held := false
			for _, e := range f.journal.events(orders.EventSkipped) {
				held = held || strings.HasPrefix(e.reason, "repair_held: call closed only under 25 DTE")
			}
			if !held {
				t.Error("the held repair is journaled as repair_held with its reason")
			}
			if f.position(f.put) == nil {
				t.Error("the other leg stays under its own rules")
			}
		})
	}
}

// Scenario: the slot re-enters on another expiry.
func TestStrategy_CloseOnlySlotReentersOnAnotherExpiry(t *testing.T) {
	f, laterCall, laterPut := newShortDatedFixture(t, 17)
	f.withBearTrendAboveFlip()
	f.withLoneCall(0.3)
	f.market.setQuote(f.call, 0.0035, 0.0045) // ROI 80 %
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.004}
		}
		return orders.Fill{}
	}
	f.startRun()

	eventually(t, 2*time.Second, "call closed and the slot re-entered", func() bool {
		return f.position(f.call) == nil && len(f.exch.sells()) >= 1
	})
	time.Sleep(50 * time.Millisecond)
	for _, o := range f.exch.sells() {
		switch o.Instrument {
		case laterCall:
		case f.call, f.put:
			t.Errorf("SelectSlotExpiry must not pick the expiry just closed, got %s", o.Instrument)
		case laterPut:
			t.Errorf("a confirmed negative regime with a bear trend still opens the call alone, got %s", o.Instrument)
		default:
			t.Errorf("unexpected sell %s", o.Instrument)
		}
	}
}

// After a restart the journal's close-only close still holds the repair.
func TestStrategy_CloseOnlyRestoredFromJournal(t *testing.T) {
	f, _, _ := newShortDatedFixture(t, 17)
	f.withLoneCall(0.1)
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: "roi_target", Instrument: f.put, At: time.Now().Add(-time.Hour),
			Slot:   &orders.SlotRef{DTE: closeOnlyFloor, Delta: 0.16},
			Detail: "take-profit: 80% of the premium earned ≥ 50% — close only: < 25 DTE (17 days left)"}})
	}
	f.startRun()
	eventually(t, 2*time.Second, "repair held", func() bool { return f.skips("repair_held: put closed only") >= 1 })
	time.Sleep(100 * time.Millisecond)
	for _, o := range f.exch.sells() {
		if o.Instrument == f.put {
			t.Errorf("the restored record holds the put's repair, got a sell %+v", o)
		}
	}
}
