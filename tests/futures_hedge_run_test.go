package tests

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
)

// The futures hedge experiment end to end (#657): Strategy.Run against the
// fake exchange, with a fake futures side on the same positions.

const testPerp = "BTC-PERPETUAL"

// fakeFutures reads the perpetual from the fake exchange's positions (where
// its fills land) and quotes it.
type fakeFutures struct {
	ex       *fakeExchange
	mu       sync.Mutex
	bid, ask float64
	contract float64
}

func (ff *fakeFutures) GetFuturePositions(context.Context, string) ([]orders.RawPosition, error) {
	ff.ex.mu.Lock()
	defer ff.ex.mu.Unlock()
	var out []orders.RawPosition
	for _, p := range ff.ex.positions {
		if strings.HasSuffix(p.InstrumentName, "-PERPETUAL") {
			out = append(out, p)
		}
	}
	return out, nil
}

func (ff *fakeFutures) GetInstrumentSpec(_ context.Context, name string) (orders.InstrumentSpec, error) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return orders.InstrumentSpec{Name: name, Kind: "future", ContractSize: ff.contract, MinTradeAmount: ff.contract, TickSize: 0.5}, nil
}

func (ff *fakeFutures) GetTicker(_ context.Context, name string) (orders.Ticker, error) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return orders.Ticker{Instrument: name, BestBid: ff.bid, BestAsk: ff.ask, Mark: (ff.bid + ff.ask) / 2, Index: 100000}, nil
}

// hedgeFills: a perpetual order fills whole (IOC at its limit, market at
// 100,000); options as by default — buys fill at 0.01, sells rest.
func hedgeFills(o orders.Order, id string) orders.Fill {
	switch {
	case o.Instrument == testPerp && o.OrderType == orders.TypeMarket:
		return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: 100000}
	case o.Instrument == testPerp:
		return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: o.LimitPrice}
	case o.Direction == orders.DirectionBuy:
		return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: 0.01}
	}
	return orders.Fill{OrderID: id}
}

// newHedgeFixture holds a 1-lot strangle whose coin delta (what the hedge
// holds at zero) is −callDelta − putDelta, with a 1.157 BTC margin balance
// (band 0.05785 BTC) and the experiment on or off. Each leg's mark is 0.02
// BTC and an inverse option's coin delta is its delta less its mark, so the
// put's Black–Scholes delta is set 0.04 higher to keep that sum.
func newHedgeFixture(t *testing.T, callDelta, putDelta float64, enabled bool) (*strategyFixture, *fakeFutures) {
	t.Helper()
	f := newStrategyFixture(t)
	f.exch.summary = orders.AccountSummary{Currency: "BTC", Equity: 1.157, AvailableFunds: 1.157, MarginBalance: 1.157}
	f.withOpenStrangle(1.0, 0.02)
	putDelta += 2 * 0.02 // the two legs' marks (see above)
	f.market.instruments[f.call].Greeks.Delta = callDelta
	f.market.instruments[f.put].Greeks.Delta = putDelta
	f.exch.positions[0].Delta, f.exch.positions[1].Delta = callDelta, putDelta
	ff := &fakeFutures{ex: f.exch, bid: 99990, ask: 100010, contract: 10}
	f.futures = ff
	f.hedgeAt = filepath.Join(t.TempDir(), "hedge_mode.json")
	if enabled {
		if err := hedge.SaveMode(f.hedgeAt, hedge.ModeState{Enabled: true, Since: time.Now(), By: hedge.Actor{User: "tester", Role: "admin"}}); err != nil {
			t.Fatal(err)
		}
	}
	f.exch.onSubmit = hedgeFills
	return f, ff
}

func (f *fakeExchange) perpOrders() []orders.Order {
	return f.ordersWhere(func(o orders.Order) bool { return o.Instrument == testPerp })
}

func (f *fakeExchange) perpSize() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.positions {
		if p.InstrumentName == testPerp {
			return p.Size
		}
	}
	return 0
}

