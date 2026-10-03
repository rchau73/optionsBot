package tests

import (
	"context"
	"encoding/csv"
	"os"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
)

// buildTestConfig returns a minimal config suitable for backtest loop tests.
func buildTestConfig() *config.Config {
	return &config.Config{
		Underlying: "BTC",
		DTEDeltaMatrix: []config.DTEDeltaEntry{
			{DTE: 30, Deltas: []float64{0.16}},
		},
		RolloutDTE:             10,
		DeltaDriftThreshold:    0.10,
		ROITakeProfit:          0.50,
		StopLossMultiplier:     2.0,
		GammaTrendLookbackDays: 1,
		IVPercentileWindow:     10,
		HedgeReportThreshold:   0.05,
		SpreadAlertThreshold:   0.05,
		Backtest: config.Backtest{
			FillModel:             "mid",
			SlippagePct:           0.001,
			LimitFillRule:         "immediate",
			CommissionPerContract: 0.0003,
		},
	}
}

// writeSyntheticCSV creates a minimal CSV with one call and one put option
// across multiple trading days.
func writeSyntheticCSV(t *testing.T, days int) string {
	t.Helper()
	f, err := os.CreateTemp("", "bt_loop_*.csv")
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
		// Call: delta 0.16
		_ = w.Write([]string{date, "BTC-X-25000-C", "BTC", "22000", "25000", expiry,
			"call", "90", "110", "100", "0.16", "0.001", "-3", "40", "0.75", "65"})
		// Put: delta -0.16
		_ = w.Write([]string{date, "BTC-X-19000-P", "BTC", "22000", "19000", expiry,
			"put", "85", "105", "95", "-0.16", "0.001", "-3", "38", "0.75", "65"})
	}
	w.Flush()
	f.Close()
	return f.Name()
}

func TestBacktestLoop_RunsWithoutError(t *testing.T) {
	cfg := buildTestConfig()
	path := writeSyntheticCSV(t, 20)
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 1, 21, 0, 0, 0, 0, time.UTC)

	feed, err := backtest.NewHistoricalFeed(path, from, to, cfg.IVPercentileWindow)
	if err != nil {
		t.Fatal(err)
	}
	exec := backtest.NewSimExecutor(cfg.Backtest, 100_000)
	engine := backtest.NewEngine(cfg, feed, exec)

	summary, err := engine.Run(context.Background())
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}
	if summary.TotalTrades < 0 {
		t.Error("expected non-negative trade count")
	}
}

// Regression: expiry selection and DTE used the wall clock instead of the
// simulated date, so a backtest over any past period never traded. A 2023
// run must open a strangle on the day the 45-day expiry is 30 DTE away and
// roll it when it reaches rollout_dte.
func TestBacktestLoop_TradesOnHistoricalDates(t *testing.T) {
	cfg := buildTestConfig()
	cfg.MaxDTEDeviation = 5
	path := writeSyntheticCSV(t, 40)
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 2, 9, 0, 0, 0, 0, time.UTC)

	feed, err := backtest.NewHistoricalFeed(path, from, to, cfg.IVPercentileWindow)
	if err != nil {
		t.Fatal(err)
	}
	engine := backtest.NewEngine(cfg, feed, backtest.NewSimExecutor(cfg.Backtest, 100_000))
	summary, err := engine.Run(context.Background())
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	opened := false
	for _, snap := range engine.Snapshots() {
		if snap.OpenPositions > 0 {
			opened = true
			break
		}
	}
	if !opened {
		t.Fatal("backtest never opened a position on historical dates")
	}
	if summary.Rollout19DTE == 0 {
		t.Errorf("legs should roll at rollout_dte (10); summary = %+v", summary)
	}
}

func TestBacktestLoop_EquityCurvePopulated(t *testing.T) {
	cfg := buildTestConfig()
	path := writeSyntheticCSV(t, 15)
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 1, 16, 0, 0, 0, 0, time.UTC)

	feed, _ := backtest.NewHistoricalFeed(path, from, to, cfg.IVPercentileWindow)
	exec := backtest.NewSimExecutor(cfg.Backtest, 100_000)
	engine := backtest.NewEngine(cfg, feed, exec)
	_, _ = engine.Run(context.Background())

	snapshots := engine.Snapshots()
	if len(snapshots) == 0 {
		t.Error("expected non-empty equity curve")
	}
	for _, s := range snapshots {
		if s.EquityUSD <= 0 {
			t.Errorf("non-positive equity on %s: %v", s.Date.Format("2006-01-02"), s.EquityUSD)
		}
	}
}
