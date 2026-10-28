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

	"optionsbot/internal/config"
	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
)

// cmd/evidence -compare-direction (#245): the direction trial scorecard —
// stable's BTC (bias) and mvp's BTC (sma), live and replayed both ways on
// the same books, percentages only.

const dirEquity = 2.345678 // coin; must never reach the report

// dirInputs is a week of both environments: stable sheds a put, repairs
// it and stops a call; mvp only stops a put. Each has a direction history.
func dirInputs(t *testing.T, snaps []recorder.Snapshot) evidence.DirectionInputs {
	t.Helper()
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.GEXDirection = "bias" // stable's
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	call, put := "BTC-30OCT26-112000-C", "BTC-30OCT26-90000-P"
	opens := func(d int) []orders.Trade {
		return []orders.Trade{
			{At: day(d, 12), Kind: "open", Slot: slot, Instrument: call, OptionType: "call", Qty: 1, Price: 0.004, Reason: "entry"},
			{At: day(d, 12), Kind: "open", Slot: slot, Instrument: put, OptionType: "put", Qty: 1, Price: 0.006, Reason: "entry"},
		}
	}
	stable := append(opens(10),
		orders.Trade{At: day(13, 9), Kind: "close", Slot: slot, Instrument: put, OptionType: "put", Qty: 1, Reason: "gamma_regime", PnL: -0.0065432},
		orders.Trade{At: day(15, 9), Kind: "open", Slot: slot, Instrument: put, OptionType: "put", Qty: 1, Price: 0.005, Reason: orders.TriggerRepair},
		orders.Trade{At: day(16, 9), Kind: "close", Slot: slot, Instrument: call, OptionType: "call", Qty: 1, Reason: "stop_loss", PnL: -0.0071234},
	)
	mvp := append(opens(11),
		orders.Trade{At: day(14, 9), Kind: "close", Slot: slot, Instrument: put, OptionType: "put", Qty: 1, Reason: "stop_loss", PnL: -0.0052345},
	)
	dir := func(at time.Time, used, shed, other string) history.DirectionRecord {
		return history.DirectionRecord{At: at, Direction: used, Shed: shed, OtherAction: other}
	}
	closes, dvol := flatHistory(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	return evidence.DirectionInputs{Bot: "btc", Window: evWeek, Base: "stable", Cfg: cfg,
		Snaps: snaps, Market: "stable/data/btc/data/market", Closes: closes, DVOL: dvol,
		Envs: []evidence.DirectionEnv{
			{Env: "stable", Direction: "bias", Config: "stable/config_btc.yaml", Trades: stable,
				Journal: "stable/data/btc/orders.log", DirHistory: "stable/data/btc/data/direction_history.jsonl",
				Directions: []history.DirectionRecord{
					dir(day(11, 0), "bias", "none", "none"),
					dir(day(13, 0), "bias", "close_puts", "none"), // 12 h of puts
					dir(day(13, 12), "bias", "none", "close_calls"),
				},
				Equity: dirEquity, EquitySource: "stable/data/stress/btc/account.json"},
			{Env: "mvp", Direction: "sma", Config: "mvp/config_btc.mvp.yaml", Trades: mvp,
				Journal: "mvp/data/btc/orders.log", DirHistory: "mvp/data/btc/data/direction_history.jsonl",
				Equity: dirEquity, EquitySource: "mvp/data/stress/btc/account.json"},
		}}
}

// Scenario: given both envs have journals and direction histories for the
// week, the scorecard writes one row per source for live and one for
// replay, and each row names its source file.
func TestDirectionScorecard_OneRowPerSourceLiveAndReplay(t *testing.T) {
	t.Parallel()
	r := evidence.BuildDirection(dirInputs(t, evRecord(t)))
	if len(r.Live) != 2 || r.Live[0].Source != "bias" || r.Live[1].Source != "sma" {
		t.Fatalf("live rows %+v: want bias (stable) and sma (mvp)", r.Live)
	}
	if len(r.Replay) != 2 || r.Replay[0].Source != "bias" || r.Replay[1].Source != "sma" {
		t.Fatalf("replay rows %+v: want bias and sma", r.Replay)
	}
	for _, l := range r.Live {
		if l.Label != evidence.LiveRowLabel || !strings.Contains(strings.Join(l.Files, " "), "orders.log") ||
			!strings.Contains(strings.Join(l.Files, " "), "direction_history.jsonl") {
			t.Errorf("live row %s: label %q files %v", l.Source, l.Label, l.Files)
		}
		if l.MarkedPct == nil || l.ReplayPct == nil || l.GapPP == nil || math.Abs(*l.GapPP-(*l.MarkedPct-*l.ReplayPct)) > 1e-12 {
			t.Errorf("live row %s: marked %v replay %v gap %v", l.Source, l.MarkedPct, l.ReplayPct, l.GapPP)
		}
	}
	for _, rp := range r.Replay {
		if !rp.Ran || rp.Label != evidence.ReplayRowLabel || !strings.Contains(strings.Join(rp.Files, " "), "data/market") {
			t.Errorf("replay row %s: ran %v (%s) label %q files %v", rp.Source, rp.Ran, rp.Note, rp.Label, rp.Files)
		}
	}

	s, m := r.Live[0], r.Live[1]
	if s.Sheds != 1 || s.Repairs != 1 || s.Stops != 1 || m.Sheds != 0 || m.Repairs != 0 || m.Stops != 1 {
		t.Errorf("counts: stable %d/%d/%d mvp %d/%d/%d; want 1 shed, 1 repair, 1 stop and 0/0/1", s.Sheds, s.Repairs, s.Stops, m.Sheds, m.Repairs, m.Stops)
	}
	if want := (-0.0065432 - 0.0071234) / dirEquity * 100; s.RealisedPct == nil || math.Abs(*s.RealisedPct-want) > 1e-9 {
		t.Errorf("stable realised %v, want %.4f %%", s.RealisedPct, want)
	}
	if v, ok := s.BySlotPct["25d/0.16Δ"]; !ok || math.Abs(v-*s.RealisedPct) > 1e-9 {
		t.Errorf("by slot %v", s.BySlotPct)
	}
	if s.Shed.Puts != 12 || s.Shed.Calls != 0 || s.Shadow.Calls != 5*24+12 || s.Shadow.Source != "sma" {
		t.Errorf("stable shed hours %+v shadow %+v", s.Shed, s.Shadow)
	}
	if m.Shed.Covered != 0 {
		t.Errorf("mvp has no history: covered %v, want 0 (unknown, not assumed)", m.Shed.Covered)
	}

	var b bytes.Buffer
	if err := evidence.WriteDirectionMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{"## Live — " + evidence.LiveRowLabel, "## Replay — " + evidence.ReplayRowLabel,
		"| bias | stable | 1 | 1 | 1 |", "| sma | mvp | 0 | 0 | 1 |", "stable/data/btc/orders.log", "mvp/data/btc/data/direction_history.jsonl",
		"stable/data/btc/data/market"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// Scenario: given the replay runs bias and sma on the same week, both runs
// start from the same book and read the same recorded books.
func TestDirectionScorecard_ReplaysStartFromTheSameBook(t *testing.T) {
	t.Parallel()
	in := dirInputs(t, evRecord(t))
	bias, err := evidence.DirectionReplaySettings(in, "bias")
	if err != nil {
		t.Fatal(err)
	}
	sma, err := evidence.DirectionReplaySettings(in, "sma")
	if err != nil {
		t.Fatal(err)
	}
	if len(bias.Book) != 2 || !reflect.DeepEqual(bias.Book, sma.Book) || bias.Equity != sma.Equity ||
		!bias.From.Equal(sma.From) || !bias.To.Equal(sma.To) || !reflect.DeepEqual(bias.GEX, sma.GEX) ||
		!reflect.DeepEqual(bias.Closes, sma.Closes) || !reflect.DeepEqual(bias.DVOL, sma.DVOL) {
		t.Errorf("settings differ beyond the direction:\nbias %+v\nsma  %+v", bias, sma)
	}
	if bias.Cfg.GEXDirection != "bias" || sma.Cfg.GEXDirection != "sma" || in.Cfg.GEXDirection != "bias" {
		t.Errorf("directions %s/%s, base config now %s: want bias/sma and the base untouched", bias.Cfg.GEXDirection, sma.Cfg.GEXDirection, in.Cfg.GEXDirection)
	}
	r := evidence.BuildDirection(in)
	a, c := r.Replay[0], r.Replay[1]
	if a.StartLegs != 2 || a.StartLegs != c.StartLegs || a.BooksRead != c.BooksRead || a.Decisions != c.Decisions ||
		!a.Start.Equal(c.Start) || !a.End.Equal(c.End) {
		t.Errorf("replays: bias %+v sma %+v; want the same start book, books and window", a, c)
	}
}

// Scenario: given a day with no recorded books, that day is listed as
// missing and never filled in.
func TestDirectionScorecard_MissingDayListedNotFilled(t *testing.T) {
	t.Parallel()
	r := evidence.BuildDirection(dirInputs(t, evRecord(t, 14)))
	if want := []string{"2026-10-14"}; !reflect.DeepEqual(r.Missing.BookDays, want) {
		t.Errorf("missing book days %v, want %v", r.Missing.BookDays, want)
	}
	for _, rp := range r.Replay {
		if !rp.Ran || rp.Decisions != 6*24 {
			t.Errorf("replay %s ran %v (%s) on %d decisions: want only the 6 recorded days", rp.Source, rp.Ran, rp.Note, rp.Decisions)
		}
	}
	var b bytes.Buffer
	_ = evidence.WriteDirectionMarkdown(&b, r)
	if !strings.Contains(b.String(), "recorded books: **1 day(s) missing** — 2026-10-14") {
		t.Errorf("markdown does not name the missing day:\n%s", b.String())
	}

	// No books at all: the replays say why, the live rows still stand.
	r = evidence.BuildDirection(dirInputs(t, nil))
	if len(r.Missing.BookDays) != 7 || r.Replay[0].Ran || !strings.Contains(r.Replay[0].Note, "no recorded book") ||
		r.Live[0].Sheds != 1 || r.Live[0].GapPP != nil {
		t.Errorf("without books: missing %v replay %+v live %+v", r.Missing.BookDays, r.Replay[0], r.Live[0])
	}
}

// Scenario: given any env runs live, the report shows only percentages,
// never coin or USD amounts.
func TestDirectionScorecard_PercentagesOnly(t *testing.T) {
	t.Parallel()
	r := evidence.BuildDirection(dirInputs(t, evRecord(t)))
	dir := t.TempDir()
	md, js := evidence.DirectionOutPaths(dir, r.Week)
	if err := evidence.WriteDirection(r, md, js); err != nil {
		t.Fatal(err)
	}
	if md != filepath.Join(dir, "data", "evidence", "direction", "2026-W42.md") || !strings.HasSuffix(js, "direction/2026-W42.json") {
		t.Errorf("out paths %s %s", md, js)
	}
	for _, path := range []string{md, js} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, amount := range []string{"2.3456", "0.0065", "0.0071", "0.0052", "USD", "$"} {
			if strings.Contains(string(data), amount) {
				t.Errorf("%s shows an amount (%q):\n%s", filepath.Base(path), amount, data)
			}
		}
	}
	var back evidence.DirectionReport
	data, _ := os.ReadFile(js)
	if err := json.Unmarshal(data, &back); err != nil || len(back.Live) != 2 || len(back.Replay) != 2 {
		t.Errorf("json does not read back: %v", err)
	}

	// Equity unknown: no percentage rather than an amount.
	in := dirInputs(t, evRecord(t))
	in.Envs[1].Equity = 0
	r = evidence.BuildDirection(in)
	if m := r.Live[1]; m.EquityKnown || m.RealisedPct != nil || m.MarkedPct != nil || len(m.BySlotPct) != 0 {
		t.Errorf("mvp without equity: %+v", m)
	}
}

// ShedHours: a record before the week sets the state at its start; each
// state lasts until the next record or the week's end.
func TestDirectionScorecard_ShedHours(t *testing.T) {
	t.Parallel()
	recs := []history.DirectionRecord{
		{At: evWeek.From.Add(-time.Hour), Direction: "sma", Shed: "close_calls", OtherAction: "close_puts"},
		{At: evWeek.From.Add(2 * time.Hour), Direction: "sma", Shed: "none", OtherAction: "none"},
		{At: evWeek.To.Add(-3 * time.Hour), Direction: "sma", Shed: "close_puts", OtherAction: "none"},
		{At: evWeek.To.Add(time.Hour), Direction: "sma", Shed: "close_calls"},
	}
	used, shadow := evidence.ShedHours(recs, "sma", evWeek)
	if used.Calls != 2 || used.Puts != 3 || used.Covered != 7*24 || used.Source != "sma" {
		t.Errorf("used %+v", used)
	}
	if shadow.Puts != 2 || shadow.Calls != 0 || shadow.Source != "bias" {
		t.Errorf("shadow %+v", shadow)
	}
	if u, _ := evidence.ShedHours(nil, "bias", evWeek); u.Covered != 0 || u.Source != "bias" {
		t.Errorf("empty history: %+v", u)
	}
}

func TestDirectionScorecard_ReadDirectionHistory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "direction_history.jsonl")
	content := `{"at":"2026-10-12T00:00:00Z","direction":"bias","shed":"close_puts","other_action":"none"}
not json
{"at":"2026-10-12T01:00:00Z","direction":"bias","shed":"none","other_action":"close_calls"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, bad, err := evidence.ReadDirectionHistory(path)
	if err != nil || bad != 1 || len(recs) != 2 || recs[0].Shed != "close_puts" || recs[1].OtherAction != "close_calls" {
		t.Errorf("read %+v bad %d err %v", recs, bad, err)
	}
	if _, _, err := evidence.ReadDirectionHistory(filepath.Join(t.TempDir(), "none.jsonl")); err == nil {
		t.Error("a missing file must be an error (reported as a note)")
	}
}
