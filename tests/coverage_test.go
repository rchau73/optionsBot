package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/coverage"
	"optionsbot/internal/gex"
	"optionsbot/internal/recorder"
)

// #233: how much recorded history stands behind a replay or a backtest.

var covDay = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)

// hourly returns one time per hour from start (at minute `at`), skipping the
// hours in skip (hours since start).
func hourly(start time.Time, hours int, at time.Duration, skip map[int]bool) []time.Time {
	var out []time.Time
	for h := 0; h < hours; h++ {
		if !skip[h] {
			out = append(out, start.Add(time.Duration(h)*time.Hour+at))
		}
	}
	return out
}

// Scenario: full coverage — hourly snapshots for every hour of three days.
func TestCoverage_FullCoverage(t *testing.T) {
	ts := hourly(covDay, 72, 17*time.Minute, nil) // the recorder runs at its start time's minute, not on the hour
	r := coverage.Coverage(ts, covDay, covDay.AddDate(0, 0, 3), time.Hour)
	if r.DaysCovered != 3 || r.DaysComplete != 3 || r.DaysInWindow != 3 {
		t.Errorf("days covered %d complete %d in window %d, want 3, 3, 3", r.DaysCovered, r.DaysComplete, r.DaysInWindow)
	}
	if r.MissingHours != 0 || r.MissingBins != 0 || len(r.Gaps) != 0 || r.LongestGapHours != 0 {
		t.Errorf("missing %v h, %d bins, gaps %v, want none", r.MissingHours, r.MissingBins, r.Gaps)
	}
	if r.Records != 72 || r.Bins != 72 || r.CoveredPct != 100 {
		t.Errorf("records %d bins %d covered %v %%, want 72, 72, 100", r.Records, r.Bins, r.CoveredPct)
	}
	if !r.First.Equal(ts[0]) || !r.Last.Equal(ts[71]) || len(r.MissingDays) != 0 {
		t.Errorf("first %v last %v missing days %v", r.First, r.Last, r.MissingDays)
	}
}

// Scenario: a restart gap — snapshots missing from 10:00 to 13:00 on one day.
func TestCoverage_RestartGap(t *testing.T) {
	ts := hourly(covDay, 72, 0, map[int]bool{34: true, 35: true, 36: true}) // day 2, 10:00–12:00
	r := coverage.Coverage(ts, covDay, covDay.AddDate(0, 0, 3), time.Hour)
	if r.MissingHours != 3 || r.LongestGapHours != 3 {
		t.Fatalf("missing %v h, longest gap %v h, want 3 and 3", r.MissingHours, r.LongestGapHours)
	}
	wantGap := coverage.Gap{From: covDay.Add(34 * time.Hour), To: covDay.Add(37 * time.Hour), Hours: 3}
	if len(r.Gaps) != 1 || r.Gaps[0] != wantGap {
		t.Errorf("gaps = %+v, want [%+v]", r.Gaps, wantGap)
	}
	for _, d := range r.Days {
		want := 0.0
		if d.Date == "2026-10-13" {
			want = 3
		}
		if d.MissingHours != want {
			t.Errorf("%s: %v h missing, want %v", d.Date, d.MissingHours, want)
		}
	}
	if r.DaysCovered != 3 || r.DaysComplete != 2 {
		t.Errorf("days covered %d complete %d, want 3 and 2", r.DaysCovered, r.DaysComplete)
	}
}

