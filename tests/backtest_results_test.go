package tests

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
)

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestResultWriter_WritesAllReports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "results")
	w, err := backtest.NewResultWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	if err := w.WriteSummary(backtest.Summary{TotalTrades: 3, SharpeRatio: 1.2}); err != nil {
		t.Fatal(err)
	}
	snaps := []backtest.PortfolioSnapshot{{Date: day, EquityUSD: 100000, OpenPositions: 2, DrawdownPct: 1.5}}
	if err := w.WriteEquityCurve(snaps); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteDrawdown(snaps); err != nil {
		t.Fatal(err)
	}
	trades := []backtest.TradeRecord{{EntryDate: day, ExitDate: day.AddDate(0, 0, 9), ExitReason: "rollout_roi", Instrument: "BTC-X-C", PnLUSD: 12.5, HoldDays: 9}}
	if err := w.WriteTrades(trades); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteScenarioComparison([]backtest.ScenarioResult{{ScenarioName: "base", Summary: backtest.Summary{SharpeRatio: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteWalkForwardSummary([]backtest.WalkForwardResult{{Window: 1, Overfit: true}}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteWindowResult(1, "train", backtest.Summary{TotalTrades: 1}); err != nil {
		t.Fatal(err)
	}

	var sum backtest.Summary
	data, _ := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err := json.Unmarshal(data, &sum); err != nil || sum.TotalTrades != 3 {
		t.Errorf("summary.json = %s (err %v)", data, err)
	}
	if rows := readCSV(t, filepath.Join(dir, "equity_curve.csv")); len(rows) != 2 || rows[1][0] != "2026-01-02" {
		t.Errorf("equity_curve.csv = %v", rows)
	}
	if rows := readCSV(t, filepath.Join(dir, "trades.csv")); len(rows) != 2 || rows[1][2] != "rollout_roi" {
		t.Errorf("trades.csv = %v", rows)
	}
	if rows := readCSV(t, filepath.Join(dir, "walk_forward", "walk_forward_summary.csv")); rows[1][8] != "true" {
		t.Errorf("walk-forward overfit flag = %v", rows[1])
	}
	for _, f := range []string{"drawdown.csv", "scenario_comparison.csv", "walk_forward/window_1_train.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
}

func TestResultWriter_UnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o600)
	if _, err := backtest.NewResultWriter(filepath.Join(file, "sub")); err == nil {
		t.Error("creating results under a file must fail")
	}
}

func TestScenario_ApplyRebuildsSlots(t *testing.T) {
	base := &config.Config{DTEDeltaMatrix: []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.30}}}}
	sc := backtest.Scenario{Name: "x", EntryDelta: 0.10, TargetDTE: []int{45, 60}, RolloutDTE: 19, StopLossMulti: 3, ROITakeProfit: 0.75}

	got := sc.Apply(base)
	slots := got.Slots()
	if len(slots) != 2 || slots[0].EntryDelta != 0.10 || slots[1].TargetDTE != 60 {
		t.Errorf("scenario slots not applied: %+v", slots)
	}
	if got.StopLossMultiplier != 3 || got.ROITakeProfit != 0.75 || got.RolloutDTE != 19 {
		t.Errorf("scenario exits not applied: %+v", got)
	}
	if base.DTEDeltaMatrix[0].Deltas[0] != 0.30 {
		t.Error("Apply must not modify the base config")
	}
}

func TestRunScenarioSweep_WritesRankedComparison(t *testing.T) {
	cfg := buildTestConfig()
	cfg.MaxDTEDeviation = 30
	path := writeSyntheticCSV(t, 40)
	out := t.TempDir()

	err := backtest.RunScenarioSweep(cfg, path, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2023, 2, 9, 0, 0, 0, 0, time.UTC), out)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	rows := readCSV(t, filepath.Join(out, "scenario_comparison.csv"))
	if len(rows) != len(backtest.DefaultScenarios())+1 {
		t.Fatalf("want one row per scenario, got %d rows", len(rows))
	}
	for _, r := range rows[1:] {
		if r[0] == "" {
			t.Error("every row must name its scenario")
		}
	}
}

func TestRunScenarioSweep_ReportsMissingData(t *testing.T) {
	err := backtest.RunScenarioSweep(buildTestConfig(), filepath.Join(t.TempDir(), "missing.csv"), time.Now(), time.Now(), t.TempDir())
	if err == nil {
		t.Fatal("a sweep over a missing CSV must fail")
	}
}

func TestRunWalkForward(t *testing.T) {
	cfg := buildTestConfig()
	path := writeSyntheticCSV(t, 40)
	out := t.TempDir()
	from, to := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2023, 2, 9, 0, 0, 0, 0, time.UTC)

	if err := backtest.RunWalkForward(cfg, path, from, to, 2, out); err != nil {
		t.Fatalf("walk-forward: %v", err)
	}
	rows := readCSV(t, filepath.Join(out, "walk_forward", "walk_forward_summary.csv"))
	if len(rows) != 3 {
		t.Errorf("want header + 2 windows, got %d rows", len(rows))
	}

	if err := backtest.RunWalkForward(cfg, path, from, to, 0, out); err == nil || !strings.Contains(err.Error(), "windows") {
		t.Errorf("zero windows must be rejected, got %v", err)
	}
}
