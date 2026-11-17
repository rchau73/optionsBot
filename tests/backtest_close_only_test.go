package tests

import (
	"context"
	"encoding/csv"
	"os"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// #753 in the backtest (EvaluateLeg is shared): a take-profit under 25 DTE
// closes the leg without re-selling it, and the empty slot does not re-enter
// on the expiry it just closed.

// writeDecayCSV lists one call and one put expiring 45 days after the start,
// worth 100 until decayDay and 40 (60 % earned) from then on.
func writeDecayCSV(t *testing.T, days, decayDay int) string {
	t.Helper()
	f, err := os.CreateTemp("", "bt_closeonly_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "instrument", "underlying", "underlying_price",
		"strike", "expiry", "option_type", "bid", "ask", "mid",
		"delta", "gamma", "theta", "vega", "iv", "dvol_index"})
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := start.AddDate(0, 0, 45).Format("2006-01-02")
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		bid, ask, mid := "90", "110", "100"
		if i >= decayDay {
			bid, ask, mid = "36", "44", "40"
		}
		_ = w.Write([]string{date, "BTC-X-25000-C", "BTC", "22000", "25000", expiry, "call", bid, ask, mid, "0.16", "0.001", "-3", "40", "0.75", "65"})
		_ = w.Write([]string{date, "BTC-X-19000-P", "BTC", "22000", "19000", expiry, "put", bid, ask, mid, "-0.16", "0.001", "-3", "38", "0.75", "65"})
	}
	w.Flush()
	f.Close()
	return f.Name()
}

func TestBacktest_CloseOnlyBelow25DTEDoesNotResellTheExpiry(t *testing.T) {
	const decayDay = 22 // 23 DTE: under the floor, inside the 30-DTE slot's 20–40 window
	run := func(floor int) (*backtest.Engine, backtest.Summary) {
		cfg := buildTestConfig()
		cfg.MaxDTEDeviation = 10
		cfg.CloseOnlyBelowDTE = floor
		from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		feed, err := backtest.NewHistoricalFeed(writeDecayCSV(t, 32, decayDay), from, from.AddDate(0, 0, 31), cfg.IVPercentileWindow)
		if err != nil {
			t.Fatal(err)
		}
		e := backtest.NewEngine(cfg, feed, backtest.NewSimExecutor(cfg.Backtest, 100_000))
		s, err := e.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return e, s
	}

	e, _ := run(config.FixedCloseOnlyBelowDTE)
	var closedAt time.Time
	n := 0
	for _, tr := range e.Trades() {
		if tr.ExitReason == orders.TriggerRolloutROI {
			n++
			closedAt = tr.ExitDate
		}
	}
	if n != 2 {
		t.Fatalf("both legs are bought back by the take-profit under 25 DTE, got %d: %+v", n, e.Trades())
	}
	for _, snap := range e.Snapshots() {
		if snap.Date.After(closedAt) && snap.OpenPositions > 0 {
			t.Errorf("%s: %d legs open — neither repair nor the slot's entry may re-sell the expiry just closed",
				snap.Date.Format("2006-01-02"), snap.OpenPositions)
		}
	}

	// The rule before #753: nothing under 25 DTE, the strangle is held.
	e, _ = run(0)
	for _, tr := range e.Trades() {
		if tr.ExitReason == orders.TriggerRolloutROI {
			t.Errorf("with the floor off, no take-profit under 25 DTE: %+v", tr)
		}
	}
}
