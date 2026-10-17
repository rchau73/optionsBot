package tests

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// cmd/stress -replay (#120): the recorded mainnet books (internal/recorder)
// read back and run through the bot's rules hour by hour.

// coinPrice is a Black–Scholes option price in the coin (USD ÷ spot).
func coinPrice(typ string, s, k, t, sigma float64) float64 {
	sq := sigma * math.Sqrt(t)
	d1 := (math.Log(s/k) + 0.5*sigma*sigma*t) / sq
	d2 := d1 - sq
	n := func(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }
	if typ == "call" {
		return (s*n(d1) - k*n(d2)) / s
	}
	return (k*n(-d2) - s*n(-d1)) / s
}

// synthBook is a BTC book at `at`: Friday expiries out to ~14 weeks, strikes
// ±40 % of spot every 2,000, IV 50 %, a ±2 % spread around the price.
func synthBook(at time.Time, spot float64) []gex.SummaryRow {
	var rows []gex.SummaryRow
	for w := 0; w < 20; w++ {
		exp := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC).AddDate(0, 0, 7*w) // Fridays
		if !exp.After(at) || exp.Sub(at) > 100*24*time.Hour {
			continue
		}
		t := exp.Sub(at).Hours() / 24 / 365
		for k := math.Round(spot*0.6/2000) * 2000; k <= spot*1.4; k += 2000 {
			for _, typ := range []string{"call", "put"} {
				p := coinPrice(typ, spot, k, t, 0.5)
				bid, ask := p*0.98, p*1.02
				if p < 0.0001 {
					bid = 0 // an empty side far out of the money
				}
				rows = append(rows, gex.SummaryRow{
					InstrumentName:  fmt.Sprintf("BTC-%s-%.0f-%s", strings.ToUpper(exp.Format("2Jan06")), k, strings.ToUpper(typ[:1])),
					UnderlyingPrice: spot, MarkIV: 50, BidPrice: bid, AskPrice: ask, MidPrice: (bid + ask) / 2, MarkPrice: p,
					OpenInterest: 100,
				})
			}
		}
	}
	return rows
}

// record writes hourly snapshots of path(hour) through the real recorder.
func record(t *testing.T, dir string, start time.Time, hours int, spot func(h int) float64) {
	t.Helper()
	src := &fakeSummary{}
	rec := recorder.New(src, func() float64 { return 50 }, dir, time.Hour)
	for h := 0; h < hours; h++ {
		src.at = start.Add(time.Duration(h) * time.Hour)
		src.rows = synthBook(src.at, spot(h))
		if _, err := rec.Snapshot(src.at); err != nil {
			t.Fatalf("hour %d: %v", h, err)
		}
	}
}

// Scenario: the reader returns every recorded snapshot, in time order, with
// each option's quote, the DVOL, and its settlement time.
func TestRecorderReadDir_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 22, 0, 0, 0, time.UTC) // spans two day files
	record(t, dir, start, 4, func(int) float64 { return 100000 })

	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 4 {
		t.Fatalf("snapshots = %d, want 4", len(snaps))
	}
	want := synthBook(start, 100000)
	for i, s := range snaps {
		if !s.At.Equal(start.Add(time.Duration(i) * time.Hour)) {
			t.Errorf("snapshot %d at %s, want hourly from %s", i, s.At, start)
		}
		if s.DVOL != 50 {
			t.Errorf("snapshot %d DVOL %v, want 50", i, s.DVOL)
		}
	}
	if len(snaps[0].Quotes) != len(want) {
		t.Fatalf("quotes = %d, want %d", len(snaps[0].Quotes), len(want))
	}
	q := snaps[0].ByName()[want[0].InstrumentName]
	if q.Bid != want[0].BidPrice || q.Ask != want[0].AskPrice || q.Mark != want[0].MarkPrice || q.OpenInterest != 100 ||
		q.IV != 0.5 || q.UnderlyingPrice != 100000 || q.Expiry.Hour() != 8 || q.Delta == 0 {
		t.Errorf("quote = %+v, want the recorded row (expiry at 08:00 UTC, greeks)", q)
	}
	if got := snaps[0].Spot(); got != 100000 {
		t.Errorf("spot = %v, want the nearest future 100000", got)
	}

	// The window: only the second day file.
	day2, _ := recorder.ReadDir(dir, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), time.Time{})
	if len(day2) != 2 || day2[0].At.Day() != 8 {
		t.Errorf("from 10-08: %d snapshots, want the 2 of that day", len(day2))
	}
}

