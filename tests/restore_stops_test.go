package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Stop records lived only in memory: the 2026-10-11 deploy restarted BTC and
// forgot that both of its puts had been closed by hand, leaving their repairs
// held by the trend rule alone. They are rebuilt from the journal's closes.

func TestStopsFromTrades(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	trades := []orders.Trade{
		{Kind: "close", Reason: orders.TriggerManualClose, Instrument: "BTC-30OCT26-77000-P", At: at(0)},
		{Kind: "open", Reason: orders.TriggerRebalanceUpsize, Instrument: "BTC-30OCT26-78000-P", At: at(40)}, // another strangle: no release
		{Kind: "close", Reason: orders.TriggerStopLoss200Pct, Instrument: "BTC-27NOV26-96000-C", At: at(1)},
		{Kind: "open", Reason: orders.TriggerRepair, Instrument: "BTC-27NOV26-97000-C", At: at(80)}, // re-sold by repair: released
		{Kind: "close", Reason: orders.TriggerDeltaExit, Instrument: "BTC-25DEC26-70000-P", At: at(2)},
		{Kind: "close", Reason: orders.TriggerRolloutROI, Instrument: "BTC-25DEC26-71000-P", At: at(3)}, // a later roll replaces it
		{Kind: "close", Reason: orders.TriggerDeltaExit, Instrument: "BTC-25DEC26-120000-C", At: at(4)},
		{Kind: "close", Reason: orders.TriggerRebalanceLegs, Instrument: "BTC-25DEC26-121000-C", At: at(5)}, // partial trim: kept
		{Kind: "close", Reason: orders.TriggerManualClose, Instrument: "not-an-option", At: at(6)},
	}
	got := strategy.StopsFromTrades(trades)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	want := map[strategy.StopSide]time.Time{
		{Expiry: day(2026, 10, 30), OptionType: "put"}:  at(0),
		{Expiry: day(2026, 12, 25), OptionType: "call"}: at(4),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if !got[k].Equal(v) {
			t.Errorf("%v: got %v, want %v", k, got[k], v)
		}
	}
}

func TestStrategy_RestoredStopHoldsRepairAfterRestart(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.withLoneCall(0.1)
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.TriggerManualClose, Instrument: f.put, At: time.Now().Add(-48 * time.Hour)}})
	}
	f.startRun()

	eventually(t, 2*time.Second, "repair held by the restored record", func() bool {
		return f.skips(strategy.SkipRepairHeld+": put stopped out, cooldown after stop-out until") > 0
	})
	time.Sleep(100 * time.Millisecond)
	if n := putSells(f); n != 0 {
		t.Errorf("the hand-closed put must stay held across the restart: %d sells", n)
	}
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, strategy.SkipRepairHeld) && !strings.Contains(e.reason, time.Now().Add(24*time.Hour).UTC().Format("Jan 2")) {
			t.Errorf("the cooldown runs from the original close (48 h ago): %q", e.reason)
		}
	}
}

// A cooldown that has run out releases the repair as usual.
func TestStrategy_RestoredStopPastCooldownRepairs(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.withLoneCall(0.1)
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.TriggerStopLoss200Pct, Instrument: f.put, At: time.Now().Add(-100 * time.Hour)}})
	}
	f.startRun()
	eventually(t, 2*time.Second, "put repaired", func() bool { return putSells(f) == 1 })
}
