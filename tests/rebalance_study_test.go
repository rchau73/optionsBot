package tests

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The rebalance model and the downsize study (#638): cmd/stress -replay
// -rebalance prorata runs the bot's rebalance on the recorded books, and
// -rebalances prorata,cheapest,winners compares which strangle a downsize
// cuts on the same books. Named TestReplay… to run in the report group
// (scripts/go_tests.sh REPORT_TESTS): each replays ten days of books.

// limitDropBooks records a quiet BTC book; the bot enters both slots at the
// 35 % IM limit on the fourth day, whose DVOL then falls from 50 to 20 at
// noon: the IV percentile drops from the middle band to the lowest (20 %),
// confirmed on that day's and the next day's closes. Spot drifts down 2 %
// from the entries, so their puts are the tested legs when the cut comes.
func limitDropBooks(t *testing.T) ([]recorder.Snapshot, replay.Settings, time.Time) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	const hours = 10 * 24
	drop := 3*24 + 12
	src := &fakeSummary{}
	dvol := 50.0
	rec := recorder.New(src, func() float64 { return dvol }, dir, time.Hour)
	for h := 0; h < hours; h++ {
		if h >= drop {
			dvol = 20
		}
		spot := 100000.0
		if h > 3*24 {
			spot *= 1 - 0.02*math.Min(1, float64(h-3*24)/(3*24))
		}
		src.at = start.Add(time.Duration(h) * time.Hour)
		src.rows = synthBook(src.at, spot)
		if _, err := rec.Snapshot(src.at); err != nil {
			t.Fatal(err)
		}
	}
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// 40 days of DVOL from 30 to 69: 50 ranks in the middle, 20 at the bottom.
	var closes, dvols []orders.DailyClose
	for d := 40; d >= 1; d-- {
		day := start.AddDate(0, 0, -d)
		closes = append(closes, orders.DailyClose{Date: day, Close: 100000})
		dvols = append(dvols, orders.DailyClose{Date: day, Close: 30 + float64(d%40)})
	}
	set := replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 1, Closes: closes, DVOL: dvols}
	return snaps, set, start.Add(time.Duration(drop) * time.Hour)
}

// Scenario: the replay models rebalancing — a confirmed limit change
// produces a downsize as the bot sizes it; without the model nothing is cut.
func TestReplayRebalance_ConfirmedLimitChangeDownsizes(t *testing.T) {
	t.Parallel()
	snaps, set, drop := limitDropBooks(t)
	off, err := replay.Run(snaps, set)
	if err != nil {
		t.Fatal(err)
	}
	set.Rebalance = replay.RebalanceProRata
	bot, err := replay.Run(snaps, set)
	if err != nil {
		t.Fatal(err)
	}
	if off.Entries < 2 || off.Days[0].LimitPct != 35 || off.Days[len(off.Days)-1].LimitPct != 20 {
		t.Fatalf("fixture: entries %d, limit %v → %v; want both slots entered at 35 %% and the limit confirmed at 20 %%",
			off.Entries, off.Days[0].LimitPct, off.Days[len(off.Days)-1].LimitPct)
	}
	overNewLimit := false
	for _, d := range off.Days {
		overNewLimit = overNewLimit || (d.LimitPct == 20 && d.IMPct > 20)
	}
	if off.Downsizes != 0 || off.RebalancePasses != 0 || !overNewLimit {
		t.Errorf("model off: %d passes, %d downsizes, a day above the new 20 %% limit %v; want no pass and the book left above it",
			off.RebalancePasses, off.Downsizes, overNewLimit)
	}
	if bot.Downsizes == 0 || len(bot.DownsizeLog) == 0 {
		t.Fatalf("prorata: no downsize after the limit fell to 20 %%")
	}
	confirmed := drop.Truncate(24*time.Hour).AddDate(0, 0, 2)
	for _, d := range bot.DownsizeLog {
		if d.At.Before(confirmed) {
			t.Errorf("%s cut at %s, before the change was confirmed (%s): an unconfirmed lower limit freezes, it does not cut",
				d.Instrument, d.At, confirmed)
		}
		if d.Qty >= d.FromQty-1e-9 {
			t.Errorf("%s cut %.1f of %.1f: the bot keeps at least a lot", d.Instrument, d.Qty, d.FromQty)
		}
	}
	if first := bot.DownsizeLog[0].At; first.Sub(confirmed) > time.Hour {
		t.Errorf("first cut at %s, want on the first decision of the confirmed limit (%s)", first, confirmed)
	}
	// Pro-rata: both legs of both strangles cut, to the limit.
	if bot.DownsizeLegs != 4 || bot.DownsizeIMFreedPP <= 0 || bot.CutsOverLimit != 0 {
		t.Errorf("legs cut %d, IM freed %.1f pp, cuts left above the limit %d; want 4 legs to the limit",
			bot.DownsizeLegs, bot.DownsizeIMFreedPP, bot.CutsOverLimit)
	}
	if bot.CutsUneven != 0 {
		t.Errorf("%d strangles uneven after a cut: call and put are cut to the same target", bot.CutsUneven)
	}
}

