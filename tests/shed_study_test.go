package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The shed study (#637): cmd/stress -replay -sheds all replays the same
// recorded books once per variant of what a GEX shed does — the bot's (buy
// the whole side back), none, partial, delta-filtered and a perp hedge — at
// the recorded costs and at twice them, and prints one table per coin.

const shedPut, shedCall = "BTC-27NOV26-90000-P", "BTC-27NOV26-120000-C"

// shedBooks is a bear trend in a negative gamma regime with spot ~9 % under
// the flip from the first recorded hour, so the put side is shed as soon as
// trading starts (10-10). The book holds the 50-day slot's strangle, 2 BTC a
// leg, its put at |Δ| ≈ 0.25. path is spot by hour from 10-07.
func shedBooks(t *testing.T, days int, path func(h int) float64) ([]recorder.Snapshot, replay.Settings) {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, days*24, path)
	snaps, err := recorder.ReadDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	from := start.AddDate(0, 0, 3)
	var recs []recorder.GEXRecord
	for h := 0; h < days*24; h++ {
		recs = append(recs, recorder.GEXRecord{ComputedAt: start.Add(time.Duration(h)*time.Hour + time.Minute), Spot: path(h),
			Regime: "NEGATIVE/ACCELERATION", Flip: 110000, FlipFound: true, GEXSum: -1e6, DVOL: 50})
	}
	var closes, dvol []orders.DailyClose // falling closes with one swing high: a bear SMA trend
	for d := 40; d >= 1; d-- {
		day := start.AddDate(0, 0, -d)
		c := 100000 + float64(d)*300
		if d == 20 {
			c += 5000
		}
		closes = append(closes, orders.DailyClose{Date: day, Close: c})
		dvol = append(dvol, orders.DailyClose{Date: day, Close: 50})
	}
	q := snaps[3*24].ByName()
	slot := config.StrangleSlot{TargetDTE: 50, EntryDelta: 0.16}
	var book []replay.StartLeg
	for _, name := range []string{shedCall, shedPut} {
		if _, ok := q[name]; !ok {
			t.Fatalf("%s not in the recording", name)
		}
		book = append(book, replay.StartLeg{Slot: slot, Instrument: name, Qty: 2, Price: q[name].LiveMid()})
	}
	return snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: from, Equity: 1, Closes: closes, DVOL: dvol, GEX: recs,
		Book: book, PerpFundingPct: 10}
}

func shedRow(t *testing.T, rows []replay.ShedRow, variant string, cost float64) replay.Result {
	t.Helper()
	for _, r := range rows {
		if r.Variant.String() == variant && r.CostMultiple == cost {
			return r.Result
		}
	}
	t.Fatalf("no row %s at %g×", variant, cost)
	return replay.Result{}
}

