package tests

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// End-to-end margin policy: Strategy.Run against the fake exchange, whose
// simulate_portfolio is linear (imPerLot per strangle lot). The fixture's
// margin balance is 10 BTC and its DVOL history puts the limit at 20 %.

func (f *strategyFixture) skips(reasonPrefix string) int {
	n := 0
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, reasonPrefix) {
			n++
		}
	}
	return n
}

func TestMarginPolicy_EntrySizedWithDeribitSimulation(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.5 // limit 20 % × 10 = 2 BTC → 4 lots
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.4) > 1e-9 {
			t.Errorf("qty = %v, want 0.4 (4 lots fill the 20%% IM limit)", o.Qty)
		}
	}
	f.exch.mu.Lock()
	sims := append([]map[string]float64(nil), f.exch.simCalls...)
	f.exch.mu.Unlock()
	// The book as it is now, the book plus one lot, then the final size.
	if len(sims) < 3 || len(sims[0]) != 0 || sims[1][f.call] != -0.1 || sims[2][f.call] != -0.4 || sims[2][f.put] != -0.4 {
		t.Errorf("baseline, then one lot priced, then the final size confirmed: %v", sims)
	}
}

func TestMarginPolicy_NoEntryWhenOneLotExceedsTheLimit(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 3 // one lot needs 3 BTC of IM; the limit allows 2
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips("margin_limit") > 0 })
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("nothing may be sold above the IM limit, got %d sells", n)
	}
}

func TestMarginPolicy_FailSafeWhenMarginDataMissing(t *testing.T) {
	t.Run("simulation fails", func(t *testing.T) {
		f := newStrategyFixture(t)
		f.exch.simErr = errors.New("too_many_requests")
		f.startRun()
		eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips("margin_limit: margin data unavailable") > 0 })
		if n := len(f.exch.sells()); n != 0 {
			t.Errorf("no simulation, no entry: got %d sells", n)
		}
	})
	t.Run("account summary fails", func(t *testing.T) {
		f := newStrategyFixture(t)
		f.exch.summaryErr = errors.New("timeout")
		f.startRun()
		eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips("margin_unknown") > 0 })
		if n := len(f.exch.sells()); n != 0 {
			t.Errorf("no margin data, no entry: got %d sells", n)
		}
	})
}

func TestMarginPolicy_DVOLSpikeFreezesEntriesAndRebalance(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.5, 0.02)
	f.exch.imPerLot = 100 // over the limit: a rebalance would downsize...
	f.exch.setMargin(3, 1)
	f.market.setIVHistory([]float64{10, 10, 10}, 90) // ...but DVOL just jumped bands
	f.cfg.DTEDeltaMatrix = append(f.cfg.DTEDeltaMatrix, f.cfg.DTEDeltaMatrix[0])
	f.cfg.DTEDeltaMatrix[1].DTE = 60 // a vacant slot that would be entered
	f.startRun()

	eventually(t, 2*time.Second, "freeze journaled", func() bool { return len(f.journal.riskChanges(orders.RiskLimitChanged)) > 0 })
	time.Sleep(100 * time.Millisecond)
	if len(f.exch.sells()) != 0 || len(f.exch.buys()) != 0 {
		t.Errorf("while a band change is unconfirmed: no entries, no rebalance; got %d sells, %d buys",
			len(f.exch.sells()), len(f.exch.buys()))
	}
	if f.skips("risk_frozen") == 0 {
		t.Error("the frozen slot must be journaled as skipped with its reason")
	}
	rec := f.journal.riskChanges(orders.RiskLimitChanged)[0]
	if !rec.Frozen || rec.LimitIMPct != 20 || !strings.Contains(rec.Detail, "not yet confirmed") {
		t.Errorf("startup record = %+v", rec)
	}
}

func TestMarginPolicy_UnconfirmedGammaRegimeFreezesEntries(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime() // GEX wired, no regime history yet (first days after deploy)
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return f.skips("risk_frozen") > 0 })
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("an unconfirmed regime freezes entries, got %d sells", n)
	}
}

