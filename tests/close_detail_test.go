package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Every close says why, with the numbers, so a leg-balance buy-back is never
// mistaken for a stop (2026-10-06: two ETH "Balance legs" closes were read as
// delta exits).

func TestEvaluateLeg_DetailNamesTheRuleAndNumbers(t *testing.T) {
	now := time.Now()
	leg := func(entry, mid, delta float64, dte int) *orders.Position {
		return &orders.Position{ID: "p", Qty: 1, EntryPrice: entry, PremiumReceived: entry, CurrentMid: mid,
			CurrentGreeks: orders.Greeks{Delta: delta}, MarkLive: true, Expiry: now.AddDate(0, 0, dte).Add(time.Hour)}
	}
	cases := []struct {
		name string
		pos  *orders.Position
		want []string
	}{
		{"stop-loss", leg(0.01, 0.035, 0.25, 40), []string{"stop-loss", "2.50×", "≥ 2.00×"}},
		{"time roll", leg(0.01, 0.01, 0.16, 10), []string{"time roll", "10 days", "≤ 15"}},
		{"delta exit", leg(0.01, 0.015, 0.32, 40), []string{"delta exit", "0.320", "≥ 0.30"}},
		{"delta drift", leg(0.01, 0.006, 0.08, 40), []string{"delta drift", "0.080", "< 0.10"}},
		{"take-profit", leg(0.01, 0.004, 0.12, 40), []string{"take-profit", "60%", "≥ 50%"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := strategy.EvaluateLeg(tc.pos, now, 15, 0.10, 0.50, 2.0, 0.30)
			for _, w := range tc.want {
				if !strings.Contains(d.Detail, w) {
					t.Errorf("detail %q lacks %q", d.Detail, w)
				}
			}
		})
	}
}

// Leg balancing journals what it saw, and says it is not a stop.
func TestBalanceLegs_JournalsWhy(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.3, 0.02)
	f.exch.positions[1].Size = -0.1 // call 0.3, put 0.1
	f.exch.summary.InitialMargin = 2.0
	f.startRun()

	eventually(t, 2*time.Second, "excess bought back", func() bool { return len(f.journal.events(orders.EventClosed)) > 0 })
	d := f.journal.events(orders.EventClosed)[0].ctx.Detail
	for _, w := range []string{"leg balance", "0.3 vs", "0.1", "0.2 excess", "not a stop"} {
		if !strings.Contains(d, w) {
			t.Errorf("detail %q lacks %q", d, w)
		}
	}
}

// The journal keeps the detail and the greeks, and the trade history reads them back.
func TestReplay_TradeCarriesDetailAndGreeks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.log")
	l, err := orders.NewLogger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	pos := &orders.Position{Instrument: "ETH-27NOV26-3300-C", OptionType: "call", Qty: 100, PremiumReceived: 1.6,
		EntryTime: time.Now().Add(-time.Hour), CurrentGreeks: orders.Greeks{Delta: 0.17, Theta: -1.2, Vega: 2.5}}
	l.LogClose(pos, orders.Fill{FillPrice: 0.016}, orders.TriggerRebalanceLegs, orders.TypeLimit,
		orders.EventContext{Detail: "leg balance: call 262 vs put 162"})
	l.Close()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"detail":"leg balance: call 262 vs put 162"`) {
		t.Errorf("journal line lacks the detail: %s", data)
	}
	r, err := orders.ReplayFile(path, 10)
	if err != nil || len(r.Trades) != 1 {
		t.Fatalf("replay: %v %+v", err, r)
	}
	tr := r.Trades[0]
	if tr.Detail != "leg balance: call 262 vs put 162" || tr.Delta != 0.17 || tr.Vega != 2.5 {
		t.Errorf("trade = %+v, want the detail and the greeks at close", tr)
	}
}
