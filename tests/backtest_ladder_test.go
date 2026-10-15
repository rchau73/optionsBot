package tests

import (
	"context"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

var ladderFrom = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// ladderConfig is the shipped slot rules (rollout_dte 15, window ±10 DTE,
// stretch 1.5) on the backtest test config.
func ladderConfig() *config.Config {
	cfg := buildTestConfig()
	cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.16}}, {DTE: 50, Deltas: []float64{0.16}}}
	cfg.RolloutDTE = 15
	cfg.MaxDTEDeviation = config.FixedMaxDTEDeviation
	cfg.ExpiryStretch = config.FixedExpiryStretch
	return cfg
}

// writeLadderCSV writes days of a listing like Deribit's: every Friday is an
// expiry, monthlies (last Friday) listed from 120 DTE, weeklies from 28 DTE.
// Each expiry has one 0.16-delta call and put at a flat price, so legs leave
// only through the DTE roll — unless callMid prices the call otherwise.
func writeLadderCSV(t *testing.T, days int, callMid func(date, expiry time.Time) float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ladder.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "instrument", "underlying", "underlying_price",
		"strike", "expiry", "option_type", "bid", "ask", "mid",
		"delta", "gamma", "theta", "vega", "iv", "dvol_index"})
	firstFriday := ladderFrom
	for firstFriday.Weekday() != time.Friday {
		firstFriday = firstFriday.AddDate(0, 0, 1)
	}
	for d := 0; d < days; d++ {
		date := ladderFrom.AddDate(0, 0, d)
		for exp := firstFriday; exp.Before(date.AddDate(0, 0, 121)); exp = exp.AddDate(0, 0, 7) {
			dte := int(exp.Sub(date).Hours() / 24)
			if dte <= 0 || (!backtest.IsMonthlyExpiry(exp) && dte > 28) {
				continue
			}
			ds, es := date.Format("2006-01-02"), exp.Format("2006-01-02")
			cm := 100.0
			if callMid != nil {
				cm = callMid(date, exp)
			}
			_ = w.Write([]string{ds, "BTC-" + es + "-110000-C", "BTC", "100000", "110000", es, "call",
				fmtNum(cm * 0.9), fmtNum(cm * 1.1), fmtNum(cm), "0.16", "0.001", "-3", "40", "0.5", "50"})
			_ = w.Write([]string{ds, "BTC-" + es + "-90000-P", "BTC", "100000", "90000", es, "put",
				"90", "110", "100", "-0.16", "0.001", "-3", "40", "0.5", "50"})
		}
	}
	w.Flush()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }

func runLadderEngine(t *testing.T, cfg *config.Config, path string, days int, monthlyOnly bool) *backtest.Engine {
	t.Helper()
	feed, err := backtest.NewHistoricalFeed(path, ladderFrom, ladderFrom.AddDate(0, 0, days-1), cfg.IVPercentileWindow)
	if err != nil {
		t.Fatal(err)
	}
	engine := backtest.NewEngine(cfg, feed, backtest.NewSimExecutor(cfg.Backtest, 100_000))
	engine.SetMonthlyOnly(monthlyOnly)
	if _, err := engine.Run(context.Background()); err != nil {
		t.Fatalf("engine.Run: %v", err)
	}
	return engine
}

