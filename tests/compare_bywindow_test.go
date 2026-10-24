package tests

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
	"optionsbot/internal/recorder"
)

// cmd/compare -windows (#301): the comparison once per UTC week or day of the
// span, and how often the proposal wins. (Not named *_windows_test.go: Go
// would build that file on Windows only.)

// The span covers two ISO weeks in four days (the race-enabled suite must stay
// fast): Saturday and Sunday of 2026-W42, Monday and Tuesday of 2026-W43.
var (
	winFrom = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) // Saturday, 2026-W42
	winW43  = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) // Monday, 2026-W43
	winTo   = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
)

// winStart is the first recorded hour: three warm-up days before the span.
var winStart = winFrom.AddDate(0, 0, -3)

// winBooks records hourly books from winStart to the span's end and writes
// the backtest CSV; it returns their paths.
func winBooks(t *testing.T) (market, csv string) {
	t.Helper()
	market = filepath.Join(t.TempDir(), "market")
	end := winTo.AddDate(0, 0, 1)
	record(t, market, winStart, int(end.Sub(winStart).Hours()), func(h int) float64 { return 100000 * (1 + 0.01*math.Sin(float64(h)/9)) })
	return market, cmpCSV(t, 30)
}

// winInputs loads base and proposed over the span on those books, with a
// starting balance of 1 BTC and no stress.
func winInputs(t *testing.T, market, csv, proposed string) compare.Inputs {
	t.Helper()
	p := compare.Paths{Base: "../config_btc.yaml", Proposed: proposed, CSV: csv, Market: market, GEX: "none"}
	in, err := compare.Load(p, winFrom, winTo, 1, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	in.Closes, in.DVOL = flatHistory(winStart)
	return in
}

// takeProfit30 writes config_btc.yaml with roi_take_profit 0.30.
func takeProfit30(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const tp = "roi_take_profit:        0.50"
	if !bytes.Contains(body, []byte(tp)) {
		t.Fatalf("config_btc.yaml no longer has %q", tp)
	}
	path := filepath.Join(t.TempDir(), "my.yaml")
	if err := os.WriteFile(path, bytes.Replace(body, []byte(tp), []byte("roi_take_profit:        0.30"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func windowsMarkdown(t *testing.T, r compare.Report) string {
	t.Helper()
	var md bytes.Buffer
	if err := compare.WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	return md.String()
}

func TestCompareWindows_Cuts(t *testing.T) {
	from, to := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	got := compare.Windows(from, to, compare.Week)
	want := []compare.Window{
		{Label: "2026-W42", From: from, To: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)},
		{Label: "2026-W43", From: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
		{Label: "2026-W44", From: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), To: to},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("weeks:\n got %v\nwant %v", got, want)
	}
	days := compare.Windows(from, to, compare.Day)
	if len(days) != 14 || days[0].Label != "2026-10-14" || !days[13].From.Equal(to) || !days[13].To.Equal(to) {
		t.Errorf("days %v", days)
	}
	// ISO week across the new year: 2027-01-01 (a Friday) is in 2026-W53.
	ny := compare.Windows(time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC), compare.Week)
	if len(ny) != 2 || ny[0].Label != "2026-W53" || ny[1].Label != "2027-W01" || ny[0].To.Day() != 3 {
		t.Errorf("new year %v", ny)
	}
	one := compare.Windows(to, to, compare.Week)
	if len(one) != 1 || !one[0].From.Equal(to) || !one[0].To.Equal(to) {
		t.Errorf("one day %v", one)
	}
	for _, s := range []string{"", "week", "day"} {
		if _, err := compare.ParseUnit(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := compare.ParseUnit("month"); err == nil {
		t.Error("month: want an error")
	}
}

// The three scenarios that run the comparison share one recording (the
// race-enabled suite must stay fast).
func TestCompareWindows_Scenarios(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	market, csv := winBooks(t)
	proposed := takeProfit30(t)
	t.Run("TwoRecordedWeeks", func(t *testing.T) { compareWindowsTwoRecordedWeeks(t, winInputs(t, market, csv, proposed)) })
	t.Run("AWeekWithoutBooks", func(t *testing.T) { compareWindowsAWeekWithoutBooks(t, winInputs(t, market, csv, proposed)) })
	t.Run("SameConfig", func(t *testing.T) { compareWindowsSameConfig(t, winInputs(t, market, csv, "../config_btc.yaml")) })
}

// Scenario: Two recorded weeks — two window rows and a summary of how many
// the proposal won.
func compareWindowsTwoRecordedWeeks(t *testing.T, in compare.Inputs) {
	wr := compare.RunWindows(in, compare.Week)
	if len(wr.Rows) != 2 || wr.Rows[0].Label != "2026-W42" || wr.Rows[1].Label != "2026-W43" ||
		!wr.Rows[0].From.Equal(winFrom) || !wr.Rows[1].To.Equal(winTo) {
		t.Fatalf("rows %+v", wr.Rows)
	}
	for _, row := range wr.Rows {
		if !row.Replay.Ran || !row.Backtest.Ran {
			t.Errorf("%s: replay %+v, backtest %+v", row.Label, row.Replay, row.Backtest)
		}
		if d := row.Replay.Proposed.Net - row.Replay.Base.Net; d != row.Replay.Diff.Net {
			t.Errorf("%s: diff %v, want proposed − base %v", row.Label, row.Replay.Diff.Net, d)
		}
	}
	s := wr.Summary.Replay
	if s.Run != 2 || len(s.Missing) != 0 || s.ProposedWins+s.BaseWins > 2 || s.WorstBase == nil || s.WorstProp == nil {
		t.Errorf("replay summary %+v", s)
	}
	if wr.Summary.Backtest.Run != 2 || wr.StressNote != compare.StressNotWindowed {
		t.Errorf("backtest summary %+v, stress note %q", wr.Summary.Backtest, wr.StressNote)
	}

	// The whole-span sections are TestCompare_*'s; here only the windows.
	r := compare.Report{Disclaimer: compare.Disclaimer, Underlying: "btc", From: winFrom, To: winTo, Windows: &wr}
	md := windowsMarkdown(t, r)
	for _, w := range []string{compare.Disclaimer, "## By week", "### Replay (BTC)", "### Backtest (USD)", "| 2026-W42 | 10-17 → 10-18 |",
		"| 2026-W43 | 10-19 → 10-20 |", "windows (base higher in", "of 2** windows", "not windowed"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}
	md2, js, err := compare.Write(r, t.TempDir())
	if err != nil || md2 == "" {
		t.Fatal(err)
	}
	var back compare.Report
	data, _ := os.ReadFile(js)
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Windows == nil || len(back.Windows.Rows) != 2 || back.Windows.Summary.Replay.Run != 2 || back.Disclaimer != compare.Disclaimer {
		t.Errorf("json windows:\n%s", data)
	}
}

// Scenario: A week without books — that window names the missing data and is
// left out of the summary counts.
func compareWindowsAWeekWithoutBooks(t *testing.T, in compare.Inputs) {
	var w42 []recorder.Snapshot // the books end with W42
	for _, s := range in.Snaps {
		if s.At.Before(winW43) {
			w42 = append(w42, s)
		}
	}
	if len(w42) == 0 || len(w42) == len(in.Snaps) {
		t.Fatalf("%d of %d books before W43", len(w42), len(in.Snaps))
	}
	in.Snaps = w42
	wr := compare.RunWindows(in, compare.Week)
	if len(wr.Rows) != 2 {
		t.Fatalf("rows %+v", wr.Rows)
	}
	if !wr.Rows[0].Replay.Ran {
		t.Errorf("W42 has books: %+v", wr.Rows[0].Replay)
	}
	w43 := wr.Rows[1].Replay
	if w43.Ran || !strings.Contains(w43.Note, "no recorded book") || !strings.Contains(w43.Note, in.Market) || w43.Diff != (compare.Figures{}) {
		t.Errorf("W43 without books: %+v", w43)
	}
	s := wr.Summary.Replay
	if s.Run != 1 || !reflect.DeepEqual(s.Missing, []string{"2026-W43"}) || s.ProposedWins+s.BaseWins > 1 {
		t.Errorf("replay summary %+v", s)
	}
	for _, f := range wr.Summary.OrderFlips {
		if f == "2026-W43" {
			t.Error("a window without a replay cannot flip")
		}
	}
	md := windowsMarkdown(t, compare.Report{Disclaimer: compare.Disclaimer, Underlying: "btc", Windows: &wr})
	if !strings.Contains(md, "no recorded book") || !strings.Contains(md, "Not run (left out of the summary): 2026-W43.") {
		t.Errorf("markdown:\n%s", md)
	}
}

// Scenario: Same config — every difference is zero and the proposal wins no
// window.
func compareWindowsSameConfig(t *testing.T, in compare.Inputs) {
	wr := compare.RunWindows(in, compare.Week)
	if len(wr.Rows) != 2 {
		t.Fatalf("rows %+v", wr.Rows)
	}
	for _, row := range wr.Rows {
		for name, sd := range map[string]compare.WindowSide{"replay": row.Replay, "backtest": row.Backtest} {
			if !sd.Ran || sd.Diff != (compare.Figures{}) || sd.Base != sd.Proposed {
				t.Errorf("%s %s: %+v", row.Label, name, sd)
			}
		}
	}
	for name, s := range map[string]compare.SourceSummary{"replay": wr.Summary.Replay, "backtest": wr.Summary.Backtest} {
		if s.Run != 2 || s.ProposedWins != 0 || s.BaseWins != 0 || s.MedianDiff != 0 || s.BestDiff != 0 || s.WorstDiff != 0 {
			t.Errorf("%s summary %+v", name, s)
		}
	}
	if len(wr.Summary.OrderFlips) != 0 {
		t.Errorf("flips %v", wr.Summary.OrderFlips)
	}
	md := windowsMarkdown(t, compare.Report{Disclaimer: compare.Disclaimer, Underlying: "btc", Windows: &wr})
	if !strings.Contains(md, "higher in **0 of 2** windows (base higher in 0, equal in 2)") {
		t.Errorf("markdown:\n%s", md)
	}
}

// Scenario: Default unchanged — without -windows the report has no windows,
// in the JSON or the Markdown (TestCompare_* check the rest of it, as before).
func TestCompareWindows_DefaultUnchanged(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	in, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: "../config_btc.yaml"}, winFrom, winTo, 0, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	r := compare.Run(in)
	if r.Windows != nil {
		t.Fatalf("windows %+v without -windows", r.Windows)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"windows"`)) {
		t.Errorf("json carries windows: %s", data)
	}
	if md := windowsMarkdown(t, r); strings.Contains(md, "## By ") {
		t.Errorf("markdown has a windows section:\n%s", md)
	}
}

// The summary: wins, ties, median, spread, worst window of each config, and
// the windows where replay and backtest disagree on the order.
func TestCompareWindows_Summary(t *testing.T) {
	ran := func(base, prop float64) compare.WindowSide {
		b, p := compare.Figures{Net: base, ReturnPct: base * 10}, compare.Figures{Net: prop, ReturnPct: prop * 10}
		return compare.WindowSide{Ran: true, Base: b, Proposed: p, Diff: compare.Figures{Net: prop - base, ReturnPct: (prop - base) * 10}}
	}
	row := func(label string, bt, rp compare.WindowSide) compare.WindowRow {
		return compare.WindowRow{Window: compare.Window{Label: label}, Backtest: bt, Replay: rp}
	}
	rows := []compare.WindowRow{
		row("W1", ran(1, 2), ran(0.10, 0.30)),  // both: proposal ahead
		row("W2", ran(1, 3), ran(0.20, 0.10)),  // flip: backtest ahead, replay behind
		row("W3", ran(-2, -2), ran(-0.5, 0.0)), // backtest tie: no flip
		row("W4", compare.WindowSide{Note: "no backtest data"}, compare.WindowSide{Note: "no recorded book"}),
	}
	s := compare.SummarizeWindows(rows)
	rp := s.Replay
	if rp.Run != 3 || rp.ProposedWins != 2 || rp.BaseWins != 1 || !reflect.DeepEqual(rp.Missing, []string{"W4"}) {
		t.Errorf("replay counts %+v", rp)
	}
	if math.Abs(rp.MedianDiff-0.2) > 1e-12 || math.Abs(rp.MedianDiffPP-2) > 1e-9 || math.Abs(rp.WorstDiff+0.1) > 1e-12 || math.Abs(rp.BestDiff-0.5) > 1e-12 {
		t.Errorf("replay median/spread %+v", rp)
	}
	if rp.WorstBase.Label != "W3" || rp.WorstBase.Net != -0.5 || rp.WorstProp.Label != "W3" || rp.WorstProp.Net != 0 {
		t.Errorf("replay worst %+v %+v", rp.WorstBase, rp.WorstProp)
	}
	bt := s.Backtest
	if bt.Run != 3 || bt.ProposedWins != 2 || bt.BaseWins != 0 || bt.MedianDiff != 1 || bt.WorstBase.Label != "W3" {
		t.Errorf("backtest %+v", bt)
	}
	if !reflect.DeepEqual(s.OrderFlips, []string{"W2"}) {
		t.Errorf("flips %v, want [W2]", s.OrderFlips)
	}
	if e := compare.SummarizeWindows(nil); e.Replay.Run != 0 || e.Replay.WorstBase != nil || len(e.OrderFlips) != 0 {
		t.Errorf("empty %+v", e)
	}
}