// A crash while the recorder appended leaves a torn last member: the
// snapshots before it are read, the torn one is dropped.
func TestRecorderReadFile_TornLastSnapshot(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	record(t, dir, start, 2, func(int) float64 { return 100000 })
	path := filepath.Join(dir, "2026-10-09.csv.gz")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	fmt.Fprintf(zw, "2026-10-09T03:00:00Z,BTC-27NOV26-100000-C,BTC,100000,100000,2026-11-27,call,0.01,0.02,0.015,0.015,0.5,0,0,0,0.5,1,50\n")
	zw.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.Write(buf.Bytes()[:buf.Len()-6]) // the trailer never written
	f.Close()

	snaps, err := recorder.ReadFile(path)
	if err != nil {
		t.Fatalf("a torn last snapshot must not fail the read: %v", err)
	}
	if len(snaps) != 2 {
		t.Errorf("snapshots = %d, want the 2 complete ones", len(snaps))
	}
}

// The recorded book feeds gex.Build exactly as the live summary did, so the
// replay's regime and flip are the bot's own.
func TestRecorderSnapshot_GEXMatchesTheLiveSummary(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	record(t, dir, at, 1, func(int) float64 { return 100000 })
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	p := gex.Params{Underlying: "BTC", StrikeRangePct: 0.15, Method: gex.MethodScript}
	live, _, err1 := gex.Build(synthBook(at, 100000), at, p)
	replayed, _, err2 := gex.Build(snaps[0].SummaryRows(), at, p)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if live.Regime != replayed.Regime || live.GammaFlip != replayed.GammaFlip || live.Spot != replayed.Spot {
		t.Errorf("replayed GEX %s flip %.0f spot %.0f, live %s flip %.0f spot %.0f",
			replayed.Regime, replayed.GammaFlip, replayed.Spot, live.Regime, live.GammaFlip, live.Spot)
	}
}

// Scenario: pricing from the recording — the live mid as the bot reads a
// ticker, sells at the bot's price floor, buy-backs at the ask, Deribit fees.
func TestMarketReplay_PricingFromTheRecording(t *testing.T) {
	q := recorder.Quote{Bid: 0.010, Ask: 0.012, Mark: 0.0111}
	if got := q.LiveMid(); math.Abs(got-0.011) > 1e-12 {
		t.Errorf("mid = %v, want (bid+ask)/2", got)
	}
	if got := (recorder.Quote{Mark: 0.02}).LiveMid(); got != 0.02 {
		t.Errorf("empty book: mid = %v, want the mark", got)
	}
	cases := []struct {
		name string
		got  replay.Fill
		want replay.Fill
	}{
		{"entry steps down to the mid", replay.SellFill(q, "mid"), replay.Fill{Price: 0.011, OK: true}},
		{"repair crosses to the bid", replay.SellFill(q, "bid"), replay.Fill{Price: 0.010, OK: true}},
		{"no bid: nobody buys", replay.SellFill(recorder.Quote{Ask: 0.012}, "mid"), replay.Fill{}},
		{"bid only: sells at the bid", replay.SellFill(recorder.Quote{Bid: 0.01}, "mid"), replay.Fill{Price: 0.01, OK: true}},
		{"buy-back at the ask", replay.BuyFill(q), replay.Fill{Price: 0.012, OK: true}},
		{"no ask: no fill", replay.BuyFill(recorder.Quote{Bid: 0.01}), replay.Fill{}},
	}
	for _, c := range cases {
		if c.got.OK != c.want.OK || math.Abs(c.got.Price-c.want.Price) > 1e-12 {
			t.Errorf("%s: %+v, want %+v", c.name, c.got, c.want)
		}
	}
	if got := replay.Fee(0.01, 2); math.Abs(got-0.0006) > 1e-12 {
		t.Errorf("fee = %v, want 0.0003 × 2", got)
	}
	if got := replay.Fee(0.001, 1); math.Abs(got-0.000125) > 1e-12 {
		t.Errorf("cheap option: fee = %v, want 12.5 %% of the price", got)
	}
}

