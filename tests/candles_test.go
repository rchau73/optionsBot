package tests

import (
	"io"
	"net/http"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Daily candles for the monitor's regime chart (#394).

func candleDay(t0 time.Time, n int) time.Time { return t0.AddDate(0, 0, n) }

func TestCandlesOf_OneLivePerDayAndFlatWhenUnknown(t *testing.T) {
	d0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	closes := []orders.DailyClose{
		{Date: candleDay(d0, 0), Open: 100, High: 110, Low: 95, Close: 105},
		{Date: candleDay(d0, 1), Close: 107},                     // no OHLC: flat at the close
		{Date: candleDay(d0, 2), Close: 0},                       // no close: left out
		{Date: candleDay(d0, 3), Open: 107, High: 108, Low: 101}, // no close either
		{Date: candleDay(d0, 4), Open: 107, High: 109, Low: 104, Close: 106},
	}
	live := orders.DailyClose{Date: candleDay(d0, 4), Open: 107, High: 112, Low: 103, Close: 111} // replaces the seeded partial of today
	got := strategy.CandlesOf(closes, live, 60)
	want := []strategy.Candle{
		{Date: "2026-10-01", Open: 100, High: 110, Low: 95, Close: 105},
		{Date: "2026-10-02", Open: 107, High: 107, Low: 107, Close: 107},
		{Date: "2026-10-05", Open: 107, High: 112, Low: 103, Close: 111},
	}
	if len(got) != len(want) {
		t.Fatalf("candles = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candle %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(closes) != 5 || closes[4].Close != 106 {
		t.Error("CandlesOf must not change its input")
	}
}

func TestCandlesOf_LastDaysPlusToday(t *testing.T) {
	d0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var closes []orders.DailyClose
	for i := 0; i < 120; i++ {
		closes = append(closes, orders.DailyClose{Date: candleDay(d0, i), Close: 100 + float64(i)})
	}
	live := orders.DailyClose{Date: candleDay(d0, 120), Open: 220, High: 225, Low: 218, Close: 221}
	got := strategy.CandlesOf(closes, live, strategy.CandleDays)
	if len(got) != strategy.CandleDays+1 {
		t.Fatalf("got %d candles, want %d", len(got), strategy.CandleDays+1)
	}
	if got[0].Date != candleDay(d0, 60).Format(time.DateOnly) || got[len(got)-1].Close != 221 {
		t.Errorf("first %+v last %+v", got[0], got[len(got)-1])
	}
	if len(strategy.CandlesOf(nil, orders.DailyClose{}, 60)) != 0 {
		t.Error("no history → no candles")
	}
}

// Today's high and low follow the pushed prices; a day roll commits the
// candle, and the trend still reads only the closes.
func TestGammaMonitor_CandleRollUp(t *testing.T) {
	d0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	g := strategy.NewGammaMonitor(120, 5)
	var seed []orders.DailyClose
	for i := 0; i < 30; i++ {
		seed = append(seed, orders.DailyClose{Date: candleDay(d0, i), Open: 100, High: 102, Low: 98, Close: 100 + float64(i%3)})
	}
	g.SeedDailyCloses(seed)
	plain := strategy.NewGammaMonitor(120, 5) // the same history without OHLC
	var closeOnly []orders.DailyClose
	for _, c := range seed {
		closeOnly = append(closeOnly, orders.DailyClose{Date: c.Date, Close: c.Close})
	}
	plain.SeedDailyCloses(closeOnly)

	today := candleDay(d0, 29) // the seed's last day is today's partial
	for _, p := range []float64{101, 104, 97, 99} {
		g.PushPriceAt(today.Add(time.Hour), p)
		plain.PushPriceAt(today.Add(time.Hour), p)
	}
	c := g.Candles()
	last := c[len(c)-1]
	if last.Date != "2026-10-30" || last.Open != 100 || last.High != 104 || last.Low != 97 || last.Close != 99 {
		t.Errorf("live candle = %+v", last)
	}
	if len(c) != 30 {
		t.Errorf("one candle per day: got %d, want 30", len(c))
	}

	next := candleDay(d0, 30)
	g.PushPriceAt(next.Add(time.Minute), 103)
	plain.PushPriceAt(next.Add(time.Minute), 103)
	g.PushPriceAt(next.Add(2*time.Hour), 96)
	plain.PushPriceAt(next.Add(2*time.Hour), 96)
	c = g.Candles()
	committed, live := c[len(c)-2], c[len(c)-1]
	if committed.Date != "2026-10-30" || committed.High != 104 || committed.Low != 97 || committed.Close != 99 {
		t.Errorf("committed candle = %+v", committed)
	}
	if live.Date != "2026-10-31" || live.Open != 103 || live.High != 103 || live.Low != 96 || live.Close != 96 {
		t.Errorf("new live candle = %+v", live)
	}
	if g.SMALevels() != plain.SMALevels() {
		t.Errorf("OHLC changed the trend: %+v vs %+v", g.SMALevels(), plain.SMALevels())
	}
}

func TestGammaMonitor_CandlesWithoutHistory(t *testing.T) {
	g := strategy.NewGammaMonitor(120, 5)
	if len(g.Candles()) != 0 {
		t.Fatal("no price yet → no candles")
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	g.PushPriceAt(now, 100)
	g.PushPriceAt(now.Add(time.Minute), 102)
	c := g.Candles()
	if len(c) != 1 || c[0] != (strategy.Candle{Date: "2026-10-27", Open: 100, High: 102, Low: 100, Close: 102}) {
		t.Errorf("candles = %+v", c)
	}
	if g.MedianFlip() != 0 {
		t.Error("median flip outside bias = 0")
	}
}

func TestGetDailyCloses_ReadsOHLC(t *testing.T) {
	fc := &fakeCaller{replies: []fakeReply{{result: `{"status":"ok","ticks":[1700000000000],"open":[36000],"high":[37800],"low":[35500],"close":[37000]}`}}}
	closes, err := orders.NewExecutor(fc).GetDailyCloses(t.Context(), "BTC-PERPETUAL", 30)
	if err != nil || len(closes) != 1 {
		t.Fatalf("closes=%v err=%v", closes, err)
	}
	if c := closes[0]; c.Open != 36000 || c.High != 37800 || c.Low != 35500 || c.Close != 37000 {
		t.Errorf("close = %+v", c)
	}
	fc = &fakeCaller{replies: []fakeReply{{result: `{"status":"ok","ticks":[1700000000000,1700086400000],"open":[1],"close":[37000,37500]}`}}}
	closes, err = orders.NewExecutor(fc).GetDailyCloses(t.Context(), "BTC-PERPETUAL", 30)
	if err != nil || closes[0].Open != 0 || closes[1].High != 0 {
		t.Errorf("partial OHLC must be left unknown: %+v err=%v", closes, err)
	}
}

// /api/status serves sma.candles [{date, open, high, low, close}] and median_flip.
func TestAPI_StatusServesCandles(t *testing.T) {
	v := sampleView()
	v.SMA = strategy.SMAView{SMALevels: strategy.SMALevels{SMA9: 100, Trend: "bull"},
		Candles: []strategy.Candle{{Date: "2026-10-27", Open: 1, High: 3, Low: 0.5, Close: 2}}}
	v.MedianFlip = 99500
	h := api.New(staticView{v}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	code, body := getJSON(t, h, "/api/status")
	if code != http.StatusOK {
		t.Fatalf("status code %d", code)
	}
	sma := body["sma"].(map[string]any)
	if sma["sma9"] != 100.0 || sma["trend"] != "bull" {
		t.Errorf("levels stay flat in sma: %v", sma)
	}
	cs, ok := sma["candles"].([]any)
	if !ok || len(cs) != 1 {
		t.Fatalf("candles = %v", sma["candles"])
	}
	c := cs[0].(map[string]any)
	if c["date"] != "2026-10-27" || c["open"] != 1.0 || c["high"] != 3.0 || c["low"] != 0.5 || c["close"] != 2.0 {
		t.Errorf("candle = %v", c)
	}
	if body["median_flip"] != 99500.0 {
		t.Errorf("median_flip = %v", body["median_flip"])
	}
	if body["gex_as_of"] != nil {
		t.Errorf("no GEX snapshot → gex_as_of null, got %v", body["gex_as_of"])
	}
}

// /api/status serves when the GEX snapshot was computed, for the monitor's freshness badge.
func TestAPI_StatusServesGEXAsOf(t *testing.T) {
	v := sampleView()
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	v.GEXAsOf = &at
	h := api.New(staticView{v}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	_, body := getJSON(t, h, "/api/status")
	if body["gex_as_of"] != "2026-10-27T12:00:00Z" {
		t.Errorf("gex_as_of = %v", body["gex_as_of"])
	}
}

// The decision loop publishes the seeded history as candles in the view.
func TestStrategy_ViewPublishesCandles(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime()
	f.startRun()
	eventually(t, 2*time.Second, "candles published", func() bool { return len(f.strat.View().SMA.Candles) > 0 })
	c := f.strat.View().SMA.Candles
	if len(c) > strategy.CandleDays+1 {
		t.Errorf("%d candles, at most %d", len(c), strategy.CandleDays+1)
	}
	if v := f.strat.View(); v.GEXAsOf != nil {
		t.Errorf("fixed GEX snapshot has no ComputedAt: gex_as_of = %v, want nil", v.GEXAsOf)
	}
	if last := c[len(c)-1]; last.Date != time.Now().UTC().Format(time.DateOnly) && last.Date != time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly) {
		t.Errorf("last candle %+v is not today's", last)
	}
}
