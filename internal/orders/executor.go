package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/gateway"
)

// ParsePriceTooLow extracts the exchange minimum price from a Deribit price_too_low
// error message. Deribit embeds the floor directly in the message:
//
//	"price_too_low 0.0046"
//
// Returns 0 if the message is not a price_too_low error or cannot be parsed.
func ParsePriceTooLow(msg string) float64 {
	const prefix = "price_too_low "
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return 0
	}
	fields := strings.Fields(msg[idx+len(prefix):])
	if len(fields) == 0 {
		return 0
	}
	price, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0
	}
	return price
}

// tickSize is the minimum price increment for Deribit BTC/ETH options (0.0001).
const tickSize = 0.0001

// RoundToTick rounds a price to the nearest Deribit tick increment,
// eliminating floating-point artifacts like 107*0.0001 = 0.010700000000000001.
func RoundToTick(price float64) float64 {
	n := math.Round(price / tickSize)
	return math.Round(n*tickSize*1e8) / 1e8
}

func roundToTick(price float64) float64 { return RoundToTick(price) }

// Executor submits, amends and cancels orders against the live Deribit API.
type Executor struct {
	gw *gateway.Gateway
}

func NewExecutor(gw *gateway.Gateway) *Executor {
	return &Executor{gw: gw}
}