func TestMarginPolicy_ConfirmedUpshiftAddsRisk(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)                        // sized for the 20 % band
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // high band confirmed: 50 %
	f.startRun()

	eventually(t, 2*time.Second, "complement submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.1) > 1e-9 {
			t.Errorf("50 %% limit fits 2 lots: complement 0.1, got %v", o.Qty)
		}
	}
	if len(f.exch.buys()) != 0 {
		t.Error("an upshift never closes existing legs")
	}
	if len(f.journal.riskChanges(orders.RiskRebalance)) == 0 {
		t.Error("the rebalance must be journaled")
	}
}

func TestMarginPolicy_MMBreachReducesAtOnceAndBlocksNewRisk(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(1.0, 0.02)
	f.exch.setMargin(1, 4)                           // MM 40 % of margin balance > 35 %
	f.market.setIVHistory([]float64{10, 10, 10}, 90) // frozen too: MM acts regardless
	f.startRun()

	eventually(t, 2*time.Second, "both legs reduced", func() bool { return len(f.exch.buys()) >= 2 })
	f.exch.setMargin(1, 1) // back under the limit
	for _, b := range f.exch.buys()[:2] {
		// keep 1.0 × 35/40 × 0.95 = 0.83 → 0.8; buy back 0.2 at market
		if b.TriggerReason != orders.TriggerMarginMM || b.OrderType != orders.TypeMarket || math.Abs(b.Qty-0.2) > 1e-9 {
			t.Errorf("MM reduction = %+v", b)
		}
	}
	if len(f.journal.riskChanges(orders.RiskMMBreach)) == 0 {
		t.Error("the MM breach must be journaled")
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no new risk during an MM breach, got %d sells", n)
	}
}

// ── Regime history (data/regime_history.jsonl) ──────────────────────────────

func TestRegimeStore_LastValueOfEachDaySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regime.jsonl")
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s, err := history.OpenRegimes(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Record(day.Add(1*time.Hour), "POSITIVE/PINNING")
	s.Record(day.Add(2*time.Hour), "POSITIVE/PINNING") // unchanged: not written
	s.Record(day.Add(20*time.Hour), "NEGATIVE/ACCELERATION")
	s.Record(day.AddDate(0, 0, 1).Add(time.Hour), "NEGATIVE/ACCELERATION")
	s.Record(day.Add(3*time.Hour), "NEUTRAL") // older than the last day: ignored
	_ = s.Close()

	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 3 {
		t.Errorf("only new days and changes are written, got %d lines", n)
	}
	_ = os.WriteFile(path, append(data, []byte("{\"day\":\n")...), 0o644) // torn line from a crash

	s2, err := history.OpenRegimes(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	days := s2.Daily()
	if len(days) != 2 || days[0].Regime != "NEGATIVE/ACCELERATION" || !days[1].Day.Equal(day.AddDate(0, 0, 1)) {
		t.Errorf("reloaded days = %+v", days)
	}
}

// ── DVOL daily percentiles ───────────────────────────────────────────────────

func TestDVOLTracker_DailyPercentiles(t *testing.T) {
	d := marketdata.NewDVOLTracker(30)
	start := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		d.Record(start.AddDate(0, 0, i), float64(40+i)) // rising every day
	}
	closes, today := d.Daily()
	if len(closes) != marketdata.DVOLHistoryExtraDays {
		t.Fatalf("closes = %d, want %d", len(closes), marketdata.DVOLHistoryExtraDays)
	}
	last := closes[len(closes)-1]
	if last.Percentile != 100 || !last.Known || today.Percentile != 100 || !today.Known {
		t.Errorf("a rising series ranks each day at 100: last %+v today %+v", last, today)
	}
	if closes[0].Known {
		t.Errorf("a day with fewer than 20 previous days is not trusted: %+v", closes[0])
	}

	empty := marketdata.NewDVOLTracker(30)
	if c, td := empty.Daily(); c != nil || td.Known {
		t.Error("no data → nothing known")
	}
}

// ── Executor: simulate_portfolio ─────────────────────────────────────────────

