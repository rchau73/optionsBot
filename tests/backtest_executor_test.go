package tests

import (
	"context"
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

func TestSimExecutor_CommissionDeducted(t *testing.T) {
	exec := newTestExec()
	feedTick(exec, "BTC-PERP", 100, time.Now())

	_, _ = exec.Submit(context.Background(), orders.Order{
		Instrument: "BTC-PERP",
		Direction:  orders.DirectionSell,
		OrderType:  orders.TypeMarket,
		Qty:        1.0,
	})
	if exec.TotalCommission() == 0 {
		t.Error("expected commission > 0 after fill")
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
