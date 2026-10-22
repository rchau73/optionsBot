package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #239: take-profit, delta drift and the DTE roll rest a buy limit at the mid
// for one fill window, then cross at the ask; every urgent exit pre-empts it.

func TestRestingClosePrice(t *testing.T) {
	cases := []struct {
		name           string
		bid, ask, tick float64
		want           float64
		ok             bool
	}{
		{"mid on a tick", 0.0090, 0.0100, 0.0001, 0.0095, true},
		{"mid between ticks rounds down", 0.0090, 0.0101, 0.0001, 0.0095, true},
		{"one-tick spread rests at the bid", 0.0090, 0.0091, 0.0001, 0.0090, true},
		{"no bid: cross at once", 0, 0.0100, 0.0001, 0, false},
		{"crossed or locked book", 0.0100, 0.0100, 0.0001, 0, false},
		{"no ask", 0.0090, 0, 0.0001, 0, false},
		{"no tick", 0.0090, 0.0100, 0, 0, false},
	}
	for _, c := range cases {
		got, ok := strategy.RestingClosePrice(c.bid, c.ask, c.tick)
		if ok != c.ok || math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: RestingClosePrice(%v, %v, %v) = %v, %v; want %v, %v", c.name, c.bid, c.ask, c.tick, got, ok, c.want, c.ok)
		}
	}
}

func TestRestsAtMid(t *testing.T) {
	for a, want := range map[strategy.RolloutAction]bool{
		strategy.ActionRollSameLeg:   true, // take-profit, delta drift
		strategy.ActionRollNextMonth: true, // DTE roll
		strategy.ActionStopLoss:      false,
		strategy.ActionDeltaExit:     false,
		strategy.ActionNone:          false,
	} {
		if got := strategy.RestsAtMid(a); got != want {
			t.Errorf("RestsAtMid(%v) = %v, want %v", a, got, want)
		}
	}
}

// restMidBuys makes resting buy limits wait on the book (filled by the test
// with f.exch.partial/fill) while IOC and market buys fill at once.
func (f *strategyFixture) restMidBuys() {
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction != orders.DirectionBuy || (o.OrderType == orders.TypeLimit && o.TimeInForce == "") {
			return orders.Fill{}
		}
		px := o.LimitPrice
		if px == 0 {
			px = 0.01
		}
		return orders.Fill{Qty: o.Qty, FillPrice: px}
	}
}

// callBuys is every buy sent on the call.
func (f *strategyFixture) callBuys() []orders.Order {
	return f.exch.ordersWhere(func(o orders.Order) bool {
		return o.Direction == orders.DirectionBuy && o.Instrument == f.call
	})
}

// callCloses is every close journaled on the call.
func (f *strategyFixture) callCloses() []journalEntry {
	var out []journalEntry
	for _, e := range f.journal.events(orders.EventClosed) {
		if e.instrument == f.call {
			out = append(out, e)
		}
	}
	return out
}

// takeProfitResting starts a 0.3 strangle sold at 0.02 whose call quotes
// 0.0090 / 0.0100 (mid 0.0095: 52 % earned), and waits for its resting close.
func (f *strategyFixture) takeProfitResting(t *testing.T) orders.Order {
	t.Helper()
	f.withOpenStrangle(0.3, 0.02)
	f.market.setQuote(f.call, 0.0090, 0.0100)
	f.restMidBuys()
	f.startRun()
	eventually(t, 2*time.Second, "take-profit resting", func() bool { return len(f.callBuys()) == 1 })
	return f.callBuys()[0]
}

// Scenario: take-profit rests at mid first.
func TestStrategy_TakeProfitRestsAtMidFirst(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 1
	rest := f.takeProfitResting(t)
	if rest.OrderType != orders.TypeLimit || rest.TimeInForce != "" || rest.LimitPrice != 0.0095 ||
		rest.TriggerReason != orders.TriggerRolloutROI || rest.Qty != 0.3 {
		t.Fatalf("take-profit must rest a buy limit at the mid 0.0095, got %+v", rest)
	}
	time.Sleep(100 * time.Millisecond) // many cycles inside the window
	if n := len(f.callBuys()); n != 1 {
		t.Fatalf("nothing else is sent while the close rests, got %d buys", n)
	}

	// One lot fills at the mid; the window runs out; the ask has moved.
	f.exch.partial(f.exch.orderIDFor(f.call, 0), 0.1, 0.0095)
	f.market.setQuote(f.call, 0.0088, 0.0098)
	eventually(t, 3*time.Second, "rest bought at the ask", func() bool { return f.position(f.call) == nil })

	buys := f.callBuys()
	if len(buys) != 2 {
		t.Fatalf("want the resting order then one IOC, got %+v", buys)
	}
	if ioc := buys[1]; ioc.TimeInForce != orders.TimeInForceIOC || ioc.LimitPrice != 0.0098 || math.Abs(ioc.Qty-0.2) > 1e-9 {
		t.Errorf("the rest (0.2) must be an IOC at the then-current ask 0.0098, got %+v", ioc)
	}
	restID := f.exch.orderIDFor(f.call, 0)
	f.exch.mu.Lock()
	cancelled := strings.Contains(strings.Join(f.exch.cancelled, ","), restID)
	f.exch.mu.Unlock()
	if !cancelled {
		t.Error("the resting order must be cancelled when its window ends")
	}
	closes := f.callCloses()
	if len(closes) != 2 || closes[0].ctx.ExitStep != strategy.ExitStepMid || closes[0].qty != 0.1 ||
		closes[1].ctx.ExitStep != strategy.ExitStepAsk || math.Abs(closes[1].qty-0.2) > 1e-9 {
		t.Fatalf("want a mid close of 0.1 then an ask close of 0.2, got %+v", closes)
	}
}

