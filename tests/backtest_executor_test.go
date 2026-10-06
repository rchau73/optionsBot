package tests

import (
	"context"
	"math"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

func newTestExec() *backtest.SimExecutor {
	return backtest.NewSimExecutor(config.Backtest{
		FillModel:             "mid",
		SlippagePct:           0.001,
		LimitFillRule:         "immediate",
		CommissionPerContract: 0.0003,
	}, 100_000)
}

func feedTick(exec *backtest.SimExecutor, instrument string, mid float64, ts time.Time) {
	exec.UpdateTick(&marketdata.Tick{
		Timestamp:  ts,
		Instrument: instrument,
		Mid:        mid,
	})
}

func TestSimExecutor_MarketOrderFillsImmediately(t *testing.T) {
	exec := newTestExec()
	now := time.Now()
	feedTick(exec, "BTC-PERP", 50000, now)

	fill, err := exec.Submit(context.Background(), orders.Order{
		Instrument:    "BTC-PERP",
		Direction:     orders.DirectionSell,
		OrderType:     orders.TypeMarket,
		Qty:           1.0,
		TriggerReason: orders.TriggerEntry,
	})
	if err != nil {
		t.Fatalf("market order failed: %v", err)
	}
	// Fill price should be approximately mid ± slippage
	if fill.FillPrice < 49900 || fill.FillPrice > 50100 {
		t.Errorf("unexpected fill price: %v", fill.FillPrice)
	}
}

// Deribit's option fee in the feed's USD prices: 0.03 % of the underlying
// per contract, capped at 12.5 % of the option price.
func TestSimExecutor_CommissionDeducted(t *testing.T) {
	cases := []struct {
		name     string
		mid, fee float64
	}{
		{"0.03 % of the underlying", 2000, 30},            // 0.0003 × 100,000
		{"capped at 12.5 % of a cheap option", 100, 12.5}, // 0.125 × 100 (fills at mid ± slippage)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := newTestExec()
			exec.UpdateTick(&marketdata.Tick{Timestamp: time.Now(), Instrument: "BTC-X-C", Mid: tc.mid, UnderlyingPrice: 100_000})
			fill, err := exec.Submit(context.Background(), orders.Order{
				Instrument: "BTC-X-C", Direction: orders.DirectionSell, OrderType: orders.TypeMarket, Qty: 2,
			})
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(fill.Fee-2*tc.fee) > 0.05 || math.Abs(exec.TotalCommission()-fill.Fee) > 1e-9 {
				t.Errorf("fee = %v (total %v), want ≈ %v for 2 contracts", fill.Fee, exec.TotalCommission(), 2*tc.fee)
			}
		})
	}
}

func TestSimExecutor_CancelRemovesPending(t *testing.T) {
	cfg := config.Backtest{
		FillModel:             "mid",
		SlippagePct:           0.001,
		LimitFillRule:         "next_tick",
		CommissionPerContract: 0.0003,
	}
	exec := backtest.NewSimExecutor(cfg, 100_000)
	feedTick(exec, "BTC-PERP", 100, time.Now())

	fill, _ := exec.Submit(context.Background(), orders.Order{
		Instrument: "BTC-PERP",
		Direction:  orders.DirectionBuy,
		OrderType:  orders.TypeLimit,
		Qty:        1.0,
		LimitPrice: 90, // won't fill at 100
	})
	if err := exec.Cancel(context.Background(), fill.OrderID); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	// Commission should remain 0 (order not yet filled)
	if exec.TotalCommission() > 0 {
		t.Error("commission should be 0 for cancelled unfilled limit order")
	}
}

func TestSimExecutor_AccountEquity(t *testing.T) {
	exec := newTestExec()
	eq, err := exec.AccountEquity(context.Background(), "BTC")
	if err != nil {
		t.Fatal(err)
	}
	if eq != 100_000 {
		t.Errorf("expected 100000, got %v", eq)
	}
	exec.AdjustEquity(5000)
	eq, _ = exec.AccountEquity(context.Background(), "BTC")
	if eq != 105_000 {
		t.Errorf("expected 105000 after adjustment, got %v", eq)
	}
}
