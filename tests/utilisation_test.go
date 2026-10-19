package tests

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// marginPoint is a history point with a margin view, as the bot records it.
func marginPoint(t time.Time, imPct, limitPct float64, allowed bool) history.Point {
	mm := imPct / 2
	return history.Point{T: t, IMPct: &imPct, IMLimitPct: &limitPct, MMPct: &mm, EntriesAllowed: &allowed}
}

// Scenario: utilisation is measured only while entries are allowed.
func TestUtilisation_MeasuredOnlyWhileEntriesAllowed(t *testing.T) {
	var pts []history.Point
	for i := 0; i < 120; i++ { // 2 h at IM 10 % of a 20 % limit, entries allowed
		pts = append(pts, marginPoint(t0.Add(time.Duration(i)*time.Minute), 10, 20, true))
	}
	for i := 120; i < 180; i++ { // then 1 h frozen at IM 2 %
		pts = append(pts, marginPoint(t0.Add(time.Duration(i)*time.Minute), 2, 20, false))
	}
	u := history.Utilisation(pts, t0, t0.Add(3*time.Hour))
	if u.MeanPct == nil || !near(*u.MeanPct, 50, 1e-9) {
		t.Errorf("utilisation = %v, want 50 %%", u.MeanPct)
	}
	if u.AllowedShare == nil || !near(*u.AllowedShare, 2.0/3, 1e-9) {
		t.Errorf("allowed share = %v, want 2/3", u.AllowedShare)
	}
	if !near(u.AllowedHours, 2, 1e-9) || !near(u.MeasuredHours, 3, 1e-9) {
		t.Errorf("hours allowed %v measured %v, want 2 and 3", u.AllowedHours, u.MeasuredHours)
	}
}

