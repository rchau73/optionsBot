package tests

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/deployeffect"
	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
)

// The E1 reliability section of the weekly evidence report (#361): margin
// use against the 60 % target, why margin sat idle, what each startup sent.

// e1Inputs is evInputs with the second day frozen, slot 25d vacant all week
// (no free expiry) and slot 50d holding a call all week.
func e1Inputs(t *testing.T) evidence.Inputs {
	t.Helper()
	in := evInputs(t, nil)
	in.Points = nil // every 10 minutes, so the whole week is measured (a line stands for ≤ 15 min)
	for i := 0; i < 7*24*6; i++ {
		at := evWeek.From.Add(time.Duration(i) * 10 * time.Minute)
		im, limit, allowed := 10.0, 20.0, at.Day() != 13
		in.Points = append(in.Points, history.Point{T: at, Spot: 100000, IMPct: &im, IMLimitPct: &limit, EntriesAllowed: &allowed})
	}
	before := evWeek.From.Add(-time.Hour)
	in.IdleEvents = []idlemargin.Event{
		{At: before, Event: orders.EventSkipped, Slot: &orders.SlotRef{DTE: 25, Delta: 0.16}, Reason: "no_free_expiry"},
		{At: before, Event: orders.EventFilled, Slot: &orders.SlotRef{DTE: 50, Delta: 0.16}, Instrument: "BTC-27NOV26-120000-C", Qty: 1, OrderID: "e1"},
	}
	in.DeployLines = []deployeffect.Line{}
	in.DeploysPath = "history/deploys.tsv"
	return in
}