// A record a second late lands in the next bin and leaves one empty although
// nothing was missed: not a gap. Two minutes without a record are.
func TestCoverage_TickerJitterIsNotAGap(t *testing.T) {
	ts := []time.Time{
		covDay.Add(59 * time.Second),                 // minute 0
		covDay.Add(2*time.Minute + 1*time.Second),    // minute 2: minute 1 is empty, 62 s after
		covDay.Add(3*time.Minute + 1*time.Second),    // minute 3
		covDay.Add(6*time.Minute + 1*time.Second),    // minutes 4 and 5 really missing
		covDay.Add(7*time.Minute + 1*time.Second),    // minute 7
		covDay.Add(8*time.Minute + 1*time.Second),    // minute 8
		covDay.Add(9*time.Minute + 1*time.Second),    // minute 9
		covDay.Add(10*time.Minute + 1*time.Second),   // after the window: bridges nothing inside it
		covDay.Add(-30 * time.Minute),                // before the window: not counted
		covDay.Add(6*time.Minute + 30*time.Second),   // a second record in minute 6
		covDay.Add(6*time.Minute + 40*time.Second),   // and a third
		covDay.Add(-1 * time.Second).Add(-time.Hour), // far before
	}
	r := coverage.Coverage(ts, covDay, covDay.Add(10*time.Minute), time.Minute)
	if r.MissingBins != 2 {
		t.Fatalf("missing bins = %d, want 2 (minutes 4 and 5); gaps %+v", r.MissingBins, r.Gaps)
	}
	if len(r.Gaps) != 1 || !r.Gaps[0].From.Equal(covDay.Add(4*time.Minute)) || !r.Gaps[0].To.Equal(covDay.Add(6*time.Minute)) {
		t.Errorf("gaps = %+v, want 00:04 → 00:06", r.Gaps)
	}
	if r.Records != 9 {
		t.Errorf("records = %d, want 9 (those inside the window)", r.Records)
	}
}

// Scenario: window outside the data — the days before the first record are
// reported as missing, never filled in.
func TestCoverage_WindowBeforeTheData(t *testing.T) {
	ts := hourly(covDay, 24, 5*time.Minute, nil)
	from := covDay.AddDate(0, 0, -2)
	r := coverage.Coverage(ts, from, covDay.AddDate(0, 0, 1), time.Hour)
	if want := []string{"2026-10-10", "2026-10-11"}; !reflect.DeepEqual(r.MissingDays, want) {
		t.Errorf("missing days = %v, want %v", r.MissingDays, want)
	}
	if r.MissingHours != 48 || r.LongestGapHours != 48 || r.DaysCovered != 1 || r.DaysInWindow != 3 {
		t.Errorf("missing %v h, longest %v h, covered %d of %d days; want 48, 48, 1 of 3", r.MissingHours, r.LongestGapHours, r.DaysCovered, r.DaysInWindow)
	}
	if r.Days[0].Records != 0 || r.Days[0].MissingBins != 24 {
		t.Errorf("first day = %+v, want no record and 24 missing hours", r.Days[0])
	}
}

func TestCoverage_EmptyInputs(t *testing.T) {
	if r := coverage.Coverage(nil, covDay, covDay, time.Hour); r.Bins != 0 || r.Gaps == nil {
		t.Errorf("empty window: %+v", r)
	}
	if r := coverage.Coverage(nil, covDay, covDay.Add(time.Hour), 0); r.Bins != 0 {
		t.Errorf("zero step: %d bins", r.Bins)
	}
	r := coverage.Coverage(nil, covDay, covDay.Add(90*time.Minute), time.Hour) // a partial last bin
	if r.Bins != 2 || r.MissingHours != 1.5 || r.LongestGapHours != 1.5 || r.DaysCovered != 0 {
		t.Errorf("no records: bins %d missing %v h longest %v h covered %d, want 2, 1.5, 1.5, 0", r.Bins, r.MissingHours, r.LongestGapHours, r.DaysCovered)
	}
}

