package tests

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/deployeffect"
	"optionsbot/internal/orders"
)

var (
	deStart = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	deSlot  = orders.SlotRef{DTE: 30, Delta: 0.15}
	deSlot2 = orders.SlotRef{DTE: 50, Delta: 0.10}
	// The shipped BTC settings (config_btc.yaml); TestDeployEffect_RulesFromConfig pins them.
	deRules = deployeffect.Rules{
		RolloutDTE: 15, DeltaDriftThreshold: 0.10, ROITakeProfit: 0.50,
		StopLossMultiplier: 2.0, DeltaExitThreshold: 0.30, RepairCooldown: 72 * time.Hour,
	}
)

const (
	deCall = "BTC-27NOV26-90000-C"
	dePut  = "BTC-27NOV26-60000-P"
)

func deAt(min int) time.Time { return deStart.Add(time.Duration(min) * time.Minute) }

// deJournal writes journal lines as the bot does (orders.OrderLog and
// orders.RiskRecord JSON) and reads them back with deployeffect.ReadJournal.
func deJournal(t *testing.T, recs ...any) []deployeffect.Line {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	lines, bad, err := deployeffect.ReadJournal(&buf)
	if err != nil || bad != 0 {
		t.Fatalf("read journal: %v, %d bad lines", err, bad)
	}
	return lines
}

// deReconciled is a startup reconciled line of a 1-lot leg sold at 0.01,
// marked at mid with |delta| 0.15 at the restart.
func deReconciled(at time.Time, inst string, slot orders.SlotRef, mid float64) orders.OrderLog {
	s := slot
	return orders.OrderLog{
		Timestamp: at, Event: orders.EventReconciled, Instrument: inst, Slot: &s, OrderID: "reconciled",
		TriggerReason: orders.TriggerReconciled, Qty: 1, FillPrice: 0.01, PremiumReceived: 0.01, Delta: 0.15,
		Detail: "strangle st-1: call " + deCall + " 1 / put " + dePut + " 1; slot 30d/0.15 opened there (journal)",
		Market: orders.MarketSnapshot{Mid: mid},
	}
}

func deStartupLines(mid float64) []any {
	return []any{deReconciled(deStart, deCall, deSlot, mid), deReconciled(deStart, dePut, deSlot, 0.008)}
}

func deSubmit(at time.Time, id, trigger, inst string, slot orders.SlotRef, qty float64) orders.OrderLog {
	s := slot
	return orders.OrderLog{Timestamp: at, Event: orders.EventSubmitted, OrderID: id, TriggerReason: trigger,
		Instrument: inst, Slot: &s, Qty: qty, LimitPrice: 0.0123}
}

func deClose(at time.Time, id, trigger, inst string, qty float64) orders.OrderLog {
	return orders.OrderLog{Timestamp: at, Event: orders.EventClosed, OrderID: id, TriggerReason: trigger,
		Instrument: inst, Qty: qty, FillPrice: 0.0456, PnL: -0.0789, PnLUSD: -5432.1, PnLUSDFmt: "-5,432.10"}
}

func deOne(t *testing.T, lines []deployeffect.Line, deploys []deployeffect.Deploy) deployeffect.Startup {
	t.Helper()
	r := deployeffect.Analyze(lines, deploys, deRules, deStart.Add(-24*time.Hour), deStart.Add(24*time.Hour))
	if len(r.Startups) != 1 {
		t.Fatalf("startups = %d, want 1: %+v", len(r.Startups), r.Startups)
	}
	return r.Startups[0]
}

// Scenario: a quiet restart.
func TestDeployEffect_QuietRestartIsNeutral(t *testing.T) {
	recs := append(deStartupLines(0.008),
		orders.OrderLog{Timestamp: deAt(2), Event: orders.EventSkipped, SkipReason: "gex_wait: no snapshot"},
		// After the 30-minute window: not this startup's.
		deSubmit(deAt(31), "o-late", orders.TriggerRebalanceUpsize, deCall, deSlot, 1),
	)
	st := deOne(t, deJournal(t, recs...), nil)
	if st.Class != deployeffect.Neutral || len(st.Orders) != 0 {
		t.Fatalf("class %s, orders %+v; want neutral with no order", st.Class, st.Orders)
	}
	if got := deployeffect.Counts(st.Triggers); got != "no order" {
		t.Errorf("counts = %q", got)
	}
}

