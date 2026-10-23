package tests

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/perf"
)

func imPoint(t time.Time, total float64, im *float64) history.Point {
	return history.Point{T: t, Total: total, Spot: 100000, IMPct: im}
}

func f64(v float64) *float64 { return &v }

// Scenario: return on margin over a week — given history points with im_pct
// and a known starting equity, return on margin = net P&L ÷ (time-weighted
// mean im_pct × equity), in %, and the measured hours are reported.
func TestPerf_ReturnOnMarginOverAWeek(t *testing.T) {
	// One day of points every 15 min: IM 10 % for the first 12 h, 30 % after.
	var pts []history.Point
	for i := 0; i < 96; i++ {
		im := 10.0
		if i >= 48 {
			im = 30
		}
		pts = append(pts, imPoint(perfDay0.Add(time.Duration(i)*15*time.Minute), 0.01*float64(i)/95, f64(im)))
	}
	from, to := perfDay0, perfDay0.Add(24*time.Hour)

	m := perf.ReturnOnMargin(pts, 2, from, to)
	if !m.Known || m.Reason != "" {
		t.Fatalf("not known: %q", m.Reason)
	}
	if !perfNear(m.MeanIMPct, 20) || !perfNear(m.MeanIM, 0.4) || !perfNear(m.NetPnL, 0.01) {
		t.Errorf("mean IM %.4f %% = %.4f coin, net %.6f; want 20 %%, 0.4, 0.01", m.MeanIMPct, m.MeanIM, m.NetPnL)
	}
	if !perfNear(m.ReturnPct, 2.5) || !perfNear(m.Return30dPct, 75) || !perfNear(m.MeasuredHours, 24) {
		t.Errorf("return %.4f %% (30 d %.4f %%) over %.2f h; want 2.5 %%, 75 %%, 24 h", m.ReturnPct, m.Return30dPct, m.MeasuredHours)
	}
	// The net P&L is Summarize's.
	if s := perf.Summarize(pts, 2, from, to); !perfNear(s.NetPnL, m.NetPnL) {
		t.Errorf("net %.6f differs from Summarize's %.6f", m.NetPnL, s.NetPnL)
	}
}

// Scenario: gaps are not filled — a 6-hour gap with no point counts at most
// MaxSampleGap toward the mean, as in history.Utilisation.
func TestPerf_ReturnOnMarginGapsNotFilled(t *testing.T) {
	pts := []history.Point{
		imPoint(perfDay0, 0, f64(10)),
		imPoint(perfDay0.Add(6*time.Hour), 0.001, f64(40)),
	}
	m := perf.ReturnOnMargin(pts, 1, perfDay0, perfDay0.Add(6*time.Hour+history.MaxSampleGap))
	// Each point stands for 15 min: (10 + 40) ÷ 2, not (10×6 h + 40×0.25 h) ÷ 6.25 h.
	if !perfNear(m.MeanIMPct, 25) || !perfNear(m.MeasuredHours, 0.5) {
		t.Errorf("mean IM %.4f %% over %.2f h; want 25 %% over 0.5 h", m.MeanIMPct, m.MeasuredHours)
	}
	// The 30-day rate is scaled by the time the net covers (6 h + the last
	// point's 15 min), not by the 0.5 measured hours.
	if !perfNear(m.ReturnPct, 0.4) || !perfNear(m.SpanHours, 6.25) || !perfNear(m.Return30dPct, 0.4*30/(6.25/24)) {
		t.Errorf("return %.4f %% (30 d %.4f over %.2f h); want 0.4 %% over 6.25 h", m.ReturnPct, m.Return30dPct, m.SpanHours)
	}
}

// Review #282: a week with a 2-day outage — the net covers all 7 days, so the
// 30-day rate is scaled by 7 days (as the replay's whole window), not by the
// 5 days measured.
func TestPerf_ReturnOnMarginOutageKeepsThirtyDayBase(t *testing.T) {
	var pts []history.Point
	for i := 0; i <= 7*24*4; i++ {
		at := perfDay0.Add(time.Duration(i) * 15 * time.Minute)
		if at.After(perfDay0.Add(48*time.Hour)) && at.Before(perfDay0.Add(96*time.Hour)) {
			continue // down from day 2 to day 4
		}
		pts = append(pts, imPoint(at, 0.07*float64(i)/(7*24*4), f64(10)))
	}
	m := perf.ReturnOnMargin(pts, 1, perfDay0, perfDay0.Add(7*24*time.Hour))
	if !m.Known || !perfNear(m.ReturnPct, 70) {
		t.Fatalf("known %v return %.4f %% (%s); want 70 %%", m.Known, m.ReturnPct, m.Reason)
	}
	if !perfNear(m.SpanHours, 7*24) || !perfNear(m.Return30dPct, 70*30.0/7) {
		t.Errorf("30 d %.4f %% over %.2f h; want %.4f %% over 168 h", m.Return30dPct, m.SpanHours, 70*30.0/7)
	}
	if m.MeasuredHours > 5*24+0.5 || m.MeasuredHours < 5*24-0.5 {
		t.Errorf("measured %.2f h, want about 5 days", m.MeasuredHours)
	}
}