// Scenario: the study compares every variant on the same recorded days —
// net P&L (coin, USD), worst day, worst week, max MM %, stops and MM cuts,
// the named windows and the modelled −20 % gap.
func TestShedStudy_ComparesEveryVariantOnTheSameDays(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	putDelta := math.Abs(snaps[3*24].ByName()[shedPut].Delta)
	variants, err := replay.ParseShedVariants(replay.DefaultShedVariants)
	if err != nil {
		t.Fatal(err)
	}
	below := replay.ShedVariant{Mode: replay.ShedDelta, MinDelta: math.Round((putDelta-0.03)*100) / 100}
	above := replay.ShedVariant{Mode: replay.ShedDelta, MinDelta: math.Round((putDelta+0.03)*100) / 100}
	variants = append(variants, below, above)
	rows, err := replay.ShedStudy(snaps, set, variants, []float64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2*len(variants) {
		t.Fatalf("rows = %d, want every variant at both cost multiples (%d)", len(rows), 2*len(variants))
	}
	for _, r := range rows {
		if r.Result.Snapshots != rows[0].Result.Snapshots || !r.Result.Start.Equal(rows[0].Result.Start) {
			t.Errorf("%s at %g×: %d decisions from %s, want the same days as the bot's (%d from %s)", r.Variant, r.CostMultiple,
				r.Result.Snapshots, r.Result.Start, rows[0].Result.Snapshots, rows[0].Result.Start)
		}
	}

	today, none := shedRow(t, rows, "today", 1), shedRow(t, rows, "none", 1)
	if today.Sheds == 0 || today.ShedBreaks != 1 || today.ShedPnL >= 0 {
		t.Fatalf("fixture: today sheds %d, breaks %d, shed closes %.5f; want the put bought back at a loss in one break",
			today.Sheds, today.ShedBreaks, today.ShedPnL)
	}
	if none.Sheds != 0 || none.ShedPnL != 0 || none.MaxGapLossPct <= today.MaxGapLossPct {
		t.Errorf("none: sheds %d, shed closes %.5f, gap loss %.1f %% (today %.1f %%); want nothing bought back and more held into the gap",
			none.Sheds, none.ShedPnL, none.MaxGapLossPct, today.MaxGapLossPct)
	}
	// Cutting to zero of the target is the bot's shed.
	if p0 := shedRow(t, rows, "partial:0", 1); math.Abs(p0.Net-today.Net) > 1e-12 || p0.Sheds != today.Sheds {
		t.Errorf("partial:0 net %.6f sheds %d, want today's %.6f %d", p0.Net, p0.Sheds, today.Net, today.Sheds)
	}
	if half := shedRow(t, rows, "partial:0.5", 1); half.Sheds == 0 || half.ShedPnL <= today.ShedPnL || half.OpenAtEnd <= today.OpenAtEnd {
		t.Errorf("partial:0.5 sheds %d, shed closes %.5f (today %.5f), legs open %d (today %d); want part of the put kept",
			half.Sheds, half.ShedPnL, today.ShedPnL, half.OpenAtEnd, today.OpenAtEnd)
	}
	if lo := shedRow(t, rows, below.String(), 1); lo.Sheds != today.Sheds {
		t.Errorf("%s: sheds %d, want today's %d (the put's |Δ| %.3f is above it)", below, lo.Sheds, today.Sheds, putDelta)
	}
	if hi := shedRow(t, rows, above.String(), 1); hi.Sheds != 0 {
		t.Errorf("%s: sheds %d, want none (the put's |Δ| %.3f is under it)", above, hi.Sheds, putDelta)
	}
	hedge := shedRow(t, rows, "hedge", 1)
	if hedge.Sheds != 0 || hedge.HedgeTrades == 0 || hedge.HedgePnL >= 0 || hedge.MaxGapLossPct >= none.MaxGapLossPct {
		t.Errorf("hedge: sheds %d, perp orders %d, hedge P&L %.5f, gap loss %.1f %% (none %.1f %%); want the put kept, hedged at a cost on a flat path, less lost in the gap",
			hedge.Sheds, hedge.HedgeTrades, hedge.HedgePnL, hedge.MaxGapLossPct, none.MaxGapLossPct)
	}
	if t2 := shedRow(t, rows, "today", 2); t2.Fees <= today.Fees || t2.Net >= today.Net {
		t.Errorf("2× costs: fees %.5f net %.5f, want more fees and less net than at 1× (%.5f, %.5f)", t2.Fees, t2.Net, today.Fees, today.Net)
	}

	md := replay.ShedStudyMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To), set.Cfg.MaxMMPct)
	for _, want := range []string{"### BTC — GEX shed variants", "Net BTC", "Net USD", "Worst day (%)", "Worst week (%)", "Max MM (%)",
		"−20 % gap loss", "Stops", "MM cuts", "9–17 Oct (%)", "29 Oct–4 Nov (%)", "not replayed", "(2 d)", "| today | 1× |",
		"| hedge | 2× |", "| baseline |", "**Indicative:**"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if replay.ShedStudyMarkdown("BTC", nil, nil, 35) != "" {
		t.Error("no rows: want no table")
	}
	if set.Shed != (replay.ShedVariant{}) || set.CostMultiple != 0 {
		t.Errorf("the study changed the caller's settings: %+v %g", set.Shed, set.CostMultiple)
	}
}