// An entry into a slot vacant at the restart keeps it neutral; into a slot
// that held a strangle it is restart-made.
func TestDeployEffect_EntryVacantSlotNeutral(t *testing.T) {
	recs := append(deStartupLines(0.008),
		deSubmit(deAt(5), "o-1", orders.TriggerEntry, "BTC-25DEC26-95000-C", deSlot2, 0.5),
		deSubmit(deAt(5), "o-2", orders.TriggerEntry, "BTC-25DEC26-55000-P", deSlot2, 0.5),
	)
	st := deOne(t, deJournal(t, recs...), nil)
	if st.Class != deployeffect.Neutral || len(st.Orders) != 2 {
		t.Fatalf("class %s, orders %+v; want neutral with 2 entries", st.Class, st.Orders)
	}
	if c := st.Triggers[orders.TriggerEntry]; c == nil || c.Orders != 2 || c.Lots != 1 {
		t.Errorf("entry count = %+v, want 2 orders 1 lot", c)
	}

	recs = append(deStartupLines(0.008), deSubmit(deAt(5), "o-1", orders.TriggerEntry, deCall, deSlot, 0.5))
	if st := deOne(t, deJournal(t, recs...), nil); st.Class != deployeffect.RestartMade {
		t.Errorf("entry into an occupied slot: class %s, want restart-made", st.Class)
	}
}

// Scenario: a restart that rebalances.
func TestDeployEffect_RebalanceIsRestartMade(t *testing.T) {
	recs := append(deStartupLines(0.008),
		orders.RiskRecord{Timestamp: deAt(1), Event: orders.EventRisk, Change: orders.RiskRebalance},
		deSubmit(deAt(2), "o-1", orders.TriggerRebalanceUpsize, deCall, deSlot, 0.3),
		deSubmit(deAt(2), "o-2", orders.TriggerRebalanceUpsize, dePut, deSlot, 0.3),
		orders.OrderLog{Timestamp: deAt(3), Event: orders.EventAmended, OrderID: "o-1", TriggerReason: orders.TriggerRebalanceUpsize},
		deClose(deAt(4), "o-3", orders.TriggerRebalanceDownsize, deCall, 0.2),
	)
	st := deOne(t, deJournal(t, recs...), nil)
	if st.Class != deployeffect.RestartMade {
		t.Fatalf("class %s, want restart-made: %+v", st.Class, st.Orders)
	}
	up := st.Triggers[orders.TriggerRebalanceUpsize]
	if up == nil || up.Orders != 2 || up.Lots != 0.6 || up.Amends != 1 {
		t.Errorf("rebalance_upsize = %+v, want 2 orders 0.6 lots 1 amend", up)
	}
	if down := st.Triggers[orders.TriggerRebalanceDownsize]; down == nil || down.Orders != 1 || down.Lots != 0.2 {
		t.Errorf("rebalance_downsize = %+v, want 1 order 0.2 lots", down)
	}
}

// A rebalance after a confirmed limit change (or a person's request) in the
// window would have been sent anyway: expected.
func TestDeployEffect_RebalanceAfterLimitChangeExpected(t *testing.T) {
	for _, change := range []string{orders.RiskLimitChanged, orders.RiskRebalanceManual} {
		recs := append(deStartupLines(0.008),
			orders.RiskRecord{Timestamp: deAt(1), Event: orders.EventRisk, Change: change},
			deSubmit(deAt(2), "o-1", orders.TriggerRebalanceUpsize, deCall, deSlot, 0.3),
		)
		if st := deOne(t, deJournal(t, recs...), nil); st.Class != deployeffect.Expected {
			t.Errorf("%s: class %s, want expected", change, st.Class)
		}
	}
}

// Scenario: a stop that was due anyway.
func TestDeployEffect_StopDueBeforeRestartIsExpected(t *testing.T) {
	// Mark 0.035 on a 0.01 premium: loss 2.5× ≥ 2× at the restart.
	recs := append(deStartupLines(0.035), deClose(deAt(1), "o-1", orders.TriggerStopLoss200Pct, deCall, 1))
	st := deOne(t, deJournal(t, recs...), nil)
	if st.Class != deployeffect.Expected {
		t.Fatalf("class %s, want expected: %+v", st.Class, st.Orders)
	}
	if !strings.Contains(st.Orders[0].Why, orders.TriggerStopLoss200Pct) {
		t.Errorf("why = %q", st.Orders[0].Why)
	}

	// Mark 0.008 at the restart: no rule applied, the stop is restart-made.
	recs = append(deStartupLines(0.008), deClose(deAt(1), "o-1", orders.TriggerStopLoss200Pct, deCall, 1))
	if st := deOne(t, deJournal(t, recs...), nil); st.Class != deployeffect.RestartMade {
		t.Errorf("stop not due at the restart: class %s, want restart-made", st.Class)
	}
}

