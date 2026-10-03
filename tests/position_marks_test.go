package tests

import (
	"slices"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// A position loaded from the exchange in an expiry the bot does not quote
// (no ticker): the case of ETH-30OCT26-2250-P on 2026-10-03, whose mark was
// overwritten with 0 every cycle — delta drift tried to close it and the
// stop-loss could never fire.

// withUnquotedPosition makes the exchange report a short put the market
// data knows but has no ticker for.
func (f *strategyFixture) withUnquotedPosition(name string, size, avgPrice, markPrice, positionDelta float64) {
	f.market.mu.Lock()
	// Known from the option chain, but no ticker has ever updated it.
	f.market.instruments[name] = &marketdata.Instrument{Name: name, Underlying: "BTC", Strike: 70000, Expiry: f.expiry, OptionType: "put", MinTradeAmount: 0.1}
	f.market.mu.Unlock()
	f.exch.positions = append(f.exch.positions, orders.RawPosition{
		InstrumentName: name, Size: size, Direction: "sell", AveragePrice: avgPrice, MarkPrice: markPrice, Delta: positionDelta,
	})
}

func TestStrategy_UnquotedPositionKeepsItsMarkAndIsNotRolled(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.DTEDeltaMatrix[0].DTE = 60 // no vacant-slot noise: entries need quotes anyway
	put := "BTC-" + strings.ToUpper(f.expiry.Format("2Jan06")) + "-70000-P"
	f.withUnquotedPosition(put, -1.2, 0.0150, 0.0065, 0.11) // position delta +0.11 → option delta −0.092
	f.startRun()

	eventually(t, 2*time.Second, "position loaded and subscribed", func() bool {
		f.market.mu.Lock()
		defer f.market.mu.Unlock()
		return f.position(put) != nil && slices.Contains(f.market.tracked, put)
	})
	time.Sleep(100 * time.Millisecond) // several decision cycles
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("a leg without a live quote must not be rolled on zeroed data: %d buys", n)
	}
	p := f.position(put)
	if p.CurrentMid != 0.0065 || p.MarkLive {
		t.Errorf("the exchange mark must be kept and flagged not live: mid %v live %v", p.CurrentMid, p.MarkLive)
	}
	if !near(p.CurrentGreeks.Delta, 0.11/-1.2, 1e-12) {
		t.Errorf("delta = %v, want the per-option delta %v", p.CurrentGreeks.Delta, 0.11/-1.2)
	}
}

func TestStrategy_StopLossFiresOnLastKnownMarkWithoutQuote(t *testing.T) {
	f := newStrategyFixture(t)
	put := "BTC-" + strings.ToUpper(f.expiry.Format("2Jan06")) + "-70000-P"
	f.withUnquotedPosition(put, -1.2, 0.0100, 0.0310, 0.5) // loss 2.1× premium
	f.startRun()

	eventually(t, 2*time.Second, "stop-loss sent", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == put && b.TriggerReason == orders.TriggerStopLoss200Pct && b.OrderType == orders.TypeMarket {
				return true
			}
		}
		return false
	})
}

func TestStrategy_QuoteArrivalMakesTheMarkLive(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "marks live from tickers", func() bool {
		p := f.position(f.call)
		return p != nil && p.MarkLive && p.CurrentMid == 0.02
	})
}

func TestPerOptionGreeks(t *testing.T) {
	g := strategy.PerOptionGreeks(orders.RawPosition{Size: -1239, Delta: 114, Gamma: 0.5, Vega: -60, Theta: 30})
	if !near(g.Delta, -0.0920, 1e-4) || !near(g.Vega, 60.0/1239, 1e-9) || !near(g.Theta, -30.0/1239, 1e-9) {
		t.Errorf("per-option greeks = %+v", g)
	}
	if (strategy.PerOptionGreeks(orders.RawPosition{Delta: 1}) != orders.Greeks{}) {
		t.Error("a zero-size position has no per-option greeks")
	}
}
