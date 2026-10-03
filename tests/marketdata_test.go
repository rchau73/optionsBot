package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
)

// ── Fake gateway for the market-data manager ─────────────────────────────────

type fakeFeedGateway struct {
	mu         sync.Mutex
	results    map[string]string // method → raw JSON result
	errs       map[string]error
	subscribed []string
	notify     chan gateway.JSONRPCResponse
}

func newFakeFeedGateway() *fakeFeedGateway {
	return &fakeFeedGateway{
		results: map[string]string{},
		errs:    map[string]error{},
		notify:  make(chan gateway.JSONRPCResponse, 16),
	}
}

func (g *fakeFeedGateway) Call(_ context.Context, method string, _ any, _ int) (gateway.JSONRPCResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.errs[method]; err != nil {
		return gateway.JSONRPCResponse{}, err
	}
	return gateway.JSONRPCResponse{Result: json.RawMessage(g.results[method])}, nil
}

func (g *fakeFeedGateway) Subscribe(_ context.Context, channels []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.subscribed = append(g.subscribed, channels...)
	return nil
}

func (g *fakeFeedGateway) Notifications() <-chan gateway.JSONRPCResponse { return g.notify }

func (g *fakeFeedGateway) push(channel, data string) {
	g.notify <- gateway.JSONRPCResponse{Method: "subscription",
		Params: &gateway.Notification{Channel: channel, Data: json.RawMessage(data)}}
}

func (g *fakeFeedGateway) subscriptions() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.subscribed...)
}

// instrumentsJSON builds a public/get_instruments result: one call and one
// put per expiry, each expiry `dte` days from now.
func instrumentsJSON(now time.Time, dtes ...int) string {
	var rows []string
	for _, d := range dtes {
		exp := now.Add(time.Duration(d) * 24 * time.Hour).UnixMilli()
		for _, typ := range []string{"call", "put"} {
			rows = append(rows, fmt.Sprintf(
				`{"instrument_name":"BTC-D%d-%s","strike":100000,"expiration_timestamp":%d,"option_type":"%s","tick_size":0.0001,"min_trade_amount":0.1,"tick_size_steps":[{"above_price":0.005,"tick_size":0.0005}]}`,
				d, strings.ToUpper(typ[:1]), exp, typ))
		}
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func newMarketCfg() *config.Config {
	return &config.Config{
		Underlying:         "BTC",
		DTEDeltaMatrix:     []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.16}}},
		RolloutDTE:         19,
		MaxDTEDeviation:    10,
		IVPercentileWindow: 30,
	}
}

func startManager(t *testing.T, gw *fakeFeedGateway) *marketdata.Manager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := marketdata.New(newMarketCfg(), gw)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return m
}

// ── Manager ──────────────────────────────────────────────────────────────────

func TestMarketData_SubscribesOnlyToTradableExpiries(t *testing.T) {
	gw := newFakeFeedGateway()
	// 17 DTE is inside 25±10 but at/below rollout_dte 19, so the strategy
	// would never pick it; 24 is the expiry SelectExpiry chooses.
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 17, 24, 80, 110, 140, 170)
	m := startManager(t, gw)

	subs := strings.Join(gw.subscriptions(), " ")
	for _, want := range []string{"deribit_volatility_index.btc_usd", "deribit_price_index.btc_usd", "ticker.BTC-D24-C.100ms"} {
		if !strings.Contains(subs, want) {
			t.Errorf("missing subscription %s in %s", want, subs)
		}
	}
	if strings.Contains(subs, "BTC-D17-") {
		t.Error("an expiry inside the rollout window must not be subscribed")
	}
	// Three expiries beyond the largest target are kept for rollouts.
	for _, d := range []int{80, 110, 140} {
		if !strings.Contains(subs, fmt.Sprintf("BTC-D%d-C", d)) {
			t.Errorf("rollout expiry %d DTE not subscribed", d)
		}
	}
	if strings.Contains(subs, "BTC-D170-") {
		t.Error("only three rollout expiries should be subscribed")
	}

	inst, ok := m.GetInstrument("BTC-D24-C")
	if !ok || inst.MinTradeAmount != 0.1 || len(inst.TickSizeSteps) != 1 {
		t.Fatalf("instrument not loaded correctly: %+v", inst)
	}
	if got := inst.EffectiveTick(0.01); got != 0.0005 {
		t.Errorf("tick above 0.005 should be 0.0005, got %v", got)
	}
	if len(m.AllInstruments()) != 12 {
		t.Errorf("all 12 instruments should be tracked, got %d", len(m.AllInstruments()))
	}
}

