package tests

import (
	"compress/gzip"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

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