// Scenario: sweep delivered — occupancy, return on margin and worst week are
// reported for each ladder.
func TestLadderSweep_SweepDelivered(t *testing.T) {
	const days = 150
	path := writeLadderCSV(t, days, nil)
	out := t.TempDir()

	results, err := backtest.RunLadderSweep(ladderConfig(), path, ladderFrom, ladderFrom.AddDate(0, 0, days-1), out)
	if err != nil {
		t.Fatalf("ladder sweep: %v", err)
	}
	ladders := backtest.DefaultLadders()
	if len(results) != len(ladders) {
		t.Fatalf("want one result per ladder, got %d", len(results))
	}
	for i, r := range results {
		if r.Ladder.Name() != ladders[i].Name() {
			t.Errorf("result %d is %q, want %q", i, r.Ladder.Name(), ladders[i].Name())
		}
		if len(r.Slots) != len(ladders[i].DTEs) {
			t.Errorf("%s: want one occupancy per slot, got %+v", r.Ladder.Name(), r.Slots)
		}
		if r.OccupancyPct <= 0 || r.Trades == 0 || r.ReturnOnMarginPct == 0 {
			t.Errorf("%s: the ladder must trade and report its metrics: %+v", r.Ladder.Name(), r)
		}
	}

	rows := readCSV(t, filepath.Join(out, "ladder_comparison.csv"))
	if len(rows) != len(ladders)+1 {
		t.Fatalf("want header + one row per ladder, got %d rows", len(rows))
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	for _, name := range []string{"ladder", "occupancy_pct", "min_slot_occupancy_pct", "return_on_margin_pct", "worst_week_usd", "worst_week_pct", "shared_expiry_days"} {
		if _, ok := col[name]; !ok {
			t.Fatalf("ladder_comparison.csv lacks column %q: %v", name, rows[0])
		}
	}
	for _, r := range rows[1:] {
		for _, name := range []string{"occupancy_pct", "return_on_margin_pct", "worst_week_pct"} {
			if _, err := strconv.ParseFloat(r[col[name]], 64); err != nil {
				t.Errorf("%s: %s = %q is not a number", r[col["ladder"]], name, r[col[name]])
			}
		}
	}
}

func TestLadderSweep_ReportsMissingData(t *testing.T) {
	if _, err := backtest.RunLadderSweep(ladderConfig(), filepath.Join(t.TempDir(), "missing.csv"), ladderFrom, ladderFrom, t.TempDir()); err == nil {
		t.Fatal("a ladder sweep over a missing CSV must fail")
	}
}

// Scenario: separate expiries kept — given any ladder, no two slots hold the
// same expiry. 25/30 overlaps on purpose: both windows want the same expiry.
func TestLadderSweep_SeparateExpiriesKept(t *testing.T) {
	const days = 150
	path := writeLadderCSV(t, days, nil)
	ladders := append(backtest.DefaultLadders(), backtest.Ladder{DTEs: []int{25, 30}, Weeklies: true}, backtest.Ladder{DTEs: []int{25, 30}})
	for _, l := range ladders {
		t.Run(l.Name(), func(t *testing.T) {
			engine := runLadderEngine(t, l.Apply(ladderConfig()), path, days, !l.Weeklies)
			if n := engine.SharedExpiryDays(); n != 0 {
				t.Errorf("two slots held the same expiry on %d days", n)
			}
			occupied := 0
			for _, s := range engine.Occupancy() {
				occupied += s.OccupiedDays
			}
			if occupied == 0 {
				t.Error("no slot ever traded: the check proved nothing")
			}
		})
	}
}

// The monthly roll gap the issue reports: on monthlies alone a 25/50 ladder
// leaves a slot vacant after each roll; weeklies fill it.
func TestLadderSweep_MonthliesLeaveAGap(t *testing.T) {
	const days = 150
	path := writeLadderCSV(t, days, nil)
	l := backtest.Ladder{DTEs: []int{25, 50}}
	minPct := func(monthlyOnly bool) float64 {
		occ := runLadderEngine(t, l.Apply(ladderConfig()), path, days, monthlyOnly).Occupancy()
		m := 100.0
		for _, s := range occ {
			m = min(m, s.Pct())
		}
		return m
	}
	monthlies, weeklies := minPct(true), minPct(false)
	if monthlies >= 85 {
		t.Errorf("25/50 on monthlies should leave a slot idle after rolls, min occupancy %.1f%%", monthlies)
	}
	if weeklies <= monthlies {
		t.Errorf("weeklies should fill the gap: min occupancy %.1f%% with, %.1f%% without", weeklies, monthlies)
	}
}

// Rollouts only close: a DTE roll frees the slot, which re-enters a fresh
// strangle, so every day with an open leg is a day the slot is occupied (the
// backtest used to reopen rolled legs outside any strangle and keep the
// dead strangle holding the slot).
func TestBacktestLoop_DTERollFreesSlot(t *testing.T) {
	const days = 150
	path := writeLadderCSV(t, days, nil)
	cfg := ladderConfig()
	cfg.DTEDeltaMatrix = cfg.DTEDeltaMatrix[:1]
	engine := runLadderEngine(t, cfg, path, days, true)

	daysOpen := 0
	for _, s := range engine.Snapshots() {
		if s.OpenPositions > 2 {
			t.Fatalf("%s: %d open legs for one slot", s.Date.Format("2006-01-02"), s.OpenPositions)
		}
		if s.OpenPositions > 0 {
			daysOpen++
		}
	}
	rolls := 0
	for _, tr := range engine.Trades() {
		if tr.ExitReason == orders.TriggerRollout19DTE {
			rolls++
		}
	}
	if occ := engine.Occupancy()[0]; occ.OccupiedDays != daysOpen || rolls < 4 {
		t.Errorf("slot occupied %d days, legs open %d days, %d DTE rolls (want equal days and several rolls)", occ.OccupiedDays, daysOpen, rolls)
	}
}

// A stop-loss is repaired like live: same strangle, same expiry.
func TestBacktestLoop_StopRepairedOnSameExpiry(t *testing.T) {
	const days = 40
	shock := ladderFrom.AddDate(0, 0, 5)
	path := writeLadderCSV(t, days, func(date, _ time.Time) float64 {
		if !date.Before(shock) {
			return 400 // 4× the premium: a stop on day 5, the repair sells at 400
		}
		return 100
	})
	cfg := ladderConfig()
	cfg.DTEDeltaMatrix = cfg.DTEDeltaMatrix[:1]
	engine := runLadderEngine(t, cfg, path, days, true)

	var stop *backtest.TradeRecord
	for i, tr := range engine.Trades() {
		if tr.ExitReason == orders.TriggerStopLoss200Pct {
			stop = &engine.Trades()[i]
			break
		}
	}
	if stop == nil {
		t.Fatalf("expected a stop-loss, trades = %+v", engine.Trades())
	}
	snaps := engine.Snapshots()
	day := snaps[int(stop.ExitDate.Sub(ladderFrom).Hours()/24)]
	if day.OpenPositions != 2 {
		t.Errorf("the stopped call should be repaired at once: %d legs open on %s", day.OpenPositions, day.Date.Format("2006-01-02"))
	}
	if occ := engine.Occupancy()[0]; occ.OccupiedDays == 0 {
		t.Error("the slot should stay occupied")
	}
	if engine.SharedExpiryDays() != 0 {
		t.Error("a repair must stay on its strangle's expiry")
	}
}

func TestIsMonthlyExpiry(t *testing.T) {
	for _, tc := range []struct {
		date string
		want bool
	}{
		{"2026-10-30", true},  // last Friday of October
		{"2026-10-23", false}, // a weekly
		{"2026-11-27", true},
		{"2026-12-25", true},  // quarterly
		{"2026-10-31", false}, // a Saturday
	} {
		d, _ := time.Parse("2006-01-02", tc.date)
		if got := backtest.IsMonthlyExpiry(d); got != tc.want {
			t.Errorf("IsMonthlyExpiry(%s) = %v, want %v", tc.date, got, tc.want)
		}
	}
}

func TestWorstWeek(t *testing.T) {
	snap := func(day int, eq float64) backtest.PortfolioSnapshot {
		return backtest.PortfolioSnapshot{Date: ladderFrom.AddDate(0, 0, day), EquityUSD: eq}
	}
	for _, tc := range []struct {
		name     string
		snaps    []backtest.PortfolioSnapshot
		usd, pct float64
	}{
		{"empty", nil, 0, 0},
		{"one day", []backtest.PortfolioSnapshot{snap(0, 100)}, 0, 0},
		{"only gains", []backtest.PortfolioSnapshot{snap(0, 100), snap(3, 110), snap(9, 120)}, 0, 0},
		{"loss inside a week", []backtest.PortfolioSnapshot{snap(0, 1000), snap(2, 950), snap(7, 900), snap(8, 990)}, -100, -10},
		// 1000 → 800 spans 8 days, not one week: the worst week is 960 → 800
		{"window is 7 days", []backtest.PortfolioSnapshot{snap(0, 1000), snap(7, 960), snap(8, 800), snap(15, 800)}, -160, -160.0 / 960 * 100},
		{"gap in the data", []backtest.PortfolioSnapshot{snap(0, 1000), snap(20, 500)}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usd, pct := backtest.WorstWeek(tc.snaps)
			if math.Abs(usd-tc.usd) > 1e-9 || math.Abs(pct-tc.pct) > 1e-9 {
				t.Errorf("WorstWeek = %.4f USD %.4f%%, want %.4f %.4f", usd, pct, tc.usd, tc.pct)
			}
		})
	}
}

func TestLadder_ApplyAndName(t *testing.T) {
	base := ladderConfig()
	base.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.12, 0.16}}}
	l := backtest.Ladder{DTEs: []int{30, 60}}
	got := l.Apply(base)
	if l.Name() != "30/60 monthlies" || (backtest.Ladder{DTEs: []int{30, 60}, Weeklies: true}).Name() != "30/60" {
		t.Errorf("names: %q", l.Name())
	}
	slots := got.Slots()
	if len(slots) != 4 || slots[0].TargetDTE != 30 || slots[3].TargetDTE != 60 || slots[3].EntryDelta != 0.16 {
		t.Errorf("slots = %+v", slots)
	}
	got.DTEDeltaMatrix[0].Deltas[0] = 0.5
	if base.DTEDeltaMatrix[0].DTE != 25 || base.DTEDeltaMatrix[0].Deltas[0] != 0.12 {
		t.Error("Apply must not modify the base config")
	}
}