func TestMarketData_AppliesTickerIndexAndDVOLPushes(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	m := startManager(t, gw)

	gw.push("deribit_price_index.btc_usd", `{"index_name":"btc_usd","price":101234.5}`)
	gw.push("ticker.BTC-D24-C.100ms", `{"instrument_name":"BTC-D24-C","best_bid_price":0.019,"best_ask_price":0.021,"mark_price":0.02,"underlying_price":101000,"mark_iv":55,"greeks":{"delta":0.16,"gamma":0.00001,"theta":-12,"vega":40,"rho":1}}`)
	gw.push("deribit_volatility_index.btc_usd", `{"volatility":55.5,"index_name":"btc_usd"}`)

	eventually(t, time.Second, "pushes applied", func() bool {
		inst, _ := m.GetInstrument("BTC-D24-C")
		return m.UnderlyingPrice() == 101234.5 && inst.Mid == 0.02 && m.DVOL() == 55.5
	})
	inst, _ := m.GetInstrument("BTC-D24-C")
	if inst.Greeks.Delta != 0.16 || inst.Greeks.IV != 0.55 || inst.UpdatedAt.IsZero() {
		t.Errorf("ticker fields not applied: %+v", inst)
	}

	// A snapshot is a copy: changing it must not touch the manager.
	inst.Mid = 99
	if again, _ := m.GetInstrument("BTC-D24-C"); again.Mid != 0.02 {
		t.Error("GetInstrument must return a copy")
	}
}

func TestMarketData_MidFallsBackToMarkWithoutQuotes(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	m := startManager(t, gw)

	gw.push("ticker.BTC-D24-P.100ms", `{"instrument_name":"BTC-D24-P","best_bid_price":0,"best_ask_price":0,"mark_price":0.013}`)
	eventually(t, time.Second, "mark fallback", func() bool {
		inst, _ := m.GetInstrument("BTC-D24-P")
		return inst.Mid == 0.013
	})
}

func TestMarketData_IgnoresUnknownAndMalformedPushes(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	m := startManager(t, gw)

	gw.push("ticker.BTC-UNKNOWN.100ms", `{"instrument_name":"BTC-UNKNOWN","best_bid_price":1,"best_ask_price":1}`)
	gw.push("ticker.BTC-D24-C.100ms", `not json`)
	gw.push("deribit_price_index.btc_usd", `{"price":0}`) // zero is not a price
	gw.notify <- gateway.JSONRPCResponse{Method: "subscription"}
	gw.push("deribit_price_index.btc_usd", `{"price":5}`) // marker: processed after the rest

	eventually(t, time.Second, "marker processed", func() bool { return m.UnderlyingPrice() == 5 })
	if _, ok := m.GetInstrument("BTC-UNKNOWN"); ok {
		t.Error("pushes for unknown instruments must not create instruments")
	}
}

func TestMarketData_SeedsDVOLHistory(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	var candles []string
	for i := 10; i >= 0; i-- { // DVOL rising every day: today is the highest
		ts := day.AddDate(0, 0, -i).UnixMilli()
		candles = append(candles, fmt.Sprintf("[%d,0,0,0,%d]", ts, 50+10-i))
	}
	gw.results["public/get_volatility_index_data"] = `{"data":[` + strings.Join(candles, ",") + `]}`
	m := startManager(t, gw)

	if p := m.IVPercentile(); p != 100 {
		t.Errorf("today's DVOL is above every previous day: percentile = %v, want 100", p)
	}
}

