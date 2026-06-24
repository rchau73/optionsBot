package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
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

func NewResultWriter(dir string) *ResultWriter {
	_ = os.MkdirAll(dir, 0755)
	return &ResultWriter{dir: dir}
}

func (w *ResultWriter) WriteSummary(s Summary) error {
	return writeJSON(w.dir+"/summary.json", s)
}

func (w *ResultWriter) WriteEquityCurve(snapshots []PortfolioSnapshot) error {
	f, err := os.Create(w.dir + "/equity_curve.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	_ = cw.Write([]string{"date", "equity_usd", "open_positions", "margin_used_pct", "iv_percentile"})
	for _, s := range snapshots {
		_ = cw.Write([]string{
			s.Date.Format("2006-01-02"),
			fmtF(s.EquityUSD),
			strconv.Itoa(s.OpenPositions),
			fmtF(s.MarginUsedPct),
			fmtF(s.IVPercentile),
		})
	}
	cw.Flush()
	return cw.Error()
}

func (w *ResultWriter) WriteDrawdown(snapshots []PortfolioSnapshot) error {
	f, err := os.Create(w.dir + "/drawdown.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	_ = cw.Write([]string{"date", "drawdown_usd", "drawdown_pct"})
	for _, s := range snapshots {
		_ = cw.Write([]string{
			s.Date.Format("2006-01-02"),
			fmtF(s.DrawdownUSD),
			fmtF(s.DrawdownPct),
		})
	}
	cw.Flush()
	return cw.Error()
}

func (w *ResultWriter) WriteTrades(trades []TradeRecord) error {
	f, err := os.Create(w.dir + "/trades.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	_ = cw.Write([]string{
		"entry_date", "exit_date", "exit_reason", "instrument", "option_type",
		"strike", "expiry", "qty", "entry_price", "exit_price",
		"premium_received", "close_cost", "pnl_usd", "roi_pct", "hold_days", "commission",
	})
	for _, t := range trades {
		_ = cw.Write([]string{
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
	cw.Flush()
	return cw.Error()
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
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
	f, err := os.Create(w.dir + "/scenario_comparison.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	_ = cw.Write([]string{
		"scenario", "sharpe_ratio", "sortino_ratio", "calmar_ratio",
		"total_pnl_usd", "win_rate_pct", "max_drawdown_pct", "total_trades",
	})
	for _, r := range results {
		_ = cw.Write([]string{
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
	cw.Flush()
	return cw.Error()
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
	dir := w.dir + "/walk_forward"
	_ = os.MkdirAll(dir, 0755)
	f, err := os.Create(dir + "/walk_forward_summary.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	_ = cw.Write([]string{
		"window", "train_from", "train_to", "validate_from", "validate_to",
		"train_sharpe", "validate_sharpe", "degradation_pct", "overfit",
	})
	for _, r := range results {
		overfit := "false"
		if r.Overfit {
			overfit = "true"
		}
		_ = cw.Write([]string{
			strconv.Itoa(r.Window),
			r.TrainFrom, r.TrainTo, r.ValidateFrom, r.ValidateTo,
			fmtF(r.TrainSharpe),
			fmtF(r.ValidateSharpe),
			fmtF(r.Degradation),
			overfit,
		})
	}
	cw.Flush()
	return cw.Error()
}

func (w *ResultWriter) Dir() string { return w.dir }

func (w *ResultWriter) WriteWindowResult(window int, phase string, s Summary) error {
	dir := w.dir + "/walk_forward"
	_ = os.MkdirAll(dir, 0755)
	path := fmt.Sprintf("%s/window_%d_%s.json", dir, window, phase)
	return writeJSON(path, s)
}
