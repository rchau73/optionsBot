package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/perf"
)

// cmd/evidence -track-record (#279) and -trend (#323): the weekly reports
// joined up, a week without a report marked, never recomputed.

// trackPoints are hourly P&L history points from 2026-10-06 00:00 to
// 2026-10-21 23:00 (ISO weeks W41, W42, W43), skipping the given days of
// October; the total rises 0.0001 an hour, spot 100,000 + 10/h.
func trackPoints(skip ...int) []history.Point {
	var pts []history.Point
	start := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 16*24; h++ {
		at := start.Add(time.Duration(h) * time.Hour)
		if contains(skip, at.Day()) {
			continue
		}
		im, limit, allowed := 10.0, 20.0, true
		pts = append(pts, history.Point{T: at, Total: 0.0001 * float64(h), Spot: 100000 + 10*float64(h),
			IMPct: &im, IMLimitPct: &limit, EntriesAllowed: &allowed})
	}
	return pts
}

// trackWeekly is the weekly report of week built the way cmd/evidence does
// (no replay: no config), with the given starting equity.
func trackWeekly(t *testing.T, pts []history.Point, week string, equity float64) evidence.Report {
	t.Helper()
	w, err := evidence.ParseWeek(week)
	if err != nil {
		t.Fatal(err)
	}
	return evidence.Build(evidence.Inputs{Env: "stable", Bot: "btc", Window: w, Points: pts, History: "h.jsonl",
		Equity: equity, EquitySource: "account.json"})
}

func TestTrackRecord_SeveralWeeks(t *testing.T) {
	pts := trackPoints()
	w41, w43 := trackWeekly(t, pts, "2026-W41", 1), trackWeekly(t, pts, "2026-W43", 1.2)
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Env: "stable", Bot: "btc", Account: "testnet",
		Points: pts, History: "h.jsonl", Weekly: []evidence.Report{w43, w41}})

	first, last := pts[0].T, pts[len(pts)-1].T
	if !tr.Known || !tr.From.Equal(first) || !tr.To.Equal(last) {
		t.Fatalf("span %v → %v, want %v → %v", tr.From, tr.To, first, last)
	}
	// The whole-span figures are perf.Summarize's over first → last point,
	// equity from the earliest weekly report that carries one.
	want := perf.Summarize(pts, 1, first, last)
	if tr.Equity != 1 || tr.Perf.NetPnL != want.NetPnL || tr.Perf.ReturnPct != want.ReturnPct ||
		tr.Perf.HoldReturnUSDPct != want.HoldReturnUSDPct || tr.Perf.MaxDrawdown != want.MaxDrawdown || !tr.Perf.WorstWeekKnown {
		t.Fatalf("whole span %+v, want %+v", tr.Perf, want)
	}
	if !strings.Contains(tr.EquitySource, "2026-W41") {
		t.Fatalf("equity source %q: names the week it comes from", tr.EquitySource)
	}
	if !tr.ROM.Known || tr.Util.MeanPct == nil {
		t.Fatalf("return on margin %+v, utilisation %+v", tr.ROM, tr.Util)
	}

	if len(tr.Weeks) != 3 {
		t.Fatalf("%d rows, want 3: %+v", len(tr.Weeks), tr.Weeks)
	}
	for i, wk := range []string{"2026-W41", "2026-W42", "2026-W43"} {
		if tr.Weeks[i].Week != wk {
			t.Fatalf("row %d is %s, want %s", i, tr.Weeks[i].Week, wk)
		}
	}
	if r := tr.Weeks[1]; r.Reported || r.Note != evidence.NotReported || r.Net != nil || r.NetPct != nil || r.HoldPct != nil {
		t.Fatalf("W42 without a report: %+v (marked, nothing recomputed)", r)
	}
	// W43's row is its report's figures as written.
	r := tr.Weeks[2]
	if !r.Reported || r.Net == nil || *r.Net != w43.Live.Perf.NetPnL || r.NetPct == nil || *r.NetPct != w43.Live.Perf.ReturnPct ||
		r.HoldPct == nil || *r.HoldPct != w43.Hold.HoldReturnUSDPct || r.ExcessPP == nil || *r.ExcessPP != w43.Hold.ExcessReturnPct ||
		r.ROMLivePct == nil || *r.ROMLivePct != w43.ROM.Live.ReturnPct || r.UtilPct == nil {
		t.Fatalf("W43 row %+v from report %+v", r, w43.Live.Perf)
	}
	if r.ROMReplPct != nil || r.GapPct != nil || len(r.NotMeasured) != 0 {
		t.Fatalf("no replay ran: replay cells unknown, not 'not measured': %+v", r)
	}
	c := tr.Counts
	if c.Reported != 2 || c.NotReported != 1 || c.Lost != 0 || c.LongestLosing != 0 || c.HoldKnown != 2 {
		t.Fatalf("counts %+v", c)
	}

	var b bytes.Buffer
	if err := evidence.WriteTrackMarkdown(&b, tr); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, s := range []string{evidence.TrackDisclaimer, "since inception", "| 2026-W42 | not reported |", "| 2026-W43 |", "Account: testnet"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}
}