func replayConfig(t *testing.T, direction string) *config.Config {
	t.Helper()
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.GEXDirection = direction
	return cfg
}

// flatHistory seeds 40 days of flat closes and DVOL before start: a known IV
// percentile and a neutral SMA trend.
func flatHistory(start time.Time) (closes, dvol []orders.DailyClose) {
	for d := 40; d >= 1; d-- {
		day := start.Truncate(24*time.Hour).AddDate(0, 0, -d)
		closes = append(closes, orders.DailyClose{Date: day, Close: 100000})
		dvol = append(dvol, orders.DailyClose{Date: day, Close: 50})
	}
	return closes, dvol
}

// Scenario: the replay runs both direction sources over the same history
// and reports comparable summaries.
func TestMarketReplay_RunsBothDirectionsOnTheSameHistory(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 6*24, func(h int) float64 { return 100000 * (1 + 0.002*math.Sin(float64(h)/7)) })
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	closes, dvol := flatHistory(start)
	from := start.AddDate(0, 0, 3) // three recorded days confirm the regime first
	for _, direction := range []string{"sma", "bias"} {
		res, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, direction), From: from, Equity: 1, Closes: closes, DVOL: dvol})
		if err != nil {
			t.Fatalf("%s: %v", direction, err)
		}
		if !res.Start.Equal(from) || len(res.Days) != 3 || res.Snapshots != 72 {
			t.Errorf("%s: start %s, %d days, %d snapshots; want from %s, 3 days, 72 snapshots", direction, res.Start, len(res.Days), res.Snapshots, from)
		}
		if res.Entries != 2 || res.Trades < 4 || res.Fees <= 0 {
			t.Errorf("%s: entries %d trades %d fees %.5f, want both slots opened (4 legs) with fees", direction, res.Entries, res.Trades, res.Fees)
		}
		if math.Abs(res.Net-(res.Equity-res.Equity0)) > 1e-12 || res.OpenAtEnd != 4 {
			t.Errorf("%s: net %.5f, equity %.5f → %.5f, open legs %d", direction, res.Net, res.Equity0, res.Equity, res.OpenAtEnd)
		}
		// Selling at the mid and marking at the mid: a quiet book only loses
		// the fees at first, then earns the decay.
		if res.Net < -res.Fees-0.01 || res.MaxIMPct <= 0 || res.MaxIMPct > 20.5 {
			t.Errorf("%s: net %.5f (fees %.5f), IM max %.1f %% (limit 20 %%)", direction, res.Net, res.Fees, res.MaxIMPct)
		}
	}
}

// A fall of a quarter in two days: the exit rules fire on the recorded
// marks and buy back at the recorded asks, at a loss.
func TestMarketReplay_CrashTriggersExits(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 6*24, func(h int) float64 {
		if h < 4*24 {
			return 100000
		}
		return 100000 * (1 - 0.25*math.Min(1, float64(h-4*24)/48))
	})
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	res, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 1, Closes: closes, DVOL: dvol})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stops+res.DeltaExits+res.Sheds == 0 || res.Realised >= 0 || res.MaxDrawdown <= 0 {
		t.Errorf("stops %d delta exits %d sheds %d realised %.4f drawdown %.4f: the crash must close the puts at a loss",
			res.Stops, res.DeltaExits, res.Sheds, res.Realised, res.MaxDrawdown)
	}
}

