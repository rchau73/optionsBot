package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The rebalance back-off (#85) counts passes whose complements all ran to
// their fill timeout unfilled — not complements, and not the bot's own
// cancels (#93). A single pass over several slots, or a freeze cancelling
// working top-ups, slowed top-ups to 30–60 min although the market never
// refused our price three times in a row.

// fixtureInstrument is a quoted option of the fixture's market.
func fixtureInstrument(name, typ string, expiry time.Time, strike, delta float64) *marketdata.Instrument {
	return &marketdata.Instrument{
		Name: name, Underlying: "BTC", Strike: strike, Expiry: expiry, OptionType: typ,
		TickSize: 0.0001, MinTradeAmount: 0.1,
		Bid: 0.019, Ask: 0.021, Mid: 0.02, UnderlyingPrice: 100000,
		Greeks: marketdata.Greeks{Delta: delta}, UpdatedAt: time.Now(),
	}
}

// upshiftThreeSlots loads one-lot strangles in three slots (45, 35 and 25
// DTE, each on its own expiry) sized for the 20 % band, then confirms the
// 50 % band: the startup pass sends one complement per slot, three in all.
func (f *strategyFixture) upshiftThreeSlots() {
	f.cfg.DTEDeltaMatrix = nil
	f.market.instruments = map[string]*marketdata.Instrument{}
	f.exch.positions = nil
	for _, dte := range []int{45, 35, 25} {
		f.cfg.DTEDeltaMatrix = append(f.cfg.DTEDeltaMatrix, config.DTEDeltaEntry{DTE: dte, Deltas: []float64{0.16}})
		expiry := time.Now().UTC().AddDate(0, 0, dte).Truncate(24 * time.Hour).Add(8 * time.Hour)
		label := strings.ToUpper(expiry.Format("2Jan06"))
		call, put := fmt.Sprintf(testCall, label), fmt.Sprintf(testPut, label)
		f.market.instruments[call] = fixtureInstrument(call, "call", expiry, 110000, 0.16)
		f.market.instruments[put] = fixtureInstrument(put, "put", expiry, 90000, -0.16)
		f.exch.positions = append(f.exch.positions,
			orders.RawPosition{InstrumentName: call, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.16},
			orders.RawPosition{InstrumentName: put, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.16})
	}
	share := 20.0 / 100 * f.exch.summary.MarginBalance / 3
	f.exch.imPerLot = share * 0.999 // one lot per slot at 20 %, two at 50 %
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80)
	f.cfg.OrderFillTimeoutSec = 0 // complements time out on the next cycle
}

func (f *strategyFixture) backoffConfig(after int) {
	f.cfg.RebalanceRetryMinutes = 15
	f.cfg.RebalanceBackoffAfter = after
	f.cfg.RebalanceRetryMaxMinutes = 60
}

// lastRetry is the detail of the latest rebalance_retry line ("" if none).
func (f *strategyFixture) lastRetry() string {
	r := f.journal.riskChanges(orders.RiskRebalanceRetry)
	if len(r) == 0 {
		return ""
	}
	return r[len(r)-1].Detail
}

func (f *strategyFixture) cancelsWith(trigger string) int {
	n := 0
	for _, e := range f.journal.events(orders.EventCancelled) {
		if e.trigger == trigger {
			n++
		}
	}
	return n
}

// Scenario: one pass with several unfilled complements is one try.
func TestRebalancePass_SeveralUnfilledComplementsAreOneTry(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftThreeSlots()
	f.backoffConfig(3)
	f.startRun()

	eventually(t, 3*time.Second, "the pass's three complements time out and re-arm",
		func() bool { return len(f.exch.sells()) == 6 && f.lastRetry() != "" })
	time.Sleep(100 * time.Millisecond) // ~10 cycles: no other line may follow
	retries := f.journal.riskChanges(orders.RiskRebalanceRetry)
	if len(retries) != 1 {
		t.Fatalf("one rebalance_retry line per pass, got %d: %+v", len(retries), retries)
	}
	d := retries[0].Detail
	if !strings.Contains(d, "3 complement(s) filled short") || !strings.Contains(d, "retry in 15m0s") || strings.Contains(d, "backed off") {
		t.Errorf("the 1st unfilled pass retries at the base wait (15m), got %q", d)
	}
	if n := len(f.exch.sells()); n != 6 {
		t.Errorf("no new complement inside the wait: want 6 sells, got %d", n)
	}
}

