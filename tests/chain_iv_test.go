package tests

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// A coin without DVOL (SOL, XRP, #834) reads its chain's 30-day ATM implied
// volatility wherever DVOL feeds a rule, and holds the IM limit at the
// lowest band until 90 daily closes of it exist.

var chainIVNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// chainBookRows is a Deribit book summary of one coin's USDC options: for
// each expiry three strikes around spot, mark IV in percent per strike and
// side ({call, put}), a quoted bid and ask.
func chainBookRows(spot float64, books map[string]map[float64][2]float64) []gex.SummaryRow {
	var rows []gex.SummaryRow
	for prefix, strikes := range books {
		for k, iv := range strikes {
			strike := strings.Replace(fmt.Sprintf("%g", k), ".", "d", 1)
			for i, side := range []string{"C", "P"} {
				rows = append(rows, gex.SummaryRow{InstrumentName: prefix + "-" + strike + "-" + side,
					UnderlyingPrice: spot, MarkIV: iv[i], BidPrice: 1, AskPrice: 1.2, MidPrice: 1.1, MarkPrice: 1.1, OpenInterest: 100})
			}
		}
	}
	return rows
}

// recordAndRead writes rows as the recorder does (data/market_usdc_sol/…) and
// reads the snapshot back, as the replay and the research tools read it.
func recordAndRead(t *testing.T, rows []gex.SummaryRow) recorder.Snapshot {
	t.Helper()
	dir := t.TempDir()
	if _, err := recorder.New(&fakeSummary{rows: rows, at: chainIVNow}, nil, dir, time.Hour).Snapshot(chainIVNow); err != nil {
		t.Fatal(err)
	}
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil || len(snaps) != 1 {
		t.Fatalf("recorded book: %d snapshots, %v", len(snaps), err)
	}
	return snaps[0]
}

// varianceAt30 is the expected interpolation, written out: σ² × T linear in T.
func varianceAt30(iv1, days1, iv2, days2 float64) float64 {
	t1, t2, t := days1/365, days2/365, 30.0/365
	w := iv1*iv1*t1 + (iv2*iv2*t2-iv1*iv1*t1)*(t-t1)/(t2-t1)
	return math.Sqrt(w/t) * 100
}

func TestChainIV_ATMInterpolatedByVarianceOnRecordedSOLBook(t *testing.T) {
	// 11 DEC is 21 d 20 h away, 25 DEC 35 d 20 h: 30 days lies between them.
	rows := chainBookRows(140, map[string]map[float64][2]float64{
		"SOL_USDC-11DEC26": {130: {70, 72}, 140: {62, 64}, 150: {66, 68}},
		"SOL_USDC-25DEC26": {130: {72, 74}, 140: {65, 67}, 150: {69, 71}},
	})
	snap := recordAndRead(t, rows)
	iv, ok := marketdata.ChainATMIV30(snap.Instruments(1), 140, chainIVNow)
	want := varianceAt30(0.63, 21+20.0/24, 0.66, 35+20.0/24)
	if !ok || math.Abs(iv-want) > 1e-6 {
		t.Fatalf("chain ATM IV 30d = %v (ok %v), want %v: the 140 strike's call/put mean, by variance between the two expiries", iv, ok, want)
	}
	if iv <= 63 || iv >= 66 {
		t.Errorf("interpolated IV %v must lie between the two expiries' 63 and 66", iv)
	}
}