// Scenario: the same for delta drift and the DTE roll.
func TestStrategy_DriftAndDTERollRestAtMidFirst(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(f *strategyFixture)
		trigger string
	}{
		{"delta drift", func(f *strategyFixture) { f.market.setDelta(f.call, 0.03) }, orders.TriggerRolloutDelta},
		{"DTE roll", func(f *strategyFixture) { f.cfg.RolloutDTE = 50 }, orders.TriggerRollout19DTE},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.OrderFillTimeoutSec = 1
			f.withOpenStrangle(0.1, 0.02)
			f.restMidBuys()
			c.setup(f)
			f.startRun()

			eventually(t, 2*time.Second, "close resting", func() bool { return len(f.callBuys()) == 1 })
			rest := f.callBuys()[0]
			if rest.TimeInForce != "" || rest.LimitPrice != 0.02 || rest.TriggerReason != c.trigger {
				t.Fatalf("must rest at the mid 0.02 (bid 0.019 / ask 0.021), got %+v", rest)
			}
			eventually(t, 3*time.Second, "crossed at the ask after the window", func() bool { return f.position(f.call) == nil })
			if ioc := f.callBuys()[1]; ioc.TimeInForce != orders.TimeInForceIOC || ioc.LimitPrice != 0.021 || ioc.TriggerReason != c.trigger {
				t.Errorf("second step must be an IOC at the ask, got %+v", ioc)
			}
		})
	}
}