func TestTrackRecord_HolesListedNotFilled(t *testing.T) {
	pts := trackPoints(9, 14)
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Bot: "btc", Points: pts,
		Weekly: []evidence.Report{trackWeekly(t, pts, "2026-W41", 1)}})
	if got := strings.Join(tr.MissingDays, ","); got != "2026-10-09,2026-10-14" {
		t.Fatalf("missing days %q", got)
	}
	if tr.Perf.Points != len(pts) {
		t.Fatalf("%d points used, %d recorded: nothing interpolated", tr.Perf.Points, len(pts))
	}
	if got := strings.Join(tr.Weeks[0].MissingDays, ","); got != "2026-10-05,2026-10-09" { // W41 starts on the 5th, before the history
		t.Fatalf("W41 row missing days %q (from its report)", got)
	}
	var b bytes.Buffer
	_ = evidence.WriteTrackMarkdown(&b, tr)
	if !strings.Contains(b.String(), "2026-10-09, 2026-10-14") {
		t.Fatalf("markdown does not list the missing days:\n%s", b.String())
	}
}

func TestTrackRecord_UnknownEquity(t *testing.T) {
	pts := trackPoints()
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Bot: "btc", Points: pts,
		Weekly: []evidence.Report{trackWeekly(t, pts, "2026-W41", 0), trackWeekly(t, pts, "2026-W43", 0)}})
	if tr.Equity != 0 || tr.Perf.EquityKnown {
		t.Fatalf("equity %v known=%v", tr.Equity, tr.Perf.EquityKnown)
	}
	if math.Abs(tr.Perf.NetPnL-(pts[len(pts)-1].Total-pts[0].Total)) > 1e-12 {
		t.Fatalf("coin net %v still shown", tr.Perf.NetPnL)
	}
	for _, r := range tr.Weeks {
		if r.Reported && (r.Net == nil || r.NetPct != nil || r.NetUSDPct != nil || r.ExcessPP != nil || r.MaxDDPct != nil) {
			t.Fatalf("%s: coin shown, every %% of equity unknown: %+v", r.Week, r)
		}
	}
	var b bytes.Buffer
	_ = evidence.WriteTrackMarkdown(&b, tr)
	md := b.String()
	if !strings.Contains(md, "Equity at the start: **unknown**") || !strings.Contains(md, "unknown % of equity") ||
		!strings.Contains(md, "| unknown |") {
		t.Fatalf("markdown does not say unknown:\n%s", md)
	}
}

func TestTrackRecord_NoHistory(t *testing.T) {
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Bot: "btc"})
	if tr.Known || len(tr.Weeks) != 0 || tr.ROM.Known {
		t.Fatalf("%+v", tr)
	}
	var b bytes.Buffer
	if err := evidence.WriteTrackMarkdown(&b, tr); err != nil || !strings.Contains(b.String(), "No P&L history point") {
		t.Fatalf("%v %s", err, b.String())
	}
}

func TestTrackRecord_LosingStreak(t *testing.T) {
	// Five weeks: lost, lost, not reported, lost, won.
	var pts []history.Point
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) // Monday of W40
	slope := []float64{-1, -1, -1, -1, 1}
	total := 0.0
	for d := 0; d < 35; d++ {
		for h := 0; h < 24; h++ {
			total += 0.0001 * slope[d/7]
			pts = append(pts, history.Point{T: start.Add(time.Duration(d*24+h) * time.Hour), Total: total, Spot: 100000})
		}
	}
	var weekly []evidence.Report
	for _, wk := range []string{"2026-W40", "2026-W41", "2026-W43", "2026-W44"} {
		weekly = append(weekly, trackWeekly(t, pts, wk, 1))
	}
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Bot: "btc", Points: pts, Weekly: weekly})
	c := tr.Counts
	if len(tr.Weeks) != 5 || c.Lost != 3 || c.LongestLosing != 2 || c.Reported != 4 || c.NotReported != 1 || c.BeatHold != 1 {
		t.Fatalf("counts %+v over %d weeks", c, len(tr.Weeks))
	}
	if c.WorstWeek == "" || c.WorstWeekPct == nil || *c.WorstWeekPct >= 0 {
		t.Fatalf("worst week %q %v", c.WorstWeek, c.WorstWeekPct)
	}
}

