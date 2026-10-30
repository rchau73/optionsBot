package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The regime-hold study (#550): cmd/stress -replay -regimeholds
// confirmed,live replays the same recorded books once per regime-side rule —
// the bot's (confirmed negative regime) and holding the trend's side while
// the live regime is negative — and prints one table per coin.

// liveNegativeBooks is a bear trend whose live GEX regime turns negative on
// the first traded day, after three positive recorded days: confirmed
// positive, live negative, spot just under the flip (inside the shed
// buffer, so nothing is shed). The book starts with the 50-day slot's call
// alone, so its put is due for repair.
func liveNegativeBooks(t *testing.T) ([]recorder.Snapshot, replay.Settings) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 5*24, func(int) float64 { return 100000 })
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	from := start.AddDate(0, 0, 3)
	var recs []recorder.GEXRecord
	for h := 0; h < 5*24; h++ {
		at := start.Add(time.Duration(h)*time.Hour + time.Minute)
		r := recorder.GEXRecord{ComputedAt: at, Spot: 100000, Regime: "POSITIVE/PINNING", Flip: 100800, FlipFound: true, GEXSum: 1e6, DVOL: 50}
		if !at.Before(from) {
			r.Regime, r.GEXSum = "NEGATIVE/ACCELERATION", -1e6
		}
		recs = append(recs, r)
	}
	// 40 falling closes with one swing high, ending above the recorded
	// 100,000: a bear SMA trend (SMA9 under SMA21, price under SMA9).
	var closes, dvol []orders.DailyClose
	for d := 40; d >= 1; d-- {
		day := start.AddDate(0, 0, -d)
		c := 100000 + float64(d)*300
		if d == 20 {
			c += 5000
		}
		closes = append(closes, orders.DailyClose{Date: day, Close: c})
		dvol = append(dvol, orders.DailyClose{Date: day, Close: 50})
	}
	call := "BTC-27NOV26-120000-C"
	q, ok := snaps[3*24].ByName()[call]
	if !ok {
		t.Fatalf("%s not in the recording", call)
	}
	book := []replay.StartLeg{{Slot: config.StrangleSlot{TargetDTE: 50, EntryDelta: 0.16}, Instrument: call, Qty: 1, Price: q.LiveMid()}}
	return snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: from, Equity: 1, Closes: closes, DVOL: dvol, GEX: recs, Book: book}
}

// Scenario: study — replayed with and without a live-negative-regime hold,
// the report states the P&L of both and how many entries and side holds
// each rule made.
func TestRegimeHoldStudy_ReplaysBothRules(t *testing.T) {
	t.Parallel()
	snaps, set := liveNegativeBooks(t)
	rows, err := replay.RegimeHoldStudy(snaps, set, replay.RegimeHolds)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Hold != replay.RegimeHoldConfirmed || rows[1].Hold != replay.RegimeHoldLive {
		t.Fatalf("rows = %+v, want confirmed then live", rows)
	}
	bot, live := rows[0].Result, rows[1].Result
	for _, d := range bot.Days {
		if d.Direction != -1 || d.ConfirmedNegative || !strings.HasPrefix(d.Regime, "NEGATIVE") {
			t.Fatalf("fixture: day %s trend %+d, confirmed negative %v, live %s; want bear, not confirmed, live negative",
				d.Date.Format(time.DateOnly), d.Direction, d.ConfirmedNegative, d.Regime)
		}
	}
	// The bot repairs the put into the live negative regime (a freeze does
	// not stop a repair); the live hold keeps it out.
	if bot.Repairs == 0 || bot.SoldIntoLiveNegative == 0 || bot.RegimeSideHolds != 0 {
		t.Errorf("confirmed: repairs %d, legs sold into the live negative regime %d, holds %d; want the put repaired, nothing held",
			bot.Repairs, bot.SoldIntoLiveNegative, bot.RegimeSideHolds)
	}
	if live.SoldIntoLiveNegative != 0 || live.RegimeSideHolds == 0 || live.Repairs != 0 {
		t.Errorf("live: legs sold into the live negative regime %d, holds %d, repairs %d; want the put held every decision",
			live.SoldIntoLiveNegative, live.RegimeSideHolds, live.Repairs)
	}
	// Entries are frozen by the unconfirmed change under both rules: the
	// live hold adds nothing there.
	if bot.Entries != 0 || live.Entries != 0 {
		t.Errorf("entries %d / %d, want none: a regime turning negative freezes new risk until confirmed", bot.Entries, live.Entries)
	}

	md := replay.RegimeHoldMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To))
	for _, want := range []string{"### BTC — regime-side hold", "Net BTC", "Entries opened without the held side", "Side holds (decisions)",
		"Legs sold into a live negative regime on the trend's side", "| confirmed |", "| live |", "**Indicative:**"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if replay.RegimeHoldMarkdown("BTC", nil, nil) != "" {
		t.Error("no rows: want no table")
	}
	if set.RegimeHold != "" {
		t.Errorf("the study changed the caller's settings: %q", set.RegimeHold)
	}
}