// Scenario: unknown inputs — no point with im_pct, unknown equity or a zero
// mean IM: the figure is not known and the reason says why.
func TestPerf_ReturnOnMarginUnknownInputs(t *testing.T) {
	day := []history.Point{imPoint(perfDay0, 0, f64(10)), imPoint(perfDay0.Add(10*time.Minute), 0.001, f64(10))}
	cases := []struct {
		name   string
		pts    []history.Point
		equity float64
		reason string
	}{
		{"no point carries im_pct", []history.Point{imPoint(perfDay0, 0, nil), imPoint(perfDay0.Add(time.Minute), 0.001, nil)}, 1, "no point carries im_pct"},
		{"equity unknown", day, 0, "margin balance at the window start unknown"},
		{"mean IM zero", []history.Point{imPoint(perfDay0, 0, f64(0)), imPoint(perfDay0.Add(time.Minute), 0.001, f64(0))}, 1, "mean IM used is zero"},
		{"no point", nil, 1, "no P&L history point in the window"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := perf.ReturnOnMargin(c.pts, c.equity, perfDay0, perfDay0.Add(time.Hour))
			if m.Known || m.Reason != c.reason || m.ReturnPct != 0 {
				t.Errorf("known %v reason %q return %.4f; want unknown with %q", m.Known, m.Reason, m.ReturnPct, c.reason)
			}
		})
	}
	// Points without im_pct are skipped, the others still count.
	mixed := []history.Point{imPoint(perfDay0, 0, nil), imPoint(perfDay0.Add(5*time.Minute), 0.001, f64(10))}
	if m := perf.ReturnOnMargin(mixed, 1, perfDay0, perfDay0.Add(time.Hour)); !m.Known || !perfNear(m.MeasuredHours, 0.25) {
		t.Errorf("mixed: known %v over %.2f h (%s); want known over 0.25 h", m.Known, m.MeasuredHours, m.Reason)
	}
}

// Scenario: the evidence report shows live and replay side by side — given
// the replay ran for the week, the report shows both return-on-margin
// figures with their sources and the approximation label.
func TestEvidence_ReturnOnMarginLiveAndReplay(t *testing.T) {
	r := evidence.Build(evInputs(t, evRecord(t)))
	if !r.Replay.Ran {
		t.Fatalf("replay did not run: %s", r.Replay.Note)
	}
	l := r.ROM.Live
	// Hourly points at IM 10 % of 1 coin, net 0.0167 → 16.7 %; each point stands for 15 min.
	if !l.Known || !perfNear(l.MeanIMPct, 10) || !perfNear(l.ReturnPct, 16.7) || !perfNear(l.MeasuredHours, 7*24*0.25) {
		t.Errorf("live: %+v", l)
	}
	rp := r.ROM.Replay
	if rp == nil {
		t.Fatal("no replay figure though the replay ran")
	}
	want := perf.ReturnOnMarginOf(r.Replay.Net, r.Replay.AvgIMPct, 1, r.Replay.End.Sub(r.Replay.Start).Hours())
	if *rp != want {
		t.Errorf("replay: %+v, want %+v", *rp, want)
	}
	if r.ROM.Label != perf.MarginApprox {
		t.Errorf("label %q", r.ROM.Label)
	}

	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, w := range []string{"## Return on margin (north star)\n\n" + perf.MarginApprox,
		"- live (data/btc/data/pnl_history.jsonl (net P&L ÷ time-weighted mean im_pct × equity)): **+16.70 %**",
		"- replay (the replay's net ÷ its average IM % (MODEL margin) × equity):"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}

	// Without a replay or history: each side says why.
	in := evInputs(t, nil)
	in.Points = nil
	r = evidence.Build(in)
	if r.ROM.Replay != nil || r.ROM.Live.Known {
		t.Errorf("empty inputs: %+v", r.ROM)
	}
	b.Reset()
	_ = evidence.WriteMarkdown(&b, r)
	for _, w := range []string{"not known — no P&L history point in the window", "- replay: not run"} {
		if !strings.Contains(b.String(), w) {
			t.Errorf("markdown lacks %q", w)
		}
	}
}
