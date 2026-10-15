package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// A rebalance complement that times out short of its size re-arms the
// rebalance: without it the book would stay under the IM limit until the next
// confirmed limit change or a restart (seen on testnet: IM 9.6 % vs 20 % for
// hours, no orders working).

// upshiftToTwoLots loads a one-lot strangle and confirms the 50 % band, which
// fits two lots: the startup rebalance sends a 0.1 complement.
func (f *strategyFixture) upshiftToTwoLots() {
	f.withOpenStrangle(0.1, 0.02)
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80)
	f.cfg.OrderFillTimeoutSec = 0 // complements time out on the next cycle
}

func TestRebalanceRetry_UnfilledComplementIsRetried(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 0
	f.startRun()

	eventually(t, 3*time.Second, "complement sent again after the first timed out",
		func() bool { return len(f.exch.sells()) >= 4 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.1) > 1e-9 {
			t.Errorf("every complement is the missing lot (0.1), got %v", o.Qty)
		}
	}
	if len(f.journal.riskChanges(orders.RiskRebalanceRetry)) == 0 {
		t.Error("the retry must be journaled")
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("an under-filled upsize never closes legs, got %d buys", n)
	}
}

func TestRebalanceRetry_WaitsForTheCooldown(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 60
	f.startRun()

	eventually(t, 3*time.Second, "retry scheduled after the timeout",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	time.Sleep(200 * time.Millisecond) // ~20 cycles
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("no new complement inside the cooldown: want 2 sells, got %d", n)
	}
}

// A complement that filled in part leaves a second strangle in the slot. The
// retry sizes the slot as a whole: it sends only what is still missing, not a
// full complement for each strangle.
func TestRebalanceRetry_PartialFillSendsOnlyTheRest(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 2.0 / 3 * 0.999 // slot share 2.0 fits 3 lots: complement 0.2
	f.cfg.OrderFillTimeoutSec = 0
	f.cfg.RebalanceRetryMinutes = 0
	sold := 0
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.01}
		}
		sold++
		if sold <= 2 { // the first complement fills 0.1 of 0.2 on each leg
			return orders.Fill{Qty: 0.1, FillPrice: 0.02}
		}
		return orders.Fill{}
	}
	f.startRun()

	eventually(t, 3*time.Second, "retry sent", func() bool { return len(f.exch.sells()) >= 4 })
	sells := f.exch.sells()
	for i, o := range sells {
		want := 0.1 // slot holds 0.2 of 0.3: one lot missing
		if i < 2 {
			want = 0.2
		}
		if math.Abs(o.Qty-want) > 1e-9 {
			t.Errorf("sell %d (%s) qty = %v, want %v", i, o.Instrument, o.Qty, want)
		}
	}
	if len(f.state.AllStrangles()) != 2 {
		t.Errorf("the filled part is its own strangle in the slot, got %d strangles", len(f.state.AllStrangles()))
	}
}

// Back-off (#85): on a book where nobody trades at mid the same complement was
// re-sent every 15 minutes for nothing (2026-10-14 BTC: 12 orders in 1.5 h,
// zero fills). From the 3rd unfilled pass in a row the wait doubles (#93: passes, not
// complements — rebalance_pass_test.go).

func TestRebalanceRetryWait(t *testing.T) {
	base, max := 15*time.Minute, 60*time.Minute
	cases := []struct {
		name     string
		unfilled int
		after    int
		max      time.Duration
		want     time.Duration
	}{
		{"no unfilled pass", 0, 3, max, 15 * time.Minute},
		{"one unfilled", 1, 3, max, 15 * time.Minute},
		{"two unfilled", 2, 3, max, 15 * time.Minute},
		{"three unfilled: doubled", 3, 3, max, 30 * time.Minute},
		{"four unfilled: doubled again", 4, 3, max, 60 * time.Minute},
		{"many unfilled: capped", 20, 3, max, 60 * time.Minute},
		{"back-off off", 10, 0, max, 15 * time.Minute},
		{"cap below base never shortens the wait", 5, 3, time.Minute, 15 * time.Minute},
	}
	for _, c := range cases {
		if got := strategy.RebalanceRetryWait(base, c.max, c.unfilled, c.after); got != c.want {
			t.Errorf("%s: RebalanceRetryWait(%d unfilled, after %d) = %s, want %s", c.name, c.unfilled, c.after, got, c.want)
		}
	}
}