// Scenario: minute GEX absent for a day — market_gex has no file for a day
// that data/market has; that day is listed as hourly-only for replays. Read
// through the real recorders' files.
func TestDataCoverage_MinuteGEXAbsentForADay(t *testing.T) {
	bot := t.TempDir()
	base := filepath.Join(bot, "data") // -dir data/btc reads data/btc/data
	books := filepath.Join(base, "market")
	src := &fakeSummary{rows: []gex.SummaryRow{
		{InstrumentName: "BTC-30OCT26-100000-C", UnderlyingPrice: 100000, MarkIV: 50, BidPrice: 0.01, AskPrice: 0.012, MidPrice: 0.011, MarkPrice: 0.011, OpenInterest: 10},
	}}
	rec := recorder.New(src, func() float64 { return 50 }, books, time.Hour)
	for _, at := range hourly(covDay, 48, 20*time.Minute, map[int]bool{30: true}) { // 2 days, one hour missing on day 2
		src.at = at
		if _, err := rec.Snapshot(at); err != nil {
			t.Fatal(err)
		}
	}
	g := &fakeGEX{}
	grec := recorder.NewGEXRecorder(g, nil, nil, 0, filepath.Join(base, "market_gex"))
	for m := 0; m < 60; m++ { // the first hour of day 1 only
		g.snap = &gex.Snapshot{ComputedAt: covDay.Add(time.Duration(m)*time.Minute + 5*time.Second), Spot: 100000, Regime: "POSITIVE"}
		if _, err := grec.Record(); err != nil {
			t.Fatal(err)
		}
	}
	var pnl bytes.Buffer
	for m := 0; m < 48*60; m++ {
		fmt.Fprintf(&pnl, `{"t":%q,"realised":0,"unrealised":0,"total":0,"spot":0}`+"\n", covDay.Add(time.Duration(m)*time.Minute).Format(time.RFC3339))
	}
	pnl.WriteString("{torn\n")
	if err := os.WriteFile(filepath.Join(base, "pnl_history.jsonl"), pnl.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	now := covDay.AddDate(0, 0, 5)
	res, err := coverage.Run(bot, time.Time{}, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.From.Equal(covDay) || !res.To.Equal(covDay.AddDate(0, 0, 2)) {
		t.Errorf("default window %v → %v, want the two recorded days", res.From, res.To)
	}
	if want := []string{"2026-10-13"}; !reflect.DeepEqual(res.HourlyOnlyDays, want) {
		t.Errorf("hourly-only days = %v, want %v", res.HourlyOnlyDays, want)
	}
	byName := map[string]coverage.Report{}
	for _, r := range res.Datasets {
		byName[r.Dataset] = r
	}
	if m := byName["market"]; !m.Found || m.Records != 47 || m.MissingHours != 1 || m.DaysCovered != 2 {
		t.Errorf("market: found %v records %d missing %v h days %d, want true, 47, 1, 2", m.Found, m.Records, m.MissingHours, m.DaysCovered)
	}
	if g := byName["market_gex"]; g.Records != 60 || g.MissingHours != 47 || !reflect.DeepEqual(g.MissingDays, []string{"2026-10-13"}) {
		t.Errorf("market_gex: records %d missing %v h days %v, want 60, 47, [2026-10-13]", g.Records, g.MissingHours, g.MissingDays)
	}
	if u := byName["market_usdc"]; u.Found || u.MissingHours != 48 {
		t.Errorf("market_usdc absent: found %v missing %v h, want false, 48", u.Found, u.MissingHours)
	}
	if p := byName["pnl_history"]; p.Records != 48*60 || p.MissingBins != 0 {
		t.Errorf("pnl_history: records %d missing %d, want 2880, 0", p.Records, p.MissingBins)
	}

	// A window that starts before the data: the days before are missing.
	early, err := coverage.Run(base, covDay.AddDate(0, 0, -1), covDay.AddDate(0, 0, 1), now)
	if err != nil {
		t.Fatal(err)
	}
	if m := early.Datasets[0]; !reflect.DeepEqual(m.MissingDays, []string{"2026-10-11"}) || m.Records != 24 {
		t.Errorf("market from 10-11: missing days %v records %d, want [2026-10-11] and 24", m.MissingDays, m.Records)
	}

	var out bytes.Buffer
	if err := coverage.Format(&out, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"market_usdc", "not found", "2026-10-13 1.0 h", "decided hourly): 2026-10-13", "longest gap"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text report lacks %q:\n%s", want, out.String())
		}
	}
	if _, err := json.Marshal(res); err != nil {
		t.Errorf("json: %v", err)
	}
}

// The default window never runs past the current hour (still being
// recorded); no records and no window is an error, not an empty report.
func TestDataCoverage_DefaultWindowAndNoData(t *testing.T) {
	dir := t.TempDir()
	if _, err := coverage.Run(dir, time.Time{}, time.Time{}, covDay); err == nil {
		t.Error("no records and no window: want an error")
	}
	res, err := coverage.Run(dir, covDay, covDay.AddDate(0, 0, 1), covDay)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Datasets {
		if r.Found || len(r.MissingDays) != 1 {
			t.Errorf("%s: found %v missing days %v, want the whole day missing", r.Dataset, r.Found, r.MissingDays)
		}
	}

	pnl := fmt.Sprintf(`{"t":%q}`+"\n", covDay.Add(10*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "pnl_history.jsonl"), []byte(pnl), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = coverage.Run(dir, time.Time{}, time.Time{}, covDay.Add(12*time.Hour+30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !res.To.Equal(covDay.Add(12 * time.Hour)) {
		t.Errorf("default end = %v, want the current hour's start", res.To)
	}
}