// Scenario: the IM limit stays respected — under every rule, IM after a cut
// is at or below the limit and call and put stay within a lot of each other.
func TestReplayRebalanceStudy_IMLimitRespected(t *testing.T) {
	t.Parallel()
	snaps, set, _ := limitDropBooks(t)
	rows, err := replay.RebalanceStudy(snaps, set, replay.Rebalances)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Rule != replay.RebalanceProRata || rows[1].Rule != replay.RebalanceCheapest || rows[2].Rule != replay.RebalanceWinners {
		t.Fatalf("rows = %d, want prorata, cheapest, winners", len(rows))
	}
	for _, r := range rows {
		res := r.Result
		t.Logf("%s: net %+.4f, downsizes %d legs %d lots %.0f cost %+.4f freed %.1f pp (%.4f/pp), over %d, slot ratio %.2f, top-ups %d, entries %d",
			r.Rule, res.Net, res.Downsizes, res.DownsizeLegs, res.DownsizeLots, res.DownsizeCost, res.DownsizeIMFreedPP, r.CostPerPP(),
			res.CutsOverLimit, res.MaxSlotShareRatio, res.TopUps, res.Entries)
		if res.Downsizes == 0 {
			t.Errorf("%s: no downsize", r.Rule)
		}
		if res.CutsOverLimit != 0 {
			t.Errorf("%s: %d cuts left IM above the limit", r.Rule, res.CutsOverLimit)
		}
		if res.CutsUneven != 0 {
			t.Errorf("%s: %d strangles uneven after a cut", r.Rule, res.CutsUneven)
		}
		if res.MaxSlotShareRatio <= 0 {
			t.Errorf("%s: largest slot ÷ its share %.2f, want measured after the cut", r.Rule, res.MaxSlotShareRatio)
		}
	}
	// Cheapest first frees IM for no more per point than cutting every
	// strangle evenly (it picks the cheapest strangle by that very measure).
	if rows[1].CostPerPP() > rows[0].CostPerPP() {
		t.Errorf("cheapest costs %.5f per pp, prorata %.5f: cheapest first must not cost more", rows[1].CostPerPP(), rows[0].CostPerPP())
	}
	if set.Rebalance != "" {
		t.Errorf("the study changed the caller's settings: %q", set.Rebalance)
	}

	md := replay.RebalanceMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To))
	for _, want := range []string{"### BTC — which strangle a downsize cuts", "Realised P&L of downsizes (% of balance)", "Cost per 1 pp of IM freed (% of balance)",
		"Cuts left above the limit", "Largest slot ÷ its share (max)", "| prorata |", "| cheapest |", "| winners |"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if replay.RebalanceMarkdown("BTC", nil, nil) != "" {
		t.Error("no rows: want no table")
	}
}

