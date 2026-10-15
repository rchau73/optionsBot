package backtest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/config"
)

// Ladder is one DTE ladder for the ladder sweep: one slot per DTE, each at the
// base config's entry deltas. Weeklies=false hides every non-monthly expiry.
type Ladder struct {
	DTEs     []int
	Weeklies bool
}

// Name reads like "25/50" or "25/50 monthlies".
func (l Ladder) Name() string {
	parts := make([]string, len(l.DTEs))
	for i, d := range l.DTEs {
		parts[i] = strconv.Itoa(d)
	}
	name := strings.Join(parts, "/")
	if !l.Weeklies {
		name += " monthlies"
	}
	return name
}

// DefaultLadders are today's ladders (BTC 25/50, ETH 30/50) and the
// candidates of issue #89, each with and without weeklies: monthlies are
// 28–35 days apart, wider than a slot's ±10-DTE window.
func DefaultLadders() []Ladder {
	var out []Ladder
	for _, dtes := range [][]int{{25, 50}, {30, 50}, {30, 60}, {35, 60}} {
		out = append(out, Ladder{DTEs: dtes, Weeklies: true}, Ladder{DTEs: dtes, Weeklies: false})
	}
	return out
}

// Apply returns a copy of cfg whose dte_delta_matrix is this ladder, every
// slot at the deltas of cfg's first matrix row (cfg.EntryDelta without one).
func (l Ladder) Apply(cfg *config.Config) *config.Config {
	deltas := []float64{cfg.EntryDelta}
	if len(cfg.DTEDeltaMatrix) > 0 {
		deltas = cfg.DTEDeltaMatrix[0].Deltas
	}
	out := *cfg
	out.DTEDeltaMatrix = make([]config.DTEDeltaEntry, len(l.DTEs))
	for i, dte := range l.DTEs {
		out.DTEDeltaMatrix[i] = config.DTEDeltaEntry{DTE: dte, Deltas: append([]float64(nil), deltas...)}
	}
	out.TargetDTE = append([]int(nil), l.DTEs...)
	return &out
}

// IsMonthlyExpiry reports whether expiry is a monthly: the last Friday of its
// month (Deribit's monthlies and quarterlies).
func IsMonthlyExpiry(expiry time.Time) bool {
	return expiry.Weekday() == time.Friday && expiry.AddDate(0, 0, 7).Month() != expiry.Month()
}

// WorstWeek is the largest equity loss over any 7 calendar days of the
// snapshots: the change from each day to the last snapshot at most 7 days
// later, in USD and in percent of the starting day's equity. Both are ≤ 0;
// with fewer than two snapshots they are 0.
func WorstWeek(snaps []PortfolioSnapshot) (usd, pct float64) {
	for i, start := range snaps {
		end := i
		for j := i + 1; j < len(snaps) && !snaps[j].Date.After(start.Date.AddDate(0, 0, 7)); j++ {
			end = j
		}
		if end == i {
			continue
		}
		change := snaps[end].EquityUSD - start.EquityUSD
		if change < usd {
			usd = change
			if start.EquityUSD > 0 {
				pct = change / start.EquityUSD * 100
			}
		}
	}
	return usd, pct
}

// LadderResult is one row of ladder_comparison.csv.
type LadderResult struct {
	Ladder              Ladder
	Slots               []SlotOccupancy
	OccupancyPct        float64 // mean over the slots
	MinSlotOccupancyPct float64
	TotalPnLUSD         float64
	ReturnOnMarginPct   float64 // net P&L ÷ mean premium held (the backtest's margin stand-in)
	WorstWeekUSD        float64
	WorstWeekPct        float64
	MaxDrawdownPct      float64
	Trades              int
	SharedExpiryDays    int // days two slots held the same expiry; must be 0
}

