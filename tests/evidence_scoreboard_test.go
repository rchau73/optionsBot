package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
)

// The twelve-week scoreboard (#537): PRD §6's kill test in the weekly
// evidence report — the engine's weekly Sharpe ratio against holding the
// coin's, over the last 12 weekly reports, with the weeks still missing.

type scoreCase struct {
	Name         string          `json:"name"`
	Weeks        [][]interface{} `json:"weeks"`
	Status       string          `json:"status"`
	WeeksToGo    int             `json:"weeks_to_go"`
	MissingWeeks []string        `json:"missing_weeks"`
	EngineRatio  *float64        `json:"engine_ratio"`
	HoldRatio    *float64        `json:"hold_ratio"`
}

// scoreCases reads the fixture scripts/product/test_go_no_go.py reads too.
func scoreCases(t *testing.T) []scoreCase {
	t.Helper()
	data, err := os.ReadFile("testdata/scoreboard/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []scoreCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

func (c scoreCase) returns() []evidence.WeekReturns {
	out := make([]evidence.WeekReturns, len(c.Weeks))
	for i, w := range c.Weeks {
		out[i] = evidence.WeekReturns{Week: w[0].(string), Engine: w[1].(float64), Hold: w[2].(float64)}
	}
	return out
}

func (c scoreCase) last() string { return c.Weeks[len(c.Weeks)-1][0].(string) }

func sameRatio(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return math.Abs(*got-*want) < 5e-5
}

// Scenarios: window not full, engine below hold, no spread — and the
// fixture both tools read (go_no_go.py row 3 must give the same status).
func TestScoreboard_SharedFixture(t *testing.T) {
	t.Parallel()
	cases := scoreCases(t)
	if len(cases) < 4 {
		t.Fatalf("%d cases; the fixture must cover the four statuses", len(cases))
	}
	for _, c := range cases {
		s := evidence.ScoreWeeks(c.returns(), c.last(), evidence.ScoreboardWeeks)
		if s.Status != c.Status || s.WeeksToGo != c.WeeksToGo || !reflect.DeepEqual(s.MissingWeeks, c.MissingWeeks) ||
			!sameRatio(s.EngineRatio, c.EngineRatio) || !sameRatio(s.HoldRatio, c.HoldRatio) {
			t.Errorf("%s: %+v (engine %v hold %v); want %s, %d to go, missing %v, engine %v hold %v", c.Name, s,
				ratio(s.EngineRatio), ratio(s.HoldRatio), c.Status, c.WeeksToGo, c.MissingWeeks, ratio(c.EngineRatio), ratio(c.HoldRatio))
		}
		if s.Status == evidence.ScoreNoSpread && (s.EngineRatio != nil && s.HoldRatio != nil) {
			t.Errorf("%s: no spread with both ratios", c.Name)
		}
		if s.WeeksNeeded != 12 || s.Label == "" {
			t.Errorf("%s: needed %d label %q", c.Name, s.WeeksNeeded, s.Label)
		}
	}
}

func ratio(r *float64) any {
	if r == nil {
		return nil
	}
	return *r
}

func TestScoreboard_SharpeRatio(t *testing.T) {
	t.Parallel()
	if evidence.SharpeRatio(nil) != nil || evidence.SharpeRatio([]float64{1}) != nil {
		t.Error("under two values: no ratio")
	}
	if evidence.SharpeRatio([]float64{0.1, 0.1, 0.1}) != nil {
		t.Error("identical values: no spread, no ratio")
	}
	if r := evidence.SharpeRatio([]float64{1, 3}); r == nil || math.Abs(*r-2/math.Sqrt2) > 1e-12 {
		t.Errorf("mean 2 ÷ sample sd √2: %v", ratio(r))
	}
}

// Weeks after asOf do not count; the window keeps the last 12; a gap is
// listed from the first counted week to asOf, the report's own week too.
func TestScoreboard_WindowAndGaps(t *testing.T) {
	t.Parallel()
	wk := func(week string, engine, hold float64) evidence.WeekReturns {
		return evidence.WeekReturns{Week: week, Engine: engine, Hold: hold}
	}
	weeks := []evidence.WeekReturns{wk("2026-W40", 1, 2), wk("2026-W41", 2, 1), wk("2026-W44", 3, 1), wk("2026-W52", 1, 1), wk("2027-W01", 1, 1)}
	s := evidence.ScoreWeeks(weeks, "2026-W43", 12)
	if s.WeeksCounted != 2 || s.WeeksToGo != 10 || s.Status != evidence.ScoreNotEnoughWeeks ||
		!reflect.DeepEqual(s.MissingWeeks, []string{"2026-W42", "2026-W43"}) || s.FirstWeek != "2026-W40" || s.LastWeek != "2026-W41" {
		t.Errorf("as of W43: %+v", s)
	}
	if s := evidence.ScoreWeeks(weeks, "2027-W01", 3); s.FirstWeek != "2026-W44" || s.WeeksToGo != 0 ||
		len(s.MissingWeeks) != 8 || s.MissingWeeks[0] != "2026-W45" || s.MissingWeeks[7] != "2026-W53" {
		t.Errorf("across the year (2026 has 53 ISO weeks): %+v", s)
	}
	if s := evidence.ScoreWeeks(nil, "2026-W43", 12); s.WeeksToGo != 12 || s.MissingWeeks == nil || len(s.MissingWeeks) != 0 {
		t.Errorf("no week: %+v", s)
	}
}

// scoreInputs is a light week (no config: the replay does not run) whose
// P&L history knows the equity and the spot, so this week's returns are
// known, plus earlier reports carrying coin amounts.
func scoreInputs(earlier ...evidence.Report) evidence.Inputs {
	var points []history.Point
	for h := 0; h < 7*24; h++ {
		points = append(points, history.Point{T: evWeek.From.Add(time.Duration(h) * time.Hour),
			Total: 0.0001 * float64(h), Spot: 100000 + 10*float64(h)})
	}
	return evidence.Inputs{Env: "stable", Bot: "btc", Window: evWeek, Points: points, History: "pnl_history.jsonl",
		Equity: 1.2345, EquitySource: "account.json", Earlier: earlier, WeeklySource: "data/evidence/<YYYY-Www>/btc.json"}
}

func earlierReport(week string, engine, hold float64) evidence.Report {
	return evidence.Report{Week: week, Equity: 1.2345, Hold: evidence.Hold{Known: true, StrategyKnown: true,
		SpotStart: 98765, SpotEnd: 99876, ReturnUSDPct: engine, HoldReturnUSDPct: hold}}
}

// Scenario: window not full — 7 weekly reports with both returns known →
// "not enough weeks", 5 weeks still needed. Scenario: no amounts — neither
// the JSON nor the Markdown section holds a coin or USD amount.
func TestScoreboard_BuildWindowNotFullNoAmounts(t *testing.T) {
	t.Parallel()
	var earlier []evidence.Report
	for i, w := range []string{"2026-W36", "2026-W37", "2026-W38", "2026-W39", "2026-W40", "2026-W41"} {
		earlier = append(earlier, earlierReport(w, float64(i), 1))
	}
	// Not counted: this week's own older report, a later week, a week with the strategy's return unknown.
	stale := earlierReport("2026-W42", 9, 9)
	unknown := earlierReport("2026-W35", 1, 1)
	unknown.Hold.StrategyKnown = false
	r := evidence.Build(scoreInputs(append(earlier, stale, earlierReport("2026-W43", 1, 1), unknown)...))
	s := r.Score
	if !r.Hold.StrategyKnown || s.Status != evidence.ScoreNotEnoughWeeks || s.WeeksCounted != 7 || s.WeeksToGo != 5 ||
		s.FirstWeek != "2026-W36" || s.LastWeek != "2026-W42" || len(s.MissingWeeks) != 0 || s.EngineRatio != nil {
		t.Fatalf("scoreboard %+v (hold %+v)", s, r.Hold)
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	i := strings.Index(md, "## Twelve-week scoreboard")
	if i < 0 {
		t.Fatalf("no scoreboard section:\n%s", md)
	}
	section := md[i:]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	for _, want := range []string{"Past performance, net of fees", "not a promise of returns", "**not enough weeks** — 7 of 12 weeks counted (2026-W36 → 2026-W42); 5 more week(s) needed", "Weeks missing: none"} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %q:\n%s", want, section)
		}
	}
	for _, amount := range []string{"1.2345", "98765", "99876", "100000", "$"} {
		if strings.Contains(string(data), amount) || strings.Contains(section, amount) {
			t.Errorf("amount %q in the scoreboard:\n%s\n%s", amount, data, section)
		}
	}
}

// Scenario: engine below hold — 12 weeks where the engine's ratio is below
// hold's → "fails", both ratios printed.
func TestScoreboard_BuildEngineBelowHold(t *testing.T) {
	t.Parallel()
	var earlier []evidence.Report
	for i := 0; i < 11; i++ {
		engine := 3.0
		if i%2 == 1 {
			engine = -3
		}
		earlier = append(earlier, earlierReport(weekBefore(11-i), engine, 1+0.01*float64(i)))
	}
	r := evidence.Build(scoreInputs(earlier...))
	s := r.Score
	if s.Status != evidence.ScoreFails || s.EngineRatio == nil || s.HoldRatio == nil || *s.EngineRatio >= *s.HoldRatio || s.WeeksToGo != 0 {
		t.Fatalf("scoreboard %+v", s)
	}
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("- Status: **fails** — 12 of 12 weeks counted (2026-W31 → 2026-W42)\n- Engine ratio **%.2f** · hold ratio **%.2f** (past performance, net of fees)",
		*s.EngineRatio, *s.HoldRatio)
	if !strings.Contains(b.String(), want) {
		t.Errorf("markdown lacks %q:\n%s", want, b.String())
	}
}

// weekBefore is the ISO week n weeks before evWeek.
func weekBefore(n int) string {
	y, w := evWeek.From.AddDate(0, 0, -7*n).ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// A -from/-to report is not an ISO week: the scoreboard says why it was not computed.
func TestScoreboard_NotAWeek(t *testing.T) {
	t.Parallel()
	in := scoreInputs()
	in.Window.Label = "2026-10-12_2026-10-18"
	r := evidence.Build(in)
	if r.Score.Status != "" || !strings.Contains(r.Score.Note, "not an ISO week") {
		t.Errorf("scoreboard %+v", r.Score)
	}
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil || !strings.Contains(b.String(), "Not computed: a -from/-to report is not an ISO week") {
		t.Errorf("markdown %v:\n%s", err, b.String())
	}
}

// Load reads the clone's earlier weekly reports for the scoreboard and notes
// an unreadable one.
func TestScoreboard_LoadReadsEarlierReports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel string, body []byte) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	good, err := json.Marshal(earlierReport("2026-W41", 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	write("data/evidence/2026-W41/btc.json", good)
	write("data/evidence/2026-W40/btc.json", []byte("{not json"))
	var notes bytes.Buffer
	in := evidence.Load("stable", "btc", evidence.BotPaths(root, root, "btc"), evWeek, 1, &notes)
	if len(in.Earlier) != 1 || in.Earlier[0].Week != "2026-W41" || !strings.Contains(in.WeeklySource, "data/evidence") {
		t.Errorf("earlier %+v source %q", in.Earlier, in.WeeklySource)
	}
	if !strings.Contains(notes.String(), "2026-W40") {
		t.Errorf("the unreadable report must be noted: %q", notes.String())
	}
}
