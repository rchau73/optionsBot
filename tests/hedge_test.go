package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/hedge"
)

func readHedgeReport(t *testing.T, path string) (hedge.Report, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return hedge.Report{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var r hedge.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r, true
}

func TestBuildReport_SideAndTranches(t *testing.T) {
	r := hedge.BuildReport(-0.5, 100000, "BTC-PERPETUAL", time.Unix(0, 0))
	if r.Side != "buy" {
		t.Errorf("short delta is hedged by buying, got %s", r.Side)
	}
	if r.UncoveredQty != 0.5 {
		t.Errorf("uncovered = %v, want 0.5", r.UncoveredQty)
	}
	total := 0.0
	for _, tr := range r.Tranches {
		total += tr.Pct
	}
	if total != 100 {
		t.Errorf("tranches should add to 100%%, got %v", total)
	}
	if hedge.BuildReport(0.5, 1, "X", time.Now()).Side != "sell" {
		t.Error("long delta is hedged by selling")
	}
}

func TestReporter_WritesOnlyAboveThresholdAndOnMaterialChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hedge_report.json")
	r := hedge.New(path, 0.05)

	r.MaybeReport(0.01, 100000, "BTC-PERPETUAL")
	if _, ok := readHedgeReport(t, path); ok {
		t.Fatal("no report below the threshold")
	}

	r.MaybeReport(-0.20, 100000, "BTC-PERPETUAL")
	rep, ok := readHedgeReport(t, path)
	if !ok || rep.NetDelta != -0.20 {
		t.Fatalf("expected a report at -0.20, got %+v (written=%v)", rep, ok)
	}

	r.MaybeReport(-0.22, 100000, "BTC-PERPETUAL") // moved < threshold
	if rep, _ := readHedgeReport(t, path); rep.NetDelta != -0.20 {
		t.Errorf("small move must not rewrite the report, got %v", rep.NetDelta)
	}

	r.MaybeReport(-0.30, 100000, "BTC-PERPETUAL") // moved ≥ threshold
	if rep, _ := readHedgeReport(t, path); rep.NetDelta != -0.30 {
		t.Errorf("material move should refresh the report, got %v", rep.NetDelta)
	}
}

func TestReporter_WriteFailureIsNotFatal(t *testing.T) {
	r := hedge.New(filepath.Join(t.TempDir(), "missing-dir", "hedge.json"), 0.05)
	r.MaybeReport(1, 100000, "BTC-PERPETUAL") // logs an error, must not panic
}