// Without a mark at the reconcile, the instrument's last journaled mark
// before the restart is used; with none at all the exit is not checkable.
func TestDeployEffect_ExitUsesLastMarkBeforeRestart(t *testing.T) {
	recs := []any{
		orders.OrderLog{Timestamp: deAt(-90), Event: orders.EventAmended, Instrument: deCall, OrderID: "x",
			TriggerReason: orders.TriggerRepair, Market: orders.MarketSnapshot{Mid: 0.035}},
	}
	recs = append(recs, deStartupLines(0)...)
	recs = append(recs, deClose(deAt(1), "o-1", orders.TriggerStopLoss200Pct, deCall, 1))
	if st := deOne(t, deJournal(t, recs...), nil); st.Class != deployeffect.Expected {
		t.Errorf("class %s, want expected on the last mark before the restart", st.Class)
	}

	recs = append(deStartupLines(0), deClose(deAt(1), "o-1", orders.TriggerStopLoss200Pct, deCall, 1))
	st := deOne(t, deJournal(t, recs...), nil)
	if st.Class != deployeffect.RestartMade || !strings.Contains(st.Orders[0].Why, "no mark") {
		t.Errorf("no mark: class %s why %q, want restart-made, not checkable", st.Class, st.Orders[0].Why)
	}
}

// The DTE roll needs no mark; an exit of a position opened after the
// restart is not the restart's doing.
func TestDeployEffect_RollAndNewPositionExpected(t *testing.T) {
	late := deStart.AddDate(0, 0, 25) // 27NOV is 13 days away: ≤ rollout_dte 15
	recs := []any{deReconciled(late, deCall, deSlot, 0), deClose(late.Add(time.Minute), "o-1", orders.TriggerRollout19DTE, deCall, 1)}
	r := deployeffect.Analyze(deJournal(t, recs...), nil, deRules, late.Add(-time.Hour), late.Add(time.Hour))
	if len(r.Startups) != 1 || r.Startups[0].Class != deployeffect.Expected {
		t.Errorf("DTE roll: %+v, want expected", r.Startups)
	}

	recs = append(deStartupLines(0.008), deClose(deAt(10), "o-1", orders.TriggerDeltaExit, "BTC-25DEC26-95000-C", 1))
	if st := deOne(t, deJournal(t, recs...), nil); st.Class != deployeffect.Expected {
		t.Errorf("exit of a position opened after the restart: class %s, want expected", st.Class)
	}
}

// Leg balancing and close_long after a startup are the restart's doing; a
// manual close or the MM cut is not.
func TestDeployEffect_RestartMadeAndExpectedTriggers(t *testing.T) {
	for trig, want := range map[string]string{
		orders.TriggerRebalanceLegs: deployeffect.RestartMade,
		orders.TriggerCloseLong:     deployeffect.RestartMade,
		orders.TriggerGammaClose:    deployeffect.RestartMade,
		"something_new":             deployeffect.RestartMade,
		orders.TriggerManualClose:   deployeffect.Expected,
		orders.TriggerMarginMM:      deployeffect.Expected,
		orders.TriggerKillSwitch:    deployeffect.Expected,
	} {
		recs := append(deStartupLines(0.008), deClose(deAt(3), "o-1", trig, deCall, 0.1))
		if st := deOne(t, deJournal(t, recs...), nil); st.Class != want {
			t.Errorf("%s: class %s, want %s", trig, st.Class, want)
		}
	}
}

// A repair of a leg the bot was holding (stopped inside the cooldown) is
// restart-made; one of a rolled leg is expected.
func TestDeployEffect_RepairOfHeldLeg(t *testing.T) {
	for _, tc := range []struct {
		close string
		ago   time.Duration
		want  string
	}{
		{orders.TriggerStopLoss200Pct, 10 * time.Hour, deployeffect.RestartMade},
		{orders.TriggerDeltaExit, 10 * time.Hour, deployeffect.RestartMade},
		{orders.TriggerStopLoss200Pct, 80 * time.Hour, deployeffect.Expected},
		{orders.TriggerRolloutROI, time.Hour, deployeffect.Expected},
	} {
		recs := []any{
			deClose(deStart.Add(-tc.ago), "o-0", tc.close, "BTC-27NOV26-62000-P", 1),
			deReconciled(deStart, deCall, deSlot, 0.008),
			deSubmit(deAt(4), "o-1", orders.TriggerRepair, dePut, deSlot, 1),
		}
		if st := deOne(t, deJournal(t, recs...), nil); st.Class != tc.want {
			t.Errorf("repair after %s %v before: class %s, want %s", tc.close, tc.ago, st.Class, tc.want)
		}
	}
}