// Scenario: an urgent exit pre-empts a resting close.
func TestStrategy_UrgentExitPreemptsRestingClose(t *testing.T) {
	cases := []struct {
		name    string
		urgent  func(f *strategyFixture)
		trigger string
		check   func(t *testing.T, o orders.Order)
	}{
		{"stop-loss", func(f *strategyFixture) { f.market.setQuote(f.call, 0.069, 0.071) }, orders.TriggerStopLoss200Pct,
			func(t *testing.T, o orders.Order) {
				if o.OrderType != orders.TypeMarket || math.Abs(o.Qty-0.2) > 1e-9 {
					t.Errorf("stop-loss must buy the 0.2 left at market, got %+v", o)
				}
			}},
		{"delta exit", func(f *strategyFixture) { f.market.setDelta(f.call, 0.35) }, orders.TriggerDeltaExit,
			func(t *testing.T, o orders.Order) {
				if o.TimeInForce != orders.TimeInForceIOC || o.LimitPrice != 0.0100 || math.Abs(o.Qty-0.2) > 1e-9 {
					t.Errorf("delta exit must buy the 0.2 left with an IOC at the ask, got %+v", o)
				}
			}},
		// The MM cut sizes its cut on the book (0.3 → 0.2): no partial fill
		// here, or the resting fill alone would cover the cut.
		{"MM cut", func(f *strategyFixture) { f.exch.setMargin(1, 4) }, orders.TriggerMarginMM,
			func(t *testing.T, o orders.Order) {
				if o.OrderType != orders.TypeMarket || math.Abs(o.Qty-0.1) > 1e-9 {
					t.Errorf("the MM cut must stay at market (0.1), got %+v", o)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.OrderFillTimeoutSec = 60 // the window never ends in this test
			f.cfg.DeltaExitThreshold = 0.30
			f.takeProfitResting(t)
			restID := f.exch.orderIDFor(f.call, 0)
			partial := c.trigger != orders.TriggerMarginMM
			if partial {
				f.exch.partial(restID, 0.1, 0.0095)
			}
			c.urgent(f)

			// 5 s: a loaded runner (parallel agents, -race) once missed 2 s.
			eventually(t, 5*time.Second, "urgent exit sent", func() bool { return len(f.callBuys()) >= 2 })
			f.exch.mu.Lock()
			cancelled := strings.Contains(strings.Join(f.exch.cancelled, ","), restID)
			f.exch.mu.Unlock()
			if !cancelled {
				t.Error("the resting order must be cancelled before the urgent exit")
			}
			urgent := f.callBuys()[1]
			if urgent.TriggerReason != c.trigger {
				t.Fatalf("second buy = %+v, want trigger %s", urgent, c.trigger)
			}
			c.check(t, urgent)
			closes := f.callCloses()
			if !partial {
				return
			}
			if len(closes) == 0 || closes[0].ctx.ExitStep != strategy.ExitStepMid || closes[0].qty != 0.1 || closes[0].fillPrice != 0.0095 {
				t.Errorf("the resting fill (0.1 at 0.0095) must be booked first, got %+v", closes)
			}
		})
	}
}

// Scenario: unchanged exits — stop-loss, MM cut, GEX shed, leg balancing and
// rebalance downsize are covered by their own tests (StopLossPartialFill…,
// MMBreachReduces…, GEXSheddingClosesOpenPuts, balance_legs_test,
// RebalanceDownsizesInWholeLots); the delta exit crosses at once:
func TestStrategy_DeltaExitStillCrossesAtOnce(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DeltaExitThreshold = 0.30
	f.withOpenStrangle(0.1, 0.02)
	f.market.setDelta(f.call, 0.35)
	f.restMidBuys()
	f.startRun()

	eventually(t, 2*time.Second, "delta exit sent", func() bool { return len(f.callBuys()) >= 1 })
	if o := f.callBuys()[0]; o.TimeInForce != orders.TimeInForceIOC || o.LimitPrice != 0.021 || o.TriggerReason != orders.TriggerDeltaExit {
		t.Errorf("delta exit must be an IOC at the ask, never resting, got %+v", o)
	}
}

// Scenario: nothing is lost on restart or a lost connection.
func TestStrategy_RestingCloseLostReplyCancelledByLabel(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.setQuote(f.call, 0.0090, 0.0100)
	first := true
	f.exch.loseReply = func(o orders.Order) bool {
		lose := first && o.Direction == orders.DirectionBuy
		first = first && !lose
		return lose
	}
	var cancelsAtSecond = -1
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy && cancelsAtSecond < 0 {
			cancelsAtSecond = len(f.exch.cancelledLabels) // Submit holds f.exch.mu
		}
		return orders.Fill{}
	}
	f.startRun()

	eventually(t, 2*time.Second, "second close sent", func() bool { return len(f.callBuys()) >= 2 })
	buys := f.callBuys()
	f.exch.mu.Lock()
	labels := append([]string(nil), f.exch.cancelledLabels...)
	atSecond := cancelsAtSecond
	f.exch.mu.Unlock()
	if len(labels) == 0 || labels[0] != buys[0].Label {
		t.Fatalf("the lost resting close must be cancelled by its label %q, got %v", buys[0].Label, labels)
	}
	if atSecond < 1 {
		t.Error("no new close may be sent on the instrument before the unknown one is cancelled by label")
	}
	if buys[0].Label == buys[1].Label || buys[0].Label == "" {
		t.Errorf("every close carries a unique label, got %q and %q", buys[0].Label, buys[1].Label)
	}
	if f.position(f.call) == nil {
		t.Error("the leg stays tracked: nothing filled")
	}
}

// Scenario: nothing is lost — the kill switch books a resting close's fill
// and flattens the rest at market.
func TestStrategy_KillSwitchBooksRestingClose(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 60
	f.takeProfitResting(t)
	f.exch.partial(f.exch.orderIDFor(f.call, 0), 0.1, 0.0095)
	f.strat.KillSwitch()

	eventually(t, 2*time.Second, "book flattened", func() bool { return len(f.state.AllPositions()) == 0 })
	closes := f.callCloses()
	if len(closes) != 2 || closes[0].ctx.ExitStep != strategy.ExitStepMid || closes[0].qty != 0.1 ||
		closes[1].trigger != orders.TriggerKillSwitch || math.Abs(closes[1].qty-0.2) > 1e-9 {
		t.Errorf("want the resting 0.1 booked, then 0.2 at market by the kill switch, got %+v", closes)
	}
}

// stopRun cancels Run's context (SIGTERM, a deploy) and waits for it to
// return; the error is put back for the fixture's cleanup.
func (f *strategyFixture) stopRun(t *testing.T) {
	t.Helper()
	f.cancel()
	select {
	case err := <-f.runErr:
		f.runErr <- err
	case <-time.After(3 * time.Second):
		t.Fatal("Strategy.Run did not stop after cancel")
	}
}

