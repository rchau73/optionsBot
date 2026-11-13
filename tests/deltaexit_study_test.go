package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The delta-exit study (#769): cmd/stress -replay -deltaexits
// 0.25,0.30,0.35,off replays the same recorded books once per
// delta_exit_threshold (off = the 2× stop alone) and prints one table per
// coin with worst week, return on margin and re-entries.

// crashBooks is three calm days, then spot falls 25 % over two days: the
// puts sold on the calm days reach every delta-exit threshold and the stop.
func crashBooks(t *testing.T) ([]recorder.Snapshot, replay.Settings) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 8*24, func(h int) float64 {
		if h < 4*24 {
			return 100000
		}
		return 100000 * (1 - 0.25*min(1, float64(h-4*24)/48))
	})
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	closes, dvol := flatHistory(start)
	return snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 1, Closes: closes, DVOL: dvol}
}

// Scenario: the study reports the variants — given the recorded books, when
// cmd/stress runs the four thresholds, then worst week, return on margin and
// re-entries are listed per threshold, and no shipped config changes.
func TestDeltaExitStudy_ReportsTheVariants(t *testing.T) {
	t.Parallel()
	snaps, set := crashBooks(t)
	shipped := set.Cfg.DeltaExitThreshold
	rows, err := replay.DeltaExitStudy(snaps, set, replay.DeltaExitThresholds)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0.25", "0.30", "0.35", "off"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.Label() != want[i] {
			t.Errorf("row %d = %s, want %s", i, r.Label(), want[i])
		}
	}
	bot, off := rows[1].Result, rows[3].Result
	if bot.DeltaExits == 0 || bot.DeltaExitPremium <= 0 || bot.DeltaExitPnL >= 0 {
		t.Errorf("0.30: delta exits %d, premium %.4f, P&L %.4f; want the crash to exit puts at a loss",
			bot.DeltaExits, bot.DeltaExitPremium, bot.DeltaExitPnL)
	}
	if off.DeltaExits != 0 || off.DeltaExitPremium != 0 {
		t.Errorf("off: delta exits %d, premium %.4f; want none", off.DeltaExits, off.DeltaExitPremium)
	}
	if off.Stops+off.MMCuts == 0 {
		t.Errorf("off: stops %d, MM cuts %d; the stop alone must still close the puts", off.Stops, off.MMCuts)
	}
	for _, r := range rows {
		if r.Result.AvgIM <= 0 {
			t.Errorf("%s: average IM %.4f; return on margin needs margin used", r.Label(), r.Result.AvgIM)
		}
	}
	if set.Cfg.DeltaExitThreshold != shipped || shipped != 0.30 {
		t.Errorf("the study changed the caller's threshold: %.2f, shipped %.2f", set.Cfg.DeltaExitThreshold, shipped)
	}

	md := replay.DeltaExitMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To))
	for _, w := range []string{"### BTC — delta exit vs the stop alone", "Worst week (% of balance)", "Return on margin (% of avg IM)",
		"Re-entries", "Delta exit P&L (% of premium)", "| 0.25 |", "| 0.30 |", "| 0.35 |", "| off |"} {
		if !strings.Contains(md, w) {
			t.Errorf("table lacks %q:\n%s", w, md)
		}
	}
	if !strings.Contains(md, "| off | ") || !strings.Contains(strings.SplitN(md, "| off |", 2)[1], "| 0 | — |") {
		t.Errorf("off row: want 0 delta exits and no P&L share:\n%s", md)
	}
	if replay.DeltaExitMarkdown("BTC", nil, nil) != "" {
		t.Error("no rows: want no table")
	}
}

// Re-entries count the repairs of a stopped or exited leg, once the cooldown
// has passed: a zero cooldown lets the crash's exited puts be re-sold.
func TestDeltaExitStudy_CountsReEntries(t *testing.T) {
	t.Parallel()
	snaps, set := crashBooks(t)
	set.Cfg.RepairCooldownHours = 0
	rows, err := replay.DeltaExitStudy(snaps, set, []float64{0.30})
	if err != nil {
		t.Fatal(err)
	}
	res := rows[0].Result
	if res.ReEntries == 0 || res.ReEntries > res.Repairs {
		t.Errorf("re-entries %d, repairs %d; want the exited puts re-sold and counted among the repairs", res.ReEntries, res.Repairs)
	}
}

func TestParseDeltaExits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []float64
		bad  bool
	}{
		{"0.25,0.30,0.35,off", []float64{0.25, 0.30, 0.35, 0}, false},
		{"all", replay.DeltaExitThresholds, false},
		{" off ", []float64{0}, false},
		{"0", nil, true},
		{"1", nil, true},
		{"-0.3", nil, true},
		{"0.3,x", nil, true},
		{"", nil, true},
	}
	for _, c := range cases {
		got, err := replay.ParseDeltaExits(c.in)
		if (err != nil) != c.bad {
			t.Errorf("%q: err %v, want error %v", c.in, err, c.bad)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%q = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

// A threshold at or under an entry delta would exit every new leg at once:
// the study refuses it, as config.Validate does.
func TestDeltaExitStudy_RefusesAThresholdUnderTheEntryDelta(t *testing.T) {
	t.Parallel()
	snaps, set := crashBooks(t)
	if _, err := replay.DeltaExitStudy(snaps, set, []float64{0.10}); err == nil || !strings.Contains(err.Error(), "entry delta") {
		t.Errorf("0.10: err %v, want a refusal naming the entry delta", err)
	}
	if _, err := replay.DeltaExitStudy(snaps, replay.Settings{}, []float64{0.30}); err == nil {
		t.Error("no settings: want an error")
	}
}
