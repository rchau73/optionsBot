package tests

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
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
	index    float64
	state    string // the ticker's state ("" = no reading)
	contract float64
	tickers  int   // ticker reads
	posErr   error // the position read fails
}

func (ff *fakeFutures) set(apply func(*fakeFutures)) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	apply(ff)
}

func (ff *fakeFutures) tickerReads() int {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return ff.tickers
}

func (ff *fakeFutures) GetFuturePositions(context.Context, string) ([]orders.RawPosition, error) {
	ff.mu.Lock()
	err := ff.posErr
	ff.mu.Unlock()
	if err != nil {
		return nil, err
	}
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
	ff.tickers++
	return orders.Ticker{Instrument: name, BestBid: ff.bid, BestAsk: ff.ask, Mark: (ff.bid + ff.ask) / 2, Index: ff.index, State: ff.state}, nil
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
	ff := &fakeFutures{ex: f.exch, bid: 99990, ask: 100010, index: 100000, contract: 10}
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

// Negative (#658 review): with the margin unknown the band is 0, and a band
// of 0 must mean no hedge, not a hedge of any size. The paper hedge waits too
// (it is the benchmark's data), then trades once the margin is back.
func TestFuturesHedge_PaperHedgeWaitsForTheMargin(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, false)
	f.exch.summaryErr = errors.New("account summary timed out")
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if v := f.strat.HedgeState().Benchmark.VirtualTrades; v != 0 {
		t.Fatalf("margin unknown: %d paper trades, want 0", v)
	}
	f.exch.mu.Lock()
	f.exch.summaryErr = nil
	f.exch.mu.Unlock()
	eventually(t, 2*time.Second, "paper hedge booked", func() bool { return f.strat.HedgeState().Benchmark.VirtualTrades == 1 })
}

// Negative (#658 review): a leg with no greeks yet counts as its mark alone,
// so the coin delta is wrong: nothing trades until the leg is priced.
func TestFuturesHedge_LegWithoutGreeksNoOrder(t *testing.T) {
	f, _ := newHedgeFixture(t, 0, -0.2905, true)
	f.market.instruments[f.call].UpdatedAt = time.Time{} // no quote yet (a live zero delta would roll the leg)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Fatalf("a leg without greeks: %d perpetual orders, want 0", n)
	}
	f.market.setDelta(f.call, 0.06)
	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.perpOrders()) == 1 })
	if o := f.exch.perpOrders()[0]; o.Direction != orders.DirectionSell || o.Qty != 23050 {
		t.Fatalf("hedge order %+v, want sell 23,050 USD once the leg is priced", o)
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
	if n := f.shedHedgedSkips(); n != 1 {
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

func (f *strategyFixture) shedHedgedSkips() int {
	n := 0
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, "shed_hedged") {
			n++
		}
	}
	return n
}

// Negative (#658 review): a restart forgets shedNoted but not the ledger, so
// a leg still shed is not counted, or journaled, a second time.
func TestFuturesHedge_RestartKeepsOneCounterfactualPerLeg(t *testing.T) {
	first, _ := newHedgeFixture(t, 0.16, -0.16, true)
	first.withPutSheddingRegime()
	first.startRun()
	eventually(t, 2*time.Second, "kept leg saved", func() bool {
		st, _ := hedge.LoadMode(first.hedgeAt)
		return len(st.Ledger.Legs) == 1
	})
	first.cancel()
	err := <-first.runErr
	first.runErr <- err // for the fixture's cleanup

	second, _ := newHedgeFixture(t, 0.16, -0.16, true)
	second.hedgeAt = first.hedgeAt
	second.withPutSheddingRegime()
	second.startRun()
	eventually(t, 2*time.Second, "the shed seen after the restart", func() bool { return second.strat.HedgeState().ShedNow == "puts" })
	time.Sleep(100 * time.Millisecond)
	if n := second.shedHedgedSkips(); n != 0 {
		t.Errorf("the restart journaled shed_hedged %d times, want 0", n)
	}
	if legs := second.strat.HedgeState().Benchmark.Legs; len(legs) != 1 {
		t.Errorf("after the restart the ledger has %d legs, want 1: %+v", len(legs), legs)
	}
}

// Only the part of a leg the ledger does not follow yet is a new
// counterfactual (a position that grew, or two sharing the instrument).
func TestFuturesHedge_CounterfactualCoversOnlyTheUnfollowedQty(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, true)
	f.withPutSheddingRegime()
	seen := time.Now().Add(-time.Hour)
	if err := hedge.SaveMode(f.hedgeAt, hedge.ModeState{Enabled: true, Since: seen, Ledger: hedge.Ledger{Since: seen, Legs: []hedge.Counterfactual{
		{At: seen, Mode: hedge.CounterHedged, Instrument: f.put, OptionType: "put", Qty: 0.4, ShedPrice: 0.02, LastPrice: 0.02, LastAt: seen},
	}}}); err != nil {
		t.Fatal(err)
	}
	f.startRun()
	eventually(t, 2*time.Second, "shed_hedged journaled", func() bool { return f.shedHedgedSkips() == 1 })
	eventually(t, 2*time.Second, "view published", func() bool { return len(f.strat.HedgeState().Benchmark.Legs) == 2 })
	if got := f.strat.HedgeState().Benchmark.Legs[1].Qty; !near(got, 0.6, 1e-9) {
		t.Errorf("new counterfactual qty %v, want 0.6 of the 1.0 held", got)
	}
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

