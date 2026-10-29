package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The liquidity-cap study (#487): cmd/stress -replay -liqcaps 5,10,20 replays
// the same recorded books once per cap and prints one table per coin.

func capStudyBooks(t *testing.T, days int) ([]recorder.Snapshot, replay.Settings) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, days*24, func(h int) float64 { return 100000 * (1 + 0.002*math.Sin(float64(h)/7)) })
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	closes, dvol := flatHistory(start)
	// A balance large against the synthetic book's 100 of open interest per
	// strike: margin alone sizes past a 5 % cap (5 BTC = 50 lots).
	return snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 20, Closes: closes, DVOL: dvol}
}

// Scenario: the study runs on recorded history — a table with margin use,
// cycles under 60 %, capped lots and worst week for each cap.
func TestLiquidityCapStudy_RunsOnRecordedHistory(t *testing.T) {
	t.Parallel()
	snaps, set := capStudyBooks(t, 6)
	rows, err := replay.CapStudy(snaps, set, []float64{5, 10, 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want one per cap", len(rows))
	}
	for i, r := range rows {
		res := r.Result
		if res.EntryCycles == 0 || res.EntryCycles > res.Snapshots || res.UnderTargetCycles > res.EntryCycles {
			t.Errorf("cap %g: entry cycles %d, under target %d, decisions %d", r.CapPct, res.EntryCycles, res.UnderTargetCycles, res.Snapshots)
		}
		if res.AvgUseOfLimitPct <= 0 {
			t.Errorf("cap %g: average use of the limit %.1f %%, want > 0 with entries open", r.CapPct, res.AvgUseOfLimitPct)
		}
		if i > 0 && res.LiquidityLotsCut > rows[i-1].Result.LiquidityLotsCut+1e-9 {
			t.Errorf("cap %g cut %.0f lots, more than the tighter cap %g (%.0f)", r.CapPct, res.LiquidityLotsCut,
				rows[i-1].CapPct, rows[i-1].Result.LiquidityLotsCut)
		}
	}
	if rows[0].Result.LiquidityEntriesCut == 0 || rows[0].Result.LiquidityLotsCut <= 0 {
		t.Errorf("a 5 %% cap on 100 of OI must cut the margin-sized entries: %+v", rows[0].Result)
	}
	// The same history with the cap off cuts nothing.
	off, _ := replay.CapStudy(snaps, set, []float64{0})
	if off[0].Result.LiquidityEntriesCut != 0 || off[0].Result.LiquidityLotsCut != 0 {
		t.Errorf("the cap off cut %d entries", off[0].Result.LiquidityEntriesCut)
	}

	md := replay.CapStudyMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To))
	for _, want := range []string{"### BTC", "Avg IM (% of limit)", "Cycles under 60 % of limit, entries allowed", "Lots cut", "Worst week", "| 5 |", "| 10 |", "| 20 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "no recording on") {
		t.Errorf("every day of the window is recorded, none is missing:\n%s", md)
	}
}

// Scenario: the shipped rule is unchanged — the study varies a copy of the
// settings; the bot's cap stays 10 in fixed.go.
func TestLiquidityCapStudy_ShippedRuleUnchanged(t *testing.T) {
	t.Parallel()
	snaps, set := capStudyBooks(t, 4)
	if _, err := replay.CapStudy(snaps, set, []float64{5, 20}); err != nil {
		t.Fatal(err)
	}
	if set.Cfg.LiquidityCapOIPct != 10 || config.FixedLiquidityCapOIPct != 10 {
		t.Errorf("cap after the study = %v (fixed %v), want the bot's 10", set.Cfg.LiquidityCapOIPct, config.FixedLiquidityCapOIPct)
	}
}

// Scenario: short history is reported, not hidden — the days with no
// recording are listed and the result is marked indicative.
func TestLiquidityCapStudy_ShortHistoryIsIndicative(t *testing.T) {
	t.Parallel()
	snaps, set := capStudyBooks(t, 5)
	set.To = time.Date(2026, 10, 13, 23, 59, 0, 0, time.UTC) // recorded Oct 7–11: two days short
	missing := replay.MissingDays(snaps, set.From, set.To)
	if len(missing) != 2 || missing[0].Format(time.DateOnly) != "2026-10-12" || missing[1].Format(time.DateOnly) != "2026-10-13" {
		t.Fatalf("missing = %v, want 2026-10-12 and 2026-10-13", missing)
	}
	rows, err := replay.CapStudy(snaps, set, []float64{10})
	if err != nil {
		t.Fatal(err)
	}
	md := replay.CapStudyMarkdown("ETH", rows, missing)
	for _, want := range []string{"**Indicative:**", "2026-10-12, 2026-10-13", "under a week"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}

	// A gap inside the window is listed too.
	var gapped []recorder.Snapshot
	for _, s := range snaps {
		if s.At.Day() != 9 {
			gapped = append(gapped, s)
		}
	}
	if m := replay.MissingDays(gapped, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), time.Time{}); len(m) != 1 || m[0].Day() != 9 {
		t.Errorf("gap = %v, want 2026-10-09", m)
	}
	if m := replay.MissingDays(nil, set.From, set.To); m != nil {
		t.Errorf("no recording: %v, want nil (the replay itself refuses it)", m)
	}
}

func TestLiquidityCapStudy_WorstWeek(t *testing.T) {
	t.Parallel()
	days := func(pnl ...float64) []replay.Day {
		out := make([]replay.Day, len(pnl))
		for i, p := range pnl {
			out[i].PnL = p
		}
		return out
	}
	cases := []struct {
		name string
		days []replay.Day
		want float64
		full bool
	}{
		{"no days", nil, 0, false},
		{"under a week: the whole window", days(-1, 2, -3), -2, false},
		{"exactly a week", days(1, 1, 1, 1, 1, 1, -10), -4, true},
		{"the worst of the rolling weeks", days(5, -1, -1, -1, -1, -1, -1, -1, 5), -7, true},
	}
	for _, c := range cases {
		got, full := replay.WorstWeek(c.days, 100)
		if math.Abs(got-c.want) > 1e-9 || full != c.full {
			t.Errorf("%s: %v %v, want %v %% %v", c.name, got, full, c.want, c.full)
		}
	}
}