// Scenario: check against reality — the journal's P&L over the window is its
// realised closes plus the change of its open book's unrealised P&L, both
// ends marked at the recorded mids.
func TestMarketReplay_ActualPnLFromTheJournal(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	call, put := "BTC-27NOV26-120000-C", "BTC-27NOV26-80000-P"
	quote := func(name string, bid, ask float64) recorder.Quote {
		return recorder.Quote{Instrument: name, Bid: bid, Ask: ask}
	}
	snaps := []recorder.Snapshot{
		{At: at(12), Quotes: []recorder.Quote{quote(call, 0.009, 0.011), quote(put, 0.019, 0.021)}},
		{At: at(14), Quotes: []recorder.Quote{quote(call, 0.004, 0.006), quote(put, 0.029, 0.031)}},
	}
	trades := []orders.Trade{
		{At: at(10), Kind: "open", Instrument: call, Qty: 1, Price: 0.012},                 // open before: unrealised at both ends
		{At: at(11), Kind: "open", Instrument: put, Qty: 1, Price: 0.015},                  // closed inside: realised
		{At: at(13), Kind: "close", Instrument: put, Qty: 1, PnL: -0.0151},                 //
		{At: at(13), Kind: "open", Instrument: "BTC-27NOV26-78000-P", Qty: 1, Price: 0.02}, // no quote at the end: valued at its premium
		{At: at(15), Kind: "close", Instrument: call, Qty: 1, PnL: 0.007},                  // after the window
	}
	a := replay.ActualPnL(trades, snaps, at(12), at(14))
	// call: 0.012 − 0.010 → 0.012 − 0.005; put: 0.015 − 0.020 at the start
	want := replay.Actual{Realised: -0.0151, Unrealised0: 0.002 - 0.005, Unrealised1: 0.007, Opens: 1, Closes: 1, Unmarked: 1}
	want.Net = want.Realised + want.Unrealised1 - want.Unrealised0
	if math.Abs(a.Realised-want.Realised) > 1e-12 || math.Abs(a.Unrealised0-want.Unrealised0) > 1e-12 ||
		math.Abs(a.Unrealised1-want.Unrealised1) > 1e-12 || math.Abs(a.Net-want.Net) > 1e-12 ||
		a.Opens != want.Opens || a.Closes != want.Closes || a.Unmarked != want.Unmarked {
		t.Errorf("actual = %+v\nwant     %+v", a, want)
	}
}

// Scenario: start from the book at a date — the journal's strangle holds
// its slot (only the other slot enters), and its P&L counts from the mid.
func TestMarketReplay_StartsFromABook(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 4*24, func(int) float64 { return 100000 })
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	from := start.AddDate(0, 0, 3)
	q := snaps[3*24].ByName()
	slot := config.StrangleSlot{TargetDTE: 25, EntryDelta: 0.16}
	var book []replay.StartLeg
	for _, name := range []string{"BTC-30OCT26-112000-C", "BTC-30OCT26-90000-P"} {
		if _, ok := q[name]; !ok {
			t.Fatalf("%s not in the recording", name)
		}
		book = append(book, replay.StartLeg{Slot: slot, Instrument: name, Qty: 1, Price: q[name].LiveMid()})
	}
	res, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: from, Equity: 1, Closes: closes, DVOL: dvol, Book: book})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries != 1 || res.OpenAtEnd != 4 {
		t.Errorf("entries %d, open legs %d: want only the 50-day slot entered and the journal's two legs kept", res.Entries, res.OpenAtEnd)
	}
	if res.Net < -res.Fees-0.005 {
		t.Errorf("net %.5f: the starting book is sold at the mid it is marked at, so only fees and decay move it", res.Net)
	}
}

// Start from the journal's book: opens and partial closes before the date.
func TestMarketReplay_BookAtFromTheJournal(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	trades := []orders.Trade{
		{At: at(10), Kind: "open", Slot: slot, Instrument: "BTC-27NOV26-120000-C", Qty: 1, Price: 0.010},
		{At: at(10), Kind: "open", Slot: slot, Instrument: "BTC-27NOV26-120000-C", Qty: 1, Price: 0.012},
		{At: at(11), Kind: "close", Slot: slot, Instrument: "BTC-27NOV26-120000-C", Qty: 0.5, PnL: 0.001},
		{At: at(11), Kind: "open", Slot: slot, Instrument: "BTC-27NOV26-80000-P", Qty: 2, Price: 0.02},
		{At: at(12), Kind: "close", Slot: slot, Instrument: "BTC-27NOV26-80000-P", Qty: 2, PnL: -0.01},
		{At: at(13), Kind: "open", Slot: slot, Instrument: "BTC-27NOV26-78000-P", Qty: 1, Price: 0.02},
	}
	book := replay.BookAt(trades, at(12).Add(time.Minute))
	if len(book) != 1 || book[0].Instrument != "BTC-27NOV26-120000-C" || book[0].Qty != 1.5 ||
		math.Abs(book[0].Price-0.011) > 1e-12 || book[0].Slot != (config.StrangleSlot{TargetDTE: 25, EntryDelta: 0.16}) {
		t.Errorf("book = %+v, want 1.5 of the call at its average 0.011 in slot 25/0.16", book)
	}
}
