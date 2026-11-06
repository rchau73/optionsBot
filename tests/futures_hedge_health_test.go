package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// A futures hedge that cannot trade gives the shed back (#664): Strategy.Run
// end to end, on the fixture of futures_hedge_run_test.go.

var lockedByAdmin = &gateway.RPCError{Code: 10019, Message: "locked_by_admin"}

func rejectPerp(err error) func(orders.Order) error {
	return func(o orders.Order) error {
		if o.Instrument == testPerp {
			return err
		}
		return nil
	}
}

func (f *fakeExchange) shedBuys(instrument string) int {
	n := 0
	for _, b := range f.buys() {
		if b.Instrument == instrument && b.TriggerReason == orders.TriggerGammaClose {
			n++
		}
	}
	return n
}

// Scenario: a hedge that cannot trade gives the shed back.
func TestFuturesHedge_AHedgeThatCannotTradeGivesTheShedBack(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2505, true) // book delta +0.1905
	f.withCallSheddingRegime()
	f.exch.rejectSubmit = rejectPerp(lockedByAdmin)
	f.startRun()

	eventually(t, 2*time.Second, "calls closed by the shed", func() bool { return f.exch.shedBuys(f.call) >= 1 })
	eventually(t, 2*time.Second, "shed recorded for the benchmark", func() bool {
		for _, l := range f.strat.HedgeState().Benchmark.Legs {
			if l.Mode == hedge.CounterShed && l.Instrument == f.call {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	un := f.journal.hedgeChanges(orders.HedgeUnavailable)
	if len(un) != 1 || !strings.Contains(un[0].Detail, "locked_by_admin") {
		t.Errorf("hedge_unavailable once, with the reason: %+v", un)
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeHalted)); n != 1 {
		t.Errorf("hedge_halted journaled %d times, want once", n)
	}
	if n := len(f.exch.perpOrders()); n != 1 {
		t.Errorf("a lasting rejection stops the orders: %d perpetual orders, want 1", n)
	}
	for _, l := range f.strat.HedgeState().Benchmark.Legs {
		if l.Mode == hedge.CounterHedged && !l.Done {
			t.Errorf("the shed ended the kept leg's holding: %+v", l)
		}
	}
}

// Scenario: a locked instrument halts the hedge.
func TestFuturesHedge_ALockedInstrumentHaltsTheHedge(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	ff.state = "locked"
	f.startRun()

	eventually(t, 2*time.Second, "halted", func() bool { return f.strat.HedgeState().Halted })
	time.Sleep(100 * time.Millisecond)
	v := f.strat.HedgeState()
	if !strings.Contains(v.HaltReason, "locked") || v.NotHolding != v.HaltReason || !v.RecheckAt.After(time.Now()) {
		t.Errorf("the hedge reports the reason and the re-check: %+v", v)
	}
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("locked: %d perpetual orders, want none", n)
	}
	if n := ff.tickerReads(); n != 1 {
		t.Errorf("it looks again only at the re-check: %d ticker reads, want 1", n)
	}
	h := f.journal.hedgeChanges(orders.HedgeHalted)
	if len(h) != 1 || !strings.Contains(h[0].Detail, "locked") {
		t.Errorf("hedge_halted with the reason: %+v", h)
	}
}

// The maintenance lock lifts: the hedge resumes by itself at a re-check, and
// the same lock seen again at a re-check is not a second halt.
func TestFuturesHedge_ResumesWhenTheLockLifts(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	f.hedgeRecheck = 20 * time.Millisecond
	ff.state = "locked"
	f.startRun()

	eventually(t, 2*time.Second, "re-checked", func() bool { return ff.tickerReads() >= 3 })
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Fatalf("still locked: %d perpetual orders", n)
	}
	ff.set(func(ff *fakeFutures) { ff.state = "open" })
	eventually(t, 2*time.Second, "hedged", func() bool { return f.exch.perpSize() == -23050 })
	eventually(t, 2*time.Second, "resumed", func() bool { return len(f.journal.hedgeChanges(orders.HedgeResumed)) == 1 })
	if v := f.strat.HedgeState(); v.Halted || v.NotHolding != "" {
		t.Errorf("no halt left: %+v", v)
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeHalted)); n != 1 {
		t.Errorf("hedge_halted %d times, want once per reason", n)
	}
}

