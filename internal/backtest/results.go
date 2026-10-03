package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Summary is written to summary.json.
type Summary struct {
	TotalTrades            int     `json:"total_trades"`
	WinRatePct             float64 `json:"win_rate_pct"`
	TotalPnLUSD            float64 `json:"total_pnl_usd"`
	MaxDrawdownUSD         float64 `json:"max_drawdown_usd"`
	MaxDrawdownPct         float64 `json:"max_drawdown_pct"`
	SharpeRatio            float64 `json:"sharpe_ratio"`
	SortinoRatio           float64 `json:"sortino_ratio"`
	CalmarRatio            float64 `json:"calmar_ratio"`
	AvgHoldDays            float64 `json:"avg_hold_days"`
	AvgROIPct              float64 `json:"avg_roi_pct"`
	AvgROIAnnualized       float64 `json:"avg_roi_annualized"`
	TotalCommissionUSD     float64 `json:"total_commission_usd"`
	StopLossTriggers       int     `json:"stop_loss_triggers"`
	GammaCloseTriggers     int     `json:"gamma_close_triggers"`
	Rollout19DTE           int     `json:"rollout_19dte"`
	RolloutDeltaDrift      int     `json:"rollout_delta_drift"`
	RolloutROI             int     `json:"rollout_roi"`
	AvgThetaCapturedUSD    float64 `json:"avg_theta_captured_usd"`
	AvgIVAtEntry           float64 `json:"avg_iv_at_entry"`
	AvgIVPercentileAtEntry float64 `json:"avg_iv_percentile_at_entry"`
	Scenario               string  `json:"scenario,omitempty"`
}

// ResultWriter writes backtest output files.
type ResultWriter struct {
	dir string
}

// NewResultWriter creates dir (if needed) and returns a writer for it.
func NewResultWriter(dir string) (*ResultWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create results dir %s: %w", dir, err)
	}
	return &ResultWriter{dir: dir}, nil
}

func (w *ResultWriter) WriteSummary(s Summary) error {
	return writeJSON(filepath.Join(w.dir, "summary.json"), s)
}

func (w *ResultWriter) WriteEquityCurve(snapshots []PortfolioSnapshot) error {
	header := []string{"date", "equity_usd", "open_positions", "margin_used_pct", "iv_percentile"}
	rows := make([][]string, 0, len(snapshots))
	for _, s := range snapshots {
		rows = append(rows, []string{
			s.Date.Format("2006-01-02"),
			fmtF(s.EquityUSD),
			strconv.Itoa(s.OpenPositions),
			fmtF(s.MarginUsedPct),
			fmtF(s.IVPercentile),
		})
	}
	return writeCSV(filepath.Join(w.dir, "equity_curve.csv"), header, rows)
}

func (w *ResultWriter) WriteDrawdown(snapshots []PortfolioSnapshot) error {
	header := []string{"date", "drawdown_usd", "drawdown_pct"}
	rows := make([][]string, 0, len(snapshots))
	for _, s := range snapshots {
		rows = append(rows, []string{
			s.Date.Format("2006-01-02"),
			fmtF(s.DrawdownUSD),
			fmtF(s.DrawdownPct),
		})
	}
	return writeCSV(filepath.Join(w.dir, "drawdown.csv"), header, rows)
}

func (w *ResultWriter) WriteTrades(trades []TradeRecord) error {
	header := []string{
		"entry_date", "exit_date", "exit_reason", "instrument", "option_type",
		"strike", "expiry", "qty", "entry_price", "exit_price",
		"premium_received", "close_cost", "pnl_usd", "roi_pct", "hold_days", "commission",
	}
	rows := make([][]string, 0, len(trades))
	for _, t := range trades {
		rows = append(rows, []string{
			t.EntryDate.Format("2006-01-02"),
			t.ExitDate.Format("2006-01-02"),
			t.ExitReason,
			t.Instrument,
			t.OptionType,
			fmtF(t.Strike),
			t.Expiry.Format("2006-01-02"),
			fmtF(t.Qty),
			fmtF(t.EntryPrice),
			fmtF(t.ExitPrice),
			fmtF(t.PremiumRecvd),
			fmtF(t.CloseCost),
			fmtF(t.PnLUSD),
			fmtF(t.ROIPct),
			strconv.Itoa(t.HoldDays),
			fmtF(t.Commission),
		})
	}
	return writeCSV(filepath.Join(w.dir, "trades.csv"), header, rows)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// writeCSV writes a header and rows to path, creating parent directories.
func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	cw := csv.NewWriter(f)
	if err := cw.Write(header); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := cw.WriteAll(rows); err != nil { // WriteAll flushes
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func fmtF(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}

// ScenarioResult adds scenario metadata to the Summary.
type ScenarioResult struct {
	Summary
	ScenarioName string    `json:"scenario_name"`
	RunAt        time.Time `json:"run_at"`
}

func (w *ResultWriter) WriteScenarioComparison(results []ScenarioResult) error {
	header := []string{
		"scenario", "sharpe_ratio", "sortino_ratio", "calmar_ratio",
		"total_pnl_usd", "win_rate_pct", "max_drawdown_pct", "total_trades",
	}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			r.ScenarioName,
			fmtF(r.SharpeRatio),
			fmtF(r.SortinoRatio),
			fmtF(r.CalmarRatio),
			fmtF(r.TotalPnLUSD),
			fmtF(r.WinRatePct),
			fmtF(r.MaxDrawdownPct),
			strconv.Itoa(r.TotalTrades),
		})
	}
	return writeCSV(filepath.Join(w.dir, "scenario_comparison.csv"), header, rows)
}

// WalkForwardResult represents one train/validate window.
type WalkForwardResult struct {
	Window         int     `json:"window"`
	TrainFrom      string  `json:"train_from"`
	TrainTo        string  `json:"train_to"`
	ValidateFrom   string  `json:"validate_from"`
	ValidateTo     string  `json:"validate_to"`
	TrainSharpe    float64 `json:"train_sharpe"`
	ValidateSharpe float64 `json:"validate_sharpe"`
	Degradation    float64 `json:"degradation_pct"`
	Overfit        bool    `json:"overfit"`
}

func (w *ResultWriter) WriteWalkForwardSummary(results []WalkForwardResult) error {
	header := []string{
		"window", "train_from", "train_to", "validate_from", "validate_to",
		"train_sharpe", "validate_sharpe", "degradation_pct", "overfit",
	}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			strconv.Itoa(r.Window),
			r.TrainFrom, r.TrainTo, r.ValidateFrom, r.ValidateTo,
			fmtF(r.TrainSharpe),
			fmtF(r.ValidateSharpe),
			fmtF(r.Degradation),
			strconv.FormatBool(r.Overfit),
		})
	}
	return writeCSV(filepath.Join(w.dir, "walk_forward", "walk_forward_summary.csv"), header, rows)
}

func (w *ResultWriter) Dir() string { return w.dir }

func (w *ResultWriter) WriteWindowResult(window int, phase string, s Summary) error {
	dir := filepath.Join(w.dir, "walk_forward")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create walk-forward dir: %w", err)
	}
	return writeJSON(filepath.Join(dir, fmt.Sprintf("window_%d_%s.json", window, phase)), s)
}
