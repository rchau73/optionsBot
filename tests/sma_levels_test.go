package tests

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #118: the SMA trend rule names the condition that set it.
func TestSMATrendOf_Reasons(t *testing.T) {
	cases := []struct {
		name                          string
		price, high, low, sma9, sma21 float64
		dir                           int
		reason                        string
	}{
		{"no swing yet", 100, 0, 0, 101, 99, 0, strategy.TrendReasonNoSwing},
		{"breakout above the swing high", 111, 110, 90, 95, 100, 1, strategy.TrendReasonBreakout},
		{"SMA9 over SMA21, price over SMA9", 105, 110, 90, 102, 100, 1, strategy.TrendReasonBullSMA},
		{"breakdown below the swing low", 89, 110, 90, 105, 100, -1, strategy.TrendReasonBreakdown},
		{"SMA9 under SMA21, price under SMA9", 95, 110, 90, 98, 100, -1, strategy.TrendReasonBearSMA},
		{"between the SMAs", 99, 110, 90, 98, 100, 0, strategy.TrendReasonInside},
		{"bull SMAs but price under SMA9", 101, 110, 90, 102, 100, 0, strategy.TrendReasonInside},
		{"only a swing low known, price above it", 95, 0, 90, 0, 0, 0, strategy.TrendReasonInside},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, reason := strategy.SMATrendOf(c.price, c.high, c.low, c.sma9, c.sma21)
			if dir != c.dir || reason != c.reason {
				t.Errorf("SMATrendOf = %d %q, want %d %q", dir, reason, c.dir, c.reason)
			}
		})
	}
}

// Scenario: /api/status serves SMA9, SMA21, the swings and their dates —
// Strategy.View publishes the levels detectTrend uses.
func TestStrategy_ViewShowsSMALevels(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime() // peak 120,000 ten days in, slide to 100,000
	f.startRun()

	var v strategy.View
	eventually(t, 2*time.Second, "levels published", func() bool { v = f.strat.View(); return v.SMA.SMA21 > 0 })
	l := v.SMA
	peakDay := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -20).Format(time.DateOnly)
	if l.SwingHigh != 120000 || l.SwingHighDate != peakDay {
		t.Errorf("swing high %v on %q, want 120000 on %s", l.SwingHigh, l.SwingHighDate, peakDay)
	}
	if l.SwingLow != 0 || l.SwingLowDate != "" { // a steady slide has no confirmed low
		t.Errorf("swing low %v on %q, want none", l.SwingLow, l.SwingLowDate)
	}
	if !(l.SMA9 > 0 && l.SMA9 < l.SMA21) || l.Price != 100000 {
		t.Errorf("sma9 %v sma21 %v price %v", l.SMA9, l.SMA21, l.Price)
	}
	if l.Trend != "bear" || l.Reason != strategy.TrendReasonBearSMA || v.SMATrend != "bear" {
		t.Errorf("trend %q (%q), view sma_trend %q", l.Trend, l.Reason, v.SMATrend)
	}
}

// Scenario: a trend change appears once in the Activity panel, with the
// reason and the levels — never once per cycle, not at startup, and under
// both direction sources.
func TestStrategy_TrendChangeJournaledOnce(t *testing.T) {
	for _, direction := range []string{"sma", "bias"} {
		t.Run(direction, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.GEXDirection = direction
			f.withPutSheddingRegime()
			f.startRun()

			eventually(t, 2*time.Second, "levels published", func() bool { return f.strat.View().SMA.Trend == "bear" })
			time.Sleep(50 * time.Millisecond) // several cycles
			if n := len(f.journal.trendChanges()); n != 0 {
				t.Fatalf("the trend seen at startup is not a change: %d events", n)
			}

			setPrice(f, 125000) // above the 120,000 swing high
			eventually(t, 2*time.Second, "bull journaled", func() bool { return len(f.journal.trendChanges()) > 0 })
			time.Sleep(50 * time.Millisecond)
			recs := f.journal.trendChanges()
			if len(recs) != 1 {
				t.Fatalf("one event per change, got %d", len(recs))
			}
			r := recs[0]
			if r.From != "bear" || r.To != "bull" || r.Reason != strategy.TrendReasonBreakout || r.Direction != direction ||
				r.Spot != 125000 || r.SwingHigh != 120000 || r.SwingHighDate == "" || r.SMA9 <= 0 || r.SMA21 <= 0 || r.Detail == "" {
				t.Errorf("record = %+v", r)
			}

			setPrice(f, 100000)
			eventually(t, 2*time.Second, "bear journaled", func() bool { return len(f.journal.trendChanges()) == 2 })
			time.Sleep(50 * time.Millisecond)
			recs = f.journal.trendChanges()
			if len(recs) != 2 || recs[1].From != "bull" || recs[1].To != "bear" || recs[1].Reason != strategy.TrendReasonBearSMA {
				t.Errorf("records = %+v", recs)
			}
		})
	}
}

func setPrice(f *strategyFixture, p float64) {
	f.market.mu.Lock()
	defer f.market.mu.Unlock()
	f.market.price = p
}

// /api/status serves the view's SMA levels as "sma".
func TestAPI_StatusServesSMALevels(t *testing.T) {
	v := sampleView()
	v.SMA = strategy.SMAView{SMALevels: strategy.SMALevels{SMA9: 82756.2, SMA21: 83741.4, SwingHigh: 86206.5, SwingHighDate: "2026-10-04",
		SwingLow: 82619.5, SwingLowDate: "2026-10-08", Price: 82500, Trend: "bear", Reason: strategy.TrendReasonBreakdown}}
	h := api.New(staticView{v}, orders.NewWriterLogger(io.Discard, 0)).Handler()

	code, body := getJSON(t, h, "/api/status")
	if code != http.StatusOK {
		t.Fatalf("status code %d", code)
	}
	sma, ok := body["sma"].(map[string]any)
	if !ok || sma["sma9"] != 82756.2 || sma["sma21"] != 83741.4 || sma["swing_high"] != 86206.5 ||
		sma["swing_high_date"] != "2026-10-04" || sma["swing_low"] != 82619.5 || sma["swing_low_date"] != "2026-10-08" ||
		sma["trend"] != "bear" || sma["reason"] != "price < swing low" || sma["price"] != 82500.0 {
		t.Errorf("sma = %v", body["sma"])
	}
}

// The journal writes trend_changed lines the activity feed counts and serves.
func TestOrderLog_TrendRecord(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0)
	l.LogTrend(orders.TrendRecord{StrategyID: "s", Direction: "sma", From: "neutral", To: "bear",
		Reason: strategy.TrendReasonBearSMA, Spot: 82500, SMA9: 82756, SMA21: 83741, SwingHigh: 86206, SwingHighDate: "2026-10-04"})

	lines := decodeLogLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	m := lines[0]
	if m["event"] != "trend_changed" || m["from"] != "neutral" || m["to"] != "bear" || m["reason"] != strategy.TrendReasonBearSMA ||
		m["sma9"] != 82756.0 || m["swing_high_date"] != "2026-10-04" || m["timestamp"] == "" {
		t.Errorf("line = %v", m)
	}
	if l.EventCounts()["trend_changed"] != 1 || len(l.Recent(0, 0)) != 1 {
		t.Errorf("counts %v, recent %d", l.EventCounts(), len(l.Recent(0, 0)))
	}
}