// Scenario: the tail protection stays — whatever the variant, a put held
// into a crash is still closed by the stop-loss, the delta exit or the MM
// cut, and the study never recommends a variant whose worst week or max MM
// is worse than today's shed.
func TestShedStudy_TailProtectionStays(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 7, func(h int) float64 {
		if h < 4*24 {
			return 100000
		}
		return 100000 * (1 - 0.25*min(1, float64(h-4*24)/48))
	})
	variants, _ := replay.ParseShedVariants(replay.DefaultShedVariants + ",delta:0.45") // above the delta exit: the put is held
	rows, err := replay.ShedStudy(snaps, set, variants, []float64{1})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"none", "delta:0.45", "hedge"} {
		r := shedRow(t, rows, name, 1)
		if r.Stops+r.DeltaExits+r.MMCuts == 0 {
			t.Errorf("%s: stops %d delta exits %d MM cuts %d; the put held into the crash must still be closed by the tail rules",
				name, r.Stops, r.DeltaExits, r.MMCuts)
		}
		if r.MaxMMPct >= set.Cfg.MaxMMPct && r.MMCuts == 0 {
			t.Errorf("%s: MM reached %.1f %% of max %.0f %% with no MM cut", name, r.MaxMMPct, set.Cfg.MaxMMPct)
		}
	}
	base := shedRow(t, rows, "today", 1)
	bw, _ := replay.WorstWeek(base.Days, base.Equity0)
	for _, r := range rows {
		ww, _ := replay.WorstWeek(r.Result.Days, r.Result.Equity0)
		v := replay.ShedVerdict(r, base, set.Cfg.MaxMMPct)
		if v == replay.VerdictCandidate && (ww < bw || r.Result.MaxMMPct > base.MaxMMPct) {
			t.Errorf("%s recommended with worst week %.2f (today %.2f), max MM %.1f (today %.1f)", r.Variant, ww, bw, r.Result.MaxMMPct, base.MaxMMPct)
		}
	}
}

// The verdict rule on its own: worse worst week, higher max MM or MM at the
// limit are never recommended; a variant must also net more.
func TestShedVerdict(t *testing.T) {
	t.Parallel()
	day := func(pnl float64) replay.Day { return replay.Day{PnL: pnl} }
	res := func(net, mm float64, pnls ...float64) replay.Result {
		r := replay.Result{Equity0: 1, Net: net, MaxMMPct: mm}
		for _, p := range pnls {
			r.Days = append(r.Days, day(p))
		}
		return r
	}
	base := res(0.01, 20, 0.01, -0.02, 0.02)
	v := func(mode string) replay.ShedVariant { return replay.ShedVariant{Mode: mode} }
	cases := []struct {
		name string
		row  replay.ShedRow
		want string
	}{
		{"the bot's own row", replay.ShedRow{Variant: replay.ShedVariant{}, Result: base}, replay.VerdictBaseline},
		{"more net, same tail", replay.ShedRow{Variant: v("none"), Result: res(0.02, 20, 0.02, -0.02, 0.02)}, replay.VerdictCandidate},
		{"more net, worse worst week", replay.ShedRow{Variant: v("none"), Result: res(0.03, 20, 0.01, -0.04, 0.02)}, replay.VerdictWorseWeek},
		{"more net, higher max MM", replay.ShedRow{Variant: v("hedge"), Result: res(0.02, 21, 0.02, -0.02, 0.02)}, replay.VerdictWorseMM},
		{"MM at max_mm_pct", replay.ShedRow{Variant: v("hedge"), Result: res(0.02, 35, 0.02, -0.02, 0.02)}, replay.VerdictReachesMaxMM},
		{"same tail, same net", replay.ShedRow{Variant: v("none"), Result: base}, replay.VerdictNoGain},
	}
	for _, c := range cases {
		if got := replay.ShedVerdict(c.row, base, 35); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	rows := []replay.ShedRow{
		{Variant: v("today"), CostMultiple: 1, Result: base}, {Variant: v("none"), CostMultiple: 1, Result: res(0.02, 20, 0.02, -0.02, 0.02)},
		{Variant: v("hedge"), CostMultiple: 1, Result: res(0.02, 20, 0.02, -0.02, 0.02)},
		{Variant: v("today"), CostMultiple: 2, Result: base}, {Variant: v("none"), CostMultiple: 2, Result: res(0.02, 20, 0.02, -0.02, 0.02)},
		{Variant: v("hedge"), CostMultiple: 2, Result: base},
	}
	if got := replay.ShedCandidates(rows, 35); len(got) != 1 || got[0] != "none" {
		t.Errorf("candidates = %v, want only none (hedge gains nothing at 2×)", got)
	}
}

func TestParseShedVariant(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"today", "today", true}, {"none", "none", true}, {"hedge", "hedge", true},
		{"partial:0.5", "partial:0.5", true}, {"partial:0", "partial:0", true}, {"partial:1", "partial:1", true},
		{"delta:0.2", "delta:0.20", true}, {" delta:0.25 ", "delta:0.25", true},
		{"partial:1.5", "", false}, {"partial:-0.1", "", false}, {"partial", "", false},
		{"delta:0", "", false}, {"delta:x", "", false}, {"hedge:1", "", false}, {"shed", "", false}, {"", "", false},
	} {
		v, err := replay.ParseShedVariant(c.in)
		if (err == nil) != c.ok || (c.ok && v.String() != c.want) {
			t.Errorf("%q: %v (%v), want %q ok=%v", c.in, v, err, c.want, c.ok)
		}
	}
	all, err := replay.ParseShedVariants("all")
	if err != nil || len(all) != 7 || all[0].String() != "today" {
		t.Errorf("all = %v (%v), want the issue's seven variants, the bot's first", all, err)
	}
	if _, err := replay.ParseShedVariants("today,bogus"); err == nil {
		t.Error("a bad variant in the list: want an error")
	}
}