// withPerpLedger holds a perpetual of exchangeUSD on the account, ledgerUSD of
// it in the bot's ledger. The mode stays off: with it on, a cycle would match
// the ledger to the exchange.
func withPerpLedger(t *testing.T, f *strategyFixture, exchangeUSD, ledgerUSD float64) {
	t.Helper()
	dir := "sell"
	if exchangeUSD > 0 {
		dir = "buy"
	}
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: exchangeUSD, Direction: dir, AveragePrice: 100000, MarkPrice: 100000, Kind: "future"})
	st := hedge.ModeState{Ledger: hedge.Ledger{Real: hedge.PerpBook{SizeUSD: ledgerUSD, AvgPrice: 100000}}}
	if err := hedge.SaveMode(f.hedgeAt, st); err != nil {
		t.Fatal(err)
	}
}

// fireKillSwitch runs the bot to a reconciled book and fires the kill switch.
func fireKillSwitch(t *testing.T, f *strategyFixture) {
	t.Helper()
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	if p := f.position(testPerp); p != nil {
		t.Fatal("the perpetual must never be adopted into the options book")
	}
	f.strat.KillSwitch()
	eventually(t, 2*time.Second, "options flat", func() bool { return len(f.state.AllPositions()) == 0 })
}

// wantLeftOpen waits for the one perp_left_open line naming usd.
func wantLeftOpen(t *testing.T, f *strategyFixture, usd float64) {
	t.Helper()
	eventually(t, 2*time.Second, "perp_left_open", func() bool { return len(f.journal.hedgeChanges(orders.HedgeLeftOpen)) > 0 })
	left := f.journal.hedgeChanges(orders.HedgeLeftOpen)
	if len(left) != 1 || left[0].AmountUSD != usd || !strings.Contains(left[0].Detail, "left open") {
		t.Errorf("perp_left_open %+v, want one line naming %.0f USD", left, usd)
	}
}

// The kill switch closes the bot's own perpetual whole.
func TestFuturesHedge_KillSwitchFlattensThePerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	withPerpLedger(t, f, -23050, -23050)
	fireKillSwitch(t, f)
	eventually(t, 2*time.Second, "hedge_closed", func() bool { return len(f.journal.hedgeChanges(orders.HedgeClosed)) == 1 })
	closes := f.exch.perpOrders()
	if len(closes) != 1 || closes[0].Direction != orders.DirectionBuy || closes[0].Qty != 23050 || !closes[0].ReduceOnly ||
		closes[0].OrderType != orders.TypeMarket || closes[0].TriggerReason != orders.TriggerKillSwitch {
		t.Errorf("kill switch perpetual close %+v, want one reduce-only market buy of 23,050", closes)
	}
	if f.exch.perpSize() != 0 {
		t.Errorf("perpetual %.0f, want flat", f.exch.perpSize())
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeLeftOpen)); n != 0 {
		t.Errorf("%d perp_left_open lines, want none: nothing is left", n)
	}
}

// Scenario: a hand-held perpetual survives the kill switch (#667).
func TestFuturesHedge_KillSwitchLeavesAHandHeldPerpetual(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	withPerpLedger(t, f, -50000, -20000)
	fireKillSwitch(t, f)
	wantLeftOpen(t, f, 30000)
	closes := f.exch.perpOrders()
	if len(closes) != 1 || closes[0].Direction != orders.DirectionBuy || closes[0].Qty != 20000 || !closes[0].ReduceOnly {
		t.Fatalf("kill switch perpetual close %+v, want one reduce-only buy of 20,000", closes)
	}
	if got := f.exch.perpSize(); got != -30000 {
		t.Errorf("perpetual %.0f, want the hand-held -30,000", got)
	}
	eventually(t, 2*time.Second, "the view shows what is left", func() bool { return f.strat.HedgeState().PerpUSD == -30000 })
	if st, _ := hedge.LoadMode(f.hedgeAt); st.Ledger.Real.SizeUSD != 0 {
		t.Errorf("ledger after the close: %+v, want flat", st.Ledger.Real)
	}
}

