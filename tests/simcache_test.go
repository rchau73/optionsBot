package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// #888: ETH's margin simulation was refused as too_many_requests (10028) on
// most cycles, and each refusal skipped the slot for the cycle. The executor
// now retries a rate-limited simulation after a backoff, counts margin calls
// per minute, and the strategy reuses a simulation of an unchanged book.

const simOKJSON = `{"currency":"ETH","initial_margin":0.3,"maintenance_margin":0.2,"margin_balance":10}`

func tooManyRequests() error {
	return &gateway.RPCError{Code: 10028, Message: "too_many_requests"}
}

// Scenario: the simulation is rate limited once — the bot retries after a
// backoff and gets a margin result, still spaced one call a second.
func TestSimulatePortfolio_RateLimitedOnceRetriesAfterBackoff(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{err: tooManyRequests()}, {result: simOKJSON}}}
	exec := orders.NewExecutor(fc)
	exec.SetSimulateBackoff(50 * time.Millisecond)

	start := time.Now()
	sum, err := exec.SimulatePortfolio(context.Background(), "ETH", map[string]float64{"ETH-X-C": -1})
	if err != nil {
		t.Fatalf("one 10028 must be retried, got %v", err)
	}
	if sum.InitialMargin != 0.3 || len(fc.calls) != 2 {
		t.Errorf("want the retry's result after 2 calls, got %+v after %d", sum, len(fc.calls))
	}
	if time.Since(start) < time.Second {
		t.Error("the retry still waits for the 1 call/s spacing")
	}
	if fc.calls[1].method != "private/simulate_portfolio" || fc.calls[1].params["add_positions"] != true {
		t.Errorf("the retry repeats the same request: %+v", fc.calls[1])
	}
}

// The ETH pattern: every other simulation refused. Before, half the sizing
// attempts failed (a margin_limit skip each); with the retry none does.
func TestSimulatePortfolio_EveryOtherRejectedNoSizingFails(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{
		{err: tooManyRequests()}, {result: simOKJSON},
		{err: tooManyRequests()}, {result: simOKJSON},
	}}
	exec := orders.NewExecutor(fc)
	exec.SetSimulateBackoff(10 * time.Millisecond)
	failed := 0
	for i := 0; i < 2; i++ {
		if _, err := exec.SimulatePortfolio(context.Background(), "ETH", nil); err != nil {
			failed++
		}
	}
	if failed != 0 {
		t.Errorf("rejected first attempts 50%%, failed simulations want 0, got %d of 2", failed)
	}
}

// The margin guard stays: a simulation refused on every attempt is an error
// (no margin result → no order), after SimulateRetries retries only.
func TestSimulatePortfolio_RateLimitedEveryTimeGivesUp(t *testing.T) {
	var replies []fakeReply
	for i := 0; i <= orders.SimulateRetries+1; i++ {
		replies = append(replies, fakeReply{err: tooManyRequests()})
	}
	fc := &fakeCaller{replies: replies}
	exec := orders.NewExecutor(fc)
	exec.SetSimulateBackoff(10 * time.Millisecond)

	_, err := exec.SimulateAlone(context.Background(), "ETH", nil)
	if !gateway.IsRateLimited(err) {
		t.Fatalf("want the rate-limit error back, got %v", err)
	}
	if len(fc.calls) != orders.SimulateRetries+1 {
		t.Errorf("calls = %d, want %d (first + retries)", len(fc.calls), orders.SimulateRetries+1)
	}
}

func TestSimulatePortfolio_OtherErrorsAreNotRetried(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{err: errors.New("connection lost")}, {result: simOKJSON}}}
	exec := orders.NewExecutor(fc)
	if _, err := exec.SimulatePortfolio(context.Background(), "ETH", nil); err == nil {
		t.Fatal("a non-rate-limit error is returned as it is")
	}
	if len(fc.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(fc.calls))
	}
}

func TestSimulatePortfolio_BackoffRespectsCancellation(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{err: tooManyRequests()}, {result: simOKJSON}}}
	exec := orders.NewExecutor(fc) // the real 2 s backoff
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := exec.SimulatePortfolio(ctx, "ETH", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a cancelled cycle stops waiting, got %v", err)
	}
}

func TestCallStats_CountsPerMinuteAndRejectedShare(t *testing.T) {
	var c orders.CallStats
	t0 := time.Date(2026, 11, 24, 7, 30, 5, 0, time.UTC)
	for i, limited := range []bool{true, false, true, true} {
		if _, ok := c.Record("private/simulate_portfolio", limited, t0.Add(time.Duration(i)*time.Second)); ok {
			t.Fatal("a minute is reported only once it is over")
		}
	}
	if _, ok := c.Record("private/get_account_summary", false, t0); ok {
		t.Fatal("methods are counted apart")
	}
	m, ok := c.Record("private/simulate_portfolio", false, t0.Add(time.Minute))
	if !ok || m.Calls != 4 || m.RateLimited != 3 || m.RejectedPct() != 75 || !m.Minute.Equal(t0.Truncate(time.Minute)) {
		t.Errorf("previous minute = %+v ok=%v, want 4 calls, 3 limited, 75 %%", m, ok)
	}
	if (orders.CallMinute{}).RejectedPct() != 0 {
		t.Error("no calls → 0 %")
	}
}