// A re-check whose order is rejected again stays halted, on one journal line,
// with at most one order per re-check.
func TestFuturesHedge_RecheckRejectedAgainStaysHalted(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true)
	f.hedgeRecheck = 20 * time.Millisecond
	f.exch.rejectSubmit = rejectPerp(&gateway.RPCError{Code: 10009, Message: "not_enough_funds"})
	f.startRun()

	eventually(t, 2*time.Second, "orders retried", func() bool { return len(f.exch.perpOrders()) >= 2 })
	if v := f.strat.HedgeState(); !v.Halted || !strings.Contains(v.HaltReason, "enough funds") {
		t.Errorf("halted on the funds: %+v", v)
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeHalted)); n != 1 {
		t.Errorf("hedge_halted %d times, want once per reason", n)
	}
}

// A rejection a retry may cure gives the shed back at once, but the next
// cycle tries again, and a fill takes it back: no halt.
func TestFuturesHedge_RetryableRejectionDoesNotHalt(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, true)
	once := true
	f.exch.rejectSubmit = func(o orders.Order) error {
		if o.Instrument == testPerp && once {
			once = false
			return &gateway.RPCError{Code: 10028, Message: "too_many_requests"}
		}
		return nil
	}
	f.startRun()

	eventually(t, 2*time.Second, "hedged on the retry", func() bool { return f.exch.perpSize() == -23050 })
	eventually(t, 2*time.Second, "resumed", func() bool { return len(f.journal.hedgeChanges(orders.HedgeResumed)) == 1 })
	if n := len(f.journal.hedgeChanges(orders.HedgeHalted)); n != 0 {
		t.Errorf("a retryable rejection does not halt: %d", n)
	}
	un := f.journal.hedgeChanges(orders.HedgeUnavailable)
	if len(un) != 1 || !strings.Contains(un[0].Detail, "too_many_requests") {
		t.Errorf("hedge_unavailable once, with the reason: %+v", un)
	}
}

// Three cycles without a fill give the shed back, whatever kept the hedge from
// filling, journaled once.
func TestFuturesHedge_NoFillForThreeCyclesGivesTheShedBack(t *testing.T) {
	cases := map[string]struct {
		setup func(*strategyFixture, *fakeFutures)
		why   string
	}{
		"the IOC never fills": {func(f *strategyFixture, _ *fakeFutures) {
			f.exch.onSubmit = func(o orders.Order, id string) orders.Fill {
				if o.Instrument == testPerp {
					return orders.Fill{OrderID: id}
				}
				return hedgeFills(o, id)
			}
		}, "nothing to fill"},
		"no bid":              {func(_ *strategyFixture, ff *fakeFutures) { ff.bid = 0 }, "no price"},
		"position unreadable": {func(_ *strategyFixture, ff *fakeFutures) { ff.posErr = errors.New("timeout") }, "cannot be read"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, ff := newHedgeFixture(t, 0.06, -0.2505, true)
			f.withPutSheddingRegime()
			c.setup(f, ff)
			f.startRun()
			eventually(t, 2*time.Second, "put shed", func() bool { return f.exch.shedBuys(f.put) >= 1 })
			un := f.journal.hedgeChanges(orders.HedgeUnavailable)
			if len(un) != 1 || !strings.Contains(un[0].Detail, c.why) {
				t.Errorf("hedge_unavailable once, naming %q: %+v", c.why, un)
			}
		})
	}
}

// What must not change: a hedge that fills keeps the shed's legs, even with an
// order due.
func TestFuturesHedge_AFillingHedgeStillKeepsTheShedLegs(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2505, true)
	f.withPutSheddingRegime()
	f.startRun()
	eventually(t, 2*time.Second, "hedged", func() bool { return f.exch.perpSize() != 0 })
	eventually(t, 2*time.Second, "shed_hedged journaled", func() bool { return f.shedHedgedSkips() == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := f.exch.shedBuys(f.put); n != 0 {
		t.Errorf("the hedge holds: %d shed buy-backs, want none", n)
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeUnavailable)); n != 0 {
		t.Errorf("hedge_unavailable %d times, want none", n)
	}
}

