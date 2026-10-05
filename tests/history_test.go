package tests

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func openHistory(t *testing.T, path string, now time.Time) *history.Store {
	t.Helper()
	h, err := history.Open(path, 30*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHistory_SurvivesRestartWithoutDoubleCountingRealised(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "pnl_history.jsonl") // dir is created
	h := openHistory(t, path, t0)
	h.RecordPnL(t0, 0.001, 0.0005, 100000)
	h.RecordPnL(t0.Add(time.Minute), 0.002, -0.001, 100000)
	h.Close()

	// After a restart the bot restores its realised P&L from the journal
	// (0.002) and adds a new close (0.0005): it passes the cumulative 0.0025.
	// The store must not add its own last point on top (it once did).
	h = openHistory(t, path, t0.Add(time.Hour))
	defer h.Close()
	h.RecordPnL(t0.Add(2*time.Minute), 0.0025, 0, 100000)

	pts := h.Range(t0.Add(-time.Hour), t0.Add(time.Hour), 1000)
	if len(pts) != 3 {
		t.Fatalf("want 3 points after reload, got %d", len(pts))
	}
	last := pts[2]
	if !near(last.Realised, 0.0025, 1e-12) || !near(last.Total, 0.0025, 1e-12) || !near(last.TotalUSD, 250, 1e-6) {
		t.Errorf("realised is the bot's cumulative figure, not doubled: %+v", last)
	}
	if !near(pts[1].Total, 0.001, 1e-12) || !near(pts[1].RealisedUSD, 200, 1e-6) {
		t.Errorf("point 2 = %+v", pts[1])
	}
}

func TestHistory_ArchiveMovesNeverDeletes(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "orders.log")
	pnl := filepath.Join(dir, "data", "pnl_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(pnl), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(journal, []byte("journal\n"), 0o644)
	os.WriteFile(pnl, []byte("pnl\n"), 0o644)
	arch := filepath.Join(dir, "data", "archive", "20261005T120000Z")

	moved, err := history.Archive([]string{journal, pnl, filepath.Join(dir, "missing.log")}, arch)
	if err != nil || len(moved) != 2 {
		t.Fatalf("moved %v, err %v (a missing file is skipped)", moved, err)
	}
	for _, p := range []string{journal, pnl} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should have moved", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(arch, "orders.log")); string(b) != "journal\n" {
		t.Errorf("archived journal content = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(arch, "pnl_history.jsonl")); string(b) != "pnl\n" {
		t.Errorf("archived pnl content = %q", b)
	}
}

func TestHistory_SkipsCorruptLinesAndOldPoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnl.jsonl")
	h := openHistory(t, path, t0)
	h.RecordPnL(t0.AddDate(0, 0, -60), 1, 0, 1) // older than maxAge (30 d) at the next load
	h.RecordPnL(t0, 2, 0, 1)
	h.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("not json\n{\"t\":\"2026-10-01T12:0") // garbage + a torn last line
	f.Close()

	h = openHistory(t, path, t0)
	defer h.Close()
	pts := h.Range(t0.AddDate(-1, 0, 0), t0.Add(time.Hour), 1000)
	if len(pts) != 1 || pts[0].Realised != 2 {
		t.Errorf("want only the recent valid point, got %+v", pts)
	}
}

func TestHistory_TrimsMemoryByAge(t *testing.T) {
	h := openHistory(t, filepath.Join(t.TempDir(), "pnl.jsonl"), t0)
	defer h.Close()
	h.RecordPnL(t0, 1, 0, 1)
	h.RecordPnL(t0.AddDate(0, 0, 31), 1, 0, 1) // the first point is now older than 30 d
	if pts := h.Range(t0.AddDate(0, 0, -1), t0.AddDate(0, 0, 32), 1000); len(pts) != 1 {
		t.Errorf("old point should be trimmed, got %d points", len(pts))
	}
}

