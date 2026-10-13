package tests

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/recorder"
)

type fakeSummary struct {
	rows []gex.SummaryRow
	at   time.Time
}

func (f *fakeSummary) LatestSummary() ([]gex.SummaryRow, time.Time) { return f.rows, f.at }

func TestBlackScholesGreeks(t *testing.T) {
	c := recorder.BlackScholes("call", 100000, 110000, 45.0/365, 0.5)
	p := recorder.BlackScholes("put", 100000, 110000, 45.0/365, 0.5)
	if math.Abs(c.Delta-p.Delta-1) > 1e-12 {
		t.Errorf("call Δ − put Δ = %v, want 1 (parity)", c.Delta-p.Delta)
	}
	if c.Gamma != p.Gamma || c.Vega != p.Vega || c.Theta != p.Theta {
		t.Error("calls and puts of one strike share gamma, vega and theta on the forward")
	}
	if c.Delta < 0.25 || c.Delta > 0.40 || c.Theta >= 0 || c.Vega <= 0 {
		t.Errorf("10 %% OTM 45-day call at 50 vol: Δ %.3f Θ %.1f vega %.1f out of range", c.Delta, c.Theta, c.Vega)
	}
	atm := recorder.BlackScholes("call", 100000, 100000, 1e-6, 0.5)
	if math.Abs(atm.Delta-0.5) > 0.01 {
		t.Errorf("ATM delta near expiry = %v, want ≈ 0.5", atm.Delta)
	}
}

func TestRecorderRows(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	rows := recorder.Rows([]gex.SummaryRow{
		{InstrumentName: "BTC-27NOV26-100000-C", UnderlyingPrice: 86000, MarkIV: 38, BidPrice: 0.012, AskPrice: 0.013, MidPrice: 0.0125, MarkPrice: 0.0126, OpenInterest: 420},
		{InstrumentName: "BTC-6OCT26-90000-P", UnderlyingPrice: 86000, MarkIV: 40}, // expired
		{InstrumentName: "BTC-PERPETUAL", UnderlyingPrice: 86000, MarkIV: 40},      // not an option
		{InstrumentName: "BTC-27NOV26-80000-P", UnderlyingPrice: 86000},            // no mark IV
	}, at, 36.5)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (expired, non-option and IV-less rows skipped)", len(rows))
	}
	r := rows[0]
	if len(r) != len(recorder.Header) {
		t.Fatalf("columns = %d, want %d", len(r), len(recorder.Header))
	}
	want := map[string]string{"date": "2026-10-07T12:00:00Z", "instrument": "BTC-27NOV26-100000-C", "strike": "100000",
		"expiry": "2026-11-27", "option_type": "call", "bid": "0.012", "mark": "0.0126", "iv": "0.38", "open_interest": "420", "dvol_index": "36.5"}
	for i, col := range recorder.Header {
		if w, ok := want[col]; ok && r[i] != w {
			t.Errorf("%s = %q, want %q", col, r[i], w)
		}
	}
}

func TestRecorderSnapshotAppendsAndSkipsStale(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().UTC()
	src := &fakeSummary{rows: []gex.SummaryRow{{InstrumentName: "BTC-27NOV26-100000-C", UnderlyingPrice: 86000, MarkIV: 38, MidPrice: 0.0125}}, at: at}
	rec := recorder.New(src, func() float64 { return 36 }, dir, time.Hour)

	for i := 0; i < 2; i++ {
		if n, err := rec.Snapshot(at.Add(time.Minute)); err != nil || n != 1 {
			t.Fatalf("snapshot %d: n=%d err=%v", i, n, err)
		}
	}
	f, err := os.Open(filepath.Join(dir, at.Format("2006-01-02")+".csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f) // reads the appended members as one stream
	if err != nil {
		t.Fatal(err)
	}
	all, err := csv.NewReader(zr).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0][0] != "date" || all[1][1] != "BTC-27NOV26-100000-C" || all[2][1] != "BTC-27NOV26-100000-C" {
		t.Errorf("file = %v, want the header once and one row per snapshot", all)
	}

	if _, err := rec.Snapshot(at.Add(10 * time.Minute)); err == nil {
		t.Error("a 10-minute-old summary must not be recorded")
	}
}

type usdcSummaryCaller struct{ params any }

func (c *usdcSummaryCaller) Call(_ context.Context, method string, params any, _ int) (gateway.JSONRPCResponse, error) {
	c.params = params
	if method != "public/get_book_summary_by_currency" {
		return gateway.JSONRPCResponse{}, fmt.Errorf("unexpected method %s", method)
	}
	return gateway.JSONRPCResponse{Result: json.RawMessage(`[
		{"instrument_name":"BTC_USDC-27NOV26-96000-C","underlying_price":87000,"mark_iv":38,"bid_price":855,"ask_price":875,"mid_price":865,"mark_price":866,"open_interest":4},
		{"instrument_name":"BTC_USDC-27NOV26-73000-P","underlying_price":87000,"mark_iv":42,"bid_price":1020,"ask_price":1040,"mid_price":1030,"mark_price":1031,"open_interest":2.1},
		{"instrument_name":"ETH_USDC-27NOV26-3000-C","underlying_price":2500,"mark_iv":60,"mid_price":20},
		{"instrument_name":"SOL_USDC-27NOV26-200-C","underlying_price":150,"mark_iv":70,"mid_price":2}]`)}, nil
}

// 2026-10-13: the USDC-settled options are recorded beside the coin-settled
// ones (data/market_usdc) to compare their liquidity; only the bot's
// underlying is kept, prices as quoted (USDC).
func TestLinearBookRecordsTheUnderlyingsUSDCOptions(t *testing.T) {
	gw := &usdcSummaryCaller{}
	book := recorder.NewLinearBook(gw, "BTC")
	if rows, _ := book.LatestSummary(); len(rows) != 0 {
		t.Fatal("nothing before the first fetch")
	}
	if err := book.FetchSummary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, _ := gw.params.(map[string]any); p["currency"] != "USDC" || p["kind"] != "option" {
		t.Errorf("params = %v, want the USDC option summary", gw.params)
	}

	dir := t.TempDir()
	n, err := recorder.New(book, func() float64 { return 36 }, dir, time.Hour).Snapshot(time.Now())
	if err != nil || n != 2 {
		t.Fatalf("snapshot n=%d err=%v, want the 2 BTC_USDC options", n, err)
	}
	rows, at := book.LatestSummary()
	rec := recorder.Rows(rows, at, 36)
	col := func(name string) int {
		for i, c := range recorder.Header {
			if c == name {
				return i
			}
		}
		t.Fatalf("no column %s", name)
		return -1
	}
	if r := rec[0]; r[col("underlying")] != "BTC_USDC" || r[col("bid")] != "855" || r[col("strike")] != "96000" || r[col("option_type")] != "call" {
		t.Errorf("row = %v", r)
	}

	// Run fetches before each snapshot only for sources that fetch on demand:
	// the GEX manager refreshes itself, and an extra refresh would count as an
	// extra GEX snapshot.
	var src recorder.Source = gex.NewManager(gw, gex.Params{Underlying: "BTC"}, 0)
	if _, ok := src.(recorder.Fetcher); ok {
		t.Error("the GEX manager must not be refreshed by the recorder")
	}
}