// Scenario: long delta is sold down with the perpetual.
func TestFuturesHedge_LongDeltaSellsThePerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true) // book delta +0.2305
	f.startRun()

	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.perpOrders()) >= 1 })
	o := f.exch.perpOrders()[0]
	if o.Direction != orders.DirectionSell || o.Qty != 23050 || o.TimeInForce != orders.TimeInForceIOC ||
		o.LimitPrice != 99990 || o.TriggerReason != orders.TriggerFuturesHedge || !strings.Contains(o.Label, ":hedge:") {
		t.Fatalf("hedge order %+v, want sell 23,050 USD IOC at the bid 99,990", o)
	}
	time.Sleep(150 * time.Millisecond) // several cycles
	if n := len(f.exch.perpOrders()); n != 1 {
		t.Errorf("the hedged book is inside the band: %d perpetual orders, want 1", n)
	}
	for _, b := range f.exch.buys() {
		if b.Instrument == f.call || b.Instrument == f.put {
			t.Errorf("no option leg is closed by the hedge, got %+v", b)
		}
	}
	if fills := f.journal.hedgeChanges(orders.HedgeFill); len(fills) != 1 || fills[0].FilledUSD != 23050 || fills[0].Market.Spot == 0 {
		t.Errorf("one hedge_fill with its market snapshot, got %+v", fills)
	}
	if st, _ := hedge.LoadMode(f.hedgeAt); st.Ledger.Real.SizeUSD != -23050 {
		t.Errorf("the ledger books the fill: %+v", st.Ledger.Real)
	}
}

// Scenario: short delta is bought back up.
func TestFuturesHedge_ShortDeltaBuysThePerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.20, -0.10, true) // book delta −0.10
	f.startRun()
	eventually(t, 2*time.Second, "perpetual bought", func() bool { return len(f.exch.perpOrders()) >= 1 })
	if o := f.exch.perpOrders()[0]; o.Direction != orders.DirectionBuy || o.Qty != 10000 || o.LimitPrice != 100010 {
		t.Fatalf("hedge order %+v, want buy 10,000 USD at the ask 100,010", o)
	}
}

// Scenario: a rally hedges too. The hedge follows the book's delta, not the
// GEX regime: a book inside the band whose call delta then rises (spot up,
// no shed) is bought back to zero with the perpetual.
func TestFuturesHedge_RallyBuysThePerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.19, true) // book delta +0.03, inside the band
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Fatalf("inside the band before the rally: %d perpetual orders, want 0", n)
	}
	f.market.setDelta(f.call, 0.28) // under the 0.30 delta exit: the leg stays
	eventually(t, 2*time.Second, "perpetual bought", func() bool { return len(f.exch.perpOrders()) >= 1 })
	// coin delta −(0.28 − 0.02) − (−0.15 − 0.02) = −0.09 BTC → buy 9,000 USD
	if o := f.exch.perpOrders()[0]; o.Direction != orders.DirectionBuy || o.Qty != 9000 || o.LimitPrice != 100010 {
		t.Fatalf("hedge order %+v, want buy 9,000 USD at the ask 100,010", o)
	}
	for _, b := range f.exch.buys() {
		if b.Instrument == f.call {
			t.Errorf("the rally hedge keeps the call, got %+v", b)
		}
	}
}

// Scenario: inside the band nothing trades.
func TestFuturesHedge_InsideTheBandNothingTrades(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.19, true) // book delta +0.03 < 0.0579
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("|net delta| inside the band: %d perpetual orders, want 0", n)
	}
}

// Scenario: OFF by default changes nothing (no file); the paper hedge runs.
func TestFuturesHedge_OffByDefaultSendsNoFuture(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, false)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("OFF: %d perpetual orders, want none", n)
	}
	eventually(t, 2*time.Second, "paper hedge booked", func() bool {
		st, _ := hedge.LoadMode(f.hedgeAt)
		return !st.Enabled && st.Ledger.Virtual.SizeUSD == -23050
	})
}

// Negative: no bid on the perpetual → no sell.
func TestFuturesHedge_NoQuoteNoOrder(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	ff.bid = 0
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("no bid: %d perpetual orders, want 0", n)
	}
}

