package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
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

// RoundToTick rounds a price to the nearest default Deribit tick (0.0001).
func RoundToTick(price float64) float64 { return RoundToStep(price, tickSize) }

// rpcCaller is the slice of the gateway the executor needs. Depending on this
// interface instead of *gateway.Gateway lets tests drive the executor with a fake.
type rpcCaller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Executor submits, amends and cancels orders against the live Deribit API.
type Executor struct {
	gw rpcCaller

	simMu   sync.Mutex
	nextSim time.Time // earliest time the next simulate_portfolio may go out
}

// simulateSpacing honours Deribit's limit of one simulate_portfolio call per
// second (portfolio margin is expensive to compute), with a little slack.
const simulateSpacing = 1100 * time.Millisecond

// NewExecutor returns an Executor that sends every request through gw.
func NewExecutor(gw rpcCaller) *Executor {
	return &Executor{gw: gw}
}

// ErrForbidden is returned when Deribit rejects a call for missing API key
// scopes or invalid credentials. Callers back off instead of retrying every tick.
var ErrForbidden = errors.New("forbidden: check API key scopes")

// deribitForbiddenCode is Deribit's error code for "forbidden".
const deribitForbiddenCode = 13021

// call sends one JSON-RPC request and decodes its result into T.
func call[T any](ctx context.Context, gw rpcCaller, method string, params any, priority int) (T, error) {
	var out T
	resp, err := gw.Call(ctx, method, params, priority)
	if err != nil {
		var rpcErr *gateway.RPCError
		if errors.As(err, &rpcErr) && (rpcErr.Code == deribitForbiddenCode || rpcErr.Message == "forbidden") {
			return out, fmt.Errorf("%s: %w: %w", method, ErrForbidden, err)
		}
		return out, fmt.Errorf("%s: %w", method, err)
	}
	if err := decodeResult(resp, &out); err != nil {
		return out, fmt.Errorf("%s: decode result: %w", method, err)
	}
	return out, nil
}

// decodeResult unmarshals the result field of a JSON-RPC response into out.
func decodeResult(resp gateway.JSONRPCResponse, out any) error {
	if len(resp.Result) == 0 {
		return nil // e.g. cancel replies we don't inspect
	}
	return json.Unmarshal(resp.Result, out)
}

// submitResult is the subset of private/buy and private/sell we use.
type submitResult struct {
	Order struct {
		OrderID    string  `json:"order_id"`
		FilledAmt  float64 `json:"filled_amount"`
		AvgPrice   float64 `json:"average_price"`
		OrderState string  `json:"order_state"`
	} `json:"order"`
}

// Submit places an order and returns the Fill on success.
func (e *Executor) Submit(ctx context.Context, order Order) (Fill, error) {
	method := "private/buy"
	if order.Direction == DirectionSell {
		method = "private/sell"
	}

	params := map[string]any{
		"instrument_name": order.Instrument,
		"amount":          order.Qty,
		"type":            order.OrderType,
	}
	tick := order.TickSize
	if tick <= 0 {
		tick = tickSize
	}
	if order.OrderType == TypeLimit {
		params["price"] = RoundToStep(order.LimitPrice, tick)
		params["post_only"] = false
	}
	if order.TimeInForce != "" {
		params["time_in_force"] = order.TimeInForce
	}
	if order.Label != "" {
		label := order.Label
		if len(label) > MaxLabelLen {
			label = label[:MaxLabelLen]
		}
		params["label"] = label
	}

	priority := submitPriority(order.TriggerReason)

	slog.Debug("submitting order",
		"method", method,
		"instrument", order.Instrument,
		"amount", order.Qty,
		"type", order.OrderType,
		"price", params["price"],
		"tick_size", tick,
		"reason", order.TriggerReason,
	)

	result, err := call[submitResult](ctx, e.gw, method, params, priority)
	if err != nil && order.OrderType == TypeLimit {
		// price_too_low (code 10005): the ask in our instrument cache was stale by
		// up to 100ms and the exchange minimum ticked up. Deribit tells us the floor
		// in the error message — round up to the next valid tick and retry once.
		var rpcErr *gateway.RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == deribitPriceTooLowCode {
			if minPrice := ParsePriceTooLow(rpcErr.Message); minPrice > 0 {
				adjusted := CeilToStep(minPrice, tick)
				slog.Info("submit: price_too_low — retrying at exchange minimum",
					"instrument", order.Instrument,
					"original_price", params["price"],
					"exchange_minimum", minPrice,
					"adjusted_price", adjusted,
				)
				params["price"] = adjusted
				result, err = call[submitResult](ctx, e.gw, method, params, priority)
			}
		}
	}
	if err != nil {
		return Fill{}, fmt.Errorf("submit order [instrument=%s amount=%v price=%v]: %w",
			order.Instrument, order.Qty, params["price"], err)
	}

	slog.Info("order submitted",
		"order_id", result.Order.OrderID,
		"instrument", order.Instrument,
		"direction", order.Direction,
		"type", order.OrderType,
		"qty", order.Qty,
		"filled_qty", result.Order.FilledAmt,
		"state", result.Order.OrderState,
		"reason", order.TriggerReason,
	)

	return Fill{
		OrderID:   result.Order.OrderID,
		FillPrice: result.Order.AvgPrice,
		Qty:       result.Order.FilledAmt,
		Timestamp: time.Now(),
	}, nil
}