// The study's helpers: the cost multiple's quote, named windows, worst day
// and the USD net.
func TestShedStudy_Helpers(t *testing.T) {
	t.Parallel()
	q := recorder.Quote{Bid: 0.010, Ask: 0.012}
	w := replay.Widen(q, 2)
	if math.Abs(w.Bid-0.009) > 1e-12 || math.Abs(w.Ask-0.013) > 1e-12 {
		t.Errorf("2× spread = %v/%v, want 0.009/0.013 around the same mid", w.Bid, w.Ask)
	}
	if w := replay.Widen(recorder.Quote{Bid: 0.001, Ask: 0.011}, 2); w.Bid != 0.0005 {
		t.Errorf("a wide spread: bid %v, want kept above zero at bid ÷ 2", w.Bid)
	}
	if w := replay.Widen(recorder.Quote{Ask: 0.012}, 2); w.Ask != 0.012 || w.Bid != 0 {
		t.Errorf("one-sided book: %+v, want as recorded", w)
	}
	if w := replay.Widen(q, 1); w != q {
		t.Errorf("1×: %+v, want as recorded", w)
	}
	d := func(m, day int, pnl, spot float64) replay.Day {
		return replay.Day{Date: time.Date(2026, time.Month(m), day, 0, 0, 0, 0, time.UTC), PnL: pnl, Spot: spot}
	}
	days := []replay.Day{d(10, 8, 0.01, 100000), d(10, 9, -0.03, 90000), d(10, 17, 0.01, 95000), d(10, 18, 0.005, 96000)}
	if p, n := replay.WindowPnL(days, replay.ShedWindows[0], 1); n != 2 || math.Abs(p-(-2)) > 1e-9 {
		t.Errorf("9–17 Oct = %.2f %% over %d days, want −2 %% over 2", p, n)
	}
	if _, n := replay.WindowPnL(days, replay.ShedWindows[1], 1); n != 0 {
		t.Errorf("29 Oct–4 Nov: %d days, want none replayed", n)
	}
	if got := replay.WorstDay(days, 1); math.Abs(got-(-3)) > 1e-9 {
		t.Errorf("worst day = %.2f %%, want −3", got)
	}
	if got := replay.NetUSD(days); math.Abs(got-(1000-2700+950+480)) > 1e-6 {
		t.Errorf("net USD = %.2f, want each day at its spot", got)
	}
}