// Negative: no contract size from the exchange → no order (never guessed).
func TestFuturesHedge_NoContractSizeNoOrder(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	ff.contract = 0
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("no contract size: %d perpetual orders, want 0", n)
	}
}

// Negative: a hedge order whose reply is lost is cancelled by label; the
// next cycle reads the position (filled) and sends nothing more.
func TestFuturesHedge_UnknownOutcomeCancelsByLabelAndReadsBack(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true)
	f.exch.loseReply = func(o orders.Order) bool { return o.Instrument == testPerp }
	f.startRun()
	eventually(t, 2*time.Second, "lost hedge cancelled by label", func() bool {
		f.exch.mu.Lock()
		defer f.exch.mu.Unlock()
		for _, l := range f.exch.cancelledLabels {
			if strings.Contains(l, ":hedge:") {
				return true
			}
		}
		return false
	})
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 1 {
		t.Errorf("the lost IOC filled on the exchange: %d perpetual orders, want 1 (no double hedge)", n)
	}
	if got := f.exch.perpSize(); got != -23050 {
		t.Errorf("perpetual %.0f, want −23,050", got)
	}
	if len(f.journal.hedgeChanges(orders.HedgeFailed)) == 0 {
		t.Error("hedge_failed journaled")
	}
}

// Scenario: the shed is replaced, the stop-loss is not.
func TestFuturesHedge_ShedKeepsLegsStopLossStillFires(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, true) // a delta-neutral book: no hedge needed
	f.withPutSheddingRegime()
	f.startRun()
	eventually(t, 2*time.Second, "shed_hedged journaled", func() bool {
		for _, e := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(e.reason, "shed_hedged "+f.put) {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	for _, b := range f.exch.buys() {
		if b.TriggerReason == orders.TriggerGammaClose {
			t.Fatalf("no shed buy-back while the hedge is on, got %+v", b)
		}
	}
	n := 0
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, "shed_hedged") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("shed_hedged journaled %d times, want once per leg", n)
	}
	if st, _ := hedge.LoadMode(f.hedgeAt); len(st.Ledger.Legs) != 1 || st.Ledger.Legs[0].Mode != hedge.CounterHedged || st.Ledger.Legs[0].ShedPrice != 0.021 {
		t.Errorf("the benchmark records the kept put at the ask: %+v", st.Ledger.Legs)
	}

	f.market.setQuote(f.put, 0.069, 0.071) // loss 2.5× the premium
	eventually(t, 2*time.Second, "stop-loss fired", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == f.put && b.TriggerReason == orders.TriggerStopLoss200Pct {
				return true
			}
		}
		return false
	})
}

// With the experiment OFF a real shed is recorded for the benchmark.
func TestFuturesHedge_OffRealShedIsACounterfactual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	f.withPutSheddingRegime()
	f.startRun()
	eventually(t, 2*time.Second, "put shed", func() bool { return f.position(f.put) == nil })
	eventually(t, 2*time.Second, "shed recorded", func() bool {
		st, _ := hedge.LoadMode(f.hedgeAt)
		return len(st.Ledger.Legs) == 1 && st.Ledger.Legs[0].Mode == hedge.CounterShed && st.Ledger.Legs[0].Qty == 1
	})
}

// Scenario: the kill switch leaves nothing open — the perpetual included,
// whatever the mode.
func TestFuturesHedge_KillSwitchFlattensThePerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: -23050, Direction: "sell", AveragePrice: 100000, MarkPrice: 100000, Kind: "future"})
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	if p := f.position(testPerp); p != nil {
		t.Fatal("the perpetual must never be adopted into the options book")
	}
	f.strat.KillSwitch()
	eventually(t, 2*time.Second, "everything flat", func() bool { return len(f.state.AllPositions()) == 0 && f.exch.perpSize() == 0 })
	var closes []orders.Order
	for _, o := range f.exch.perpOrders() {
		closes = append(closes, o)
	}
	if len(closes) != 1 || closes[0].Direction != orders.DirectionBuy || closes[0].Qty != 23050 || !closes[0].ReduceOnly ||
		closes[0].OrderType != orders.TypeMarket || closes[0].TriggerReason != orders.TriggerKillSwitch {
		t.Errorf("kill switch perpetual close %+v, want one reduce-only market buy of 23,050", closes)
	}
	if len(f.journal.hedgeChanges(orders.HedgeClosed)) != 1 {
		t.Error("hedge_closed journaled")
	}
}