func TestSimulatePortfolio_ParamsAndOnePerSecond(t *testing.T) {
	reply := fakeReply{result: `{"currency":"BTC","initial_margin":0.3,"maintenance_margin":0.2,"margin_balance":1.5,` +
		`"cross_collateral_enabled":true,"total_margin_balance_usd":150000,"total_initial_margin_usd":30000,"total_maintenance_margin_usd":21000}`}
	fc := &fakeCaller{replies: []fakeReply{reply, reply}}
	exec := orders.NewExecutor(fc)

	start := time.Now()
	sum, err := exec.SimulatePortfolio(context.Background(), "BTC", map[string]float64{"BTC-X-C": -0.1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.SimulatePortfolio(context.Background(), "BTC", nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < time.Second {
		t.Error("Deribit allows one simulate_portfolio per second: calls must be spaced")
	}
	c := fc.calls[0]
	if c.method != "private/simulate_portfolio" || c.params["currency"] != "BTC" || c.params["add_positions"] != true {
		t.Errorf("call = %+v", c)
	}
	if u := sum.MarginUsage(); u.Unit != "USD" || u.IM != 30000 || u.MarginBalance != 150000 {
		t.Errorf("cross collateral → USD totals, got %+v", u)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := exec.SimulatePortfolio(ctx, "BTC", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("waiting for the slot must respect cancellation, got %v", err)
	}
}

func TestMarginUsage_SegregatedUsesCurrencyFigures(t *testing.T) {
	u := orders.AccountSummary{Currency: "ETH", InitialMargin: 2, MaintenanceMargin: 1, MarginBalance: 10}.MarginUsage()
	if u.Unit != "ETH" || u.IMPct() != 20 || u.MMPct() != 10 {
		t.Errorf("usage = %+v", u)
	}
}

// A vacant slot in an otherwise full book is sized to its slot share
// (limit ÷ slots), not handed all the remaining headroom.
func TestMarginPolicy_EntryCappedAtSlotShare(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.withOpenStrangle(0.2, 0.02) // fills one slot at its share (2 lots)
	f.exch.imPerLot = 0.5         // slot share = 20% × 10 ÷ 2 = 1.0 → 2 lots; headroom 2.0 would allow 4
	f.withSecondExpiry()          // the vacant slot needs its own expiry
	f.startRun()

	eventually(t, 2*time.Second, "vacant slot entered", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("entry qty = %v, want 0.2 (its slot share), not 0.4 (all the headroom)", o.Qty)
		}
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("the existing strangle is at its share: no rebalance, got %d buys", n)
	}
}

// The example from the design discussion: the book offsets the new strangle,
// so each lot looks almost free (0.001 BTC of IM) although on its own it costs
// 0.5. Margin alone would allow 2,000 lots; the cap keeps it at 2× the slot's
// normal size (2.0 ÷ 0.5 = 4 lots → 8 lots).
func TestMarginPolicy_EntryCappedAtTwiceNormalSize(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MaxLegSizeMultiple = 2
	f.exch.imPerLot = 0.001
	f.exch.aloneIMPerLot = 0.5
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.8) > 1e-9 {
			t.Errorf("entry qty = %v, want 0.8 (2 × the normal 4 lots), not 200 BTC", o.Qty)
		}
	}
}

func TestMarginPolicy_NormalEntryIsNotCapped(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MaxLegSizeMultiple = 2
	f.exch.imPerLot = 0.5 // no offset: book and standalone cost agree → 4 lots
	f.startRun()
	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	if q := f.exch.sells()[0].Qty; math.Abs(q-0.4) > 1e-9 {
		t.Errorf("an ordinary entry keeps its margin-based size: %v", q)
	}
}

// Regression (2026-10-05, ETH): an order placed earlier in the cycle (a
// top-up) counts in Deribit's simulation but not in the summary read at the
// start of the cycle, so one lot looked 25× its cost and the next entry got
// 2 ETH. Two slots opening in the same cycle must get the same size.
func TestMarginPolicy_SecondEntryInACycleIsNotShrunkByTheFirst(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.exch.imPerLot = 0.5               // slot share = 20% × 10 ÷ 2 = 1.0 → 2 lots per slot
	f.exch.simCountsOrders = true       // the first entry's resting orders count in the next simulation
	call2, put2 := f.withSecondExpiry() // one expiry per slot
	f.startRun()

	eventually(t, 2*time.Second, "both slots entered", func() bool { return len(f.exch.sells()) == 4 })
	onSecond := 0
	for _, o := range f.exch.sells() {
		if o.Instrument == call2 || o.Instrument == put2 {
			onSecond++
		}
	}
	if onSecond != 2 {
		t.Errorf("the second slot must use the second expiry: %d of its legs there", onSecond)
	}
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("%s qty = %v, want 0.2 for both slots", o.Instrument, o.Qty)
		}
	}
}
