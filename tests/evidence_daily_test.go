package tests

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/coverage"
	"optionsbot/internal/evidence"
)

// #716: the daily recording check — yesterday's covered hours per market
// recording against the weekly report's floor, told the next morning.

var dailyNow = covDay.AddDate(0, 0, 1).Add(6 * time.Hour) // the morning after covDay

// writeGEXDay writes one GEX day file of covDay with a record every minute,
// except the minutes in [skipFrom, skipTo) (minutes since midnight).
func writeGEXDay(t *testing.T, base string, skipFrom, skipTo int) {
	t.Helper()
	dir := filepath.Join(base, "market_gex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	for m := 0; m < 24*60; m++ {
		if m >= skipFrom && m < skipTo {
			continue
		}
		at := covDay.Add(time.Duration(m)*time.Minute + 5*time.Second)
		fmt.Fprintf(zw, `{"computed_at":%q,"spot":100000,"regime":"POSITIVE"}`+"\n", at.Format(time.RFC3339))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, covDay.Format(time.DateOnly)+".jsonl.gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dailySet(t *testing.T, d evidence.DailyRecording, name string) evidence.DailyDataset {
	t.Helper()
	for _, ds := range d.Datasets {
		if ds.Dataset == name {
			return ds
		}
	}
	t.Fatalf("dataset %s not in the check: %+v", name, d.Datasets)
	return evidence.DailyDataset{}
}

// Scenario: A complete day is silent.
func TestDailyRecording_ACompleteDayIsSilent(t *testing.T) {
	base := t.TempDir()
	record(t, filepath.Join(base, "market"), covDay, 24, func(int) float64 { return 100000 })
	writeGEXDay(t, base, 0, 0)

	d, err := evidence.CheckYesterday(base, dailyNow)
	if err != nil {
		t.Fatal(err)
	}
	if d.Day != "2026-10-12" || d.Short {
		t.Errorf("day %s short %v, want 2026-10-12 and not short", d.Day, d.Short)
	}
	for _, name := range []string{"market", "market_gex"} {
		if ds := dailySet(t, d, name); ds.Short || ds.CoveredPct != 100 || ds.CoveredHours != 24 || ds.MarketHours != 24 {
			t.Errorf("%s: %+v, want 100 %% of 24 h, not short", name, ds)
		}
	}
	for _, ds := range d.Datasets {
		if ds.Dataset == "pnl_history" {
			t.Error("the P&L history is not a market recording")
		}
	}
	var out bytes.Buffer
	if err := evidence.FormatDaily(&out, d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"market_gex", "100.0 %", "24.0 of 24 h", "not recorded"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "SHORT") {
		t.Errorf("a complete day is not short:\n%s", out.String())
	}
}

// Scenario: A short day is reported the next morning — a 5.5 h GEX gap.
func TestDailyRecording_AShortDayIsReported(t *testing.T) {
	base := t.TempDir()
	record(t, filepath.Join(base, "market"), covDay, 24, func(int) float64 { return 100000 })
	writeGEXDay(t, base, 3*60, 8*60+30)

	d, err := evidence.CheckYesterday(base, dailyNow)
	if err != nil {
		t.Fatal(err)
	}
	g := dailySet(t, d, "market_gex")
	want := coverage.Gap{From: covDay.Add(3 * time.Hour), To: covDay.Add(8*time.Hour + 30*time.Minute), Hours: 5.5}
	if !d.Short || !g.Short || g.Longest != want || len(g.LongGaps) != 1 || g.LongGaps[0] != want || g.CoveredHours != 18.5 {
		t.Errorf("market_gex: %+v (check short %v), want short with the 5.5 h gap %+v and 18.5 h covered", g, d.Short, want)
	}
	if m := dailySet(t, d, "market"); m.Short {
		t.Errorf("the complete books are not short: %+v", m)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"short":true`, `"longest_gap":{"from":"2026-10-12T03:00:00Z"`, `"covered_pct":77.08`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("json lacks %s: %s", key, raw)
		}
	}
}

// Scenario: A day with no file — the folder exists (the bot records it), the
// day's file does not: 0 %, short. A folder that does not exist is a dataset
// this bot never records, never short.
func TestDailyRecording_ADayWithNoFileIsZero(t *testing.T) {
	base := t.TempDir()
	record(t, filepath.Join(base, "market"), covDay.AddDate(0, 0, -1), 24, func(int) float64 { return 100000 })

	d, err := evidence.CheckYesterday(base, dailyNow)
	if err != nil {
		t.Fatalf("a day without a file is not an error: %v", err)
	}
	m := dailySet(t, d, "market")
	if !m.Short || m.CoveredPct != 0 || m.CoveredHours != 0 || m.Records != 0 || m.Status != evidence.CoverageRecorded {
		t.Errorf("market: %+v, want recorded, 0 %%, short", m)
	}
	if u := dailySet(t, d, "market_usdc"); u.Short || u.Status != evidence.CoverageNotRecorded {
		t.Errorf("market_usdc (no folder): %+v, want not recorded, not short", u)
	}
	if !d.Short {
		t.Error("the check is short")
	}
}

// Scenario: A damaged day is a gap, not an error.
func TestDailyRecording_ADamagedDayIsAGap(t *testing.T) {
	base := t.TempDir()
	books := filepath.Join(base, "market")
	record(t, books, covDay, 24, func(int) float64 { return 100000 })
	corruptMember(t, filepath.Join(books, covDay.Format(time.DateOnly)+".csv.gz"), 5)

	d, err := evidence.CheckYesterday(base, dailyNow)
	if err != nil {
		t.Fatalf("a damaged file must not fail the check: %v", err)
	}
	m := dailySet(t, d, "market")
	want := coverage.Gap{From: covDay.Add(5 * time.Hour), To: covDay.Add(6 * time.Hour), Hours: 1}
	if m.Records != 23 || m.CoveredHours != 23 || m.Longest != want {
		t.Errorf("market: %+v, want the 23 complete members covered and the damaged hour %+v as the gap", m, want)
	}
}

// Scenario: The same threshold as the weekly report — the floor and the long
// gap are the weekly report's constants, and the boundary is the same
// (at the floor is enough, under it is short).
func TestDailyRecording_TheSameThresholdAsTheWeeklyReport(t *testing.T) {
	if evidence.ReplayFullCoveragePct != 90 || evidence.LongGapHours != 3 {
		t.Fatalf("the weekly constants moved: %v %% and %v h — the daily check follows them", evidence.ReplayFullCoveragePct, evidence.LongGapHours)
	}
	from, to := evidence.YesterdayWindow(dailyNow)
	if !from.Equal(covDay) || !to.Equal(covDay.AddDate(0, 0, 1)) {
		t.Fatalf("window %v → %v, want covDay", from, to)
	}
	minutes := func(missing int) []time.Time { // missing minutes at the day's end
		var ts []time.Time
		for m := 0; m < 24*60-missing; m++ {
			ts = append(ts, covDay.Add(time.Duration(m)*time.Minute))
		}
		return ts
	}
	for _, c := range []struct {
		name    string
		missing int
		short   bool
	}{
		{"at the floor (90.0 %)", 144, false},
		{"one minute under", 145, true},
		{"complete", 0, false},
	} {
		r := coverage.Coverage(minutes(c.missing), from, to, time.Minute)
		r.Dataset, r.Found = "market_gex", true
		d := evidence.BuildDailyRecording(coverage.Result{From: from, To: to, Datasets: []coverage.Report{r}})
		if d.FloorPct != evidence.ReplayFullCoveragePct || d.LongGapHours != evidence.LongGapHours {
			t.Fatalf("floor %v / gap %v, want the weekly report's", d.FloorPct, d.LongGapHours)
		}
		if d.Short != c.short || d.Datasets[0].Short != c.short {
			t.Errorf("%s: short %v (%.2f %%), want %v", c.name, d.Short, d.Datasets[0].CoveredPct, c.short)
		}
	}
}
