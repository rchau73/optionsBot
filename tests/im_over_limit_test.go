package tests

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The IM-over-limit alert (#166, option B): when IM sits over the limit with
// no rebalance due — a transfer out, a price move — the bot journals one
// im_over_limit line per episode and shows it in /api/status. It sends
// nothing: the rebalance still runs only on a confirmed limit change, at
// startup or by the monitor's button.

func TestIMOverLimitNext(t *testing.T) {
	cases := []struct {
		name               string
		open               bool
		im, limit, bandPct float64
		want               bool
	}{
		{"inside the band is not an episode", false, 21.4, 20, 1.5, false},
		{"at the band edge is not an episode", false, 21.5, 20, 1.5, false},
		{"beyond the band opens", false, 26, 20, 1.5, true},
		{"under the limit stays closed", false, 18, 20, 1.5, false},
		{"open stays open inside the band", true, 21, 20, 1.5, true},
		{"open stays open just over the limit", true, 20.01, 20, 1.5, true},
		{"open clears at the limit", true, 20, 20, 1.5, false},
		{"open clears under the limit", true, 19, 20, 1.5, false},
		{"band off: any overshoot opens", false, 20.1, 20, 0, true},
		{"negative band counts as none", false, 20.1, 20, -1, true},
	}
	for _, c := range cases {
		if got := strategy.IMOverLimitNext(c.open, c.im, c.limit, c.bandPct); got != c.want {
			t.Errorf("%s: IMOverLimitNext(%v, %g, %g, %g) = %v, want %v", c.name, c.open, c.im, c.limit, c.bandPct, got, c.want)
		}
	}
}

func TestIMOverLimitDetail(t *testing.T) {
	u := risk.Usage{IM: 1.8, MarginBalance: 1.8 / 26 * 100, Unit: "BTC"}
	got := strategy.IMOverLimitDetail(u, 20, 1.5, 10)
	for _, want := range []string{"IM 26.0%", "limit 20%", "6.923 BTC now", "10 BTC at the last rebalance", "-30.8%", "Rebalance dry run"} {
		if !strings.Contains(got, want) {
			t.Errorf("detail %q misses %q", got, want)
		}
	}
	if got := strategy.IMOverLimitDetail(u, 20, 1.5, 0); !strings.Contains(got, "no rebalance since startup") {
		t.Errorf("without a reference balance the detail says so: %q", got)
	}
}

// imAlertFixture starts a bot whose one slot holds a strangle at IM 19 % of
// its 20 % limit, margin balance 10 BTC: inside the startup band, so the
// book is left alone (the scenario's 18 % would start with a top-up pass).
func imAlertFixture(t *testing.T) *strategyFixture {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.withOpenStrangle(0.5, 0.02)
	f.exch.setMargin(1.9, 0.5)
	f.startRun()
	eventually(t, 2*time.Second, "in-band startup journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalanceInBand)) == 1 })
	return f
}

func (f *fakeExchange) setMarginBalance(b float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary.MarginBalance = b
}

// Scenario: a transfer out puts IM over the limit.
func TestIMOverLimit_TransferOutPutsIMOverTheLimit(t *testing.T) {
	f := imAlertFixture(t)
	h := api.New(f.strat, orders.NewWriterLogger(io.Discard, 0.05)).Handler()
	f.exch.setMarginBalance(1.9 / 0.26) // IM 26 % of the smaller balance

	eventually(t, 2*time.Second, "im_over_limit journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMOverLimit)) == 1 })
	r := f.journal.riskChanges(orders.RiskIMOverLimit)[0]
	if math.Abs(r.IMPct-26) > 1e-9 || r.LimitIMPct != 20 {
		t.Errorf("record IM %v limit %v, want 26 and 20", r.IMPct, r.LimitIMPct)
	}
	if math.Abs(r.MarginBalance-1.9/0.26) > 1e-9 || r.RefMarginBalance != 10 {
		t.Errorf("record balances now %v, ref %v; want %v and 10", r.MarginBalance, r.RefMarginBalance, 1.9/0.26)
	}
	if !strings.Contains(r.Detail, "Rebalance dry run") {
		t.Errorf("detail must point to the dry run: %q", r.Detail)
	}
	time.Sleep(100 * time.Millisecond) // many cycles over the limit
	if n, m := len(f.exch.sells()), len(f.exch.buys()); n != 0 || m != 0 {
		t.Errorf("no order is sent by the alert: %d sells, %d buys", n, m)
	}
	_, status := getJSON(t, h, "/api/status")
	rv, _ := status["risk"].(map[string]any)
	if rv["im_over_limit"] != true || rv["im_over_limit_since"] == nil {
		t.Errorf("/api/status risk = %v, want im_over_limit true with its start", rv)
	}
}

// Scenario: no repeat while it lasts.
func TestIMOverLimit_NoRepeatWhileItLasts(t *testing.T) {
	f := imAlertFixture(t)
	f.exch.setMarginBalance(1.9 / 0.26)
	eventually(t, 2*time.Second, "im_over_limit journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMOverLimit)) == 1 })
	f.exch.setMarginBalance(1.9 / 0.21) // 21 %: inside the band, still over the limit
	time.Sleep(150 * time.Millisecond)  // well over 100 cycles at the fixture's interval
	if n := len(f.journal.riskChanges(orders.RiskIMOverLimit)); n != 1 {
		t.Errorf("one im_over_limit per episode, got %d", n)
	}
	if n := len(f.journal.riskChanges(orders.RiskIMOverLimitDone)); n != 0 {
		t.Errorf("still over the limit: not cleared, got %d", n)
	}
}

// Scenario: inside the band is not an episode.
func TestIMOverLimit_InsideTheBandIsNotAnEpisode(t *testing.T) {
	f := imAlertFixture(t)
	f.exch.setMarginBalance(1.9 / 0.214) // IM 21.4 % of a 20 % limit
	time.Sleep(150 * time.Millisecond)
	if n := len(f.journal.riskChanges(orders.RiskIMOverLimit)); n != 0 {
		t.Errorf("inside the band nothing is journaled, got %d", n)
	}
	if f.strat.View().Risk.IMOverLimit {
		t.Error("inside the band the view shows no episode")
	}
}

// Scenario: it clears.
func TestIMOverLimit_ItClears(t *testing.T) {
	f := imAlertFixture(t)
	f.exch.setMarginBalance(1.9 / 0.26)
	eventually(t, 2*time.Second, "episode open in the view", func() bool { return f.strat.View().Risk.IMOverLimit })
	f.exch.setMarginBalance(1.9 / 0.19) // a deposit: IM 19 %
	eventually(t, 2*time.Second, "im_over_limit_cleared journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMOverLimitDone)) == 1 })
	if rv := f.strat.View().Risk; rv.IMOverLimit || rv.IMOverLimitSince != nil {
		t.Errorf("cleared episode still in the view: %+v", rv)
	}
	// A new crossing is a new episode.
	f.exch.setMarginBalance(1.9 / 0.26)
	eventually(t, 2*time.Second, "second episode journaled",
		func() bool { return len(f.journal.riskChanges(orders.RiskIMOverLimit)) == 2 })
	if n := len(f.exch.sells()) + len(f.exch.buys()); n != 0 {
		t.Errorf("no order is sent by the alert, got %d", n)
	}
}

// Scenario: the protections stay as they are.
func TestIMOverLimit_ProtectionsStayAsTheyAre(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.withOpenStrangle(1.0, 0.02)
	f.exch.setMargin(2.6, 3.6) // IM 26 % of a 20 % limit, MM 36 % of a 35 % limit
	f.startRun()

	eventually(t, 2*time.Second, "both legs reduced at market", func() bool { return len(f.exch.buys()) >= 2 })
	f.exch.setMargin(1, 1)
	for _, b := range f.exch.buys()[:2] {
		if b.TriggerReason != orders.TriggerMarginMM || b.OrderType != orders.TypeMarket {
			t.Errorf("the MM cut runs at market as today, got %+v", b)
		}
	}
	if len(f.journal.riskChanges(orders.RiskMMBreach)) == 0 {
		t.Error("the MM breach is journaled as today")
	}
}