// trackClone writes a clone with a P&L history and the weekly reports of
// weeks; it returns its root.
func trackClone(t *testing.T, pts []history.Point, weeks ...string) string {
	t.Helper()
	root := t.TempDir()
	hist := filepath.Join(root, "data", "btc", "data", "pnl_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(hist), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for _, p := range pts {
		line, _ := json.Marshal(p)
		b.Write(append(line, '\n'))
	}
	if err := os.WriteFile(hist, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, wk := range weeks {
		md, js := evidence.OutPaths(root, wk, "btc")
		if err := evidence.Write(trackWeekly(t, pts, wk, 1), md, js); err != nil {
			t.Fatal(err)
		}
	}
	// Folders that are not ISO weeks are not read.
	md, js := evidence.DirectionOutPaths(root, "2026-W42")
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(js, []byte("{}"), 0o644)
	return root
}

func TestTrackRecord_SameInputsSameReport(t *testing.T) {
	root := trackClone(t, trackPoints(9), "2026-W41", "2026-W43")
	run := func() []byte {
		in := evidence.LoadTrack("stable", "btc", root, os.Stderr)
		md, js := evidence.TrackOutPaths(root, "btc")
		if err := evidence.WriteTrack(evidence.BuildTrackRecord(in), md, js); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(js)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	a, b := run(), run()
	if !bytes.Equal(a, b) {
		t.Fatal("two runs on the same files differ")
	}
	var tr evidence.TrackRecord
	if err := json.Unmarshal(a, &tr); err != nil || len(tr.Weeks) != 3 || tr.Weeks[1].Reported {
		t.Fatalf("%v %+v", err, tr.Weeks)
	}
	if md, _ := evidence.TrackOutPaths(root, "btc"); filepath.Base(filepath.Dir(md)) != "track-record" {
		t.Fatal("track-record folder")
	}
}

func TestTrend_ThreeWeeklyReportsBecomeOneTable(t *testing.T) {
	pts := trackPoints()
	root := trackClone(t, pts, "2026-W43", "2026-W41", "2026-W42")
	in := evidence.LoadTrack("stable", "btc", root, os.Stderr)
	tr := evidence.BuildTrend("stable", "btc", in.WeeklySource, in.Weekly, in.Unreadable)
	if len(tr.Weeks) != 3 {
		t.Fatalf("%d rows", len(tr.Weeks))
	}
	for i, wk := range []string{"2026-W41", "2026-W42", "2026-W43"} {
		r := tr.Weeks[i]
		rep := trackWeekly(t, pts, wk, 1)
		if r.Week != wk || !r.Reported || r.NetPct == nil || *r.NetPct != rep.Live.Perf.ReturnPct ||
			r.ROMLivePct == nil || *r.ROMLivePct != rep.ROM.Live.ReturnPct || r.HoldPct == nil || *r.HoldPct != rep.Hold.HoldReturnUSDPct {
			t.Fatalf("row %d %+v from %s's JSON", i, r, wk)
		}
	}
	md, js := evidence.TrendOutPaths(root, "btc")
	if err := evidence.WriteTrend(tr, md, js); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(md)
	text := string(data)
	if a, b, c := strings.Index(text, "| 2026-W41 |"), strings.Index(text, "| 2026-W42 |"), strings.Index(text, "| 2026-W43 |"); a < 0 || a > b || b > c {
		t.Fatalf("rows not in order:\n%s", text)
	}
	if !strings.Contains(text, "not a promise") || !strings.Contains(text, "net of fees") {
		t.Fatalf("header:\n%s", text)
	}
}

func TestTrend_MissingWeekReportedNotFilled(t *testing.T) {
	pts := trackPoints()
	tr := evidence.BuildTrend("stable", "btc", "src", []evidence.Report{trackWeekly(t, pts, "2026-W41", 1), trackWeekly(t, pts, "2026-W43", 1)}, nil)
	if len(tr.Weeks) != 3 {
		t.Fatalf("%d rows", len(tr.Weeks))
	}
	if r := tr.Weeks[1]; r.Week != "2026-W42" || r.Reported || r.NetPct != nil || r.HoldPct != nil || r.ROMLivePct != nil {
		t.Fatalf("W42 %+v", r)
	}
	var b bytes.Buffer
	_ = evidence.WriteTrendMarkdown(&b, tr)
	if !strings.Contains(b.String(), "**Weeks with no report:** 2026-W42") {
		t.Fatalf("missing week not listed:\n%s", b.String())
	}
}

func TestTrend_FieldAbsentInOlderReport(t *testing.T) {
	pts := trackPoints()
	root := trackClone(t, pts, "2026-W41", "2026-W42")
	// W41 written before #278: no return_on_margin key.
	_, js := evidence.OutPaths(root, "2026-W41", "btc")
	data, _ := os.ReadFile(js)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "return_on_margin")
	data, _ = json.Marshal(raw)
	if err := os.WriteFile(js, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// A broken file is listed, its week not reported.
	_, bad := evidence.OutPaths(root, "2026-W43", "btc")
	_ = os.MkdirAll(filepath.Dir(bad), 0o755)
	_ = os.WriteFile(bad, []byte("{not json"), 0o644)

	in := evidence.LoadTrack("stable", "btc", root, os.Stderr)
	tr := evidence.BuildTrend("stable", "btc", in.WeeklySource, in.Weekly, in.Unreadable)
	if len(tr.Weeks) != 2 || len(tr.Unreadable) != 1 {
		t.Fatalf("weeks %+v unreadable %v", tr.Weeks, tr.Unreadable)
	}
	old := tr.Weeks[0]
	if old.ROMLivePct != nil || strings.Join(old.NotMeasured, ",") != "rom_live,rom_replay" || old.NetPct == nil || old.HoldPct == nil {
		t.Fatalf("old row %+v: RoM not measured, the rest shown", old)
	}
	var b bytes.Buffer
	_ = evidence.WriteTrendMarkdown(&b, tr)
	line := ""
	for _, l := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(l, "| 2026-W41 |") {
			line = l
		}
	}
	// RoM live and replay not measured; the gap is unknown (no replay ran), the rest shown.
	if strings.Count(line, evidence.NotMeasured) != 2 || strings.Count(line, "unknown") != 1 || !strings.Contains(line, fmt.Sprintf("%+.2f", *old.NetPct)) {
		t.Fatalf("W41 row %q", line)
	}
}

func TestTrend_PercentagesOnly(t *testing.T) {
	pts := trackPoints()
	root := trackClone(t, pts, "2026-W41", "2026-W42")
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DERIBIT_ENV=live\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := evidence.LoadTrack("stable", "btc", root, os.Stderr)
	if in.Account != "live" {
		t.Fatalf("account %q", in.Account)
	}
	tr := evidence.BuildTrend("stable", "btc", in.WeeklySource, in.Weekly, in.Unreadable)
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"net":`, `"max_drawdown":`, `"gap":`, `"equity"`} {
		if bytes.Contains(data, []byte(key)) {
			t.Fatalf("trend JSON holds an amount %s: %s", key, data)
		}
	}
	var b bytes.Buffer
	_ = evidence.WriteTrendMarkdown(&b, tr)
	net := tr.Weeks[0]
	coin := trackWeekly(t, pts, "2026-W41", 1).Live.Perf.NetPnL
	if net.Net != nil || coin == 0 || strings.Contains(b.String(), fmt.Sprintf("%.4f", coin)) {
		t.Fatalf("trend shows the coin net %v:\n%s", coin, b.String())
	}
}

// #324: the direction trial's decision brief states its rule before the
// result, names the scorecard rows that decide, and is linked from the README.
func TestDirectionTrialBrief_StatesTheRule(t *testing.T) {
	data, err := os.ReadFile("../docs/direction-trial.md")
	if err != nil {
		t.Fatal(err)
	}
	brief := string(data)
	for _, s := range []string{
		"## Setups", "stable BTC", "mvp BTC", "2026-10-18", // the setups
		"## Minimum span", "## Outcomes", "noise", "## Kill rule", "## Review date", "2026-12-07",
		"-compare-direction", "replay[].net_pct", "replay[].max_drawdown_pct", "live_minus_replay_pp", // the deciding rows
		"**Assumption", "agent:needs-human",
	} {
		if !strings.Contains(brief, s) {
			t.Errorf("brief lacks %q", s)
		}
	}
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "(docs/direction-trial.md)") {
		t.Error("README does not link the brief")
	}
}

func TestDeribitAccount(t *testing.T) {
	root := t.TempDir()
	if got := evidence.DeribitAccount(root); got != "testnet" {
		t.Fatalf("no .env: %q", got)
	}
	for body, want := range map[string]string{
		"DERIBIT_ENV=live\n":            "live",
		"export DERIBIT_ENV=\"live\"\n": "live",
		"DERIBIT_ENV=testnet\n":         "testnet",
		"OTHER=1\n":                     "testnet",
	} {
		_ = os.WriteFile(filepath.Join(root, ".env"), []byte(body), 0o600)
		if got := evidence.DeribitAccount(root); got != want {
			t.Errorf("%q → %q, want %q", body, got, want)
		}
	}
}
