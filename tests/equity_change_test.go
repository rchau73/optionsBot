package tests

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"optionsbot/internal/orders"
)

// #124: about 127 ETH left the testnet account by hand (sold and moved to
// another environment). The bot never moves funds and journals no transfer;
// these tests pin what it does when the margin balance changes under it.

// withdraw lowers the margin balance as a transfer out would.
func (f *fakeExchange) withdraw(amount float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary.MarginBalance -= amount
	f.summary.Equity -= amount
	f.summary.AvailableFunds -= amount
}

// Scenario: the bot sizes on what the exchange reports — a withdrawal shrinks
// the next entry. The balance is read from Deribit every cycle, never cached.
func TestEquityChange_EntrySizedOnTheBalanceAfterAWithdrawal(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.imPerLot = 0.5 // at 10 BTC: limit 20 % × 10 = 2 BTC → 4 lots
	f.exch.summaryErr = errors.New("timeout")
	f.startRun()
	eventually(t, 2*time.Second, "entry held on missing margin", func() bool { return f.skips("margin_unknown") > 0 })

	f.exch.withdraw(5) // 5 BTC left: limit 1 BTC → 2 lots
	f.exch.mu.Lock()
	f.exch.summaryErr = nil
	f.exch.mu.Unlock()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("%s qty = %v, want 0.2 (2 lots fill 20 %% of the 5 BTC left)", o.Instrument, o.Qty)
		}
	}
}

// Scenario: a withdrawal that leaves IM under its limit changes nothing —
// no buy-back, no top-up (2026-10-17 ETH: IM about 2 % of 789 ETH, limit 20 %).
func TestEquityChange_WithdrawalUnderTheLimitSendsNoOrder(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.5, 0.02)
	f.exch.setMargin(5*f.exch.imPerLot, 1)
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllStrangles()) == 1 })
	time.Sleep(50 * time.Millisecond)
	sells, buys := len(f.exch.sells()), len(f.exch.buys())

	f.exch.withdraw(3) // IM ≈ 0.67 of 7 BTC: about 9.5 %, under 20 %
	time.Sleep(200 * time.Millisecond)

	if n := len(f.exch.sells()) - sells; n != 0 {
		t.Errorf("a withdrawal under the limit must sell nothing, got %d sells", n)
	}
	if n := len(f.exch.buys()) - buys; n != 0 {
		t.Errorf("a withdrawal under the limit must buy nothing back, got %d buys", n)
	}
	if p := f.position(f.call); p == nil || p.Qty != 0.5 {
		t.Errorf("call position = %+v, want 0.5 kept", p)
	}
}

// Scenario: the bot does not touch other positions — a long option it never
// traded stays untouched through a change in equity.
func TestEquityChange_PositionTheBotNeverTradedGetsNoOrder(t *testing.T) {
	const untraded = "BTC-25DEC26-60000-P"
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.positions = append(f.exch.positions,
		orders.RawPosition{InstrumentName: untraded, Size: 1.7, Direction: orders.DirectionBuy, AveragePrice: 0.067})
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return len(f.state.AllStrangles()) == 1 })

	f.exch.withdraw(3)
	time.Sleep(200 * time.Millisecond)

	if o := f.exch.ordersWhere(func(o orders.Order) bool { return o.Instrument == untraded }); len(o) != 0 {
		t.Errorf("a position the bot never traded must get no order, got %+v", o)
	}
	if f.position(untraded) != nil {
		t.Error("a long the bot never traded must not enter the book")
	}
}

// Scenario: the bot does not touch other positions — it reads only option
// positions, so futures and spot held on the account never reach the strategy.
func TestEquityChange_PositionsReadAreOptionsOnly(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `[]`}}}
	if _, err := orders.NewExecutor(fc).GetPositions(context.Background(), "ETH"); err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	c := fc.calls[0]
	if c.method != "private/get_positions" || c.params["currency"] != "ETH" || c.params["kind"] != "option" {
		t.Errorf("call = %s %v, want private/get_positions for ETH options only", c.method, c.params)
	}
}
