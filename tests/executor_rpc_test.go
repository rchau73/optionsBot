package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// fakeCaller stands in for the gateway: it records every call and replays
// scripted replies in order.
type fakeCaller struct {
	calls   []fakeCall
	replies []fakeReply
}

type fakeCall struct {
	method   string
	params   map[string]any
	priority int
}

type fakeReply struct {
	result string // raw JSON result
	err    error
}

func (f *fakeCaller) Call(_ context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error) {
	p, _ := params.(map[string]any)
	f.calls = append(f.calls, fakeCall{method: method, params: p, priority: priority})
	if len(f.replies) == 0 {
		return gateway.JSONRPCResponse{}, errors.New("fakeCaller: no reply scripted")
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	if r.err != nil {
		return gateway.JSONRPCResponse{}, r.err
	}
	return rpcResponse(r.result), nil
}

// rpcResponse builds a JSON-RPC response carrying the given raw JSON result.
func rpcResponse(raw string) gateway.JSONRPCResponse {
	if raw != "" && !json.Valid([]byte(raw)) {
		panic("rpcResponse: invalid JSON in test fixture: " + raw)
	}
	return gateway.JSONRPCResponse{Result: json.RawMessage(raw)}
}

const filledOrderJSON = `{"order":{"order_id":"o-1","filled_amount":0.1,"average_price":0.012,"order_state":"filled"}}`

func TestSubmit_LimitSellRoundsPriceAndUsesSellMethod(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: filledOrderJSON}}}
	exec := orders.NewExecutor(fc)

	fill, err := exec.Submit(context.Background(), orders.Order{
		Instrument: "BTC-27DEC24-100000-C", Direction: orders.DirectionSell,
		OrderType: orders.TypeLimit, Qty: 0.1, LimitPrice: 0.01234, TickSize: 0.0005,
		TriggerReason: orders.TriggerEntry,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fill.OrderID != "o-1" || fill.Qty != 0.1 || fill.FillPrice != 0.012 {
		t.Errorf("unexpected fill: %+v", fill)
	}
	c := fc.calls[0]
	if c.method != "private/sell" {
		t.Errorf("method = %s, want private/sell", c.method)
	}
	if c.params["price"] != 0.0125 {
		t.Errorf("price = %v, want 0.0125 (rounded to 0.0005 tick)", c.params["price"])
	}
	if c.priority != gateway.PriorityLow {
		t.Errorf("entry order should be low priority, got %d", c.priority)
	}
}

func TestSubmit_RiskReducingOrdersAreHighPriority(t *testing.T) {
	for _, reason := range []string{orders.TriggerStopLoss200Pct, orders.TriggerKillSwitch, orders.TriggerGammaClose} {
		t.Run(reason, func(t *testing.T) {
			fc := &fakeCaller{replies: []fakeReply{{result: filledOrderJSON}}}
			_, err := orders.NewExecutor(fc).Submit(context.Background(), orders.Order{
				Instrument: "X", Direction: orders.DirectionBuy, OrderType: orders.TypeMarket,
				Qty: 0.1, TriggerReason: reason,
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if fc.calls[0].method != "private/buy" {
				t.Errorf("method = %s, want private/buy", fc.calls[0].method)
			}
			if fc.calls[0].priority != gateway.PriorityHigh {
				t.Errorf("priority = %d, want high", fc.calls[0].priority)
			}
			if _, hasPrice := fc.calls[0].params["price"]; hasPrice {
				t.Error("market order must not carry a price")
			}
		})
	}
}

func TestSubmit_PriceTooLowRetriesOnceAtExchangeMinimum(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{
		{err: &gateway.RPCError{Code: 10005, Message: "price_too_low 0.0046"}},
		{result: filledOrderJSON},
	}}
	_, err := orders.NewExecutor(fc).Submit(context.Background(), orders.Order{
		Instrument: "X", Direction: orders.DirectionSell, OrderType: orders.TypeLimit,
		Qty: 0.1, LimitPrice: 0.004, TickSize: 0.0005,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(fc.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (original + one retry)", len(fc.calls))
	}
	if got := fc.calls[1].params["price"]; got != 0.005 {
		t.Errorf("retry price = %v, want 0.005", got)
	}
}

func TestSubmit_OtherRejectionIsNotRetried(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{
		{err: &gateway.RPCError{Code: 10009, Message: "not_enough_funds"}},
	}}
	_, err := orders.NewExecutor(fc).Submit(context.Background(), orders.Order{
		Instrument: "X", Direction: orders.DirectionSell, OrderType: orders.TypeLimit,
		Qty: 0.1, LimitPrice: 0.01,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *gateway.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != 10009 {
		t.Errorf("error should wrap the RPC error, got %v", err)
	}
	if len(fc.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(fc.calls))
	}
}

func TestSubmit_MalformedResultReturnsError(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"order":"not-an-object"}`}}}
	_, err := orders.NewExecutor(fc).Submit(context.Background(), orders.Order{
		Instrument: "X", Direction: orders.DirectionBuy, OrderType: orders.TypeMarket, Qty: 0.1,
	})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestExecutor_ForbiddenMapsToErrForbidden(t *testing.T) {
	cases := []*gateway.RPCError{
		{Code: 13021, Message: "forbidden"},
		{Code: 0, Message: "forbidden"},
	}
	for _, rpcErr := range cases {
		fc := &fakeCaller{replies: []fakeReply{{err: rpcErr}}}
		_, err := orders.NewExecutor(fc).GetAccountSummary(context.Background(), "BTC")
		if !errors.Is(err, orders.ErrForbidden) {
			t.Errorf("%v: want ErrForbidden, got %v", rpcErr, err)
		}
	}
}

func TestExecutor_ReadEndpointsDecodeResults(t *testing.T) {
	ctx := context.Background()

	t.Run("account summary and equity", func(t *testing.T) {
		const summary = `{"equity":1.5,"available_funds":1.2,"initial_margin":0.1}`
		fc := &fakeCaller{replies: []fakeReply{{result: summary}, {result: summary}}}
		exec := orders.NewExecutor(fc)
		s, err := exec.GetAccountSummary(ctx, "BTC")
		if err != nil || s.Equity != 1.5 || s.InitialMargin != 0.1 {
			t.Errorf("summary = %+v, err = %v", s, err)
		}
		eq, err := exec.AccountEquity(ctx, "BTC")
		if err != nil || eq != 1.2 {
			t.Errorf("AccountEquity = %v, err = %v; want available_funds 1.2", eq, err)
		}
	})

	t.Run("positions", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `[{"instrument_name":"BTC-X-C","size":-0.1,"direction":"sell"}]`}}}
		pos, err := orders.NewExecutor(fc).GetPositions(ctx, "BTC")
		if err != nil || len(pos) != 1 || pos[0].Size != -0.1 {
			t.Errorf("positions = %+v, err = %v", pos, err)
		}
		if fc.calls[0].params["kind"] != "option" {
			t.Error("positions must be filtered to options")
		}
	})

	t.Run("order state", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `{"order_id":"o-1","order_state":"filled","filled_amount":0.1}`}}}
		st, err := orders.NewExecutor(fc).GetOrderState(ctx, "o-1")
		if err != nil || st.State != "filled" {
			t.Errorf("state = %+v, err = %v", st, err)
		}
	})

	t.Run("margins", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `{"initial_margin":0.02,"maintenance_margin":0.01}`}}}
		m, err := orders.NewExecutor(fc).GetMargins(ctx, "X", 0.1, 0.01)
		if err != nil || m.InitialMargin != 0.02 {
			t.Errorf("margins = %+v, err = %v", m, err)
		}
	})
}

func TestExecutor_CancelAndAmendUseExpectedMethods(t *testing.T) {
	ctx := context.Background()
	fc := &fakeCaller{replies: []fakeReply{{result: `{}`}, {result: `{}`}, {result: `{}`}, {result: `{}`}}}
	exec := orders.NewExecutor(fc)

	if err := exec.Cancel(ctx, "o-1"); err != nil {
		t.Fatal(err)
	}
	if err := exec.CancelAll(ctx, "BTC-X-C"); err != nil {
		t.Fatal(err)
	}
	if err := exec.CancelAllOrders(ctx, "ETH"); err != nil {
		t.Fatal(err)
	}
	if err := exec.AmendOrder(ctx, "o-1", 0.1, 0.012345); err != nil {
		t.Fatal(err)
	}

	want := []struct {
		method   string
		priority int
	}{
		{"private/cancel", gateway.PriorityHigh},
		{"private/cancel_all_by_instrument", gateway.PriorityHigh},
		{"private/cancel_all_by_currency", gateway.PriorityHigh},
		{"private/edit", gateway.PriorityLow},
	}
	for i, w := range want {
		if fc.calls[i].method != w.method || fc.calls[i].priority != w.priority {
			t.Errorf("call %d = %s/%d, want %s/%d", i, fc.calls[i].method, fc.calls[i].priority, w.method, w.priority)
		}
	}
	// Startup cleanup must stay within this bot's currency: an ETH bot
	// restarting must not cancel a BTC bot's orders.
	if got := fc.calls[2].params["currency"]; got != "ETH" {
		t.Errorf("cancel-all currency = %v, want ETH", got)
	}
	if got := fc.calls[3].params["price"]; got != 0.0123 {
		t.Errorf("amend price = %v, want 0.0123", got)
	}
}

func TestGetDailyCloses(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		result  string
		wantN   int
		wantErr bool
	}{
		{"ok", `{"status":"ok","ticks":[1700000000000,1700086400000],"close":[37000,37500]}`, 2, false},
		{"no data status", `{"status":"no_data","ticks":[],"close":[]}`, 0, true},
		{"length mismatch", `{"status":"ok","ticks":[1700000000000,1700086400000],"close":[37000]}`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeCaller{replies: []fakeReply{{result: tc.result}}}
			closes, err := orders.NewExecutor(fc).GetDailyCloses(ctx, "BTC-PERPETUAL", 30)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if len(closes) != tc.wantN {
				t.Errorf("closes = %d, want %d", len(closes), tc.wantN)
			}
			if tc.wantN > 0 && closes[0].Date.Hour() != 0 {
				t.Errorf("dates should be truncated to UTC midnight, got %v", closes[0].Date)
			}
		})
	}
}