func TestSimCache_ReuseWithinTTLOnly(t *testing.T) {
	c := strategy.NewSimCache(strategy.SimReuse)
	t0 := time.Now()
	sum := orders.AccountSummary{InitialMargin: 1}
	c.Put("k", sum, t0)
	if got, ok := c.Get("k", t0.Add(strategy.SimReuse-time.Second)); !ok || got.InitialMargin != 1 {
		t.Error("a result younger than SimReuse is reused")
	}
	if _, ok := c.Get("k", t0.Add(strategy.SimReuse)); ok {
		t.Error("a result SimReuse old is dropped")
	}
	c.Put("k", sum, t0)
	c.Drop()
	if _, ok := c.Get("k", t0); ok {
		t.Error("Drop forgets every result")
	}
	if _, ok := c.Get("other", t0); ok {
		t.Error("an unknown key is a miss")
	}
}

func TestSimCache_KeyChangesWithTheBook(t *testing.T) {
	u := risk.Usage{Unit: "ETH", IM: 0.3, MM: 0.2, MarginBalance: 10}
	pos := []*orders.Position{{Instrument: "ETH-X-C", Qty: 1}, {Instrument: "ETH-X-P", Qty: 1}}
	book := strategy.SimBookKey(u, 20, pos)

	a := strategy.SimKey(false, map[string]float64{"A": -1, "B": -1}, book)
	if b := strategy.SimKey(false, map[string]float64{"B": -1, "A": -1}, book); a != b {
		t.Error("the same request gives the same key")
	}
	if strategy.SimKey(true, map[string]float64{"A": -1, "B": -1}, book) == a {
		t.Error("alone and added to the book are different requests")
	}
	if strategy.SimKey(false, map[string]float64{"A": -2, "B": -1}, book) == a {
		t.Error("another size is another request")
	}
	filled := []*orders.Position{{Instrument: "ETH-X-C", Qty: 2}, {Instrument: "ETH-X-P", Qty: 1}}
	moved := risk.Usage{Unit: "ETH", IM: 0.31, MM: 0.2, MarginBalance: 10}
	for name, other := range map[string]string{
		"a fill":          strategy.SimBookKey(u, 20, filled),
		"a limit change":  strategy.SimBookKey(u, 15, pos),
		"a new reading":   strategy.SimBookKey(moved, 20, pos),
		"a position gone": strategy.SimBookKey(u, 20, pos[:1]),
	} {
		if strategy.SimKey(false, nil, other) == strategy.SimKey(false, nil, book) {
			t.Errorf("%s must change the key", name)
		}
	}
}

// Two vacant slots that cannot be entered (one lot over the limit) ask for
// the book as it is once, not once per slot and cycle (#888 item 3).
func TestSimCache_SlotsShareTheBookSimulation(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.exch.imPerLot = 3 // one lot needs 3 of the 2 allowed: both slots skip
	f.withSecondExpiry()
	f.startRun()

	eventually(t, 2*time.Second, "both slots skipped", func() bool { return f.skips("margin_limit") >= 2 })
	time.Sleep(100 * time.Millisecond) // more cycles on the same book
	f.exch.mu.Lock()
	empty := 0
	for _, p := range f.exch.simCalls {
		if len(p) == 0 {
			empty++
		}
	}
	f.exch.mu.Unlock()
	if empty != 1 {
		t.Errorf("book simulations = %d, want 1 (same book, nothing sent)", empty)
	}
	if len(f.exch.sells()) != 0 {
		t.Error("nothing is sold over the limit")
	}
}

// Scenario: the margin guard stays — an order sent drops the cache, so the
// second slot of a cycle is sized on a fresh simulation that counts the first
// slot's resting orders, never on the result taken before them.
func TestSimCache_DroppedWhenAnOrderIsSent(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.exch.imPerLot = 0.5
	f.exch.simCountsOrders = true
	f.withSecondExpiry()
	f.startRun()

	eventually(t, 2*time.Second, "both slots entered", func() bool { return len(f.exch.sells()) == 4 })
	f.exch.mu.Lock()
	empty := 0
	for _, p := range f.exch.simCalls {
		if len(p) == 0 {
			empty++
		}
	}
	f.exch.mu.Unlock()
	if empty < 2 {
		t.Errorf("book simulations = %d, want one per slot: the first entry's orders changed the book", empty)
	}
}

// Scenario: the simulation is rate limited — no order without a margin
// result; a refusal is never cached, so the next cycle asks again and enters.
func TestSimCache_RefusedSimulationIsAskedAgain(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.5
	f.exch.simErr = tooManyRequests()
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips("margin_limit: margin data unavailable") > 0 })
	if n := len(f.exch.sells()); n != 0 {
		t.Fatalf("no margin result, no order: got %d sells", n)
	}
	f.exch.mu.Lock()
	f.exch.simErr = nil
	f.exch.mu.Unlock()
	eventually(t, 2*time.Second, "entry once Deribit answers", func() bool { return len(f.exch.sells()) == 2 })
}