// deribitPriceTooLowCode is Deribit's error code for a limit price below the
// exchange minimum; the message carries the minimum (see ParsePriceTooLow).
const deribitPriceTooLowCode = 10005

// submitPriority sends risk-reducing orders (stop-loss, kill switch, gamma
// close) through the gateway's high-priority queue so they jump ahead of
// market-data and entry traffic.
func submitPriority(reason string) int {
	switch reason {
	case TriggerStopLoss200Pct, TriggerKillSwitch, TriggerGammaClose:
		return gateway.PriorityHigh
	default:
		return gateway.PriorityLow
	}
}

// Cancel cancels an open order by order ID.
// CancelByLabel cancels every open order of currency carrying label and
// returns how many were cancelled. Used to make sure an order whose submit
// outcome is unknown can no longer fill.
func (e *Executor) CancelByLabel(ctx context.Context, currency, label string) (int, error) {
	return call[int](ctx, e.gw, "private/cancel_by_label", map[string]any{
		"label":    label,
		"currency": currency,
	}, gateway.PriorityHigh)
}

// MaybePlaced reports whether a failed order submit may still have reached
// the exchange: the connection dropped or the reply timed out after the
// request may have been written. A Deribit rejection (RPC error) or an open
// circuit breaker (never sent) is a definite "not placed".
func MaybePlaced(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *gateway.RPCError
	if errors.As(err, &rpcErr) || errors.Is(err, gateway.ErrCircuitOpen) || errors.Is(err, ErrForbidden) {
		return false
	}
	return true
}

func (e *Executor) Cancel(ctx context.Context, orderID string) error {
	_, err := call[any](ctx, e.gw, "private/cancel", map[string]any{
		"order_id": orderID,
	}, gateway.PriorityHigh)
	return err
}

// CancelAll cancels all open orders for the given instrument.
func (e *Executor) CancelAll(ctx context.Context, instrument string) error {
	_, err := call[any](ctx, e.gw, "private/cancel_all_by_instrument", map[string]any{
		"instrument_name": instrument,
	}, gateway.PriorityHigh)
	return err
}

// CancelAllOrders cancels every open order for one currency.
// Called on startup to clear stale orders from before a restart. Scoped to the
// currency (private/cancel_all_by_currency — private/cancel_all takes no
// currency and would clear every currency) so a BTC bot restart leaves an ETH
// bot's orders alone.
func (e *Executor) CancelAllOrders(ctx context.Context, currency string) error {
	_, err := call[any](ctx, e.gw, "private/cancel_all_by_currency", map[string]any{
		"currency": currency,
	}, gateway.PriorityHigh)
	return err
}

// GetOrderState returns the current fill status of a single order.
func (e *Executor) GetOrderState(ctx context.Context, orderID string) (OrderStateInfo, error) {
	return call[OrderStateInfo](ctx, e.gw, "private/get_order_state", map[string]any{
		"order_id": orderID,
	}, gateway.PriorityLow)
}

// AmendOrder updates the price (and optionally qty) of an open limit order.
func (e *Executor) AmendOrder(ctx context.Context, orderID string, qty, price float64) error {
	_, err := call[any](ctx, e.gw, "private/edit", map[string]any{
		"order_id": orderID,
		"amount":   qty,
		"price":    RoundToTick(price),
	}, gateway.PriorityLow)
	return err
}