func TestBucket_KeepsLastPointPerAlignedBucket(t *testing.T) {
	// One point every 10 s for 2 minutes; 4 buckets over 2 minutes = 30 s each.
	var pts []history.Point
	for i := 0; i <= 12; i++ {
		pts = append(pts, history.Point{T: t0.Add(time.Duration(i) * 10 * time.Second), Total: float64(i)})
	}
	out := history.Bucket(pts, t0, t0.Add(2*time.Minute), 4)
	// Buckets start at :00, :30, 1:00, 1:30, 2:00 (aligned to the clock); last values 2, 5, 8, 11, 12.
	want := []float64{2, 5, 8, 11, 12}
	if len(out) != len(want) {
		t.Fatalf("got %d buckets: %+v", len(out), out)
	}
	for i, w := range want {
		if out[i].Total != w || out[i].T.Unix()%30 != 0 {
			t.Errorf("bucket %d = %v at %v, want %v at a 30 s boundary", i, out[i].Total, out[i].T, w)
		}
	}
	if len(history.Bucket(pts, t0, t0, 4)) != 0 || len(history.Bucket(nil, t0, t0.Add(time.Hour), 4)) != 0 {
		t.Error("empty range or no points → no buckets")
	}
}

func TestBucketWidth(t *testing.T) {
	tests := []struct {
		span    time.Duration
		buckets int
		want    time.Duration
	}{
		{15 * time.Minute, 300, 3 * time.Second},
		{24 * time.Hour, 300, 288 * time.Second},
		{time.Minute, 300, time.Second},            // never below 1 s
		{1000 * time.Second, 300, 4 * time.Second}, // 3.33 s rounded up
	}
	for _, tc := range tests {
		if got := history.BucketWidth(tc.span, tc.buckets); got != tc.want {
			t.Errorf("BucketWidth(%v, %d) = %v, want %v", tc.span, tc.buckets, got, tc.want)
		}
	}
}

func TestAPI_PnLHistory(t *testing.T) {
	h := openHistory(t, filepath.Join(t.TempDir(), "pnl.jsonl"), time.Now())
	defer h.Close()
	h.RecordPnL(time.Now().Add(-2*time.Hour), 0.001, 0, 100000)
	h.RecordPnL(time.Now().Add(-time.Minute), 0.002, 0.001, 100000)
	srv := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), api.WithPnLHistory(h)).Handler()

	_, day := getJSON(t, srv, "/api/pnl/history?range=1d")
	if n := len(day["points"].([]any)); n != 2 || day["bucket_sec"].(float64) != 288 {
		t.Errorf("1d: %d points, bucket %v", n, day["bucket_sec"])
	}
	_, hour := getJSON(t, srv, "/api/pnl/history?range=1h")
	if n := len(hour["points"].([]any)); n != 1 {
		t.Errorf("1h should only include the recent point, got %d", n)
	}
	if code, _ := getJSON(t, srv, "/api/pnl/history?range=2y"); code != http.StatusBadRequest {
		t.Errorf("unknown range → %d, want 400", code)
	}

	none := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	if _, body := getJSON(t, none, "/api/pnl/history"); len(body["points"].([]any)) != 0 {
		t.Error("no history configured → empty list")
	}
}

type recordedPnL struct {
	mu     sync.Mutex
	points []history.Point
}

func (r *recordedPnL) RecordPnL(t time.Time, realised, unrealised, spot float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points = append(r.points, history.Point{T: t, Realised: realised, Unrealised: unrealised, Spot: spot})
}

func TestStrategy_RecordsPnLHistoryEachInterval(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ReportIntervalSec = 1
	f.withOpenStrangle(0.1, 0.02)
	rec := &recordedPnL{}
	f.strat = strategy.New(f.cfg, strategy.Deps{
		Market: f.market, Exchange: f.exch, State: f.state, Journal: f.journal, Hedge: nopHedge{}, History: rec,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.strat.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	eventually(t, 3*time.Second, "history point recorded", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.points) >= 1
	})
	rec.mu.Lock()
	p := rec.points[0]
	rec.mu.Unlock()
	if p.Spot != 100000 || p.Realised != 0 {
		t.Errorf("recorded point = %+v", p)
	}
}
