package backtest

import (
	"math"

	"gonum.org/v1/gonum/stat"
)

// Sharpe computes the annualised Sharpe ratio from daily returns.
func Sharpe(dailyReturns []float64) float64 {
	if len(dailyReturns) < 2 {
		return 0
	}
	mean := stat.Mean(dailyReturns, nil)
	std := stat.StdDev(dailyReturns, nil)
	if std == 0 {
		return 0
	}
	return (mean / std) * math.Sqrt(252)
}

// Sortino computes the annualised Sortino ratio using downside deviation.
func Sortino(dailyReturns []float64) float64 {
	if len(dailyReturns) < 2 {
		return 0
	}
	mean := stat.Mean(dailyReturns, nil)
	var downside []float64
	for _, r := range dailyReturns {
		if r < 0 {
			downside = append(downside, r*r)
		}
	}
	if len(downside) == 0 {
		return 0
	}
	dd := math.Sqrt(stat.Mean(downside, nil))
	if dd == 0 {
		return 0
	}
	return (mean / dd) * math.Sqrt(252)
}

// Calmar computes the Calmar ratio: annualised return / max drawdown.
func Calmar(dailyReturns []float64, maxDrawdown float64) float64 {
	if maxDrawdown == 0 || len(dailyReturns) == 0 {
		return 0
	}
	annReturn := stat.Mean(dailyReturns, nil) * 252
	return annReturn / math.Abs(maxDrawdown)
}

// MaxDrawdown computes max drawdown (as a fraction) from the equity curve.
func MaxDrawdown(equityCurve []float64) (maxDD float64, maxDDUSD float64) {
	if len(equityCurve) == 0 {
		return 0, 0
	}
	peak := equityCurve[0]
	for _, v := range equityCurve {
		if v > peak {
			peak = v
		}
		dd := (peak - v) / peak
		ddUSD := peak - v
		if dd > maxDD {
			maxDD = dd
			maxDDUSD = ddUSD
		}
	}
	return maxDD, maxDDUSD
}

// WinRate returns the fraction of trades with positive PnL.
func WinRate(pnls []float64) float64 {
	if len(pnls) == 0 {
		return 0
	}
	wins := 0
	for _, p := range pnls {
		if p > 0 {
			wins++
		}
	}
	return float64(wins) / float64(len(pnls)) * 100
}

// Mean is a convenience wrapper.
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	return stat.Mean(xs, nil)
}