// Scenario: switching off previews, then closes the hedge.
func TestFuturesHedge_SwitchOffPreviewsThenCloses(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true) // +0.2305, hedged below
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: -23050, Direction: "sell", AveragePrice: 100000, MarkPrice: 100000, Kind: "future"})
	f.exch.summary.InitialMargin, f.exch.summary.MaintenanceMargin = 0.1, 0.07
	f.exch.perpIMPerUSD = 0.000002
	f.startRun()
	eventually(t, 2*time.Second, "loop running", func() bool { return f.strat.HedgeState().Enabled && !f.strat.HedgeState().PerpAsOf.IsZero() })
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Fatalf("already hedged: %d perpetual orders", n)
	}

	p, err := f.strat.HedgePreview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !near(p.DeltaNow, 0, 0.001) || !near(p.DeltaAfter, 0.2305, 1e-9) || len(p.Scenarios) != 4 {
		t.Errorf("preview delta now %.4f after %.4f scenarios %d", p.DeltaNow, p.DeltaAfter, len(p.Scenarios))
	}
	if p.IMPctAfter <= p.IMPctNow || p.MarginError != "" {
		t.Errorf("IM after removing the hedge %.2f %% vs now %.2f %% (%s): want higher", p.IMPctAfter, p.IMPctNow, p.MarginError)
	}
	if len(p.Warnings) == 0 {
		t.Error("the preview names the risk the switch-off leaves")
	}

	res, err := f.strat.SetFuturesHedge(context.Background(), false, orders.Actor{User: "tester", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Enabled || res.ClosedUSD != 23050 {
		t.Errorf("switch-off result %+v, want OFF with 23,050 USD closed", res)
	}
	if f.exch.perpSize() != 0 {
		t.Error("the perpetual is closed")
	}
	o := f.exch.perpOrders()[0]
	if o.Direction != orders.DirectionBuy || !o.ReduceOnly || o.OrderType != orders.TypeMarket || o.TriggerReason != orders.TriggerFuturesHedgeClose {
		t.Errorf("close order %+v, want a reduce-only market buy", o)
	}
	if st, _ := hedge.LoadMode(f.hedgeAt); st.Enabled || st.By.User != "tester" {
		t.Errorf("mode file after the switch: %+v", st)
	}
	if len(f.journal.hedgeChanges(orders.HedgeClosed)) != 1 || len(f.journal.hedgeChanges(orders.HedgeModeChanged)) != 1 {
		t.Error("hedge_closed and hedge_mode_changed journaled")
	}
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 1 {
		t.Errorf("OFF: no new hedge, got %d perpetual orders", n)
	}
}

// Switching on writes the mode file and the loop starts hedging.
func TestFuturesHedge_SwitchOnStartsHedging(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, false)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	if _, err := f.strat.SetFuturesHedge(context.Background(), true, orders.Actor{User: "tester", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.perpOrders()) == 1 })
	st, _ := hedge.LoadMode(f.hedgeAt)
	if !st.Enabled || st.Ledger.Virtual.SizeUSD != 0 {
		t.Errorf("mode on and the paper hedge closed at the switch: %+v", st)
	}
	if math.Abs(f.exch.perpSize()+23050) > 0.5 {
		t.Errorf("perpetual %.0f", f.exch.perpSize())
	}
}

// Without a futures connection the experiment cannot be switched on.
func TestFuturesHedge_UnavailableWithoutFutures(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()
	if _, err := f.strat.SetFuturesHedge(context.Background(), true, orders.Actor{User: "tester", Role: "admin"}); err == nil {
		t.Error("no futures connection: the switch is refused")
	}
	eventually(t, 2*time.Second, "view published", func() bool { return f.strat.HedgeState().Unavailable != "" })
}
