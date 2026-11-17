package tests

import (
	"strings"
	"testing"

	"optionsbot/internal/replay"
)

// The one-sided hedge study (#812): cmd/stress -replay -hedgemodes replays
// the same recorded books with the bot's futures hedge and three changes —
// standing down on a one-sided book, hedging only the delta that opposes the
// trend, and trading after repairs and entries. The fixture is shedBooks (a
// bear trend in a negative regime), cut to its call alone: a calls-only book
// leaning with the trend, as the mvp books since 2026-11-05.

func hedgeModes(t *testing.T, list string) []replay.HedgeMode {
	t.Helper()
	m, err := replay.ParseHedgeModes(list)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func hedgeRow(t *testing.T, rows []replay.HedgeModeRow, mode string, cost float64) replay.Result {
	t.Helper()
	for _, r := range rows {
		if r.Mode.String() == mode && r.CostMultiple == cost {
			return r.Result
		}
	}
	t.Fatalf("no row %s at %g×", mode, cost)
	return replay.Result{}
}

func TestHedgeModeStudy_CallsOnlyBook(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	set.Book = set.Book[:1] // the call alone
	rows, err := replay.HedgeModeStudy(snaps, set, hedgeModes(t, replay.DefaultHedgeModes), []float64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 14 {
		t.Fatalf("rows = %d, want 7 modes at 2 cost multiples", len(rows))
	}
	off, bot := hedgeRow(t, rows, "off", 1), hedgeRow(t, rows, "bot", 1)
	plain, err := replay.Run(snaps, set)
	if err != nil {
		t.Fatal(err)
	}
	if off.Net != plain.Net || off.HedgeTrades != 0 {
		t.Errorf("off: net %.6f, %d perp orders; want the plain replay's %.6f and no perpetual", off.Net, off.HedgeTrades, plain.Net)
	}
	if bot.HedgeTrades == 0 || bot.MaxPerpUSD <= 0 {
		t.Fatalf("fixture: bot traded %d perp orders, largest %.0f USD; want the calls' short delta hedged", bot.HedgeTrades, bot.MaxPerpUSD)
	}
	// A calls-only book in a bear trend: both changes leave its delta alone.
	for _, mode := range []string{"onesided", "trend", "onesided+after", "trend+after"} {
		if r := hedgeRow(t, rows, mode, 1); r.HedgeTrades != 0 || r.MaxPerpUSD != 0 {
			t.Errorf("%s: %d perp orders, largest %.0f USD; want none on a calls-only book in a bear trend", mode, r.HedgeTrades, r.MaxPerpUSD)
		}
	}
	if b2 := hedgeRow(t, rows, "bot", 2); b2.PerpFees <= bot.PerpFees {
		t.Errorf("2× costs: perp fees %.6f, want more than at 1× (%.6f)", b2.PerpFees, bot.PerpFees)
	}

	md := replay.HedgeModeMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To), set.Cfg.MaxMMPct)
	for _, want := range []string{"the futures hedge on a one-sided book", "Hedge P&L BTC", "Options BTC", "Perp orders", "Worst week (%)",
		"| bot | 1× |", "| onesided+after | 2× |", "| baseline |"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if set.Hedge != (replay.HedgeMode{}) || set.CostMultiple != 0 {
		t.Errorf("the study changed the caller's settings: %+v %g", set.Hedge, set.CostMultiple)
	}
}

// Both legs held: onesided hedges as the bot does.
func TestHedgeModeStudy_BothSidesHedged(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	rows, err := replay.HedgeModeStudy(snaps, set, hedgeModes(t, "onesided"), []float64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Mode.String() != "bot" {
		t.Fatalf("rows = %v, want the bot's added first", rows)
	}
	bot, one := hedgeRow(t, rows, "bot", 1), hedgeRow(t, rows, "onesided", 1)
	if one.HedgeTrades != bot.HedgeTrades || one.Net != bot.Net {
		t.Errorf("onesided: %d perp orders, net %.6f; want the bot's %d and %.6f while both sides are held", one.HedgeTrades, one.Net, bot.HedgeTrades, bot.Net)
	}
}

func TestHedgedDelta(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		rule      string
		options   float64
		trend     int
		oneSided  bool
		want      float64
		standDown bool
	}{
		{replay.HedgeBot, -28.8, -1, true, -28.8, false},
		{replay.HedgeOneSided, -28.8, -1, true, 0, true},
		{replay.HedgeOneSided, -3, -1, false, -3, false},
		{replay.HedgeTrend, -28.8, -1, true, 0, false},    // bear book in a bear trend: kept
		{replay.HedgeTrend, 0.05, -1, false, 0.05, false}, // long book in a bear trend: hedged
		{replay.HedgeTrend, 0.05, 1, false, 0, false},
		{replay.HedgeTrend, -0.05, 1, false, -0.05, false},
		{replay.HedgeTrend, -0.05, 0, false, -0.05, false},
	} {
		got, stand := replay.HedgedDelta(c.rule, c.options, c.trend, c.oneSided)
		if got != c.want || stand != c.standDown {
			t.Errorf("HedgedDelta(%s, %g, %d, %v) = %g, %v; want %g, %v", c.rule, c.options, c.trend, c.oneSided, got, stand, c.want, c.standDown)
		}
	}
}

func TestParseHedgeMode(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]replay.HedgeMode{
		"off": {Rule: "off"}, "bot": {Rule: "bot"}, " trend+after ": {Rule: "trend", After: true}, "onesided": {Rule: "onesided"},
	} {
		if got, err := replay.ParseHedgeMode(in); err != nil || got != want {
			t.Errorf("ParseHedgeMode(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "off+after", "hedged", "bot+before"} {
		if _, err := replay.ParseHedgeMode(bad); err == nil {
			t.Errorf("ParseHedgeMode(%q): want an error", bad)
		}
	}
}