func TestChainIV_OneExpiryAndEdgeCasesOnRecordedXRPBook(t *testing.T) {
	// One expiry only → its own ATM IV, the 2.2 strike's (nearest spot 2.24).
	rows := chainBookRows(2.24, map[string]map[float64][2]float64{
		"XRP_USDC-25DEC26": {2.2: {80, 84}, 2.3: {90, 94}, 3: {100, 100}},
	})
	snap := recordAndRead(t, rows)
	if q := snap.ByName()["XRP_USDC-25DEC26-2d2-P"]; q.Strike != 2.2 {
		t.Fatalf("recorded XRP put strike = %v, want 2.2", q.Strike)
	}
	chain := snap.Instruments(1)
	if iv, ok := marketdata.ChainATMIV30(chain, 2.24, chainIVNow); !ok || math.Abs(iv-82) > 1e-9 {
		t.Errorf("one expiry: IV = %v (ok %v), want its ATM 82", iv, ok)
	}
	// All expiries beyond 30 days: the nearest, never extrapolated.
	far := chainBookRows(140, map[string]map[float64][2]float64{
		"SOL_USDC-25DEC26": {140: {60, 60}},
		"SOL_USDC-29JAN27": {140: {80, 80}},
	})
	if iv, ok := marketdata.ChainATMIV30(recordAndRead(t, far).Instruments(1), 140, chainIVNow.AddDate(0, 0, -10)); !ok || math.Abs(iv-60) > 1e-9 {
		t.Errorf("all expiries past 30 days: IV = %v (ok %v), want the nearest's 60", iv, ok)
	}
	// No spot, an empty chain, a strike with one side quoted, an expired
	// expiry, an instrument with no quote: nothing to read.
	if _, ok := marketdata.ChainATMIV30(chain, 0, chainIVNow); ok {
		t.Error("no spot: no reading")
	}
	if _, ok := marketdata.ChainATMIV30(nil, 2.24, chainIVNow); ok {
		t.Error("empty chain: no reading")
	}
	callOnly := []*marketdata.Instrument{{Name: "XRP_USDC-25DEC26-2d2-C", Strike: 2.2, OptionType: "call",
		Expiry: time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC), Greeks: marketdata.Greeks{IV: 0.8}, UpdatedAt: chainIVNow}}
	if _, ok := marketdata.ChainATMIV30(callOnly, 2.24, chainIVNow); ok {
		t.Error("a strike needs both its call and its put")
	}
	if _, ok := marketdata.ChainATMIV30(chain, 2.24, time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)); ok {
		t.Error("an expired expiry is not read")
	}
	for _, inst := range chain {
		inst.UpdatedAt = time.Time{}
	}
	if _, ok := marketdata.ChainATMIV30(chain, 2.24, chainIVNow); ok {
		t.Error("instruments without a quote are not read")
	}
}

