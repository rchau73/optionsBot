package tests

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// A rebalance pass not driven by a confirmed limit change (a retry, startup or
// manual pass) buys legs back only above the limit + the band (#555: stable
// BTC topped up to 20 %, then a retry pass at 20.4 % cut four legs at a loss).
// A confirmed limit change is still met exactly.

func TestDownsizeDue(t *testing.T) {
	cases := []struct {
		name               string
		im, limit, bandPct float64
		limitChanged       bool
		want               bool
	}{
		{"drift just over the limit", 20.4, 20, 1.5, false, false},
		{"at the band's edge", 21.5, 20, 1.5, false, false},
		{"just past the band", 21.6, 20, 1.5, false, true},
		{"under the limit", 19, 20, 1.5, false, false},
		{"limit change: just over is cut", 20.4, 20, 1.5, true, true},
		{"limit change: at the limit is not", 20, 20, 1.5, true, false},
		{"limit dropped 20 → 15, IM 19", 19, 15, 1.5, true, true},
		{"band off", 20.1, 20, 0, false, true},
		{"negative band is off", 20.1, 20, -1, false, true},
	}
	for _, c := range cases {
		if got := strategy.DownsizeDue(c.im, c.limit, c.bandPct, c.limitChanged); got != c.want {
			t.Errorf("%s: DownsizeDue(%g, %g, %g, %v) = %v, want %v", c.name, c.im, c.limit, c.bandPct, c.limitChanged, got, c.want)
		}
	}
}

// inBandStart boots the startup-band fixture (0.55 lots, slot target 0.1)
// with IM 20.9 % of 20 %: the startup pass leaves it alone, so the limit is
// applied with no limit change behind it.
func inBandStart(t *testing.T) *strategyFixture {
	f := startupBandFixture(t)
	f.exch.setMargin(2.09, 1)
	f.startRun()
	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })
	return f
}

// Scenario: a drift just over the limit is not cut.
func TestRebalanceDriftBand_DriftJustOverTheLimitIsNotCut(t *testing.T) {
	f := inBandStart(t)
	f.exch.setMargin(2.04, 1) // IM 20.4 % of 20 %, no limit change

	dry, err := f.strat.RequestRebalance(context.Background(), true, testActor)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	rep, err := f.strat.RequestRebalance(context.Background(), false, testActor)
	if err != nil {
		t.Fatalf("rebalance: %v", err)
	}
	if n := downsizes(f); n != 0 {
		t.Errorf("no leg is bought back on a drift inside the band, got %d", n)
	}
	for _, r := range []strategy.RebalanceReport{dry, rep} {
		kept := 0
		for _, a := range r.Actions {
			switch a.Action {
			case strategy.ActionDownsize:
				t.Errorf("dry_run=%v: no downsize in the report, got %+v", r.DryRun, a)
			case strategy.ActionKept:
				if strings.Contains(a.Detail, "drift") {
					kept++
				}
			}
		}
		if kept != 1 {
			t.Errorf("dry_run=%v: the strangle is reported kept as drift, got %+v", r.DryRun, r.Actions)
		}
	}
	recs := f.journal.riskChanges(orders.RiskRebalance)
	if len(recs) != 1 || !strings.Contains(recs[0].Detail, "drift, no leg bought back") {
		t.Errorf("the pass journals why nothing was cut, got %+v", recs)
	}
}

// Past the band a pass with no limit change still cuts to the slot target.
func TestRebalanceDriftBand_PastTheBandIsCut(t *testing.T) {
	f := inBandStart(t)
	f.exch.setMargin(2.2, 1) // IM 22 % of 20 %: past the 1.5 pp band

	if _, err := f.strat.RequestRebalance(context.Background(), false, testActor); err != nil {
		t.Fatalf("rebalance: %v", err)
	}
	if n := downsizes(f); n != 2 {
		t.Errorf("both legs cut past the band, got %d", n)
	}
}

// Scenario: a limit change is still met exactly.
func TestRebalanceDriftBand_LimitChangeIsStillMetExactly(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = 1.5
	f.cfg.IVMarginBands = []risk.Band{{MinIVPct: 30, MaxIMPct: 20}, {MinIVPct: 0, MaxIMPct: 15}}
	f.market.setIVHistory([]float64{50, 50, 50}, 50) // the 20 % band
	f.withOpenStrangle(0.55, 0.02)
	f.exch.imPerLot = 100 // any limit fits only the minimum lot (0.1)
	f.exch.setMargin(2.09, 1)
	f.startRun()
	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })

	f.exch.setMargin(1.9, 1)                             // IM 19 %
	f.market.setIVHistory([]float64{50, 50, 10, 10}, 10) // the 15 % band, confirmed on two closes
	eventually(t, 3*time.Second, "both legs cut toward the 15 % target",
		func() bool { return downsizes(f) == 2 })
	for _, b := range f.exch.buys() {
		if b.TriggerReason == orders.TriggerRebalanceDownsize && math.Abs(b.Qty-0.4) > 1e-9 {
			t.Errorf("downsize toward the 0.1 target buys back 0.4 (whole lots), got %+v", b)
		}
	}
}