// Scenario: three unfilled passes in a row back off. The count is the pure
// NextUnfilledPasses; the end-to-end wording of the backed-off line is
// checked by TestRebalanceRetry_RepeatedUnfilledComplementsBackOff (a real
// 3rd pass would take 30 min of wall clock).
func TestRebalancePass_ThreeUnfilledPassesInARowBackOff(t *testing.T) {
	count := 0
	for pass := 1; pass <= 3; pass++ {
		count = strategy.NextUnfilledPasses(count, 3, 3, false) // 3 complements, all timed out unfilled
		if pass < 3 {
			if w := strategy.RebalanceRetryWait(15*time.Minute, 60*time.Minute, count, 3); w != 15*time.Minute {
				t.Errorf("unfilled pass %d: wait %s, want 15m", pass, w)
			}
		}
	}
	if count != 3 {
		t.Fatalf("three unfilled passes count 3, got %d", count)
	}
	if w := strategy.RebalanceRetryWait(15*time.Minute, 60*time.Minute, count, 3); w != 30*time.Minute {
		t.Errorf("3rd unfilled pass: wait %s, want 30m", w)
	}
	if w := strategy.RebalanceRetryWait(15*time.Minute, 60*time.Minute, strategy.NextUnfilledPasses(count, 1, 1, false), 3); w != 60*time.Minute {
		t.Errorf("4th unfilled pass: wait %s, want 60m", w)
	}
}

func TestNextUnfilledPasses(t *testing.T) {
	cases := []struct {
		name                     string
		count, finished, timeout int
		filled                   bool
		want                     int
	}{
		{"every complement timed out unfilled", 2, 3, 3, false, 3},
		{"a fill resets", 5, 3, 3, true, 0},
		{"a fill in a cancelled complement resets too", 5, 2, 1, true, 0},
		{"one complement cancelled by the bot: unchanged", 2, 3, 2, false, 2},
		{"all cancelled by the bot: unchanged", 2, 3, 0, false, 2},
		{"nothing finished: unchanged", 2, 0, 0, false, 2},
	}
	for _, c := range cases {
		if got := strategy.NextUnfilledPasses(c.count, c.finished, c.timeout, c.filled); got != c.want {
			t.Errorf("%s: NextUnfilledPasses = %d, want %d", c.name, got, c.want)
		}
	}
}

// Scenario: our own cancels are not market refusals — a freeze. With the
// back-off from the 1st unfilled pass, a counted cancel would retry at 30m.
func TestRebalancePass_FreezeCancelLeavesCountAlone(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.OrderFillTimeoutSec = 60 // the complement rests
	f.backoffConfig(1)
	f.startRun()

	eventually(t, 3*time.Second, "complement resting", func() bool { return len(f.exch.sells()) == 2 })
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 10) // band change, not yet confirmed: freeze
	eventually(t, 3*time.Second, "freeze cancels the complement and re-arms",
		func() bool { return f.cancelsWith(orders.TriggerRiskFrozen) > 0 && f.lastRetry() != "" })
	if d := f.lastRetry(); !strings.Contains(d, "retry in 15m0s") || strings.Contains(d, "backed off") {
		t.Errorf("a freeze cancel is not an unfilled pass: want the base wait, got %q", d)
	}
}