func TestMarketData_StartFailsWhenChainUnavailable(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.errs["public/get_instruments"] = errors.New("boom")
	if err := marketdata.New(newMarketCfg(), gw).Start(context.Background()); err == nil {
		t.Fatal("Start must fail without an option chain")
	}
}

func TestMarketData_DVOLSeedFailureIsNotFatal(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	gw.errs["public/get_volatility_index_data"] = errors.New("unavailable")
	m := startManager(t, gw)
	if m.IVPercentile() != 50 {
		t.Errorf("without history the percentile is neutral 50, got %v", m.IVPercentile())
	}
}

// ── DVOL tracker ─────────────────────────────────────────────────────────────

func TestDVOLTracker_OneValuePerDay(t *testing.T) {
	d := marketdata.NewDVOLTracker(30)
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d.Record(day, 40)
	d.Record(day.Add(time.Hour), 60) // same day: replaces
	d.Record(day.AddDate(0, 0, 1), 50)

	// Previous days: [60]. Today 50 is below it → 0th percentile.
	if p := d.Percentile(); p != 0 {
		t.Errorf("percentile = %v, want 0", p)
	}
	if d.Current() != 50 {
		t.Errorf("current = %v, want 50", d.Current())
	}

	// Many intraday readings must not flood the window.
	for i := 0; i < 1000; i++ {
		d.Record(day.AddDate(0, 0, 1).Add(time.Duration(i)*time.Second), 70)
	}
	if p := d.Percentile(); p != 100 {
		t.Errorf("intraday updates should only replace today's value, percentile = %v", p)
	}
}

func TestDVOLTracker_WindowAndOrdering(t *testing.T) {
	d := marketdata.NewDVOLTracker(2)
	if d.Percentile() != 50 || d.Current() != 0 {
		t.Error("empty tracker: percentile 50, current 0")
	}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []float64{10, 90, 80, 85} {
		d.Record(day.AddDate(0, 0, i), v)
	}
	// Window of 2 previous days: [90, 80]; today 85 beats one of two.
	if p := d.Percentile(); p != 50 {
		t.Errorf("percentile = %v, want 50 (oldest day dropped)", p)
	}
	d.Record(day, 1) // a reading for an older day is ignored
	if d.Current() != 85 {
		t.Errorf("older readings must not change today's value, current = %v", d.Current())
	}
}

// ── Expiry selection ─────────────────────────────────────────────────────────

func TestExpiryWindow(t *testing.T) {
	tests := []struct{ target, dev, rollout, lo, hi int }{
		{45, 10, 19, 35, 55},
		{25, 10, 19, 20, 35}, // rollout floor raises the low end
		{30, 0, 10, 30, 30},
	}
	for _, tc := range tests {
		lo, hi := marketdata.ExpiryWindow(tc.target, tc.dev, tc.rollout)
		if lo != tc.lo || hi != tc.hi {
			t.Errorf("ExpiryWindow(%d,%d,%d) = [%d,%d], want [%d,%d]", tc.target, tc.dev, tc.rollout, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestNearestExpiry_SkipsAndBounds(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	at := func(d int) time.Time { return now.AddDate(0, 0, d) }
	insts := []*marketdata.Instrument{{Expiry: at(30)}, {Expiry: at(30)}, {Expiry: at(37)}, {Expiry: at(60)}}

	if got, ok := marketdata.NearestExpiry(insts, now, 20, 40, nil); !ok || !got.Equal(at(30)) {
		t.Errorf("nearest = %v, want 30 DTE", got)
	}
	if got, ok := marketdata.NearestExpiry(insts, now, 20, 40, map[time.Time]bool{at(30): true}); !ok || !got.Equal(at(37)) {
		t.Errorf("with 30 DTE skipped want 37 DTE, got %v", got)
	}
	if _, ok := marketdata.NearestExpiry(insts, now, 41, 59, nil); ok {
		t.Error("no expiry in [41,59]")
	}
}
