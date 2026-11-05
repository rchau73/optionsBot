package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
)

// The futures hedge's exchange calls, journal record and edge cases (#657).

func TestExecutor_FuturesHedgeCalls(t *testing.T) {
	ctx := context.Background()
	t.Run("future positions ask kind=future", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `[{"instrument_name":"BTC-PERPETUAL","size":-17700,"direction":"sell","size_currency":-0.23,"realized_funding":0.00001,"kind":"future"}]`}}}
		pos, err := orders.NewExecutor(fc).GetFuturePositions(ctx, "BTC")
		if err != nil || len(pos) != 1 || pos[0].Size != -17700 || pos[0].RealizedFunding != 0.00001 {
			t.Fatalf("positions = %+v, err = %v", pos, err)
		}
		if c := fc.calls[0]; c.method != "private/get_positions" || c.params["kind"] != "future" || c.params["currency"] != "BTC" {
			t.Errorf("call %+v", c)
		}
	})
	t.Run("instrument spec", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `{"instrument_name":"BTC-PERPETUAL","kind":"future","contract_size":10,"min_trade_amount":10,"tick_size":0.5}`}}}
		spec, err := orders.NewExecutor(fc).GetInstrumentSpec(ctx, "BTC-PERPETUAL")
		if err != nil || spec.ContractSize != 10 || spec.TickSize != 0.5 || fc.calls[0].method != "public/get_instrument" {
			t.Errorf("spec = %+v, err = %v, call %+v", spec, err, fc.calls[0])
		}
	})
	t.Run("ticker", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: `{"instrument_name":"BTC-PERPETUAL","best_bid_price":76770,"best_ask_price":76775.5,"mark_price":76772,"index_price":76780}`}}}
		tk, err := orders.NewExecutor(fc).GetTicker(ctx, "BTC-PERPETUAL")
		if err != nil || tk.BestBid != 76770 || tk.BestAsk != 76775.5 || fc.calls[0].method != "public/ticker" {
			t.Errorf("ticker = %+v, err = %v", tk, err)
		}
	})
	t.Run("reduce-only close is high priority", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: filledOrderJSON}}}
		_, err := orders.NewExecutor(fc).Submit(ctx, orders.Order{Instrument: "BTC-PERPETUAL", Direction: orders.DirectionBuy,
			OrderType: orders.TypeMarket, Qty: 17700, TriggerReason: orders.TriggerFuturesHedgeClose, ReduceOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if c := fc.calls[0]; c.params["reduce_only"] != true || c.priority != gateway.PriorityHigh || c.params["amount"] != 17700.0 {
			t.Errorf("call %+v, want reduce_only, high priority, amount in USD", c)
		}
	})
	t.Run("a hedge order is not reduce-only", func(t *testing.T) {
		fc := &fakeCaller{replies: []fakeReply{{result: filledOrderJSON}}}
		_, _ = orders.NewExecutor(fc).Submit(ctx, orders.Order{Instrument: "BTC-PERPETUAL", Direction: orders.DirectionSell,
			OrderType: orders.TypeLimit, TimeInForce: orders.TimeInForceIOC, Qty: 17700, LimitPrice: 76770, TickSize: 0.5, TriggerReason: orders.TriggerFuturesHedge})
		if c := fc.calls[0]; c.params["reduce_only"] != nil || c.params["price"] != 76770.0 || c.params["time_in_force"] != orders.TimeInForceIOC {
			t.Errorf("call %+v", c)
		}
	})
}

func TestLogger_LogHedgeWritesAHedgeLine(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0)
	on := true
	l.LogHedge(orders.HedgeRecord{Change: orders.HedgeModeChanged, Enabled: &on, Actor: &orders.Actor{User: "roberto", Role: "admin"}, Detail: "futures hedge ON"})
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
	if got["event"] != orders.EventHedge || got["change"] != orders.HedgeModeChanged || got["enabled"] != true || got["timestamp"] == "" {
		t.Errorf("line %v", got)
	}
	if n := l.EventCounts()[orders.EventHedge]; n != 1 {
		t.Errorf("event counted %d times", n)
	}
}