// Margin unknown says nothing about the hedge: it does not count as a cycle
// without a fill, so it alone does not bring a shed back.
func TestFuturesHedge_MarginUnknownDoesNotGiveTheShedBack(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2505, true)
	f.withPutSheddingRegime()
	f.exch.summaryErr = errors.New("account summary timed out")
	f.startRun()
	eventually(t, 2*time.Second, "shed_hedged journaled", func() bool { return f.shedHedgedSkips() == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := f.exch.shedBuys(f.put); n != 0 {
		t.Errorf("%d shed buy-backs, want none", n)
	}
}

// Switching off clears what the hedge knew: on again, it starts clean.
func TestFuturesHedge_SwitchClearsTheHalt(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	ff.state = "locked"
	f.startRun()
	eventually(t, 2*time.Second, "halted", func() bool { return f.strat.HedgeState().Halted })
	if _, err := f.strat.SetFuturesHedge(context.Background(), false, orders.Actor{User: "tester", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, "no halt left", func() bool {
		v := f.strat.HedgeState()
		return !v.Enabled && !v.Halted && v.NotHolding == ""
	})
}

// Scenario: testnet books the hedge at the index.
func TestFuturesHedge_TestnetBooksTheHedgeAtTheIndex(t *testing.T) {
	f, ff := newHedgeFixture(t, 0.16, -0.2259, true) // net +0.0659 × 75,888 ≈ 5,000 USD
	f.market.price = 75888
	ff.bid, ff.ask, ff.index = 78562.5, 78563, 75888
	f.startRun()

	eventually(t, 2*time.Second, "hedged", func() bool { return len(f.journal.hedgeChanges(orders.HedgeFill)) == 1 })
	if o := f.exch.perpOrders()[0]; o.LimitPrice != 78562.5 || o.Qty != 5000 {
		t.Fatalf("the real order goes out at the bid (no guard on testnet): %+v", o)
	}
	if r := f.journal.hedgeChanges(orders.HedgeFill)[0]; r.Price != 78562.5 || r.LedgerPrice != 75888 {
		t.Errorf("hedge_fill says both prices: %+v", r)
	}
	st, _ := hedge.LoadMode(f.hedgeAt)
	if st.Ledger.Real.SizeUSD != -5000 || !near(st.Ledger.Real.AvgPrice, 75888, 1e-6) {
		t.Errorf("the ledger books the sell at the index: %+v", st.Ledger.Real)
	}
	eventually(t, 2*time.Second, "view published", func() bool { return f.strat.HedgeState().Benchmark.RealTrades == 1 })
	if b := f.strat.HedgeState().Benchmark; b.Pricing != "index" || !strings.Contains(b.PricingNote, "benchmark priced at the index (testnet)") {
		t.Errorf("the view says which pricing: %+v", b)
	}
}

// liveFixture is a live account with the hedge on. The experiment itself
// stays testnet-only (the mode file and the switch refuse live), so the
// account turns live after the mode is read: this reaches the live rules.
func liveFixture(t *testing.T) (*strategyFixture, *fakeFutures) {
	t.Helper()
	f, ff := newHedgeFixture(t, 0.06, -0.2905, true)
	f.beforeRun = func(*strategy.Strategy) { f.cfg.Environment = "live" }
	return f, ff
}

// Scenario: the price guard applies on live only.
func TestFuturesHedge_ThePriceGuardAppliesOnLiveOnly(t *testing.T) {
	f, ff := liveFixture(t)
	ff.bid, ff.ask = 101500, 101520 // 1.5 % above the index
	f.startRun()

	eventually(t, 2*time.Second, "hedge_price_away", func() bool { return len(f.journal.hedgeChanges(orders.HedgePriceAway)) >= 1 })
	eventually(t, 2*time.Second, "the shed is back", func() bool { return f.strat.HedgeState().NotHolding != "" })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.perpOrders()); n != 0 {
		t.Errorf("1.5 %% from the index: %d perpetual orders, want none", n)
	}
	away := f.journal.hedgeChanges(orders.HedgePriceAway)
	if len(away) != 1 || !strings.Contains(away[0].Detail, "1.50 %") {
		t.Errorf("hedge_price_away once, with the gap: %+v", away)
	}
}

// Live, in the guard: the order goes out and the ledger books the exchange's price.
func TestFuturesHedge_LiveBooksTheExchangesPrice(t *testing.T) {
	f, ff := liveFixture(t)
	ff.bid, ff.ask = 100900, 100920 // 0.9 % above the index
	f.startRun()

	eventually(t, 2*time.Second, "hedged", func() bool { return f.exch.perpSize() == -22870 || len(f.journal.hedgeChanges(orders.HedgeFill)) == 1 })
	eventually(t, 2*time.Second, "view published", func() bool { return f.strat.HedgeState().Benchmark.RealTrades == 1 })
	st, _ := hedge.LoadMode(f.hedgeAt)
	if !near(st.Ledger.Real.AvgPrice, 100900, 1e-6) {
		t.Errorf("live books the fill's own price: %+v", st.Ledger.Real)
	}
	if b := f.strat.HedgeState().Benchmark; b.Pricing != "exchange" {
		t.Errorf("pricing %q, want exchange", b.Pricing)
	}
}

// ── The pure rules ───────────────────────────────────────────────────────────

func TestHedgeHealth_NotHolding(t *testing.T) {
	now := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	const maxCycles = 3
	cases := map[string]struct {
		do   func(*hedge.Health)
		want string
	}{
		"new":                 {func(*hedge.Health) {}, ""},
		"rejected":            {func(h *hedge.Health) { h.Rejected("x") }, "the last hedge order was rejected: x"},
		"rejected then fill":  {func(h *hedge.Health) { h.Rejected("x"); h.Holding() }, ""},
		"two cycles unfilled": {func(h *hedge.Health) { h.NotFilled("a"); h.NotFilled("b") }, ""},
		"three cycles unfilled": {func(h *hedge.Health) {
			h.NotFilled("a")
			h.NotFilled("b")
			h.NotFilled("c")
		}, "c"},
		"a fill restarts the count": {func(h *hedge.Health) {
			h.NotFilled("a")
			h.NotFilled("b")
			h.Holding()
			h.NotFilled("c")
		}, ""},
		"halted":                  {func(h *hedge.Health) { h.Halt("locked", now, time.Minute) }, "locked"},
		"a band reading keeps it": {func(h *hedge.Health) { h.Halt("locked", now, time.Minute); h.Holding() }, "locked"},
		"resumed":                 {func(h *hedge.Health) { h.Halt("locked", now, time.Minute); h.Resume() }, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var h hedge.Health
			c.do(&h)
			if got := h.NotHolding(maxCycles); got != c.want {
				t.Errorf("NotHolding = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHedgeHealth_HaltWaitsForTheRecheck(t *testing.T) {
	now := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	wait := time.Duration(config.FixedFuturesHedgeRecheckMinutes) * time.Minute
	var h hedge.Health
	if h.Waiting(now) {
		t.Error("not halted: nothing to wait for")
	}
	if !h.Halt("locked", now, wait) {
		t.Error("the first halt is new")
	}
	if !h.Waiting(now.Add(wait-time.Second)) || h.Waiting(now.Add(wait)) {
		t.Error("waits until the re-check, not past it")
	}
	if h.Halt("locked", now.Add(wait), wait) {
		t.Error("the same reason at a re-check is not new")
	}
	if !h.HaltedAt.Equal(now) || !h.RecheckAt.Equal(now.Add(2*wait)) {
		t.Errorf("the halt keeps its start and moves its re-check: %+v", h)
	}
	if !h.Halt("funds", now.Add(wait), wait) {
		t.Error("another reason is new")
	}
}

func TestHedge_InstrumentClosed(t *testing.T) {
	for state, want := range map[string]bool{"open": false, "": false, "locked": true, "closed": true} {
		if got := hedge.InstrumentClosed(state); got != want {
			t.Errorf("state %q: %v, want %v", state, got, want)
		}
	}
}

func TestHedgePricing_Blocks(t *testing.T) {
	const maxGap = config.FixedFuturesHedgeMaxIndexGapPct
	cases := []struct {
		name         string
		p            hedge.Pricing
		price, index float64
		want         bool
	}{
		{"live, in the guard", hedge.PricingExchange, 100500, 100000, false},
		{"live, at the limit", hedge.PricingExchange, 101000, 100000, false},
		{"live, just past it", hedge.PricingExchange, 101001, 100000, true},
		{"live, 1.5 % above", hedge.PricingExchange, 101500, 100000, true},
		{"live, 1.5 % below", hedge.PricingExchange, 98500, 100000, true},
		{"live, no index", hedge.PricingExchange, 101500, 0, false},
		{"testnet, 3.5 % above", hedge.PricingIndex, 78562.5, 75888, false},
	}
	for _, c := range cases {
		if got := c.p.Blocks(c.price, c.index, maxGap); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHedgePricing_BookAndSummarise(t *testing.T) {
	if got := hedge.PricingIndex.Book(78562.5, 75888); got != 75888 {
		t.Errorf("index pricing books %v", got)
	}
	if got := hedge.PricingIndex.Book(78562.5, 0); got != 78562.5 {
		t.Errorf("no index: the fill's price, got %v", got)
	}
	if got := hedge.PricingExchange.Book(78562.5, 75888); got != 78562.5 {
		t.Errorf("exchange pricing books %v", got)
	}

	var l hedge.Ledger
	l.Real.Book(-5000, 75888, 0)
	const funding, mark, index = 0.001, 78000.0, 75888.0
	if s := hedge.PricingIndex.Summarise(l, funding, mark, index); !near(s.RealCoin, 0, 1e-12) || s.Pricing != "index" {
		t.Errorf("at the index with no funding the position is flat: %+v", s)
	}
	want := l.Summarise(funding, mark)
	if s := hedge.PricingExchange.Summarise(l, funding, mark, index); s.RealCoin != want.RealCoin || s.Pricing != "exchange" || s.PricingNote == "" {
		t.Errorf("exchange pricing: %+v, want %+v", s, want)
	}
}

func TestLedger_EndKept(t *testing.T) {
	l := hedge.Ledger{Legs: []hedge.Counterfactual{
		{Mode: hedge.CounterHedged, Instrument: "A", Qty: 1},
		{Mode: hedge.CounterShed, Instrument: "A", Qty: 1},
		{Mode: hedge.CounterHedged, Instrument: "B", Qty: 1},
	}}
	if !l.EndKept("A") || l.EndKept("A") {
		t.Error("ends once")
	}
	if !l.Legs[0].Done || l.Legs[1].Done || l.Legs[2].Done {
		t.Errorf("only A's kept leg ends: %+v", l.Legs)
	}
}

func TestLastingRejection(t *testing.T) {
	cases := []struct {
		err  error
		want string // a word of the reason; "" = none
	}{
		{lockedByAdmin, "locked_by_admin"},
		{fmt.Errorf("private/sell: %w", lockedByAdmin), "locked_by_admin"},
		{&gateway.RPCError{Code: 10009, Message: "not_enough_funds"}, "enough funds"},
		{&gateway.RPCError{Code: 13021, Message: "forbidden"}, "API key"},
		{orders.ErrForbidden, "API key"},
		{&gateway.RPCError{Code: 10028, Message: "too_many_requests"}, ""},
		{gateway.ErrConnectionLost, ""},
		{nil, ""},
	}
	for _, c := range cases {
		got := orders.LastingRejection(c.err)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%v: %q, want %q", c.err, got, c.want)
		}
	}
}

func TestTicker_ReadsTheInstrumentState(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"instrument_name":"ETH-PERPETUAL","best_bid_price":1,"best_ask_price":2,"state":"locked"}`}}}
	tk, err := orders.NewExecutor(fc).GetTicker(context.Background(), "ETH-PERPETUAL")
	if err != nil || tk.State != "locked" {
		t.Errorf("ticker = %+v, err = %v", tk, err)
	}
}
