package tests

import (
	"math"
	"testing"

	"optionsbot/internal/backtest"
)

func TestSharpe_ZeroStdDev(t *testing.T) {
	// Constant returns → zero std dev → Sharpe = 0
	returns := []float64{0.01, 0.01, 0.01}
	got := backtest.Sharpe(returns)
	if got != 0 {
		t.Errorf("expected 0 for constant returns, got %v", got)
	}
}

func TestSharpe_PositiveReturns(t *testing.T) {
	// Alternating slightly different returns so std dev > 0
	returns := make([]float64, 252)
	for i := range returns {
		if i%2 == 0 {
			returns[i] = 0.002
		} else {
			returns[i] = 0.001
		}
	}
	got := backtest.Sharpe(returns)
	if got <= 0 {
		t.Errorf("expected positive Sharpe for net-positive varying returns, got %v", got)
	}
}

func TestSharpe_InsufficientData(t *testing.T) {
	got := backtest.Sharpe([]float64{0.01})
	if got != 0 {
		t.Errorf("expected 0 for single data point, got %v", got)
	}
}

func TestSortino_NoDownsideReturns(t *testing.T) {
	returns := []float64{0.01, 0.02, 0.005}
	got := backtest.Sortino(returns)
	if got != 0 {
		t.Errorf("expected 0 when no downside returns, got %v", got)
	}
}

func TestSortino_WithDownside(t *testing.T) {
	returns := []float64{0.01, -0.02, 0.005, -0.01}
	got := backtest.Sortino(returns)
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("Sortino returned invalid value: %v", got)
	}
}

func TestMaxDrawdown_FlatCurve(t *testing.T) {
	curve := []float64{100, 100, 100}
	dd, ddUSD := backtest.MaxDrawdown(curve)
	if dd != 0 || ddUSD != 0 {
		t.Errorf("expected 0 drawdown for flat curve, got %.4f / %.4f", dd, ddUSD)
	}
}

func TestMaxDrawdown_SingleDip(t *testing.T) {
	curve := []float64{100, 80, 90, 100}
	dd, ddUSD := backtest.MaxDrawdown(curve)
	if math.Abs(dd-0.20) > 0.001 {
		t.Errorf("expected 20%% drawdown, got %.4f", dd)
	}
	if math.Abs(ddUSD-20) > 0.001 {
		t.Errorf("expected $20 drawdown, got %.4f", ddUSD)
	}
}

func TestWinRate_AllWinners(t *testing.T) {
	pnls := []float64{10, 20, 5}
	got := backtest.WinRate(pnls)
	if got != 100 {
		t.Errorf("expected 100%% win rate, got %.1f", got)
	}
}

func TestWinRate_Mixed(t *testing.T) {
	pnls := []float64{10, -5, 20, -3}
	got := backtest.WinRate(pnls)
	if math.Abs(got-50) > 0.01 {
		t.Errorf("expected 50%% win rate, got %.1f", got)
	}
}
