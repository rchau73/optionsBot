package tests

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
	"optionsbot/internal/orders"
)

// The lab follow-up (#855, cmd/labfollow): one test per scenario of the
// ticket, on a temporary COMPARE_DIR (the record of
// tests/testdata/lab_applied_record.json, a 28-day run report) and clone
// (a BTC P&L history).

const labFollowID = "20261104T230000Z"

var labFollowLive = time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

// labFollowDirs builds COMPARE_DIR and the clone. The run backtested 2 %
// return over 28 days (0.5 % over 7), worst week −0.9 %, drawdown 1.3 %, fees
// 400 of 100 000 (0.1 % over 7 days); its replay equity is 2 coin. history
// is the clone's P&L points as (time, total in coin); nil writes none.
func labFollowDirs(t *testing.T, history [][2]any) (compareDir, root string) {
	t.Helper()
	compareDir, root = t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(compareDir, "applied"), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := mustRead(t, filepath.Join("testdata", "lab_applied_record.json"))
	if err := os.WriteFile(filepath.Join(compareDir, "applied", labFollowID+".json"), rec, 0o600); err != nil {
		t.Fatal(err)
	}
	r := compare.Report{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}
	r.Backtest.Ran, r.Backtest.EquityUSD = true, 100_000
	r.Backtest.Proposed = compare.BacktestRun{ReturnPct: 2, WorstWeekPct: -0.9, MaxDrawdownPct: 1.3, FeesUSD: 400}
	r.Replay.Ran, r.Replay.Equity = true, 2
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(compareDir, labFollowID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compareDir, labFollowID, "report.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if history != nil {
		var lines []string
		for _, p := range history {
			lines = append(lines, fmt.Sprintf(`{"t":%q,"total":%v,"spot":90000}`, p[0].(time.Time).Format(time.RFC3339), p[1]))
		}
		dir := filepath.Join(root, "data", "btc", "data")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pnl_history.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return compareDir, root
}

// labFollowHistory: up 0.01, down to −0.02 (a 1.5 % fall from the peak of 2
// coin), ending at +0.01 (0.5 %).
func labFollowHistory() [][2]any {
	return [][2]any{
		{labFollowLive.Add(-time.Hour), 5.0}, // before the window: not counted
		{labFollowLive.Add(30 * time.Minute), 0.0},
		{labFollowLive.Add(26 * time.Hour), 0.01},
		{labFollowLive.Add(50 * time.Hour), -0.02},
		{labFollowLive.Add(6*24*time.Hour + 23*time.Hour), 0.01},
		{labFollowLive.Add(8 * 24 * time.Hour), 9.0}, // after the window
	}
}

func labFollow(t *testing.T, compareDir, root string, now time.Time, trades []orders.Trade) compare.LabFollow {
	t.Helper()
	in, err := compare.ReadLabFollow(compare.LabFollowOptions{CompareDir: compareDir, Root: root}, labFollowID, labFollowLive, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	in.Trades = trades
	return compare.BuildLabFollow(in)
}

func labFigure(t *testing.T, f compare.LabFollow, name string) compare.LabFollowFigure {
	t.Helper()
	for _, fig := range f.Figures {
		if fig.Name == name {
			return fig
		}
	}
	t.Fatalf("no figure %q in %+v", name, f.Figures)
	return compare.LabFollowFigure{}
}

func wantPct(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Scenario: A recorded change has been live for a full window.
func TestLabFollow_FullWindow(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	fees := []orders.Trade{
		{Kind: "close", At: labFollowLive.Add(48 * time.Hour), Fees: 0.002},
		{Kind: "close", At: labFollowLive.Add(-48 * time.Hour), Fees: 1}, // before the window
		{Kind: "open", At: labFollowLive.Add(48 * time.Hour), OpenFee: 1},
	}
	f := labFollow(t, compareDir, root, labFollowLive.AddDate(0, 0, 8), fees)
	if f.State != compare.LabFollowCompared || f.Points != 4 || f.Closes != 1 || f.BacktestDays != 28 {
		t.Fatalf("state %q points %d closes %d backtest days %d", f.State, f.Points, f.Closes, f.BacktestDays)
	}
	for _, c := range []struct {
		name              string
		back, real, gapPP float64
	}{
		{"Return", 0.5, 0.5, 0},
		{"Worst week", -0.9, 0.5, 1.4},
		{"Largest drawdown", -1.3, -1.5, -0.2},
		{"Fees", 0.1, 0.1, 0},
	} {
		fig := labFigure(t, f, c.name)
		wantPct(t, c.name+" backtested", fig.Backtested, c.back)
		wantPct(t, c.name+" realised", fig.Realised, c.real)
		wantPct(t, c.name+" gap", fig.GapPP, c.gapPP)
		if fig.BacktestedLabel != "backtested, not a forecast" || fig.RealisedLabel != "realised, one short window" {
			t.Errorf("%s labels %q / %q", c.name, fig.BacktestedLabel, fig.RealisedLabel)
		}
	}
	md := compare.LabFollowSectionMarkdown([]compare.LabFollow{f}, nil)
	for _, want := range []string{"## Lab follow-up", "### Run " + labFollowID, "Backtested, not a forecast", "Realised, one short window",
		"| Return | 0.50 % | 0.50 % | +0.00 |", "| Largest drawdown | −1.30 % | −1.50 % | −0.20 |", "`backtest.slippage_pct` (Backtest market slippage)",
		"A single week is not evidence"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// Scenario: The window is not over.
func TestLabFollow_TooEarly(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	f := labFollow(t, compareDir, root, labFollowLive.AddDate(0, 0, 3).Add(time.Hour), nil)
	if f.State != compare.LabFollowTooEarly || f.DaysPassed != 3 {
		t.Fatalf("state %q days passed %d", f.State, f.DaysPassed)
	}
	for _, fig := range f.Figures {
		if fig.GapPP != nil || fig.Realised != nil || fig.Worse {
			t.Errorf("%s: a gap or realised figure before the window is over: %+v", fig.Name, fig)
		}
	}
	md := compare.LabFollowMarkdown(f)
	if !strings.Contains(md, "too early to judge: 3 of 7 whole UTC days") || strings.Contains(md, "+0.") || strings.Contains(md, "Worse") {
		t.Errorf("markdown:\n%s", md)
	}
}

// Scenario: No journal data in the window — and the command exits 0.
func TestLabFollow_NoData(t *testing.T) {
	compareDir, root := labFollowDirs(t, [][2]any{{labFollowLive.Add(-time.Hour), 0.0}})
	f := labFollow(t, compareDir, root, labFollowLive.AddDate(0, 0, 8), nil)
	if f.State != compare.LabFollowNoData {
		t.Fatalf("state %q", f.State)
	}
	for _, fig := range f.Figures {
		if fig.GapPP != nil || fig.Realised != nil || fig.RealisedNote != "no data" {
			t.Errorf("%s: %+v", fig.Name, fig)
		}
	}
	if md := compare.LabFollowMarkdown(f); !strings.Contains(md, "| Return | 0.50 % | no data | – |") {
		t.Errorf("markdown:\n%s", md)
	}
	out, code := runLabFollow(t, "-id", labFollowID, "-live-from", "2026-11-10", "-compare", compareDir, "-root", root)
	if code != 0 || !strings.Contains(out, "no data") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// Scenario: Worse than the backtest.
func TestLabFollow_WorseThanTheBacktest(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	f := labFollow(t, compareDir, root, labFollowLive.AddDate(0, 0, 8), nil)
	if !labFigure(t, f, "Largest drawdown").Worse || labFigure(t, f, "Worst week").Worse {
		t.Fatalf("flags %+v", f.Figures)
	}
	want := "Worse than the backtest: the realised largest drawdown (−1.50 %) was deeper than the backtested one (−1.30 %)."
	if len(f.Flags) != 1 || f.Flags[0] != want || !strings.Contains(compare.LabFollowMarkdown(f), want) {
		t.Errorf("flags %q", f.Flags)
	}
}

// Scenario: Bad input — exit 1, a short reason, nothing written.
func TestLabFollow_BadInput(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	now := labFollowLive.AddDate(0, 0, 8)
	opts := compare.LabFollowOptions{CompareDir: compareDir, Root: root}
	for _, c := range []struct {
		id       string
		liveFrom time.Time
		want     string
	}{
		{"20261101T000000Z", labFollowLive, "has no recorded change"},
		{labFollowID, now.AddDate(0, 0, 1), "is in the future"},
		{labFollowID, time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), "before the change was recorded"},
		{"../x", labFollowLive, "want a folder time"},
	} {
		if _, err := compare.ReadLabFollow(opts, c.id, c.liveFrom, 7, now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: error %v, want %q", c.id, c.liveFrom.Format("2006-01-02"), err, c.want)
		}
	}
	if err := compare.CheckLiveFrom(compare.AppliedRecord{RecordedAt: "2026-11-05T09:12:00.000Z"}, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), now); err != nil {
		t.Errorf("the record's own day: %v", err)
	}

	before := treeFiles(t, compareDir)
	for _, args := range [][]string{
		{"-id", "20261101T000000Z", "-live-from", "2026-11-10"},
		{"-id", labFollowID, "-live-from", "2099-01-01"},
		{"-id", labFollowID, "-live-from", "2026-11-04"},
		{"-id", labFollowID, "-live-from", "10/11/2026"},
		{"-id", labFollowID}, // no day given and none kept
	} {
		out, code := runLabFollow(t, append(args, "-record-live-from", "-compare", compareDir, "-root", root)...)
		if code != 1 || !strings.Contains(out, "labfollow:") {
			t.Errorf("%v: exit %d:\n%s", args, code, out)
		}
	}
	if after := treeFiles(t, compareDir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("bad input wrote files: %v → %v", before, after)
	}
}

// Scenario: Read-only — no file but the asked-for .live changes, no bot,
// order or exchange package is imported, no setting value, amount or name
// is printed.
func TestLabFollow_ReadOnly(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	before := map[string]string{}
	for _, d := range []string{compareDir, root} {
		for path, body := range treeFiles(t, d) {
			before[path] = body
		}
	}
	for _, args := range [][]string{{}, {"-json"}} {
		out, code := runLabFollow(t, append(args, "-id", labFollowID, "-live-from", "2026-11-10", "-compare", compareDir, "-root", root)...)
		if code != 0 {
			t.Fatalf("exit %d:\n%s", code, out)
		}
		stdout := out[:strings.LastIndex(out, "\ntime=")+1] // the slog lines come last, on stderr
		for _, banned := range []string{"maria", "0.002", "100000", "100 000", "token", "aaaaaaaa"} {
			if strings.Contains(stdout, banned) {
				t.Errorf("output %v carries %q:\n%s", args, banned, stdout)
			}
		}
	}
	after := map[string]string{}
	for _, d := range []string{compareDir, root} {
		for path, body := range treeFiles(t, d) {
			after[path] = body
		}
	}
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Error("a run without -record-live-from changed a file")
	}
	for _, file := range []string{"../cmd/labfollow/main.go", "../internal/compare/labfollow.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, banned := range []string{"net", "os/exec", "optionsbot/internal/gateway", "optionsbot/internal/exchange", "optionsbot/internal/strategy", "optionsbot/internal/aianalysis", "optionsbot/internal/config"} {
				if p == banned || strings.HasPrefix(p, banned+"/") {
					t.Errorf("%s imports %s", file, p)
				}
			}
		}
	}
}

// -record-live-from keeps the day in applied/<id>.live; -all then follows
// every record with a day on file as the usage report's section.
func TestLabFollow_RecordLiveFromAndAll(t *testing.T) {
	compareDir, root := labFollowDirs(t, labFollowHistory())
	out, code := runLabFollow(t, "-all", "-compare", compareDir, "-root", root)
	if code != 0 || !strings.Contains(out, "## Lab follow-up\n\n"+compare.LabNoFollowUps) {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if _, code := runLabFollow(t, "-id", labFollowID, "-live-from", "2026-11-10", "-record-live-from", "-compare", compareDir, "-root", root); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := string(mustRead(t, compare.LiveFromPath(compareDir, labFollowID))); got != "2026-11-10\n" {
		t.Fatalf(".live = %q", got)
	}
	day, ok, err := compare.ReadLiveFrom(compareDir, labFollowID)
	if err != nil || !ok || !day.Equal(labFollowLive) {
		t.Fatalf("ReadLiveFrom = %v %v %v", day, ok, err)
	}
	// A kept day in the future (the clock is real here) is listed, not fatal.
	if err := os.WriteFile(compare.LiveFromPath(compareDir, "20261105T000000Z"), []byte("2099-01-01\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ids, err := compare.LiveFromIDs(compareDir)
	if err != nil || fmt.Sprint(ids) != "["+labFollowID+" 20261105T000000Z]" {
		t.Fatalf("ids %v %v", ids, err)
	}
	out, code = runLabFollow(t, "-all", "-compare", compareDir, "-root", root)
	for _, want := range []string{"## Lab follow-up", "### Run " + labFollowID, "Not followed:", "- run 20261105T000000Z: run 20261105T000000Z has no recorded change"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("exit %d, lacks %q:\n%s", code, want, out)
		}
	}
	// The usage report's own folder listing ignores .live files.
	files, err := compare.ReadLabUsage(compareDir)
	if err != nil || len(files.Applied) != 1 || files.AppliedUnreadable != 0 {
		t.Errorf("ReadLabUsage = %+v %v", files, err)
	}
}

// labFollowBins is the command built once per test, in its temporary folder.
var labFollowBins = map[*testing.T]string{}

// runLabFollow runs the command and returns stdout+stderr and the exit code.
func runLabFollow(t *testing.T, args ...string) (string, int) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs cmd/labfollow")
	}
	bin, ok := labFollowBins[t]
	if !ok {
		bin = filepath.Join(t.TempDir(), "labfollow")
		if out, err := exec.Command("go", "build", "-o", bin, "../cmd/labfollow").CombinedOutput(); err != nil {
			t.Fatalf("build labfollow: %v\n%s", err, out)
		}
		labFollowBins[t] = bin
		t.Cleanup(func() { delete(labFollowBins, t) })
	}
	cmd := exec.Command(bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String() + stderr.String(), code
}

// treeFiles maps every file under dir to its contents.
func treeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		out[path] = string(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