// The study's rule is the bot's plus the live case; under a confirmed
// negative regime both block the same side, and nothing blocks the side
// against the trend.
func TestLiveRegimeSideBlockReason(t *testing.T) {
	t.Parallel()
	confirmedNeg := risk.Status{RegimeUsed: true, RegimeNegative: true}
	neg := strategy.GammaDecision{Regime: "NEGATIVE/ACCELERATION"}
	pos := strategy.GammaDecision{Regime: "POSITIVE/PINNING"}
	withTrend := func(d strategy.GammaDecision, trend int) strategy.GammaDecision { d.TrendDir = trend; return d }
	cases := []struct {
		name, hold, typ string
		dec             strategy.GammaDecision
		st              risk.Status
		blocked         bool
	}{
		{"bot: live negative, bear, put sold", replay.RegimeHoldConfirmed, "put", withTrend(neg, -1), risk.Status{}, false},
		{"live: live negative, bear, put held", replay.RegimeHoldLive, "put", withTrend(neg, -1), risk.Status{}, true},
		{"live: live negative, bull, call held", replay.RegimeHoldLive, "call", withTrend(neg, 1), risk.Status{}, true},
		{"live: live negative, bear, call sold", replay.RegimeHoldLive, "call", withTrend(neg, -1), risk.Status{}, false},
		{"live: live negative, neutral, put sold", replay.RegimeHoldLive, "put", withTrend(neg, 0), risk.Status{}, false},
		{"live: live positive, bear, put sold", replay.RegimeHoldLive, "put", withTrend(pos, -1), risk.Status{}, false},
		{"live: unknown regime, bear, put sold", replay.RegimeHoldLive, "put", strategy.GammaDecision{TrendDir: -1}, risk.Status{}, false},
		{"both: confirmed negative, bear, put held", replay.RegimeHoldConfirmed, "put", withTrend(pos, -1), confirmedNeg, true},
		{"both: confirmed negative, bear, put held (live)", replay.RegimeHoldLive, "put", withTrend(pos, -1), confirmedNeg, true},
	}
	for _, c := range cases {
		if got := replay.LiveRegimeSideBlockReason(c.hold, c.typ, c.dec, c.st) != ""; got != c.blocked {
			t.Errorf("%s: blocked %v, want %v", c.name, got, c.blocked)
		}
	}
}

// Scenario: protections stay — under the live hold the stop-loss, the
// delta exit and the MM cut still close an open put: the crash replay exits
// exactly as under the bot's rule.
func TestRegimeHoldStudy_ProtectionsStay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 6*24, func(h int) float64 {
		if h < 4*24 {
			return 100000
		}
		return 100000 * (1 - 0.25*min(1, float64(h-4*24)/48))
	})
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	set := replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 1, Closes: closes, DVOL: dvol}
	rows, err := replay.RegimeHoldStudy(snaps, set, replay.RegimeHolds)
	if err != nil {
		t.Fatal(err)
	}
	bot, live := rows[0].Result, rows[1].Result
	if live.Stops+live.DeltaExits+live.MMCuts+live.Sheds == 0 || live.Realised >= 0 {
		t.Errorf("live hold: stops %d delta exits %d MM cuts %d sheds %d realised %.4f: the crash must still close the puts at a loss",
			live.Stops, live.DeltaExits, live.MMCuts, live.Sheds, live.Realised)
	}
	if live.Stops != bot.Stops || live.DeltaExits != bot.DeltaExits || live.MMCuts != bot.MMCuts {
		t.Errorf("exits differ: live stops %d delta exits %d MM cuts %d, bot %d %d %d",
			live.Stops, live.DeltaExits, live.MMCuts, bot.Stops, bot.DeltaExits, bot.MMCuts)
	}
}