// GetPositions returns all open option positions for the given currency.
func (e *Executor) GetPositions(ctx context.Context, currency string) ([]RawPosition, error) {
	return call[[]RawPosition](ctx, e.gw, "private/get_positions", map[string]any{
		"currency": currency,
		"kind":     "option",
	}, gateway.PriorityLow)
}

// GetAccountSummary returns the full account summary including margin details.
func (e *Executor) GetAccountSummary(ctx context.Context, currency string) (AccountSummary, error) {
	return call[AccountSummary](ctx, e.gw, "private/get_account_summary", map[string]any{
		"currency": currency,
		"extended": true,
	}, gateway.PriorityLow)
}

// SimulatePortfolio asks Deribit what the account's margin would be with
// positions (instrument → size in coin; negative = short) added to the current
// portfolio. Calls are spaced at least simulateSpacing apart.
func (e *Executor) SimulatePortfolio(ctx context.Context, currency string, positions map[string]float64) (AccountSummary, error) {
	return e.simulate(ctx, currency, positions, true)
}

// SimulateAlone asks Deribit for the margin of positions on their own, as if
// the account held nothing else: the standalone cost of a trade, without
// the offsets the rest of the book gives it.
func (e *Executor) SimulateAlone(ctx context.Context, currency string, positions map[string]float64) (AccountSummary, error) {
	return e.simulate(ctx, currency, positions, false)
}

func (e *Executor) simulate(ctx context.Context, currency string, positions map[string]float64, addToBook bool) (AccountSummary, error) {
	e.simMu.Lock()
	at := time.Now()
	if at.Before(e.nextSim) {
		at = e.nextSim
	}
	e.nextSim = at.Add(simulateSpacing)
	e.simMu.Unlock()

	if wait := time.Until(at); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return AccountSummary{}, ctx.Err()
		case <-t.C:
		}
	}
	sum, err := call[AccountSummary](ctx, e.gw, "private/simulate_portfolio", map[string]any{
		"currency":            currency,
		"add_positions":       addToBook,
		"simulated_positions": positions,
	}, gateway.PriorityLow)
	if err == nil && sum.Currency == "" {
		sum.Currency = currency
	}
	return sum, err
}

// chartData is the subset of public/get_tradingview_chart_data we use.
type chartData struct {
	Status string    `json:"status"`
	Ticks  []int64   `json:"ticks"`
	Close  []float64 `json:"close"`
}

// GetDailyCloses fetches daily closing prices for the given instrument going back
// the requested number of days. Uses Deribit's TradingView chart data endpoint.
func (e *Executor) GetDailyCloses(ctx context.Context, instrument string, days int) ([]DailyClose, error) {
	now := time.Now().UTC()
	result, err := call[chartData](ctx, e.gw, "public/get_tradingview_chart_data", map[string]any{
		"instrument_name": instrument,
		"start_timestamp": now.AddDate(0, 0, -days).UnixMilli(),
		"end_timestamp":   now.UnixMilli(),
		"resolution":      "1D",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, fmt.Errorf("daily closes for %s: %w", instrument, err)
	}
	if result.Status != "ok" {
		return nil, fmt.Errorf("daily closes for %s: status=%s", instrument, result.Status)
	}
	if len(result.Ticks) != len(result.Close) {
		return nil, fmt.Errorf("daily closes for %s: %d timestamps but %d closes",
			instrument, len(result.Ticks), len(result.Close))
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
	return call[MarginInfo](ctx, e.gw, "private/get_margins", map[string]any{
		"instrument_name": instrument,
		"amount":          amount,
		"price":           price,
	}, gateway.PriorityLow)
}

// AccountEquity returns the account's available funds in the underlying currency.
// For Portfolio Margin accounts, available_funds already reflects Deribit's
// actual margin requirements. The backtest's SimExecutor implements the same
// method; live trading reads margin through GetAccountSummary.
func (e *Executor) AccountEquity(ctx context.Context, currency string) (float64, error) {
	summary, err := e.GetAccountSummary(ctx, currency)
	if err != nil {
		return 0, err
	}
	return summary.AvailableFunds, nil
}