// Scenario: no hedge, no futures order (#667): the ledger holds nothing or
// opposes the position, or an old file gives a live account a ledger.
func TestFuturesHedge_KillSwitchNoHedgeNoFuturesOrder(t *testing.T) {
	for _, tc := range []struct {
		name             string
		exchange, ledger float64
		live             bool
	}{
		{name: "never switched on", exchange: -50000},
		{name: "never switched on, long", exchange: 50000},
		{name: "live, never switched on", exchange: -50000, live: true},
		{name: "live, an old ledger in the mode file", exchange: -50000, ledger: -20000, live: true},
		{name: "the ledger is short, the account long", exchange: 50000, ledger: -20000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newHedgeFixture(t, 0.16, -0.16, false)
			withPerpLedger(t, f, tc.exchange, tc.ledger)
			if tc.live {
				f.beforeRun = func(*strategy.Strategy) { f.cfg.Environment = "live" }
			}
			fireKillSwitch(t, f)
			wantLeftOpen(t, f, math.Abs(tc.exchange))
			if o := f.exch.perpOrders(); len(o) != 0 {
				t.Errorf("futures orders %+v, want none", o)
			}
		})
	}
}

// The exchange holds less than the ledger (a hand close): only what is there
// is bought back, and the ledger comes down with it (#667).
func TestFuturesHedge_KillSwitchCappedByTheExchange(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	withPerpLedger(t, f, -10000, -20000)
	fireKillSwitch(t, f)
	eventually(t, 2*time.Second, "ledger flat", func() bool {
		st, _ := hedge.LoadMode(f.hedgeAt)
		return st.Ledger.Real.SizeUSD == 0
	})
	if o := f.exch.perpOrders(); len(o) != 1 || o[0].Qty != 10000 {
		t.Fatalf("kill switch perpetual close %+v, want one buy of 10,000", o)
	}
	if f.exch.perpSize() != 0 {
		t.Errorf("perpetual %.0f, want flat", f.exch.perpSize())
	}
}

// A close that never fills retries and leaves the hand-held part alone: the
// kill switch's check counts only the bot's size (#667).
func TestFuturesHedge_KillSwitchUnfilledCloseStaysWithinTheLedger(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	withPerpLedger(t, f, -50000, -20000)
	f.exch.onSubmit = func(o orders.Order, id string) orders.Fill {
		if o.Instrument == testPerp {
			return orders.Fill{OrderID: id} // never fills
		}
		return hedgeFills(o, id)
	}
	fireKillSwitch(t, f)
	wantLeftOpen(t, f, 30000)
	closes := f.exch.perpOrders()
	if len(closes) == 0 {
		t.Fatal("no close sent")
	}
	for _, o := range closes {
		if o.Qty != 20000 {
			t.Errorf("close of %.0f, want the ledger's 20,000 each attempt", o.Qty)
		}
	}
}

// Scenario: the monitor's view prices the whole book (#666): the moves with
// and without the perpetual, its closed result and fees, and testnet.
func TestFuturesHedge_ViewPricesTheBook(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true) // +0.2305, hedged below
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: -23050, Direction: "sell", AveragePrice: 100000, MarkPrice: 100000, Kind: "future"})
	f.startRun()
	eventually(t, 2*time.Second, "perpetual read", func() bool { return f.strat.HedgeState().PerpUSD == -23050 })
	v := f.strat.HedgeState()
	if !v.Testnet || len(v.BookScenarios) != len(hedge.ScenarioMoves) {
		t.Fatalf("testnet %v, %d scenarios: want testnet with every move", v.Testnet, len(v.BookScenarios))
	}
	for _, sc := range v.BookScenarios {
		if want := sc.OptionsUSD - 23050*sc.MovePct/100; !near(sc.BookUSD, want, 0.01) {
			t.Errorf("%+v: book USD, want %.2f", sc, want)
		}
	}
}

// A perpetual a failed close left behind counts in the net delta with the
// mode off (#666): it is on the account, whatever the mode.
func TestFuturesHedge_ViewCountsWhatIsLeftOpenWhileOff(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.16, -0.16, false)
	withPerpLedger(t, f, -23050, -23050)
	f.exch.onSubmit = func(o orders.Order, id string) orders.Fill {
		if o.Instrument == testPerp {
			return orders.Fill{OrderID: id} // never fills
		}
		return hedgeFills(o, id)
	}
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	f.strat.KillSwitch()
	eventually(t, 5*time.Second, "the failed close published", func() bool {
		v := f.strat.HedgeState()
		return !v.Enabled && v.PerpUSD == -23050 && near(v.NetDelta, v.OptionsDelta-0.2305, 1e-3)
	})
}

// Scenario: switching off previews, then closes the hedge.
func TestFuturesHedge_SwitchOffPreviewsThenCloses(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true) // +0.2305, hedged below
	f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: testPerp, Size: -23050, Direction: "sell", AveragePrice: 100000, MarkPrice: 100000, RealizedFunding: -0.0001, Kind: "future"})
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
	closed := f.journal.hedgeChanges(orders.HedgeClosed)
	if len(closed) != 1 || len(f.journal.hedgeChanges(orders.HedgeModeChanged)) != 1 {
		t.Fatal("hedge_closed and hedge_mode_changed journaled")
	}
	if closed[0].FundingCoin != -0.0001 {
		t.Errorf("hedge_closed carries the funding reading, got %v", closed[0].FundingCoin)
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
