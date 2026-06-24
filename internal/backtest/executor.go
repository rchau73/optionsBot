package backtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// SimExecutor is the backtest simulated order executor. It fills limit orders on
// the next tick where the price condition is met, and market orders immediately.
type SimExecutor struct {
	mu           sync.Mutex
	cfg          config.Backtest
	equity       float64
	instruments  map[string]*marketdata.Tick // latest tick per instrument
	pendingLimits []pendingLimit
	nextOrderID  int
	commission   float64
	totalCommission float64
}

type pendingLimit struct {
	orderID    string
	order      orders.Order
	submitTime time.Time
}

func NewSimExecutor(cfg config.Backtest, startEquity float64) *SimExecutor {
	return &SimExecutor{
		cfg:         cfg,
		equity:      startEquity,
		instruments: make(map[string]*marketdata.Tick),
		commission:  cfg.CommissionPerContract,
	}
}

// UpdateTick records the latest market data for fill processing.
func (e *SimExecutor) UpdateTick(tick *marketdata.Tick) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.instruments[tick.Instrument] = tick
	e.processPendingLimits(tick)
}

func (e *SimExecutor) Submit(ctx context.Context, order orders.Order) (orders.Fill, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.nextOrderID++
	orderID := fmt.Sprintf("bt-%d", e.nextOrderID)

	tick, ok := e.instruments[order.Instrument]
	if !ok {
		return orders.Fill{}, fmt.Errorf("no market data for %s", order.Instrument)
	}

	var fillPrice float64

	switch order.OrderType {
	case orders.TypeMarket:
		slippage := e.cfg.SlippagePct * tick.Mid
		if order.Direction == orders.DirectionBuy {
			fillPrice = tick.Mid + slippage
		} else {
			fillPrice = tick.Mid - slippage
		}

	case orders.TypeLimit:
		if e.cfg.LimitFillRule == "next_tick" {
			e.pendingLimits = append(e.pendingLimits, pendingLimit{
				orderID:    orderID,
				order:      order,
				submitTime: tick.Timestamp,
			})
			// Return provisional fill — will be confirmed on next tick
			return orders.Fill{
				OrderID:   orderID,
				FillPrice: order.LimitPrice,
				Qty:       order.Qty,
				Timestamp: tick.Timestamp,
			}, nil
		}
		fillPrice = order.LimitPrice
	}

	fillPrice = roundPrice(fillPrice)
	commission := e.commission * order.Qty
	e.totalCommission += commission
	e.equity -= commission

	return orders.Fill{
		OrderID:   orderID,
		FillPrice: fillPrice,
		Qty:       order.Qty,
		Timestamp: tick.Timestamp,
	}, nil
}

func (e *SimExecutor) processPendingLimits(tick *marketdata.Tick) {
	var remaining []pendingLimit
	for _, pl := range e.pendingLimits {
		if pl.order.Instrument != tick.Instrument {
			remaining = append(remaining, pl)
			continue
		}

		// 1-day expiry for limit fallback (rule 4.1)
		expired := tick.Timestamp.Sub(pl.submitTime) > 24*time.Hour

		filled := false
		switch pl.order.Direction {
		case orders.DirectionBuy:
			filled = tick.Mid <= pl.order.LimitPrice || expired
		case orders.DirectionSell:
			filled = tick.Mid >= pl.order.LimitPrice || expired
		}

		if !filled {
			remaining = append(remaining, pl)
		} else {
			commission := e.commission * pl.order.Qty
			e.totalCommission += commission
			e.equity -= commission
		}
	}
	e.pendingLimits = remaining
}

func (e *SimExecutor) Cancel(_ context.Context, orderID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	newPending := e.pendingLimits[:0]
	for _, pl := range e.pendingLimits {
		if pl.orderID != orderID {
			newPending = append(newPending, pl)
		}
	}
	e.pendingLimits = newPending
	return nil
}

func (e *SimExecutor) AccountEquity(_ context.Context, _ string) (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.equity, nil
}

func (e *SimExecutor) AdjustEquity(delta float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.equity += delta
}

func (e *SimExecutor) TotalCommission() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.totalCommission
}

func roundPrice(p float64) float64 {
	// Round to 4 decimal places
	const factor = 10000
	return float64(int64(p*factor+0.5)) / factor
}