// RunLadderSweep runs every ladder of DefaultLadders over the CSV and writes
// ladder_comparison.csv (slot occupancy, return on margin, worst week) in the
// order of DefaultLadders. It fails if any ladder fails.
func RunLadderSweep(cfg *config.Config, csvPath string, from, to time.Time, outputDir string) ([]LadderResult, error) {
	ladders := DefaultLadders()
	results := make([]LadderResult, len(ladders))
	errs := make([]error, len(ladders))
	var wg sync.WaitGroup
	for i, l := range ladders {
		wg.Add(1)
		go func(i int, l Ladder) {
			defer wg.Done()
			res, err := runLadder(cfg, l, csvPath, from, to)
			if err != nil {
				errs[i] = fmt.Errorf("ladder %s: %w", l.Name(), err)
				return
			}
			results[i] = res
		}(i, l)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	w, err := NewResultWriter(outputDir)
	if err != nil {
		return nil, err
	}
	return results, w.WriteLadderComparison(results)
}

func runLadder(base *config.Config, l Ladder, csvPath string, from, to time.Time) (LadderResult, error) {
	cfg := l.Apply(base)
	feed, err := NewHistoricalFeed(csvPath, from, to, cfg.IVPercentileWindow)
	if err != nil {
		return LadderResult{}, err
	}
	engine := NewEngine(cfg, feed, NewSimExecutor(cfg.Backtest, 100_000))
	engine.SetMonthlyOnly(!l.Weeklies)
	summary, err := engine.Run(context.Background())
	if err != nil {
		return LadderResult{}, err
	}

	res := LadderResult{
		Ladder:           l,
		Slots:            engine.Occupancy(),
		TotalPnLUSD:      summary.TotalPnLUSD,
		MaxDrawdownPct:   summary.MaxDrawdownPct,
		Trades:           summary.TotalTrades,
		SharedExpiryDays: engine.SharedExpiryDays(),
	}
	res.MinSlotOccupancyPct = math.Inf(1)
	for _, s := range res.Slots {
		res.OccupancyPct += s.Pct() / float64(len(res.Slots))
		res.MinSlotOccupancyPct = math.Min(res.MinSlotOccupancyPct, s.Pct())
	}
	if len(res.Slots) == 0 {
		res.MinSlotOccupancyPct = 0
	}
	if m := engine.AvgPremiumHeld(); m > 0 {
		res.ReturnOnMarginPct = summary.TotalPnLUSD / m * 100
	}
	res.WorstWeekUSD, res.WorstWeekPct = WorstWeek(engine.Snapshots())

	slog.Info("ladder backtest done", "ladder", l.Name(),
		"occupancy_pct", fmt.Sprintf("%.1f", res.OccupancyPct),
		"min_slot_occupancy_pct", fmt.Sprintf("%.1f", res.MinSlotOccupancyPct),
		"return_on_margin_pct", fmt.Sprintf("%.1f", res.ReturnOnMarginPct),
		"worst_week_pct", fmt.Sprintf("%.2f", res.WorstWeekPct),
		"shared_expiry_days", res.SharedExpiryDays)
	return res, nil
}

// WriteLadderComparison writes ladder_comparison.csv, one row per ladder;
// slot_occupancy lists each slot as "dte:pct".
func (w *ResultWriter) WriteLadderComparison(results []LadderResult) error {
	header := []string{"ladder", "weeklies", "slot_occupancy", "occupancy_pct", "min_slot_occupancy_pct",
		"total_pnl_usd", "return_on_margin_pct", "worst_week_usd", "worst_week_pct",
		"max_drawdown_pct", "trades", "shared_expiry_days"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		slots := make([]string, len(r.Slots))
		for i, s := range r.Slots {
			slots[i] = fmt.Sprintf("%d:%.1f", s.TargetDTE, s.Pct())
		}
		rows = append(rows, []string{
			r.Ladder.Name(),
			strconv.FormatBool(r.Ladder.Weeklies),
			strings.Join(slots, " "),
			fmtF(r.OccupancyPct),
			fmtF(r.MinSlotOccupancyPct),
			fmtF(r.TotalPnLUSD),
			fmtF(r.ReturnOnMarginPct),
			fmtF(r.WorstWeekUSD),
			fmtF(r.WorstWeekPct),
			fmtF(r.MaxDrawdownPct),
			strconv.Itoa(r.Trades),
			strconv.Itoa(r.SharedExpiryDays),
		})
	}
	return writeCSV(filepath.Join(w.dir, "ladder_comparison.csv"), header, rows)
}