// solManager starts a SOL manager on the fake feed, its two options of one
// expiry 24 days out, with an optional chain-IV history file.
func solManager(t *testing.T, gw *fakeFeedGateway, history *marketdata.IVHistory) *marketdata.Manager {
	t.Helper()
	gw.results["public/get_instruments"] = usdcInstrumentsJSON(time.Now().Add(24 * 24 * time.Hour))
	cfg := newMarketCfg()
	cfg.Underlying = "SOL"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newFeedManager(cfg, gw)
	if history != nil {
		m.UseIVHistory(history)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return m
}

// Scenario: the flip buffer uses chain IV.
func TestChainIV_FlipBufferUsesChainIV(t *testing.T) {
	gw := newFakeFeedGateway()
	m := solManager(t, gw, nil)
	gw.push("deribit_price_index.sol_usdc", `{"index_name":"sol_usdc","price":80}`)
	gw.push("ticker.SOL_USDC-25DEC26-80-C.100ms", `{"instrument_name":"SOL_USDC-25DEC26-80-C","best_bid_price":4,"best_ask_price":4.4,"mark_price":4.2,"mark_iv":63}`)
	gw.push("ticker.SOL_USDC-25DEC26-80-P.100ms", `{"instrument_name":"SOL_USDC-25DEC26-80-P","best_bid_price":4,"best_ask_price":4.4,"mark_price":4.2,"mark_iv":65}`)
	eventually(t, time.Second, "tickers applied", func() bool {
		p, _ := m.GetInstrument("SOL_USDC-25DEC26-80-P")
		return m.UnderlyingPrice() == 80 && p.Greeks.IV == 0.65
	})
	if m.DVOL() != 0 {
		t.Fatalf("before the first chain reading DVOL() = %v, want 0", m.DVOL())
	}
	m.ReadChainIV(time.Now())

	if got := m.DVOL(); math.Abs(got-64) > 1e-9 {
		t.Fatalf("SOL's volatility reading = %v, want its chain ATM IV 30d of 64", got)
	}
	if got, want := strategy.FlipBufferPct(1.5, m.DVOL()), 1.5*64/math.Sqrt(365); math.Abs(got-want) > 1e-12 {
		t.Errorf("FlipBufferPct = %v, want 1.5 × 64 ÷ √365 = %v", got, want)
	}
	if m.IVSource() != marketdata.ChainIVSource {
		t.Errorf("IVSource = %q, want %q", m.IVSource(), marketdata.ChainIVSource)
	}
	// SOL has no DVOL at Deribit: no DVOL channel, no DVOL history request.
	for _, ch := range gw.subscriptions() {
		if strings.HasPrefix(ch, "deribit_volatility_index.") {
			t.Errorf("a SOL bot subscribed to %s", ch)
		}
	}
	for _, c := range gw.calls {
		if c.method == "public/get_volatility_index_data" {
			t.Error("a SOL bot asked Deribit for a DVOL history")
		}
	}
}

// BTC keeps DVOL: its source, channel and history request are unchanged.
func TestChainIV_BTCKeepsDVOL(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	m := startManager(t, gw)
	if m.IVSource() != marketdata.DVOLSource || !marketdata.HasVolIndex("BTC") || !marketdata.HasVolIndex("ETH") {
		t.Errorf("BTC/ETH read DVOL: source %q", m.IVSource())
	}
	if subs := gw.subscriptions(); len(subs) == 0 || subs[0] != "deribit_volatility_index.btc_usd" {
		t.Errorf("BTC's first channel is still DVOL's: %v", subs)
	}
	asked := false
	for _, c := range gw.calls {
		asked = asked || c.method == "public/get_volatility_index_data"
	}
	if !asked {
		t.Error("BTC still seeds its DVOL history from Deribit")
	}
	p := config.Config{}
	base := p.RiskPolicy(true)
	if got := strategy.IVPolicy(base, marketdata.DVOLSource, config.FixedChainIVMinDays); !reflect.DeepEqual(got, base) {
		t.Errorf("DVOL policy changed: %+v", got)
	}
	for _, u := range []string{"BTC", "ETH"} {
		cfg := &config.Config{Underlying: u}
		if !reflect.DeepEqual(orders.StartSettings(cfg), config.Effective(cfg)) {
			t.Errorf("%s: the config_loaded settings must stay config.Effective's", u)
		}
	}
	sol := orders.StartSettings(&config.Config{Underlying: "SOL"})
	if sol[orders.IVSourceKey] != marketdata.ChainIVSource {
		t.Errorf("SOL's settings name its source: %v", sol[orders.IVSourceKey])
	}
}

// chainPolicy is the bot's policy for a coin without DVOL, no regime rule.
func chainPolicy() risk.Config {
	return strategy.IVPolicy((&config.Config{}).RiskPolicy(false), marketdata.ChainIVSource, config.FixedChainIVMinDays)
}

// highCloses is n completed daily closes in the top band (percentile 80).
func highCloses(n int) []risk.Day {
	today := chainIVNow.Truncate(24 * time.Hour)
	out := make([]risk.Day, n)
	for i := range out {
		out[i] = risk.Day{Date: today.AddDate(0, 0, i-n), IVPct: 80, IVKnown: true}
	}
	return out
}

// Scenario: the margin limit has an input without a DVOL.
func TestChainIV_MarginLimitWithoutDVOL(t *testing.T) {
	today := chainIVNow.Truncate(24 * time.Hour)
	st := risk.Evaluate(chainPolicy(), highCloses(10), risk.Day{Date: today, IVPct: 80, IVKnown: true, IVDays: 10})
	if st.LimitIMPct != 20 || st.Band != nil || len(st.Pending) != 0 || st.Frozen {
		t.Errorf("10 of 90 days: limit %v band %v pending %v frozen %v, want the lowest band 20, nothing pending",
			st.LimitIMPct, st.Band, st.Pending, st.Frozen)
	}
	for _, want := range []string{"chain ATM IV 30d", "10 of 90 days"} {
		if !strings.Contains(st.Reason, want) {
			t.Errorf("reason %q lacks %q", st.Reason, want)
		}
	}
	if !st.CanRebalance {
		t.Error("the warm-up's lowest band is a rule: the book may be resized toward it")
	}
	if st.Reason != "chain ATM IV 30d: 10 of 90 days of history: lowest band" {
		t.Errorf("reason = %q", st.Reason)
	}

	// The same through Strategy.Run: the startup risk_limit line.
	f := newStrategyFixture(t)
	f.market.ivSource = marketdata.ChainIVSource
	f.market.setIVHistory([]float64{80, 80, 80, 80, 80, 80, 80, 80, 80, 80}, 80)
	f.startRun()
	eventually(t, 2*time.Second, "startup limit journaled", func() bool { return len(f.journal.riskChanges(orders.RiskLimitChanged)) > 0 })
	rec := f.journal.riskChanges(orders.RiskLimitChanged)[0]
	if rec.LimitIMPct != 20 || !strings.Contains(rec.Detail, "chain ATM IV 30d") || !strings.Contains(rec.Detail, "10 of 90 days") {
		t.Errorf("startup risk_limit = limit %v, detail %q; want 20 %% with the source and 10 of 90 days", rec.LimitIMPct, rec.Detail)
	}
}

// Scenario: bands start after 90 days.
func TestChainIV_BandsStartAfter90Days(t *testing.T) {
	today := chainIVNow.Truncate(24 * time.Hour)
	st := risk.Evaluate(chainPolicy(), highCloses(90), risk.Day{Date: today, IVPct: 80, IVKnown: true, IVDays: 90})
	dvol := risk.Evaluate((&config.Config{}).RiskPolicy(false), highCloses(90), risk.Day{Date: today, IVPct: 80, IVKnown: true})
	if st.LimitIMPct != 50 || st.LimitIMPct != dvol.LimitIMPct || st.Band == nil || st.Band.MinIVPct != 70 {
		t.Errorf("90 closes: limit %v (band %v), want the top band 50 as BTC/ETH get (%v)", st.LimitIMPct, st.Band, dvol.LimitIMPct)
	}
	if st.Reason != "chain ATM IV 30d band ≥70 (IM 50%) (confirmed)" || dvol.Reason != "DVOL band ≥70 (IM 50%) (confirmed)" {
		t.Errorf("reasons: chain %q, DVOL %q", st.Reason, dvol.Reason)
	}
	// One day short: still the lowest band.
	if st := risk.Evaluate(chainPolicy(), highCloses(89), risk.Day{Date: today, IVPct: 80, IVKnown: true, IVDays: 89}); st.LimitIMPct != 20 {
		t.Errorf("89 closes: limit %v, want the lowest band 20", st.LimitIMPct)
	}

	// The same through Strategy.Run.
	f := newStrategyFixture(t)
	f.market.ivSource = marketdata.ChainIVSource
	closes := make([]float64, 90)
	for i := range closes {
		closes[i] = 80
	}
	f.market.setIVHistory(closes, 80)
	f.startRun()
	eventually(t, 2*time.Second, "startup limit journaled", func() bool { return len(f.journal.riskChanges(orders.RiskLimitChanged)) > 0 })
	if rec := f.journal.riskChanges(orders.RiskLimitChanged)[0]; rec.LimitIMPct != 50 {
		t.Errorf("90 closes through Run: limit %v (%q), want 50", rec.LimitIMPct, rec.Detail)
	}
}

// Under a confirmed negative regime the warm-up keeps the lowest band and
// says how far the history is.
func TestChainIV_WarmupUnderNegativeRegime(t *testing.T) {
	p := strategy.IVPolicy((&config.Config{}).RiskPolicy(true), marketdata.ChainIVSource, 90)
	today := chainIVNow.Truncate(24 * time.Hour)
	closes := highCloses(3)
	for i := range closes {
		closes[i].Negative, closes[i].RegimeKnown = true, true
	}
	st := risk.Evaluate(p, closes, risk.Day{Date: today, IVDays: 3, Negative: true, RegimeKnown: true})
	if st.LimitIMPct != 20 || !strings.Contains(st.Reason, "negative gamma regime") || !strings.Contains(st.Reason, "3 of 90 days") {
		t.Errorf("limit %v reason %q", st.LimitIMPct, st.Reason)
	}
}

func TestChainIV_HistoryKeepsDailyClosesAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain_iv_history.jsonl")
	h, err := marketdata.OpenIVHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	h.Record(day.Add(1*time.Hour), 60)                   // a new day: written
	h.Record(day.Add(1*time.Hour+time.Minute), 61)       // within the hour: held
	h.Record(day.Add(23*time.Hour), 62)                  // an hour later: written
	h.Record(day.Add(23*time.Hour+time.Minute), 63)      // held…
	h.Record(day.Add(24*time.Hour+time.Minute), 70)      // …written as the 17th's close when the 18th starts
	h.Record(day.Add(-time.Hour), 99)                    // an older day: ignored
	h.Record(day.Add(24*time.Hour+2*time.Minute), 0)     // no reading: ignored
	h.Record(day.Add(48*time.Hour+time.Minute), 75)      // the 19th
	h.Record(day.Add(48*time.Hour+30*time.Minute), 76.5) // held: lost at a restart, the next reading replaces it
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 5 {
		t.Errorf("%d lines written, want 5 (new days, hourly, the close at day end):\n%s", n, data)
	}

	again, err := marketdata.OpenIVHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	want := []marketdata.VolClose{{At: day, Close: 63}, {At: day.AddDate(0, 0, 1), Close: 70}, {At: day.AddDate(0, 0, 2), Close: 75}}
	if got := again.Daily(); !reflect.DeepEqual(got, want) {
		t.Errorf("reloaded closes = %v, want %v", got, want)
	}

	// The manager seeds its tracker from it: two completed closes before the 19th.
	tr := marketdata.NewDVOLTracker(252)
	for _, d := range again.Daily() {
		tr.Record(d.At, d.Close)
	}
	if n := tr.Closes(day.AddDate(0, 0, 2).Add(time.Hour)); n != 2 {
		t.Errorf("tracker closes before the 19th = %d, want 2", n)
	}
}

// A SOL manager seeds its percentile and close count from the history file.
func TestChainIV_ManagerSeedsFromHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain_iv_history.jsonl")
	h, err := marketdata.OpenIVHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 10; i >= 1; i-- {
		h.Record(today.AddDate(0, 0, -i), float64(50+10-i)) // rising: yesterday is the highest
	}
	m := solManager(t, newFakeFeedGateway(), h)
	if n := m.IVCloses(time.Now()); n != 10 {
		t.Errorf("IVCloses = %d, want the 10 recorded days", n)
	}
	if m.DVOL() != 59 {
		t.Errorf("latest reading = %v, want yesterday's 59", m.DVOL())
	}
}