// Submit places an order and returns the Fill on success.
func (e *Executor) Submit(ctx context.Context, order Order) (Fill, error) {
	method := "private/buy"
	if order.Direction == DirectionSell {
		method = "private/sell"
	}

	params := map[string]interface{}{
		"instrument_name": order.Instrument,
		"amount":          order.Qty,
		"type":            order.OrderType,
	}
	tick := order.TickSize
	if tick <= 0 {
		tick = tickSize
	}
	if order.OrderType == TypeLimit {
		// Round to tick, then eliminate floating-point residue (e.g. 107*0.0001 = 0.010700000000000001).
		n := math.Round(order.LimitPrice / tick)
		params["price"] = math.Round(n*tick*1e8) / 1e8
		params["post_only"] = false
	}

	priority := gateway.PriorityLow
	if order.TriggerReason == TriggerStopLoss200Pct ||
		order.TriggerReason == TriggerKillSwitch ||
		order.TriggerReason == TriggerGammaClose {
		priority = gateway.PriorityHigh
	}

	slog.Debug("submitting order",
		"method", method,
		"instrument", order.Instrument,
		"amount", order.Qty,
		"type", order.OrderType,
		"price", params["price"],
		"tick_size", tick,
		"reason", order.TriggerReason,
	)

	resp, err := e.gw.Call(ctx, method, params, priority)
	if err != nil && order.OrderType == TypeLimit {
		// price_too_low (code 10005): the ask in our instrument cache was stale by
		// up to 100ms and the exchange minimum ticked up. Deribit tells us the floor
		// in the error message — round up to the next valid tick and retry once.
		if rpcErr, ok := err.(*gateway.RPCError); ok && rpcErr.Code == 10005 {
			if minPrice := ParsePriceTooLow(rpcErr.Message); minPrice > 0 {
				n := math.Ceil(minPrice / tick)
				adjusted := math.Round(n*tick*1e8) / 1e8
				slog.Info("submit: price_too_low — retrying at exchange minimum",
					"instrument", order.Instrument,
					"original_price", params["price"],
					"exchange_minimum", minPrice,
					"adjusted_price", adjusted,
				)
				params["price"] = adjusted
				resp, err = e.gw.Call(ctx, method, params, priority)
			}
		}
	}
	if err != nil {
		return Fill{}, fmt.Errorf("submit order [instrument=%s amount=%v price=%v]: %w",
			order.Instrument, order.Qty, params["price"], err)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return Fill{}, err
	}

	var result struct {
		Order struct {
			OrderID    string  `json:"order_id"`
			FilledAmt  float64 `json:"filled_amount"`
			AvgPrice   float64 `json:"average_price"`
			OrderState string  `json:"order_state"`
		} `json:"order"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return Fill{}, err
	}

	slog.Info("order submitted",
		"order_id", result.Order.OrderID,
		"instrument", order.Instrument,
		"direction", order.Direction,
		"type", order.OrderType,
		"qty", order.Qty,
		"reason", order.TriggerReason,
	)

	return Fill{
		OrderID:   result.Order.OrderID,
		FillPrice: result.Order.AvgPrice,
		Qty:       result.Order.FilledAmt,
		Timestamp: time.Now(),
	}, nil
}

// Cancel cancels an open order by order ID.
func (e *Executor) Cancel(ctx context.Context, orderID string) error {
	_, err := e.gw.Call(ctx, "private/cancel", map[string]interface{}{
		"order_id": orderID,
	}, gateway.PriorityHigh)
	return err
}

// CancelAll cancels all open orders for the given instrument.
func (e *Executor) CancelAll(ctx context.Context, instrument string) error {
	_, err := e.gw.Call(ctx, "private/cancel_all_by_instrument", map[string]interface{}{
		"instrument_name": instrument,
	}, gateway.PriorityHigh)
	return err
}

// CancelAllOrders cancels every open order for the entire account.
// Called on startup because the bot is the sole manager of this account;
// any orders left from before a restart are stale and must be cleared.
func (e *Executor) CancelAllOrders(ctx context.Context) error {
	_, err := e.gw.Call(ctx, "private/cancel_all", map[string]any{}, gateway.PriorityHigh)
	return err
}

// GetOrderState returns the current fill status of a single order.
func (e *Executor) GetOrderState(ctx context.Context, orderID string) (OrderStateInfo, error) {
	resp, err := e.gw.Call(ctx, "private/get_order_state", map[string]interface{}{
		"order_id": orderID,
	}, gateway.PriorityLow)
	if err != nil {
		return OrderStateInfo{}, err
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return OrderStateInfo{}, err
	}
	var s OrderStateInfo
	if err := json.Unmarshal(b, &s); err != nil {
		return OrderStateInfo{}, err
	}
	return s, nil
}

// AmendOrder updates the price (and optionally qty) of an open limit order.
func (e *Executor) AmendOrder(ctx context.Context, orderID string, qty, price float64) error {
	_, err := e.gw.Call(ctx, "private/edit", map[string]interface{}{
		"order_id": orderID,
		"amount":   qty,
		"price":    RoundToTick(price),
	}, gateway.PriorityLow)
	return err
}

// GetPositions returns all open option positions for the given currency.
func (e *Executor) GetPositions(ctx context.Context, currency string) ([]RawPosition, error) {
	resp, err := e.gw.Call(ctx, "private/get_positions", map[string]interface{}{
		"currency": currency,
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	var positions []RawPosition
	if err := json.Unmarshal(b, &positions); err != nil {
		return nil, err
	}
	return positions, nil
}

// GetAccountSummary returns the full account summary including margin details.
func (e *Executor) GetAccountSummary(ctx context.Context, currency string) (AccountSummary, error) {
	resp, err := e.gw.Call(ctx, "private/get_account_summary", map[string]interface{}{
		"currency": currency,
		"extended": true,
	}, gateway.PriorityLow)
	if err != nil {
		return AccountSummary{}, err
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return AccountSummary{}, err
	}
	var s AccountSummary
	if err := json.Unmarshal(b, &s); err != nil {
		return AccountSummary{}, err
	}
	return s, nil
}

// GetDailyCloses fetches daily closing prices for the given instrument going back
// the requested number of days. Uses Deribit's TradingView chart data endpoint.
func (e *Executor) GetDailyCloses(ctx context.Context, instrument string, days int) ([]DailyClose, error) {
	now := time.Now().UTC()
	startMs := now.AddDate(0, 0, -days).UnixMilli()
	endMs := now.UnixMilli()

	resp, err := e.gw.Call(ctx, "public/get_tradingview_chart_data", map[string]interface{}{
		"instrument_name": instrument,
		"start_timestamp": startMs,
		"end_timestamp":   endMs,
		"resolution":      "1D",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, fmt.Errorf("get_tradingview_chart_data %s: %w", instrument, err)
	}

	b, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status string    `json:"status"`
		Ticks  []int64   `json:"ticks"`
		Close  []float64 `json:"close"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	if result.Status != "ok" {
		return nil, fmt.Errorf("get_tradingview_chart_data %s: status=%s", instrument, result.Status)
	}

	closes := make([]DailyClose, len(result.Ticks))
	for i, ts := range result.Ticks {
		closes[i] = DailyClose{
			Date:  time.UnixMilli(ts).UTC().Truncate(24 * time.Hour),
			Close: result.Close[i],
		}
	}
	return closes, nil
}

// GetMargins returns the incremental initial and maintenance margin that
// private/get_margins estimates for a given instrument, amount and price.
// Under Portfolio Margin this reflects the portfolio-level impact of the order.
func (e *Executor) GetMargins(ctx context.Context, instrument string, amount, price float64) (MarginInfo, error) {
	resp, err := e.gw.Call(ctx, "private/get_margins", map[string]interface{}{
		"instrument_name": instrument,
		"amount":          amount,
		"price":           price,
	}, gateway.PriorityLow)
	if err != nil {
		return MarginInfo{}, err
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return MarginInfo{}, err
	}
	var info MarginInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return MarginInfo{}, err
	}
	return info, nil
}

// AccountEquity fetches the current account equity in USD.
func (e *Executor) AccountEquity(ctx context.Context, currency string) (float64, error) {
	resp, err := e.gw.Call(ctx, "private/get_account_summary", map[string]interface{}{
		"currency":  currency,
		"extended":  true,
	}, gateway.PriorityLow)
	if err != nil {
		return 0, err
	}
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return 0, err
	}
	// For Portfolio Margin accounts, available_funds already reflects Deribit's
	// actual margin requirements. Using it (not equity) lets max_margin_pct act
	// as a pure safety cap on top of Deribit's own risk model.
	var summary struct {
		AvailableFunds float64 `json:"available_funds"`
	}
	if err := json.Unmarshal(resultBytes, &summary); err != nil {
		return 0, err
	}
	return summary.AvailableFunds, nil
}