func e1Markdown(t *testing.T, r evidence.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Scenario: A week with entries allowed and idle margin.
func TestEvidenceE1_AWeekWithEntriesAllowedAndIdleMargin(t *testing.T) {
	r := evidence.Build(e1Inputs(t))
	e := r.E1
	m := e.Margin
	if e.TargetPct != 60 || !m.Known || m.MeanPct == nil || math.Abs(*m.MeanPct-50) > 1e-9 {
		t.Fatalf("margin use %+v, want the history's 50 %% (IM 10 of 20)", m)
	}
	if m.MeanPct != r.Util.Stats.MeanPct || m.AllowedShare == nil || math.Abs(*m.AllowedShare-6.0/7) > 1e-9 {
		t.Errorf("margin use must be history.Utilisation's own figures: %+v vs %+v", m, r.Util.Stats)
	}
	if m.TargetMet == nil || *m.TargetMet {
		t.Errorf("50 %% < 60 %%: target not met, got %v", m.TargetMet)
	}

	d := e.Idle
	if !d.Known || d.Slots != 2 || math.Abs(d.OccupiedHours-168) > 1e-6 || math.Abs(d.IdleHours-168) > 1e-6 || d.NotMeasuredHours > 1e-6 {
		t.Fatalf("idle %+v: want 2 slots, 168 h occupied, 168 h idle", d)
	}
	var sum float64
	want := map[string]float64{"no_free_expiry": 144, idlemargin.RowFrozen: 24}
	for _, c := range d.Causes {
		sum += c.Share
		if math.Abs(c.Hours-want[c.Cause]) > 1e-6 {
			t.Errorf("cause %s %.2f h, want %.0f", c.Cause, c.Hours, want[c.Cause])
		}
	}
	if len(d.Causes) != 2 || d.Causes[0].Cause != "no_free_expiry" || math.Abs(sum-1) > 1e-9 {
		t.Errorf("causes %+v: want the idle time split, most first, shares summing to 1 (got %.4f)", d.Causes, sum)
	}

	md := e1Markdown(t, r)
	for _, w := range []string{"## E1 reliability", "target 60 % of the IM limit", "**50.0 %** — target **not met**",
		"no_free_expiry 86 % (144.0 h); frozen 14 % (24.0 h);", "data/btc/data/pnl_history.jsonl", "data/btc/orders.log",
		"0 startup(s)"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}

	in := e1Inputs(t)
	for i := range in.Points {
		im := 13.0 // 13 of 20 = 65 %
		in.Points[i].IMPct = &im
	}
	if r := evidence.Build(in); r.E1.Margin.TargetMet == nil || !*r.E1.Margin.TargetMet {
		t.Errorf("65 %%: target met, got %+v", r.E1.Margin)
	}
}

// Scenario: A restart that changed the book.
func TestEvidenceE1_ARestartThatChangedTheBook(t *testing.T) {
	in := e1Inputs(t)
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	in.DeployLines = []deployeffect.Line{
		{At: at, Event: orders.EventReconciled, Slot: slot, Instrument: "BTC-30OCT26-112000-C", Qty: 1, Detail: "strangle 1 of 1"},
		{At: at, Event: orders.EventReconciled, Slot: slot, Instrument: "BTC-30OCT26-90000-P", Qty: 1.3, Detail: "strangle 1 of 1"},
		{At: at.Add(5 * time.Minute), Event: orders.EventSubmitted, Slot: slot, Instrument: "BTC-30OCT26-90000-P",
			OrderID: "o1", Trigger: orders.TriggerRebalanceLegs, Qty: 0.3},
	}
	in.Deploys = []deployeffect.Deploy{{At: at.Add(3 * time.Minute), Commit: "abc1234"}}
	r := evidence.Build(in).E1.Restarts
	if !r.Known || r.Startups != 1 || r.RestartMade != 1 || math.Abs(r.MadeLots-0.3) > 1e-9 || len(r.List) != 1 {
		t.Fatalf("restarts %+v: want one restart-made startup with 0.3 lots", r)
	}
	if s := r.List[0]; !s.At.Equal(at) || s.Commit != "abc1234" || s.Class != deployeffect.RestartMade || s.Orders != 1 {
		t.Errorf("startup %+v", s)
	}
	md := e1Markdown(t, evidence.Build(in))
	for _, w := range []string{"1 startup(s): neutral 0 · expected 0 · **restart-made 1** (0.3 lots)",
		"2026-10-14 10:00 UTC (abc1234, journal): restart-made · 1 order(s), 0.3 lots, 0.3 restart-made", "history/deploys.tsv"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}
}

// Scenario: Missing input — a day without a P&L history line, or an
// unreadable journal: "not measured" with the reason, no figure invented.
func TestEvidenceE1_MissingInput(t *testing.T) {
	in := e1Inputs(t)
	var kept = in.Points[:0:0]
	for _, p := range in.Points {
		if p.T.Day() != 15 {
			kept = append(kept, p)
		}
	}
	in.Points = kept
	r := evidence.Build(in)
	if got := r.E1.Margin.MissingDays; len(got) != 1 || got[0] != "2026-10-15" {
		t.Errorf("missing days %v, want 2026-10-15", got)
	}
	// The last line before the gap still stands for its 15 minutes (5 of them in the day).
	if d := r.E1.Idle; math.Abs(d.NotMeasuredHours-2*(24-5.0/60)) > 1e-6 {
		t.Errorf("a day without history: ~24 h per slot not measured, got %.2f", d.NotMeasuredHours)
	}
	if md := e1Markdown(t, r); !strings.Contains(md, "not measured on 1 day(s) with no P&L history line: 2026-10-15") {
		t.Errorf("markdown does not name the missing day:\n%s", md)
	}

	in = e1Inputs(t)
	in.Points, in.IdleEvents, in.DeployLines = nil, nil, nil
	in.JournalNote = "open data/btc/orders.log: permission denied"
	r = evidence.Build(in)
	e := r.E1
	if e.Margin.Known || e.Margin.MeanPct != nil || e.Margin.TargetMet != nil || e.Margin.Note != "P&L history not read" {
		t.Errorf("margin use without history: %+v", e.Margin)
	}
	if e.Idle.Known || e.Idle.SlotHours != 0 || len(e.Idle.Causes) != 0 || !strings.Contains(e.Idle.Note, "permission denied") {
		t.Errorf("idle without journal: %+v", e.Idle)
	}
	if e.Restarts.Known || e.Restarts.Startups != 0 || !strings.Contains(e.Restarts.Note, "journal not read") {
		t.Errorf("restarts without journal: %+v", e.Restarts)
	}
	md := e1Markdown(t, r)
	for _, w := range []string{"- not measured: P&L history not read", "- not measured: journal not read: open data/btc/orders.log: permission denied"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}
	if strings.Contains(md[strings.Index(md, "## E1 reliability"):], "startup(s)") {
		t.Errorf("no startup count without a journal:\n%s", md)
	}

	in = e1Inputs(t)
	in.Cfg = nil
	if r := evidence.Build(in).E1.Restarts; r.Known || !strings.Contains(r.Note, "no config") {
		t.Errorf("restarts without config: %+v", r)
	}
}

// Load reads the journal for the section and the env's lines of
// <envs>/history/deploys.tsv; a missing deploys.tsv is noted, not fatal.
func TestEvidenceE1_LoadReadsJournalAndDeploys(t *testing.T) {
	envs := t.TempDir()
	root := filepath.Join(envs, "stable")
	put := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := os.ReadFile("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(root, "config_btc.yaml"), string(cfg))
	put(filepath.Join(root, "data/btc/orders.log"),
		`{"timestamp":"2026-10-14T10:00:00Z","event":"reconciled","instrument":"BTC-30OCT26-90000-P","qty":1,"slot":{"dte":25,"delta":0.16},"detail":"strangle 1 of 1"}`+"\n")
	put(filepath.Join(root, "data/btc/data/pnl_history.jsonl"), `{"t":"2026-10-12T00:00:00Z","total":0,"spot":100000}`+"\n")

	var notes bytes.Buffer
	p := evidence.BotPaths(root, root, "btc")
	if p.Deploys != filepath.Join(envs, "history", "deploys.tsv") {
		t.Fatalf("deploys path %s", p.Deploys)
	}
	in := evidence.Load("stable", "btc", p, evWeek, 1, &notes)
	if len(in.IdleEvents) != 1 || len(in.DeployLines) != 1 || in.Deploys != nil || !strings.Contains(in.DeploysNote, "deploys.tsv not read") {
		t.Fatalf("idle %d lines %d deploys %v note %q", len(in.IdleEvents), len(in.DeployLines), in.Deploys, in.DeploysNote)
	}

	put(p.Deploys, "2026-10-14T10:02:00Z\tstable\tabc1234\n2026-10-14T10:02:00Z\tmvp\tdef5678\n")
	in = evidence.Load("stable", "btc", p, evWeek, 1, &notes)
	if len(in.Deploys) != 1 || in.Deploys[0].Commit != "abc1234" || in.DeploysNote != "" {
		t.Fatalf("deploys %+v note %q: want stable's line only", in.Deploys, in.DeploysNote)
	}
	r := evidence.Build(in)
	if s := r.E1.Restarts; !s.Known || s.Startups != 1 || s.List[0].Commit != "abc1234" {
		t.Errorf("restarts %+v", s)
	}
	data, err := json.Marshal(r)
	if err != nil || !bytes.Contains(data, []byte(`"e1_reliability":`)) || !bytes.Contains(data, []byte(`"restart_made":0`)) {
		t.Errorf("json %v:\n%s", err, data)
	}

	in = evidence.Load("stable", "btc", evidence.BotPaths(t.TempDir(), root, "btc"), evWeek, 1, &notes)
	if in.IdleEvents != nil || in.DeployLines != nil || in.JournalNote == "" {
		t.Errorf("no journal: idle %v lines %v note %q", in.IdleEvents, in.DeployLines, in.JournalNote)
	}
}