// Scenario: old history still loads, and utilisation skips it.
func TestUtilisation_OldHistoryStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnl_history.jsonl")
	old := `{"t":"2026-10-01T12:00:00Z","realised":0.001,"unrealised":0,"total":0.001,"spot":100000,"realised_usd":100,"total_usd":100}
{"t":"2026-10-01T12:01:00Z","realised":0.002,"unrealised":0,"total":0.002,"spot":100000,"realised_usd":200,"total_usd":200}
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	h := openHistory(t, path, t0.Add(time.Hour))
	defer h.Close()
	if pts := h.Range(t0.Add(-time.Hour), t0.Add(time.Hour), 1000); len(pts) != 2 || pts[1].Realised != 0.002 {
		t.Fatalf("old lines should load, got %+v", pts)
	}
	u := h.Utilisation(t0.Add(-time.Hour), t0.Add(time.Hour))
	if u.MeanPct != nil || u.AllowedShare != nil || u.MeasuredHours != 0 {
		t.Errorf("old lines carry no margin and must be skipped: %+v", u)
	}

	// A new point after them is measured on its own (until the window's end).
	h.RecordPnL(t0.Add(2*time.Minute), 0.002, 0, 100000,
		&history.Margin{Known: true, IMPct: 15, IMLimitPct: 20, MMPct: 5, EntriesAllowed: true})
	u = h.Utilisation(t0.Add(-time.Hour), t0.Add(7*time.Minute))
	if u.MeanPct == nil || !near(*u.MeanPct, 75, 1e-9) || !near(u.MeasuredHours, 5.0/60, 1e-9) {
		t.Errorf("new point: %+v", u)
	}
}

func TestUtilisation_EdgeCases(t *testing.T) {
	allowed := func(at time.Duration, im float64) history.Point { return marginPoint(t0.Add(at), im, 20, true) }

	// A gap longer than MaxSampleGap (the bot was down) counts only MaxSampleGap.
	u := history.Utilisation([]history.Point{allowed(0, 10), allowed(5*time.Hour, 10)}, t0, t0.Add(5*time.Hour))
	if !near(u.MeasuredHours, history.MaxSampleGap.Hours(), 1e-9) {
		t.Errorf("gap: measured %v h, want %v", u.MeasuredHours, history.MaxSampleGap.Hours())
	}

	// Points outside the window are ignored; the last one stops at `to`.
	u = history.Utilisation([]history.Point{allowed(-time.Hour, 20), allowed(0, 5), allowed(time.Minute, 15)}, t0, t0.Add(2*time.Minute))
	if u.MeanPct == nil || !near(*u.MeanPct, 50, 1e-9) || !near(u.MeasuredHours, 2.0/60, 1e-9) {
		t.Errorf("window: %+v", u)
	}

	// No points, or a zero limit while allowed: nothing to report.
	if u := history.Utilisation(nil, t0, t0.Add(time.Hour)); u.MeanPct != nil || u.AllowedShare != nil {
		t.Errorf("no points: %+v", u)
	}
	zero := marginPoint(t0, 10, 0, true)
	if u := history.Utilisation([]history.Point{zero}, t0, t0.Add(time.Minute)); u.MeanPct != nil || u.MeasuredHours != 0 {
		t.Errorf("zero limit: %+v", u)
	}

	// Margin unknown (the read failed): entries not allowed, no IM recorded.
	h := openHistory(t, filepath.Join(t.TempDir(), "pnl.jsonl"), t0)
	defer h.Close()
	h.RecordPnL(t0, 0, 0, 1, &history.Margin{Known: false, IMLimitPct: 20})
	pts := h.Range(t0.Add(-time.Minute), t0.Add(time.Minute), 10)
	if len(pts) != 1 || pts[0].IMPct != nil || pts[0].MMPct != nil || pts[0].EntriesAllowed == nil || *pts[0].EntriesAllowed {
		t.Fatalf("unknown margin point = %+v", pts)
	}
	if u := h.Utilisation(t0, t0.Add(time.Minute)); u.AllowedShare == nil || *u.AllowedShare != 0 || u.MeanPct != nil {
		t.Errorf("unknown margin counts as not allowed: %+v", u)
	}
}

// Scenario: the API reports it.
func TestAPI_StatusReportsUtilisation(t *testing.T) {
	h := openHistory(t, filepath.Join(t.TempDir(), "pnl.jsonl"), time.Now())
	defer h.Close()
	h.RecordPnL(time.Now().Add(-2*time.Minute), 0, 0, 100000,
		&history.Margin{Known: true, IMPct: 12, IMLimitPct: 20, MMPct: 4, EntriesAllowed: true})
	srv := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), api.WithPnLHistory(h)).Handler()

	_, body := getJSON(t, srv, "/api/status")
	for _, key := range []string{"utilisation_7d", "utilisation_30d"} {
		u, ok := body[key].(map[string]any)
		if !ok {
			t.Fatalf("%s missing: %v", key, body[key])
		}
		if mean, _ := u["mean_pct"].(float64); !near(mean, 60, 1e-6) || u["allowed_share"] != 1.0 {
			t.Errorf("%s = %v, want mean 60 %% allowed all the time", key, u)
		}
	}

	none := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	_, body = getJSON(t, none, "/api/status")
	for _, key := range []string{"utilisation_7d", "utilisation_30d"} {
		if v, ok := body[key]; !ok || v != nil {
			t.Errorf("no history: %s should be present and null, got %v (present %v)", key, v, ok)
		}
	}
}

func TestHistoryMargin_FromRiskView(t *testing.T) {
	now := t0
	rv := strategy.RiskView{Status: risk.Status{LimitIMPct: 20}, IMPct: 8, MMPct: 3, AsOf: now.Add(-time.Minute), EntriesAllowed: true}
	m := strategy.HistoryMargin(rv, now)
	if m == nil || !m.Known || m.IMPct != 8 || m.IMLimitPct != 20 || m.MMPct != 3 || !m.EntriesAllowed {
		t.Errorf("fresh view = %+v", m)
	}
	if strategy.HistoryMargin(strategy.RiskView{}, now) != nil {
		t.Error("no margin read yet → nil")
	}
	stale := rv
	stale.AsOf = now.Add(-time.Hour)
	if strategy.HistoryMargin(stale, now) != nil {
		t.Error("a stalled loop's old view → nil")
	}
	failed := strategy.RiskView{Status: risk.Status{LimitIMPct: 20}, Error: "timeout", AsOf: now}
	if m := strategy.HistoryMargin(failed, now); m == nil || m.Known || m.EntriesAllowed {
		t.Errorf("failed margin read = %+v", m)
	}
}

// The bot records the margin view the policy used with each P&L point, and
// entries_allowed follows what blocks entries (here the MM limit).
func TestStrategy_RecordsMarginWithPnLHistory(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ReportIntervalSec = 1
	f.exch.setMargin(1, 0.5) // IM 10 %, MM 5 % of a 10 BTC balance
	rec := &recordedPnL{}
	f.strat = strategy.New(f.cfg, strategy.Deps{
		Market: f.market, Exchange: f.exch, State: f.state, Journal: f.journal, Hedge: nopHedge{}, History: rec,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.strat.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	eventually(t, 3*time.Second, "point with margin", func() bool {
		p, ok := rec.last()
		return ok && p.IMPct != nil
	})
	p, _ := rec.last()
	rv := f.strat.View().Risk
	if !near(*p.IMPct, 10, 1e-9) || !near(*p.MMPct, 5, 1e-9) || *p.IMLimitPct != rv.Status.LimitIMPct || *p.IMLimitPct <= 0 {
		t.Errorf("recorded margin = im %v mm %v limit %v (view limit %v)", *p.IMPct, *p.MMPct, *p.IMLimitPct, rv.Status.LimitIMPct)
	}
	if !*p.EntriesAllowed || !rv.EntriesAllowed {
		t.Errorf("nothing blocks entries: recorded %v, view %v", *p.EntriesAllowed, rv.EntriesAllowed)
	}

	f.exch.setMargin(1, 4) // MM 40 % ≥ the 35 % limit: entries blocked
	eventually(t, 4*time.Second, "point with entries blocked", func() bool {
		p, ok := rec.last()
		return ok && p.MMPct != nil && *p.MMPct > 35 && p.EntriesAllowed != nil && !*p.EntriesAllowed
	})
}
