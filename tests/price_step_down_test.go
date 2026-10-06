package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/strategy"
)

func TestStepDownPrice(t *testing.T) {
	const timeout = 90 * time.Second
	cases := []struct {
		name          string
		bid, ask, tic float64
		age           time.Duration
		floor         string
		want          float64
	}{
		{"first third: ask", 0.0125, 0.0135, 0.0005, 10 * time.Second, strategy.PriceFloorBid, 0.0135},
		{"second third: mid", 0.0125, 0.0135, 0.0005, 40 * time.Second, strategy.PriceFloorBid, 0.013},
		{"last third, bid floor: bid", 0.0125, 0.0135, 0.0005, 70 * time.Second, strategy.PriceFloorBid, 0.0125},
		{"last third, mid floor: stays at mid", 0.0125, 0.0135, 0.0005, 70 * time.Second, strategy.PriceFloorMid, 0.013},
		{"ask floor never moves", 0.0125, 0.0135, 0.0005, 70 * time.Second, strategy.PriceFloorAsk, 0.0135},
		{"one-tick spread: mid rounds up to the ask", 0.0125, 0.013, 0.0005, 40 * time.Second, strategy.PriceFloorMid, 0.013},
		{"no bid (testnet): stays at the ask", 0, 0.0135, 0.0005, 70 * time.Second, strategy.PriceFloorBid, 0.0135},
		{"crossed quote: stays at the ask", 0.014, 0.0135, 0.0005, 70 * time.Second, strategy.PriceFloorBid, 0.0135},
		{"no ask: no price", 0.0125, 0, 0.0005, 70 * time.Second, strategy.PriceFloorBid, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strategy.StepDownPrice(tc.bid, tc.ask, tc.tic, tc.age, timeout, tc.floor)
			if diff := got - tc.want; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("StepDownPrice = %v, want %v", got, tc.want)
			}
		})
	}
}

// amendedPrices returns the prices of every amend sent to the exchange.
func (f *fakeExchange) amendedPrices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.amended {
		out = append(out, a[strings.Index(a, "@")+1:])
	}
	return out
}

// The fixture quotes bid 0.019 / ask 0.021 (mid 0.02). With a 1 s timeout the
// ladder steps at ~0.33 s and ~0.67 s, before the cancel.
func TestStepDown_RepairGoesDownToTheBid(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.cfg.OrderFillTimeoutSec = 1
	f.cfg.RepairPriceFloor = strategy.PriceFloorBid
	f.startRun()

	eventually(t, 2*time.Second, "repair stepped to the bid", func() bool {
		p := f.exch.amendedPrices()
		return len(p) == 2
	})
	if got := strings.Join(f.exch.amendedPrices(), ","); got != "0.02,0.019" {
		t.Errorf("repair steps = %s, want 0.02,0.019 (mid, then bid)", got)
	}
}

func TestStepDown_EntryStopsAtMid(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 1
	f.cfg.EntryPriceFloor = strategy.PriceFloorMid
	f.startRun()

	eventually(t, 2*time.Second, "entry timed out", func() bool { return len(f.exch.sells()) >= 4 })
	for _, p := range f.exch.amendedPrices() {
		if p != "0.02" {
			t.Errorf("an entry steps to mid (0.02) and no lower, got amend at %s", p)
		}
	}
	if len(f.exch.amendedPrices()) == 0 {
		t.Error("the entry must step down to mid before the timeout")
	}
}

// The premium floor is checked at the lowest price the ladder can reach: a
// repair that could only fill under min_premium_btc is not sent.
func TestStepDown_PremiumFloorCheckedAtTheFloorPrice(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.cfg.RepairPriceFloor = strategy.PriceFloorBid
	f.cfg.MinPremiumBTC = 0.0195 // above the bid (0.019), below the ask (0.021)
	f.startRun()

	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("the bid is under the premium floor: no repair, got %d sells", n)
	}
}