// restingCancels is every cancel journaled for the call's take-profit order.
func (f *strategyFixture) restingCancels() []journalEntry {
	var out []journalEntry
	for _, e := range f.journal.events(orders.EventCancelled) {
		if e.instrument == f.call && e.trigger == orders.TriggerRolloutROI {
			out = append(out, e)
		}
	}
	return out
}

// Scenario: nothing is lost on restart — a shutdown (SIGTERM, a deploy)
// cancels the resting close and books the fill that came after the last poll,
// so the journal (realised P&L's one source) has it.
func TestStrategy_ShutdownBooksRestingCloseFill(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 60
	f.takeProfitResting(t)
	restID := f.exch.orderIDFor(f.call, 0)
	f.exch.onCancel = func(id string) error {
		if id == restID { // 0.1 filled between the last poll and the cancel
			f.exch.orderStates[id] = orders.OrderStateInfo{OrderID: id, State: "open", FilledAmount: 0.1, AvgPrice: 0.0095}
			f.exch.applyFillLocked(f.exch.byID[id], id, 0.1, 0.0095)
		}
		return nil
	}
	if n := len(f.callCloses()); n != 0 {
		t.Fatalf("nothing filled before the shutdown, got %d closes", n)
	}
	f.stopRun(t)

	closes := f.callCloses()
	if len(closes) != 1 || closes[0].ctx.ExitStep != strategy.ExitStepMid || closes[0].qty != 0.1 || closes[0].fillPrice != 0.0095 {
		t.Fatalf("the shutdown must book the resting fill (0.1 at 0.0095), got %+v", closes)
	}
	if len(f.restingCancels()) != 1 {
		t.Error("the resting order's cancel must be journaled")
	}
	if p := f.position(f.call); p == nil || math.Abs(p.Qty-0.2) > 1e-9 {
		t.Errorf("0.2 stays short in the book, got %+v", p)
	}
}

// A resting close that filled whole just before its cancel reached it is a
// fill, not a cancel: one close at the mid, no cancelled line.
func TestStrategy_RestingCloseFilledDuringCancelNotJournaledCancelled(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 60
	f.takeProfitResting(t)
	restID := f.exch.orderIDFor(f.call, 0)
	f.exch.onCancel = func(id string) error {
		if id != restID {
			return nil
		}
		f.exch.fillLocked(id, 0.3, 0.0095)
		return errors.New("order already filled")
	}
	f.stopRun(t)

	closes := f.callCloses()
	if len(closes) != 1 || closes[0].ctx.ExitStep != strategy.ExitStepMid || closes[0].qty != 0.3 {
		t.Fatalf("want one mid close of 0.3, got %+v", closes)
	}
	if c := f.restingCancels(); len(c) != 0 {
		t.Errorf("a filled resting close must not be journaled as cancelled, got %+v", c)
	}
	if f.position(f.call) != nil {
		t.Error("the call is closed")
	}
}

// Scenario: journaled — the close records the step, the price and the
// mid/ask at the trigger; the resting order's submit is journaled too.
func TestStrategy_RestingCloseJournaled(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 60
	f.takeProfitResting(t)
	f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.3, 0.0095)

	eventually(t, 2*time.Second, "closed at the mid", func() bool { return f.position(f.call) == nil })
	closes := f.callCloses()
	if len(closes) != 1 {
		t.Fatalf("one close, got %+v", closes)
	}
	c := closes[0]
	if c.ctx.ExitStep != strategy.ExitStepMid || c.fillPrice != 0.0095 || c.orderType != orders.TypeLimit ||
		!strings.Contains(c.ctx.Detail, "rested at 0.0095") || !strings.Contains(c.ctx.Detail, "bid 0.0090 / mid 0.0095 / ask 0.0100 at the trigger") {
		t.Errorf("close journal = %+v (detail %q)", c, c.ctx.Detail)
	}
	submitted := false
	for _, e := range f.journal.events(orders.EventSubmitted) {
		submitted = submitted || (e.instrument == f.call && e.trigger == orders.TriggerRolloutROI)
	}
	if !submitted {
		t.Error("the resting order's submit must be journaled")
	}
}

func TestOrderLog_ExitStepWritten(t *testing.T) {
	var buf bytes.Buffer
	ctx := sampleContext()
	ctx.ExitStep = strategy.ExitStepAsk
	orders.NewWriterLogger(&buf, 0).LogClose(samplePosition(), orders.Fill{FillPrice: 0.01, Qty: 1}, orders.TriggerRolloutROI, orders.TypeLimit, ctx)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["exit_step"] != "ask" {
		t.Errorf("exit_step = %v, want ask", rec["exit_step"])
	}
}