// Scenario: our own cancels are not market refusals — a GEX shed.
func TestRebalancePass_ShedCancelLeavesCountAlone(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.OrderFillTimeoutSec = 60
	f.backoffConfig(1)
	f.withPutSheddingRegime()                 // bear trend from the daily closes
	f.withConfirmedRegime("POSITIVE/PINNING") // top-ups allowed
	g := &switchGEX{snap: &gex.Snapshot{Regime: "POSITIVE/PINNING", Spot: 100000, GammaFlip: 95000, GammaFlipFound: true}}
	f.gex = g
	f.startRun()

	eventually(t, 3*time.Second, "complement resting", func() bool { return len(f.exch.sells()) == 2 })
	g.set(&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true})
	eventually(t, 3*time.Second, "shed cancels the complement and re-arms",
		func() bool { return f.cancelsWith(orders.TriggerGammaClose) > 0 && f.lastRetry() != "" })
	if d := f.lastRetry(); !strings.Contains(d, "retry in 15m0s") || strings.Contains(d, "backed off") {
		t.Errorf("a shed cancel is not an unfilled pass: want the base wait, got %q", d)
	}
}

// Scenario: our own cancels are not market refusals — the kill switch
// journals no rebalance_retry.
func TestRebalancePass_KillSwitchJournalsNoRetry(t *testing.T) {
	f := newStrategyFixture(t)
	f.upshiftToTwoLots()
	f.cfg.OrderFillTimeoutSec = 60
	f.backoffConfig(1)
	f.startRun()

	eventually(t, 3*time.Second, "complement resting", func() bool { return len(f.exch.sells()) == 2 })
	f.strat.KillSwitch()
	eventually(t, 3*time.Second, "book flattened", func() bool { return len(f.state.AllPositions()) == 0 })
	if n := f.cancelsWith(orders.TriggerKillSwitch); n == 0 {
		t.Error("the working complement is settled by the kill switch")
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalanceRetry)); n != 0 {
		t.Errorf("a halted bot never re-arms the rebalance, got %d rebalance_retry lines: %q", n, f.lastRetry())
	}
}

// Scenario: a held top-up waits the base time. A first pass times out
// unfilled (back-off from 1: count 1, retry 30m); a stop-loss on the slot's
// second strangle then holds the next top-up (slotStopHold), which re-arms
// at 15m.
func TestRebalancePass_HeldTopUpWaitsTheBaseTime(t *testing.T) {
	f := newStrategyFixture(t)
	f.backoffConfig(1)
	f.cfg.RepairCooldownHours = 72
	call2 := strings.Replace(f.call, "110000", "115000", 1)
	put2 := strings.Replace(f.put, "90000", "85000", 1)
	f.market.instruments[call2] = fixtureInstrument(call2, "call", f.expiry, 115000, 0.12)
	f.market.instruments[put2] = fixtureInstrument(put2, "put", f.expiry, 85000, -0.12)
	f.withOpenStrangle(0.2, 0.02) // imPerLot fits the slot's 0.2 at 20 %
	f.exch.positions = []orders.RawPosition{
		{InstrumentName: f.call, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.16},
		{InstrumentName: f.put, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.16},
		{InstrumentName: call2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: 0.12},
		{InstrumentName: put2, Size: -0.1, Direction: "sell", AveragePrice: 0.02, MarkPrice: 0.02, Delta: -0.12},
	}
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80)
	f.cfg.OrderFillTimeoutSec = 0
	f.startRun()

	eventually(t, 3*time.Second, "first pass unfilled: backed-off retry",
		func() bool { return strings.Contains(f.lastRetry(), "backed off to 30m0s") })

	f.market.setQuote(put2, 0.079, 0.081) // mark 4× the premium: stop-loss
	eventually(t, 3*time.Second, "put stopped out", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == put2 {
				return true
			}
		}
		return false
	})

	rep, err := f.strat.RequestRebalance(context.Background(), false, testActor)
	if err != nil {
		t.Fatalf("manual rebalance: %v", err)
	}
	held := false
	for _, a := range rep.Actions {
		held = held || a.Action == strategy.ActionHeld
	}
	if !held {
		t.Fatalf("the top-up must be held by the stop, got %+v", rep.Actions)
	}
	d := f.lastRetry()
	if !strings.Contains(d, "held") || !strings.Contains(d, "retry in 15m0s") || strings.Contains(d, "backed off") {
		t.Errorf("a held top-up re-arms at the base wait, got %q", d)
	}
}