// Scenario (#85): repeated unfilled passes — the back-off doubles the wait
// and a single rebalance_retry line records the count.
func TestRebalanceRetry_RepeatedUnfilledComplementsBackOff(t *testing.T) {
	if got := strategy.RebalanceRetryWait(15*time.Minute, 60*time.Minute, 3, 3); got != 30*time.Minute {
		t.Fatalf("3 unfilled in a row: wait %s, want 30m", got)
	}

	// End to end with the back-off from the first unfilled complement, so the
	// test does not wait out real retries.
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 15
	f.cfg.RebalanceBackoffAfter = 1
	f.cfg.RebalanceRetryMaxMinutes = 60
	f.startRun()

	eventually(t, 3*time.Second, "retry scheduled after the timeout",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	time.Sleep(200 * time.Millisecond) // ~20 cycles
	retries := f.journal.riskChanges(orders.RiskRebalanceRetry)
	if len(retries) != 1 {
		t.Fatalf("one rebalance_retry line per unfilled pass, got %d", len(retries))
	}
	if d := retries[0].Detail; !strings.Contains(d, "1 passes in a row unfilled") || !strings.Contains(d, "backed off to 30m0s") {
		t.Errorf("retry line must carry the count and the backed-off wait, got %q", d)
	}
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("no new complement inside the backed-off wait: want 2 sells, got %d", n)
	}
}

// A fill resets the count: the book does trade at our price.
func TestRebalanceRetry_FillResetsBackoff(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot = 2.0 / 3 * 0.999 // slot share fits 3 lots: complement 0.2
	f.cfg.OrderFillTimeoutSec = 0
	f.cfg.RebalanceRetryMinutes = 15
	f.cfg.RebalanceBackoffAfter = 1
	f.cfg.RebalanceRetryMaxMinutes = 60
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionSell {
			return orders.Fill{Qty: 0.1, FillPrice: 0.02} // half of each leg fills
		}
		return orders.Fill{Qty: o.Qty, FillPrice: 0.01}
	}
	f.startRun()

	eventually(t, 3*time.Second, "retry scheduled after the partial fill",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	if d := f.journal.riskChanges(orders.RiskRebalanceRetry)[0].Detail; !strings.Contains(d, "retry in 15m0s") {
		t.Errorf("a complement that filled something retries at the base wait, got %q", d)
	}
}

// Scenario: risk reductions are untouched — given an MM breach, when it fires
// during a back-off, then it executes at once.
func TestRebalanceRetry_MMBreachDuringBackoffExecutesAtOnce(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 15
	f.cfg.RebalanceBackoffAfter = 1
	f.cfg.RebalanceRetryMaxMinutes = 60
	f.startRun()

	eventually(t, 3*time.Second, "backed-off retry scheduled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	f.exch.setMargin(4, 4) // MM 40 % of the margin balance, above the 35 % limit
	eventually(t, 3*time.Second, "MM cut bought back at once despite the back-off",
		func() bool { return len(f.exch.buys()) > 0 })
}

// A confirmed limit change resets the back-off: the new limit gets a pass at
// once, not after the backed-off wait.
func TestRebalanceRetry_LimitChangeResetsBackoff(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.RebalanceRetryMinutes = 15
	f.cfg.RebalanceBackoffAfter = 1
	f.cfg.RebalanceRetryMaxMinutes = 60
	f.startRun()

	eventually(t, 3*time.Second, "backed-off retry scheduled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceRetry)) > 0 })
	passes := len(f.journal.riskChanges(orders.RiskRebalance))
	f.market.setIVHistory([]float64{80, 80, 10, 10}, 10) // back to the 20 % band, confirmed
	eventually(t, 3*time.Second, "rebalance toward the new limit despite the back-off",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalance)) > passes })
}
