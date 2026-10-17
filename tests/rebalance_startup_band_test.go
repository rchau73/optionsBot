package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The startup rebalance leaves the book alone while IM is within
// rebalance_startup_band_pct of the limit (#123: two deploys 38 min apart cut
// and then re-sold BTC for no change in risk). Confirmed limit changes and
// the MM cut ignore the band.

func TestRebalanceInBand(t *testing.T) {
	cases := []struct {
		name               string
		im, limit, bandPct float64
		want               bool
	}{
		{"overshoot inside the band", 20.9, 20, 1.5, true},
		{"shortfall inside the band", 19.0, 20, 1.5, true},
		{"exactly at the limit", 20, 20, 1.5, true},
		{"at the upper edge", 21.5, 20, 1.5, true},
		{"just above the band", 21.6, 20, 1.5, false},
		{"just below the band", 18.4, 20, 1.5, false},
		{"real overshoot", 25, 20, 1.5, false},
		{"empty book", 0, 20, 1.5, false},
		{"band off", 20.1, 20, 0, false},
		{"negative band is off", 20, 20, -1, false},
	}
	for _, c := range cases {
		if got := strategy.RebalanceInBand(c.im, c.limit, c.bandPct); got != c.want {
			t.Errorf("%s: RebalanceInBand(%g, %g, %g) = %v, want %v", c.name, c.im, c.limit, c.bandPct, got, c.want)
		}
	}
}

// startupBandFixture loads a 0.55-lot strangle whose slot target is 0.1 (so
// any pass without the band would cut it) with the shipped band.
func startupBandFixture(t *testing.T) *strategyFixture {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.withOpenStrangle(0.55, 0.02)
	f.exch.imPerLot = 100 // the 20 % limit fits only the minimum lot (0.1)
	return f
}

func downsizes(f *strategyFixture) int {
	n := 0
	for _, b := range f.exch.buys() {
		if b.TriggerReason == orders.TriggerRebalanceDownsize {
			n++
		}
	}
	return n
}

// Scenario: a restart inside the band does not trade.
func TestRebalanceStartupBand_RestartInsideTheBandDoesNotTrade(t *testing.T) {
	f := startupBandFixture(t)
	f.exch.setMargin(2.09, 1) // IM 20.9 % of the margin balance, limit 20 %
	f.startRun()

	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })
	time.Sleep(100 * time.Millisecond) // many cycles: still nothing
	if n := downsizes(f); n != 0 {
		t.Errorf("no rebalance_downsize inside the band, got %d", n)
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalance)); n != 0 {
		t.Errorf("no rebalance pass inside the band, got %d", n)
	}
	rec := f.journal.riskChanges(orders.RiskRebalanceInBand)
	if len(rec) != 1 {
		t.Fatalf("the in-band startup is journaled once, got %d", len(rec))
	}
	if r := rec[0]; r.LimitIMPct != 20 || math.Abs(r.IMPct-20.9) > 1e-9 || r.Detail == "" {
		t.Errorf("in-band record = %+v", r)
	}
	if p := f.position(f.call); p == nil || math.Abs(p.Qty-0.55) > 1e-9 {
		t.Errorf("the call keeps its 0.55, got %+v", p)
	}
}

// Scenario: a real overshoot is still cut.
func TestRebalanceStartupBand_RealOvershootIsStillCut(t *testing.T) {
	f := startupBandFixture(t)
	f.exch.setMargin(2.5, 1) // IM 25 % of the margin balance, limit 20 %
	f.startRun()

	eventually(t, 2*time.Second, "both legs downsized", func() bool { return downsizes(f) == 2 })
	for _, b := range f.exch.buys() {
		if math.Abs(b.Qty-0.4) > 1e-9 {
			t.Errorf("downsize toward the 0.1 target buys back 0.4, got %+v", b)
		}
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalanceInBand)); n != 0 {
		t.Errorf("an overshoot is not in the band, got %d in-band records", n)
	}
}

// Scenario: the MM limit is untouched.
func TestRebalanceStartupBand_MMLimitIsUntouched(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.withOpenStrangle(1.0, 0.02)
	f.exch.setMargin(2.0, 4) // IM 20 %: in the band; MM 40 % > 35 %
	f.startRun()

	eventually(t, 2*time.Second, "both legs reduced at market", func() bool { return len(f.exch.buys()) >= 2 })
	f.exch.setMargin(1, 1)
	for _, b := range f.exch.buys()[:2] {
		if b.TriggerReason != orders.TriggerMarginMM || b.OrderType != orders.TypeMarket {
			t.Errorf("MM cut must run at market whatever the band, got %+v", b)
		}
	}
}

// A confirmed limit change after an in-band startup gets its pass, even with
// IM within the band of the new limit: only the startup pass has a band.
func TestRebalanceStartupBand_ConfirmedLimitChangeIgnoresTheBand(t *testing.T) {
	f := startupBandFixture(t)
	f.exch.setMargin(2.09, 1)
	f.startRun()

	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })
	f.exch.setMargin(4.95, 1)                            // IM 49.5 %: within 1.5 pp of the new limit
	f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // the 50 % band, confirmed
	eventually(t, 3*time.Second, "a rebalance pass toward the new limit",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalance)) > 0 })
	if n := len(f.journal.riskChanges(orders.RiskRebalanceInBand)); n != 1 {
		t.Errorf("only the startup pass has a band, got %d in-band records", n)
	}
}