// Scenario: the replayed downsizes are checked against the journal's — per
// UTC day and side, within one lot step (the replay's strikes may differ).
func TestReplayCompareDownsizes_JournalBesideReplay(t *testing.T) {
	t.Parallel()
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	put, call := "BTC-27NOV26-75000-P", "BTC-27NOV26-92000-C"
	trades := []orders.Trade{
		{At: at(22, 19), Kind: "close", Instrument: put, Qty: 1.8, Reason: orders.TriggerRebalanceDownsize},
		{At: at(22, 19), Kind: "close", Instrument: call, Qty: 1.8, Reason: orders.TriggerRebalanceDownsize},
		{At: at(22, 19), Kind: "close", Instrument: "BTC-27NOV26-95000-C", Qty: 0.3, Reason: orders.TriggerRebalanceDownsize},
		{At: at(22, 20), Kind: "close", Instrument: put, Qty: 0.5, Reason: "stop_loss"},                    // not a downsize
		{At: at(26, 9), Kind: "close", Instrument: put, Qty: 0.4, Reason: orders.TriggerRebalanceDownsize}, // replay cut less
		{At: at(30, 9), Kind: "close", Instrument: put, Qty: 0.3, Reason: orders.TriggerRebalanceDownsize}, // after the window
		{At: at(21, 9), Kind: "open", Instrument: put, Qty: 3.1, Reason: orders.TriggerRebalanceDownsize},  // an open
	}
	replayed := []replay.Downsize{
		{At: at(22, 18), Instrument: "BTC-27NOV26-73000-P", Qty: 1.7}, // another strike, the same side
		{At: at(22, 18), Instrument: call, Qty: 2.0},
		{At: at(26, 10), Instrument: put, Qty: 0.2},
		{At: at(27, 10), Instrument: call, Qty: 0.1}, // the replay alone
	}
	got := replay.CompareDownsizes(trades, replayed, at(20, 0), at(28, 0), 0.1)
	want := []replay.DownsizeMatch{
		{Day: at(22, 0), Side: "call", Journal: 2.1, Replay: 2.0, Match: true},
		{Day: at(22, 0), Side: "put", Journal: 1.8, Replay: 1.7, Match: true},
		{Day: at(26, 0), Side: "put", Journal: 0.4, Replay: 0.2, Match: false},
		{Day: at(27, 0), Side: "call", Journal: 0, Replay: 0.1, Match: true},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.Day.Equal(w.Day) || g.Side != w.Side || math.Abs(g.Journal-w.Journal) > 1e-9 ||
			math.Abs(g.Replay-w.Replay) > 1e-9 || g.Match != w.Match {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
	}
	md := replay.DownsizeMatchMarkdown("BTC", got)
	if !strings.Contains(md, "3 of 4 day × side rows within one lot step") || !strings.Contains(md, "| 2026-10-26 | put | 0.4 | 0.2 | no |") {
		t.Errorf("journal check:\n%s", md)
	}
	if !strings.Contains(replay.DownsizeMatchMarkdown("BTC", nil), "no downsize") {
		t.Error("no rows: want the empty line")
	}
}

// The journal's risk_limit lines give the policy the bot held and its
// starts; the margin balance at a time is the last one journaled.
func TestReplayJournalPolicy_LimitsAndRestarts(t *testing.T) {
	t.Parallel()
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	recs := []orders.RiskRecord{
		{Timestamp: at(22, 19), Change: orders.RiskLimitChanged, Detail: "startup: DVOL band ≥0 (IM 20%) (confirmed)", LimitIMPct: 20, MarginBalance: 1.70},
		{Timestamp: at(21, 0), Change: orders.RiskLimitChanged, Detail: "DVOL band ≥0 (IM 20%) (confirmed)", LimitIMPct: 20, MarginBalance: 1.65},
		{Timestamp: at(24, 9), Change: orders.RiskFrozen, Detail: "gamma regime turned negative, not yet confirmed", LimitIMPct: 20, Frozen: true},
		{Timestamp: at(25, 0), Change: orders.RiskLimitChanged, Detail: "DVOL band ≥30 (IM 35%) (confirmed)", LimitIMPct: 35, MarginBalance: 1.60},
		{Timestamp: at(25, 1), Change: orders.RiskIMUnderUse}, // no limit on it: skipped
	}
	limits, restarts := replay.JournalPolicy(recs)
	want := []replay.LimitAt{{At: at(21, 0), LimitPct: 20}, {At: at(22, 19), LimitPct: 20}, {At: at(24, 9), LimitPct: 20, Frozen: true}, {At: at(25, 0), LimitPct: 35}}
	if len(limits) != len(want) {
		t.Fatalf("limits = %+v, want %+v", limits, want)
	}
	for i := range want {
		if !limits[i].At.Equal(want[i].At) || limits[i].LimitPct != want[i].LimitPct || limits[i].Frozen != want[i].Frozen {
			t.Errorf("limit %d = %+v, want %+v", i, limits[i], want[i])
		}
	}
	if len(restarts) != 1 || !restarts[0].Equal(at(22, 19)) {
		t.Errorf("restarts = %v, want the one startup line", restarts)
	}
	balances := replay.BalanceSamples(recs, nil, "BTC")
	if b, ok := replay.JournalBalanceAt(balances, at(23, 0)); !ok || b.Coin != 1.70 || b.Source != "risk_limit" {
		t.Errorf("balance on 23 Oct = %+v, want 1.70 (the last journaled before it)", b)
	}

	// Read from orders.log: only the risk_limit lines, a torn one skipped.
	path := filepath.Join(t.TempDir(), "orders.log")
	log := `{"timestamp":"2026-10-22T19:48:45Z","event":"risk_limit","change":"limit_changed","detail":"startup: x","limit_im_pct":20,"frozen":false}
{"timestamp":"2026-10-22T19:49:00Z","event":"order_filled","instrument":"BTC-27NOV26-75000-P"}
{"timestamp":"2026-10-24T09:36:28Z","event":"risk_limit","change":"frozen","limit_im_pct":20,"frozen":true}
{"timestamp":"2026-10-24T09:40:28Z","event":"risk_limit","change":"unfro
`
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := replay.ReadRiskRecords(path)
	if err != nil || len(read) != 2 || read[1].Change != orders.RiskFrozen || !read[1].Frozen {
		t.Errorf("read %+v (%v), want the two complete risk_limit lines", read, err)
	}
	if _, err := replay.ReadRiskRecords(filepath.Join(t.TempDir(), "none.log")); err == nil {
		t.Error("a missing journal must fail")
	}
}

// The replay starts from the bot's own margin balance at -from (PM on #640:
// a 1-coin fallback reported ETH's −0.35 ETH as −35 % of a ~489 ETH book):
// risk_limit lines and P&L points both count, a USD balance is converted at
// the spot of its P&L point, a start's line without a balance is skipped,
// and before the first record the first one after -from is taken.
func TestReplayJournalBalance_StartingBalance(t *testing.T) {
	t.Parallel()
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	f := func(v float64) *float64 { return &v }
	recs := []orders.RiskRecord{
		{Timestamp: at(22, 19), Change: orders.RiskLimitChanged, Detail: "startup: x", LimitIMPct: 20}, // before the first margin read
		{Timestamp: at(24, 9), Change: orders.RiskFrozen, LimitIMPct: 20, Unit: "ETH", MarginBalance: 489.1},
		{Timestamp: at(26, 9), Change: orders.RiskFrozen, LimitIMPct: 20, Unit: "USD", MarginBalance: 1_000_000}, // no P&L point near it
		{Timestamp: at(27, 9), Change: orders.RiskFrozen, LimitIMPct: 20, Unit: "USD", MarginBalance: 1_000_000},
	}
	points := []history.Point{
		{T: at(23, 0), Spot: 4000}, // margin unknown that cycle
		{T: at(25, 0), Spot: 4000, MarginBalance: f(486.0), Unit: "ETH"},
		{T: at(27, 9).Add(20 * time.Minute), Spot: 2000, MarginBalance: f(1_000_000), Unit: "USD"},
		{T: at(28, 0), Spot: 2000, MarginBalance: f(480.1), Unit: "BTC"}, // another coin: left out
	}
	samples := replay.BalanceSamples(recs, points, "ETH")
	if len(samples) != 4 {
		t.Fatalf("samples = %+v, want 24 Oct (journal), 25 Oct (P&L), 27 Oct (journal, at the P&L spot), 27 Oct (P&L, USD)", samples)
	}
	for _, c := range []struct {
		name   string
		t      time.Time
		coin   float64
		source string
	}{
		{"before any record: the first after -from", at(20, 0), 489.1, "risk_limit"},
		{"a start's line has no balance: the last before it", at(24, 12), 489.1, "risk_limit"},
		{"the P&L history between journal lines", at(26, 12), 486.0, "pnl_history"},
		{"a USD balance at the nearest P&L spot", at(27, 9).Add(time.Minute), 500, "risk_limit"},
		{"after every record: the last", at(31, 0), 500, "pnl_history"},
	} {
		b, ok := replay.JournalBalanceAt(samples, c.t)
		if !ok || math.Abs(b.Coin-c.coin) > 1e-9 || b.Source != c.source {
			t.Errorf("%s: %+v (%v), want %.1f from %s", c.name, b, ok, c.coin, c.source)
		}
	}
	if _, ok := replay.JournalBalanceAt(replay.BalanceSamples(recs[:1], nil, "ETH"), at(20, 0)); ok {
		t.Error("no balance recorded: want none (the replay then refuses to guess one)")
	}

	// Read from pnl_history.jsonl: a torn line skipped, a missing file is no points.
	path := filepath.Join(t.TempDir(), "pnl_history.jsonl")
	lines := `{"t":"2026-10-28T00:00:00Z","spot":4000,"margin_balance":489.1,"unit":"ETH"}
{"t":"2026-10-28T00:01:00Z","spot":4000,"margin_bal
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := replay.ReadPnLHistory(path)
	if err != nil || len(read) != 1 || read[0].MarginBalance == nil || *read[0].MarginBalance != 489.1 {
		t.Errorf("read %+v (%v), want the one complete point", read, err)
	}
	if read, err := replay.ReadPnLHistory(filepath.Join(t.TempDir(), "none.jsonl")); err != nil || read != nil {
		t.Errorf("missing file: %v (%v), want no points and no error", read, err)
	}
}

// journalStrangles records a quiet BTC book and starts the replay holding the
// two journal strangles, 3 lots a leg, so no slot enters.
func journalStrangles(t *testing.T) ([]recorder.Snapshot, replay.Settings, time.Time) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 6*24, func(int) float64 { return 100000 })
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	set := replay.Settings{Cfg: replayConfig(t, "sma"), From: start.AddDate(0, 0, 3), Equity: 1, Closes: closes, DVOL: dvol,
		Book: strangleBook(t, snaps[3*24], 3)}
	return snaps, set, start
}

// strangleBook is the two journal strangles at the snapshot's mids.
func strangleBook(t *testing.T, snap recorder.Snapshot, qty float64) []replay.StartLeg {
	t.Helper()
	q := snap.ByName()
	var book []replay.StartLeg
	for _, l := range []struct {
		dte  int
		name string
	}{{25, "BTC-30OCT26-112000-C"}, {25, "BTC-30OCT26-90000-P"}, {50, "BTC-27NOV26-120000-C"}, {50, "BTC-27NOV26-84000-P"}} {
		if _, ok := q[l.name]; !ok {
			t.Fatalf("%s not in the recording", l.name)
		}
		book = append(book, replay.StartLeg{Slot: config.StrangleSlot{TargetDTE: l.dte, EntryDelta: 0.16}, Instrument: l.name, Qty: qty, Price: q[l.name].LiveMid()})
	}
	return book
}

// A pass under the limit tops up as the bot does: both sides on the strangle's
// expiry, or none. The startup pass after a restart runs it on a book well
// under the limit; each guard of topUps leaves the book as it was.
func TestReplayRebalance_TopUpsAddBothSidesOrNone(t *testing.T) {
	t.Parallel()
	snaps, journal, start := journalStrangles(t)
	off, err := replay.Run(snaps, journal)
	if err != nil {
		t.Fatal(err)
	}
	if off.Entries != 0 {
		t.Fatalf("fixture: %d entries, want the two journal strangles alone", off.Entries)
	}
	limit := off.Days[1].IMPct + 15
	bear, bearSet := liveNegativeBooks(t)
	for _, c := range []struct {
		name  string
		snaps []recorder.Snapshot
		set   replay.Settings
		tune  func(*config.Config, *replay.Settings)
		check func(t *testing.T, r replay.Result)
	}{
		{name: "under the limit: a complement on each strangle", snaps: snaps, set: journal, check: func(t *testing.T, r replay.Result) {
			if r.TopUps == 0 || r.TopUpLots <= 0 || r.TopUpsHeld != 0 || r.Downsizes != 0 || r.Unfilled != 0 {
				t.Errorf("top-ups %d (%.0f lots), held %d, downsizes %d, unfilled %d; want top-ups alone",
					r.TopUps, r.TopUpLots, r.TopUpsHeld, r.Downsizes, r.Unfilled)
			}
		}},
		{name: "premium under the floor", snaps: snaps, set: journal,
			tune: func(cfg *config.Config, _ *replay.Settings) { cfg.MinPremiumBTC = 1 }, check: noTopUp},
		{name: "the strike's market takes less than a lot", snaps: snaps, set: journal,
			tune: func(cfg *config.Config, _ *replay.Settings) { cfg.LiquidityCapOIPct = 0.001 }, check: noTopUp},
		{name: "no strike in reach", snaps: snaps, set: journal,
			tune: func(cfg *config.Config, _ *replay.Settings) { cfg.DeltaSlippage = 1e-9 }, check: noTopUp},
		{name: "live negative regime holds a side: held and re-armed", snaps: bear, set: bearSet,
			tune: func(_ *config.Config, s *replay.Settings) {
				s.RegimeHold = replay.RegimeHoldLive
				s.Book = strangleBook(t, bear[3*24], 3)
			}, check: func(t *testing.T, r replay.Result) {
				if r.TopUps != 0 || r.TopUpsHeld < 2 {
					t.Errorf("top-ups %d, held %d; want none sold and the held pass retried", r.TopUps, r.TopUpsHeld)
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := c.set
			set.Cfg = replayConfig(t, "sma")
			set.Rebalance = replay.RebalanceProRata
			set.Limits = []replay.LimitAt{{At: start, LimitPct: limit}}
			set.Restarts = []time.Time{set.From.Add(36 * time.Hour)}
			if c.tune != nil {
				c.tune(set.Cfg, &set)
			}
			res, err := replay.Run(c.snaps, set)
			if err != nil {
				t.Fatal(err)
			}
			if res.RebalancePasses == 0 {
				t.Fatal("no pass ran")
			}
			c.check(t, res)
		})
	}
}

func noTopUp(t *testing.T, r replay.Result) {
	t.Helper()
	if r.TopUps != 0 || r.Downsizes != 0 {
		t.Errorf("top-ups %d, downsizes %d; want the book left as it was", r.TopUps, r.Downsizes)
	}
}

func TestReplayRebalanceStudy_NoConfigAndNothingFreed(t *testing.T) {
	t.Parallel()
	if _, err := replay.RebalanceStudy(nil, replay.Settings{}, replay.Rebalances); err == nil {
		t.Error("no config: want an error")
	}
	if got := (replay.RebalanceRow{}).CostPerPP(); got != 0 {
		t.Errorf("nothing freed: cost per pp %v, want 0", got)
	}
}

// Scenario: the replay models rebalancing — at a restart (the startup pass),
// IM above limit + 1.5 pp is cut to the target, IM within the band is left
// alone (#123); without a restart nothing is cut (alert only, #166).
func TestReplayRebalance_StartupPassBand(t *testing.T) {
	t.Parallel()
	snaps, journal, start := journalStrangles(t)
	restart := journal.From.Add(36 * time.Hour)
	run := func(limit float64, restarts []time.Time) replay.Result {
		t.Helper()
		set := journal
		set.Rebalance, set.Limits, set.Restarts = replay.RebalanceProRata, []replay.LimitAt{{At: start, LimitPct: limit}}, restarts
		res, err := replay.Run(snaps, set)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	base := run(50, nil)
	im := base.Days[1].IMPct // the book's IM around the restart (both slots held: no entry)
	if base.Entries != 0 || im <= 5 {
		t.Fatalf("fixture: entries %d, IM %.1f %%; want the two journal strangles alone", base.Entries, im)
	}
	if res := run(im-5, nil); res.Downsizes != 0 || res.RebalancePasses != 0 {
		t.Errorf("no restart, IM 5 pp over the limit: %d passes, %d downsizes; want none (alert only)", res.RebalancePasses, res.Downsizes)
	}
	over := run(im-5, []time.Time{restart})
	if over.Downsizes != 1 || len(over.DownsizeLog) == 0 || over.DownsizeLog[0].At.Before(restart) || over.CutsOverLimit != 0 {
		t.Errorf("restart, IM 5 pp over: %d downsizes (%+v), %d left above the limit; want one cut at the restart, to the limit",
			over.Downsizes, over.DownsizeLog, over.CutsOverLimit)
	}
	if res := run(im-0.5, []time.Time{restart}); res.Downsizes != 0 || res.StartupInBand != 1 {
		t.Errorf("restart, IM 0.5 pp over: %d downsizes, %d startup passes in the band; want the book left alone", res.Downsizes, res.StartupInBand)
	}
}