func TestLedger_AddLegKeepsTheNewestWithinTheBound(t *testing.T) {
	var l hedge.Ledger
	for i := 0; i < hedge.MaxLegs; i++ {
		l.AddLeg(hedge.Counterfactual{Instrument: "open", Done: i == 3})
	}
	l.AddLeg(hedge.Counterfactual{Instrument: "newest"})
	if len(l.Legs) != hedge.MaxLegs || l.Legs[len(l.Legs)-1].Instrument != "newest" {
		t.Fatalf("len %d, last %+v", len(l.Legs), l.Legs[len(l.Legs)-1])
	}
	for _, c := range l.Legs {
		if c.Done {
			t.Error("a finished leg goes first")
		}
	}
	l.AddLeg(hedge.Counterfactual{Instrument: "newer"}) // none finished: the oldest goes
	if len(l.Legs) != hedge.MaxLegs || l.Legs[len(l.Legs)-1].Instrument != "newer" {
		t.Errorf("len %d", len(l.Legs))
	}
}

func TestSaveMode_UnwritablePathFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hedge.SaveMode(filepath.Join(blocker, "hedge_mode.json"), hedge.ModeState{}); err == nil {
		t.Error("a path under a file cannot be written")
	}
	if _, err := hedge.LoadMode(dir); err == nil {
		t.Error("a directory is not a mode file")
	}
}

func TestHedgeHelpers_EdgeCases(t *testing.T) {
	if hedge.PerpDelta(100, 0) != 0 || hedge.TakerFee(100, 0, 0.0005) != 0 || hedge.PerpPnL(100, 0, 1) != 0 {
		t.Error("no price: nothing")
	}
	// No mark: the preview values the perpetual at spot and computes its P&L.
	p := hedge.BuildPreview(hedge.PreviewInput{Spot: 80000, PerpUSD: -8000, PerpAvg: 80000})
	if p.PerpMark != 80000 || p.UnrealisedCoin != 0 || !near(p.PerpDeltaCoin, -0.1, 1e-12) {
		t.Errorf("preview %+v", p)
	}
	if s := hedge.ScenarioPnL(100, 1, 0, -200); s.PnLCoin != 0 {
		t.Error("a move to or below zero has no coin value")
	}
	var b hedge.PerpBook
	b.Book(-100, 100, 0)
	b.Book(300, 110, 0) // flips long: the rest opens at 110
	if b.SizeUSD != 200 || b.AvgPrice != 110 {
		t.Errorf("flip: %+v", b)
	}
	b.Book(0, 110, 0)
	if b.Trades != 2 {
		t.Error("an empty trade books nothing")
	}
}

// A switch-off whose close fails keeps the experiment ON.
func TestFuturesHedge_SwitchOffStaysOnWhenTheCloseFails(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true)
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: -23050, Direction: "sell", AveragePrice: 100000, MarkPrice: 100000})
	f.startRun()
	eventually(t, 2*time.Second, "loop running", func() bool { return !f.strat.HedgeState().PerpAsOf.IsZero() })
	f.exch.mu.Lock()
	f.exch.onSubmit = func(o orders.Order, id string) orders.Fill { return orders.Fill{OrderID: id} } // nothing fills
	f.exch.mu.Unlock()
	_, err := f.strat.SetFuturesHedge(context.Background(), false, orders.Actor{User: "tester", Role: "admin"})
	if err == nil || !strings.Contains(err.Error(), "stays ON") {
		t.Fatalf("err %v, want the experiment to stay ON", err)
	}
	if st, _ := hedge.LoadMode(f.hedgeAt); !st.Enabled {
		t.Error("the mode file still says ON")
	}
	if !f.strat.HedgeState().Enabled {
		t.Error("the view still says ON")
	}
	// Unchanged switches are a no-op.
	res, err := f.strat.SetFuturesHedge(context.Background(), true, orders.Actor{User: "tester", Role: "admin"})
	if err != nil || !res.Enabled || !strings.Contains(res.Detail, "unchanged") {
		t.Errorf("switching on an ON experiment: %+v %v", res, err)
	}
}

// On a live account a mode file that says ON is ignored and the switch is refused.
func TestFuturesHedge_LiveStaysOff(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true)
	f.cfg.Environment = "live"
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 || f.strat.HedgeState().Enabled {
		t.Errorf("live: %d perpetual orders, enabled %v; want none, OFF", n, f.strat.HedgeState().Enabled)
	}
	if _, err := f.strat.SetFuturesHedge(context.Background(), true, orders.Actor{User: "tester", Role: "admin"}); err == nil {
		t.Error("live: the switch is refused")
	}
}

// After the kill switch the experiment cannot be switched.
func TestFuturesHedge_NoSwitchAfterTheKillSwitch(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	f.strat.KillSwitch()
	eventually(t, 2*time.Second, "halted", func() bool { return f.strat.View().Halted })
	if _, err := f.strat.SetFuturesHedge(context.Background(), true, orders.Actor{User: "tester", Role: "admin"}); err == nil {
		t.Error("a halted bot refuses the switch")
	}
}
