package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The delta exit closes a short leg whose |delta| has reached
// delta_exit_threshold — the move against it is real — before the 2× stop,
// and holds it like a stopped leg. In stress simulations (±25–41 % moves,
// chop, ordinary months) exiting at 0.25–0.30 and holding beat today's
// stop-only rule in every group; rolling and re-selling at once lost more.

func TestEvaluateLeg_DeltaExit(t *testing.T) {
	cases := []struct {
		name  string
		pos   *orders.Position
		exit  float64
		want  strategy.RolloutAction
		label string
	}{
		{"call at 0.30 exits", makePos("call", 30, 100, 160, 0.30), 0.30, strategy.ActionDeltaExit, orders.TriggerDeltaExit},
		{"put at -0.35 exits (sign ignored)", makePos("put", 30, 100, 160, -0.35), 0.30, strategy.ActionDeltaExit, orders.TriggerDeltaExit},
		{"just below the threshold holds", makePos("call", 30, 100, 150, 0.29), 0.30, strategy.ActionNone, ""},
		{"disabled (0) holds", makePos("call", 30, 100, 160, 0.45), 0, strategy.ActionNone, ""},
		{"stop-loss wins when both fire", makePos("call", 30, 100, 300, 0.45), 0.30, strategy.ActionStopLoss, orders.TriggerStopLoss200Pct},
		{"the DTE roll wins inside the window", makePos("call", 15, 100, 160, 0.45), 0.30, strategy.ActionRollNextMonth, orders.TriggerRollout19DTE},
	}
	for _, c := range cases {
		dec := strategy.EvaluateLeg(c.pos, time.Now(), 19, 0.10, 0.50, 2.0, c.exit)
		if dec.Action != c.want || dec.Reason != c.label {
			t.Errorf("%s: got %v %q, want %v %q", c.name, dec.Action, dec.Reason, c.want, c.label)
		}
	}
}

func TestEvaluateLeg_DeltaExitNeedsALiveMark(t *testing.T) {
	pos := makePos("call", 30, 100, 160, 0.45)
	pos.MarkLive = false // last known greeks only
	if dec := strategy.EvaluateLeg(pos, time.Now(), 19, 0.10, 0.50, 2.0, 0.30); dec.Action != strategy.ActionNone {
		t.Errorf("no delta exit on a stale delta: %v", dec.Action)
	}
}

// setDelta changes an instrument's delta (a live quote keeps flowing).
func (m *fakeMarket) setDelta(name string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instruments[name].Greeks.Delta = delta
	m.instruments[name].UpdatedAt = time.Now()
}

// breachTheCall leaves the fixture's call at |Δ| 0.35, worth 1.5× its
// premium (below the 2× stop), so only the delta exit can close it.
func (f *strategyFixture) breachTheCall() {
	f.cfg.DeltaExitThreshold = 0.30
	f.withOpenStrangle(0.1, 0.02)
	f.market.setQuote(f.call, 0.029, 0.031)
	f.market.setDelta(f.call, 0.35)
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: o.LimitPrice}
		}
		return orders.Fill{}
	}
}

func TestStrategy_DeltaExitClosesAtTheAskAndHoldsTheLeg(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 72
	f.breachTheCall()
	f.startRun()

	eventually(t, 2*time.Second, "call closed by the delta exit", func() bool {
		for _, e := range f.journal.events(orders.EventClosed) {
			if e.trigger == orders.TriggerDeltaExit {
				return true
			}
		}
		return false
	})
	for _, o := range f.exch.buys() {
		if o.Instrument == f.call {
			if o.OrderType != orders.TypeLimit || o.LimitPrice != 0.031 {
				t.Errorf("delta exit must be a limit at the ask (0.031): %s %v", o.OrderType, o.LimitPrice)
			}
		}
		if o.Instrument == f.put {
			t.Error("only the breached leg is closed")
		}
	}
	time.Sleep(150 * time.Millisecond) // many cycles
	if n := f.callSells(); n != 0 {
		t.Errorf("a delta-exited call is held like a stopped one, not re-sold: %d sells", n)
	}
	held := false
	for _, e := range f.journal.events(orders.EventSkipped) {
		held = held || strings.HasPrefix(e.reason, "repair_held: call stopped out, cooldown")
	}
	if !held {
		t.Error("the held repair must be journaled")
	}
}

func TestStrategy_DeltaExitedLegIsResoldOnceCalm(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.RepairCooldownHours = 0 // calm market, cooldown over
	f.breachTheCall()
	f.startRun()

	eventually(t, 2*time.Second, "call delta-exited", func() bool {
		return len(f.journal.events(orders.EventClosed)) >= 1
	})
	f.market.setDelta(f.call, 0.16) // the replacement is chosen at the entry delta
	eventually(t, 2*time.Second, "re-sold by repair once calm", func() bool { return f.callSells() >= 1 })
}