// A resting close (submit then fills) is one order; an IOC filling in
// pieces (closes only, one order id) is one order with the pieces' lots.
func TestDeployEffect_OrdersCountedOnce(t *testing.T) {
	recs := append(deStartupLines(0.008),
		deSubmit(deAt(1), "o-rest", orders.TriggerRebalanceLegs, deCall, deSlot, 0.4),
		deClose(deAt(2), "o-rest", orders.TriggerRebalanceLegs, deCall, 0.4),
		deClose(deAt(3), "o-ioc", orders.TriggerRebalanceLegs, dePut, 0.1),
		deClose(deAt(3), "o-ioc", orders.TriggerRebalanceLegs, dePut, 0.2),
	)
	st := deOne(t, deJournal(t, recs...), nil)
	c := st.Triggers[orders.TriggerRebalanceLegs]
	if c == nil || c.Orders != 2 || c.Lots < 0.7-1e-9 || c.Lots > 0.7+1e-9 {
		t.Errorf("rebalance_legs = %+v, want 2 orders 0.7 lots", c)
	}
}

// Deploys name a startup's commit; a deploy into a flat book (no reconciled
// line) is a startup of its own; the next startup ends the window.
func TestDeployEffect_DeploysAndWindows(t *testing.T) {
	tsv := "2026-10-20T12:03:00Z\tmvp\tabcdef1234567\n" +
		"2026-10-20T12:20:00Z\tmvp\t1111111\trollback\n" +
		"2026-10-20T12:25:00Z\tstable\t2222222\n" +
		"not a line\n"
	deps, bad, err := deployeffect.ReadDeploys(strings.NewReader(tsv), "mvp")
	if err != nil || bad != 1 || len(deps) != 2 || !deps[1].Rollback {
		t.Fatalf("deploys = %+v, bad %d, err %v", deps, bad, err)
	}
	recs := append(deStartupLines(0.008),
		deSubmit(deAt(25), "o-1", orders.TriggerRebalanceLegs, deCall, deSlot, 0.1), // after the second startup
	)
	r := deployeffect.Analyze(deJournal(t, recs...), deps, deRules, deStart.Add(-time.Hour), deStart.Add(time.Hour))
	if len(r.Startups) != 2 {
		t.Fatalf("startups = %+v, want 2", r.Startups)
	}
	first, second := r.Startups[0], r.Startups[1]
	if first.Commit != "abcdef1234567" || first.Source != "journal" || first.Class != deployeffect.Neutral || !first.End.Equal(deAt(20)) {
		t.Errorf("first = %+v", first)
	}
	if second.Source != "deploy" || !second.Rollback || second.Class != deployeffect.RestartMade {
		t.Errorf("second = %+v", second)
	}
	if share, ok := r.RestartMadeShare(); !ok || share != 0.5 {
		t.Errorf("restart-made share = %v %v, want 0.5", share, ok)
	}
}

// A position adopted mid-run (reconciled with no detail) is not a startup.
func TestDeployEffect_AdoptionIsNotAStartup(t *testing.T) {
	adopted := deReconciled(deStart, deCall, deSlot, 0.008)
	adopted.Detail = ""
	r := deployeffect.Analyze(deJournal(t, adopted), nil, deRules, deStart.Add(-time.Hour), deStart.Add(time.Hour))
	if len(r.Startups) != 0 {
		t.Errorf("startups = %+v, want none", r.Startups)
	}
}

// Scenario: no amounts.
func TestDeployEffect_ReportShowsCountsAndLotsOnly(t *testing.T) {
	recs := append(deStartupLines(0.035),
		deClose(deAt(1), "o-1", orders.TriggerStopLoss200Pct, deCall, 1),
		deSubmit(deAt(2), "o-2", orders.TriggerRebalanceUpsize, dePut, deSlot, 0.3),
		orders.OrderLog{Timestamp: deAt(3), Event: orders.EventCancelled, OrderID: "o-2", TriggerReason: orders.TriggerRebalanceUpsize},
	)
	r := deployeffect.Analyze(deJournal(t, recs...), nil, deRules, deStart.Add(-time.Hour), deStart.Add(time.Hour))
	var buf bytes.Buffer
	if err := deployeffect.Format(&buf, "btc", r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"restart-made", "rebalance_upsize 1 order 0.3 lots 1 cancel", "stop_loss_200pct 1 order 1 lots", "Summary: 1 startups"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	for _, amount := range []string{"0.01", "0.035", "0.0123", "0.0456", "0.0789", "5432", "5,432", "$", "USD", "pnl", "premium"} {
		if strings.Contains(out, amount) {
			t.Errorf("report shows an amount (%q):\n%s", amount, out)
		}
	}
}

// The rules come from the bot's config.
func TestDeployEffect_RulesFromConfig(t *testing.T) {
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := deployeffect.RulesFrom(cfg); got != deRules {
		t.Errorf("rules = %+v, want %+v (update deRules with config_btc.yaml)", got, deRules)
	}
}
