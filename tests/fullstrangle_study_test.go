package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
	"optionsbot/internal/risk"
)

// The full-strangle study (#677): cmd/stress -replay -fullstrangle
// today,hedged replays the same recorded books under today's rules and under
// the perpetual holding the book's delta with the rules that leave a leg
// missing relaxed, at the recorded costs and at twice them, in one table per
// coin. The fixture is shedBooks: a bear trend in a negative regime with the
// put side shed from the first traded hour.

func fullVariants(t *testing.T, list string) []replay.FullStrangleVariant {
	t.Helper()
	v, err := replay.ParseFullStrangles(list)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func fullRow(t *testing.T, rows []replay.FullStrangleRow, variant string, cost float64) replay.Result {
	t.Helper()
	for _, r := range rows {
		if r.Variant.String() == variant && r.CostMultiple == cost {
			return r.Result
		}
	}
	t.Fatalf("no row %s at %g×", variant, cost)
	return replay.Result{}
}

// Scenario: the replay compares both — one table shows both variants at 1×
// and 2× costs with the gap test, and the bot's rules are unchanged.
func TestFullStrangleStudy_ComparesBothVariants(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 6, func(int) float64 { return 100000 })
	rows, err := replay.FullStrangleStudy(snaps, set, fullVariants(t, "today,hedged"), []float64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want both variants at both cost multiples", len(rows))
	}
	for _, r := range rows {
		if r.Result.Snapshots != rows[0].Result.Snapshots || !r.Result.Start.Equal(rows[0].Result.Start) {
			t.Errorf("%s at %g×: %d decisions from %s, want the same days as today's", r.Variant, r.CostMultiple, r.Result.Snapshots, r.Result.Start)
		}
	}

	today, hedged := fullRow(t, rows, "today", 1), fullRow(t, rows, "hedged", 1)
	// The bot's rules are unchanged: today is a plain replay, no perpetual.
	plain, err := replay.Run(snaps, set)
	if err != nil {
		t.Fatal(err)
	}
	if today.Net != plain.Net || today.Sheds != plain.Sheds || today.Repairs != plain.Repairs || today.HedgeTrades != 0 || today.PerpTradedUSD != 0 {
		t.Errorf("today: net %.6f sheds %d repairs %d perp orders %d, want the plain replay's %.6f %d %d and no perpetual",
			today.Net, today.Sheds, today.Repairs, today.HedgeTrades, plain.Net, plain.Sheds, plain.Repairs)
	}
	if today.Sheds == 0 || today.MissingLegHours == 0 {
		t.Fatalf("fixture: today sheds %d, missing %.1f leg-hours; want the put shed and held", today.Sheds, today.MissingLegHours)
	}
	if hedged.Sheds != 0 || hedged.MissingLegHours != 0 {
		t.Errorf("hedged: sheds %d, missing %.1f leg-hours; want every strangle full", hedged.Sheds, hedged.MissingLegHours)
	}
	if hedged.HedgeTrades == 0 || hedged.PerpTradedUSD <= 0 || hedged.MaxPerpUSD <= 0 || hedged.PerpFees <= 0 || hedged.PerpFunding <= 0 {
		t.Errorf("hedged: %d perp orders, %.0f USD traded, largest %.0f, fees %.6f, funding %.6f; want all of them",
			hedged.HedgeTrades, hedged.PerpTradedUSD, hedged.MaxPerpUSD, hedged.PerpFees, hedged.PerpFunding)
	}
	if t2 := fullRow(t, rows, "hedged", 2); t2.PerpFees <= hedged.PerpFees || t2.Net >= hedged.Net {
		t.Errorf("2× costs: perp fees %.6f net %.6f, want more fees and less net than at 1× (%.6f, %.6f)", t2.PerpFees, t2.Net, hedged.PerpFees, hedged.Net)
	}

	md := replay.FullStrangleMarkdown("BTC", rows, replay.MissingDays(snaps, set.From, set.To), set.Cfg.MaxMMPct)
	for _, want := range []string{"### BTC — full strangles under the hedge", "Net BTC", "Net USD", "Legs missing (leg-hours)", "Perp traded (USD)",
		"Perp fees BTC", "Funding BTC", "Worst day (%)", "Worst week (%)", "Avg IM (%)", "Net ÷ avg IM", "Max MM (%)", "−20 % gap loss", "Stops", "MM cuts", "9–17 Oct (%)", "29 Oct–4 Nov (%)",
		"| today | 1× |", "| today | 2× |", "| hedged | 1× |", "| hedged | 2× |", "| baseline |", "**Indicative:**"} {
		if !strings.Contains(md, want) {
			t.Errorf("table lacks %q:\n%s", want, md)
		}
	}
	if replay.FullStrangleMarkdown("BTC", nil, nil, 35) != "" {
		t.Error("no rows: want no table")
	}
	if set.FullStrangle != (replay.FullStrangleVariant{}) || set.CostMultiple != 0 {
		t.Errorf("the study changed the caller's settings: %+v %g", set.FullStrangle, set.CostMultiple)
	}
}

// Today's rules are added when the list lacks them: every verdict is against them.
func TestFullStrangleStudy_AddsTodayFirst(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	rows, err := replay.FullStrangleStudy(snaps, set, fullVariants(t, "hedged:0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Variant.Hedged || rows[1].Variant.String() != "hedged:0" {
		t.Errorf("rows = %+v, want today then hedged:0", rows)
	}
	if _, err := replay.FullStrangleStudy(snaps, replay.Settings{}, nil, nil); err == nil {
		t.Error("no settings: want an error")
	}
}

// The relaxed rules: a one-legged strangle in the negative regime gets its
// held side back only under the proposal, and a vacant slot opens both sides.
func TestFullStrangleStudy_HeldSidesAreSold(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	set.Book = set.Book[:1] // the call alone: its put is held by the shed signal and the regime side
	rows, err := replay.FullStrangleStudy(snaps, set, fullVariants(t, "today,hedged"), []float64{1})
	if err != nil {
		t.Fatal(err)
	}
	today, hedged := fullRow(t, rows, "today", 1), fullRow(t, rows, "hedged", 1)
	if today.Repairs != 0 || today.MissingLegHours == 0 {
		t.Errorf("today: %d repairs, missing %.1f leg-hours; want the put held", today.Repairs, today.MissingLegHours)
	}
	if hedged.Repairs == 0 {
		t.Errorf("hedged: no repair, want the put re-sold in the negative regime")
	}
	if hedged.MissingLegHours >= today.MissingLegHours {
		t.Errorf("hedged: missing %.1f leg-hours, want under today's %.1f", hedged.MissingLegHours, today.MissingLegHours)
	}
}

// What stays: a put held into a crash is still closed by the stop-loss, the
// delta exit or the MM cut, and the study never recommends a worse tail.
func TestFullStrangleStudy_TailRulesStay(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 7, func(h int) float64 {
		if h < 4*24 {
			return 100000
		}
		return 100000 * (1 - 0.25*min(1, float64(h-4*24)/48))
	})
	rows, err := replay.FullStrangleStudy(snaps, set, fullVariants(t, "today,hedged"), []float64{1})
	if err != nil {
		t.Fatal(err)
	}
	hedged := fullRow(t, rows, "hedged", 1)
	if hedged.Stops+hedged.DeltaExits+hedged.MMCuts == 0 {
		t.Errorf("hedged: stops %d delta exits %d MM cuts %d; the put held into the crash must still be closed", hedged.Stops, hedged.DeltaExits, hedged.MMCuts)
	}
	if hedged.MaxMMPct >= set.Cfg.MaxMMPct && hedged.MMCuts == 0 {
		t.Errorf("hedged: MM reached %.1f %% of max %.0f %% with no MM cut", hedged.MaxMMPct, set.Cfg.MaxMMPct)
	}
}

// The hedge is the bot's: nothing inside the band, at most 12 orders a
// rolling hour, the perpetual following the book's coin delta.
func TestFullStrangleStudy_HedgeBandAndRate(t *testing.T) {
	t.Parallel()
	snaps, set := shedBooks(t, 5, func(int) float64 { return 100000 })
	set.FullStrangle = fullVariants(t, "hedged")[0]

	inBand := set
	inBand.Equity = 1000 // 5 % of the balance is 50 BTC: more than the whole book's delta
	res, err := replay.Run(snaps, inBand)
	if err != nil {
		t.Fatal(err)
	}
	if res.HedgeTrades != 0 {
		t.Errorf("inside the band: %d perp orders, want none", res.HedgeTrades)
	}

	// Spot swinging 30 % each minute: the perpetual's coin delta leaves the
	// band on every decision, far more often than the limit allows.
	from := set.From
	var recs []recorder.GEXRecord
	for m := 0; m < 120; m++ {
		spot := 100000.0
		if m%2 == 1 {
			spot = 130000
		}
		recs = append(recs, recorder.GEXRecord{ComputedAt: from.Add(time.Duration(m) * time.Minute), Spot: spot,
			Regime: "NEGATIVE/ACCELERATION", Flip: 200000, FlipFound: true, GEXSum: -1e6, DVOL: 50})
	}
	swings := set
	swings.GEX = recs
	swings.Equity = 0.5
	var orders []time.Time
	swings.Trace = func(at time.Time, line string) {
		if strings.Contains(line, "hold the book's delta") {
			orders = append(orders, at)
		}
	}
	if res, err = replay.Run(snaps, swings); err != nil {
		t.Fatal(err)
	}
	if res.HedgeRateLimited == 0 {
		t.Fatalf("fixture: %d perp orders, none rate limited; want the limit reached", res.HedgeTrades)
	}
	for i, at := range orders {
		n := 0
		for _, o := range orders[:i+1] {
			if at.Sub(o) < time.Hour {
				n++
			}
		}
		if n > config.FixedFuturesHedgeMaxPerHour {
			t.Fatalf("%d perp orders in the hour to %s, want at most %d", n, at, config.FixedFuturesHedgeMaxPerHour)
		}
	}
}

// The stopped leg's gate: today's rule, or under the hedge the cooldown alone.
func TestFullStrangleVariant_StopHold(t *testing.T) {
	t.Parallel()
	stopped := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	calm := risk.Status{}
	frozen := risk.Status{Frozen: true, FreezeReason: "band change"}
	negative := risk.Status{RegimeUsed: true, RegimeNegative: true}
	today := replay.FullStrangleVariant{}
	hedged := fullVariants(t, "hedged")[0]
	none := fullVariants(t, "hedged:0")[0]
	const botCooldown = 72 * time.Hour
	for _, c := range []struct {
		name  string
		v     replay.FullStrangleVariant
		after time.Duration
		st    risk.Status
		held  bool
	}{
		{"today: in the cooldown", today, 71 * time.Hour, calm, true},
		{"today: at the cooldown's end", today, 72 * time.Hour, calm, false},
		{"today: past it, frozen", today, 100 * time.Hour, frozen, true},
		{"today: past it, negative regime", today, 100 * time.Hour, negative, true},
		{"hedged: an hour before its cooldown ends", hedged, 23 * time.Hour, calm, true},
		{"hedged: at its end", hedged, 24 * time.Hour, calm, false},
		{"hedged: frozen", hedged, 24 * time.Hour, frozen, false},
		{"hedged: negative regime", hedged, 24 * time.Hour, negative, false},
		{"hedged:0 at once", none, 0, frozen, false},
	} {
		if got := c.v.StopHold(stopped, stopped.Add(c.after), botCooldown, c.st); (got != "") != c.held {
			t.Errorf("%s: %q, want held=%v", c.name, got, c.held)
		}
	}
}

// The verdict: gap first, then the shed study's tail rule, then the net.
func TestFullStrangleVerdict(t *testing.T) {
	t.Parallel()
	res := func(net, gap float64, pnls ...float64) replay.Result {
		r := replay.Result{Equity0: 1, Net: net, MaxMMPct: 20, MaxGapLossPct: gap, AvgIMPct: 10}
		for _, p := range pnls {
			r.Days = append(r.Days, replay.Day{PnL: p})
		}
		return r
	}
	base := res(0.01, 30, 0.01, -0.02, 0.02)
	small, half := res(0.02, 5, 0.02, -0.01, 0.02), res(0.02, 15, 0.02, -0.01, 0.02)
	small.AvgIMPct, half.AvgIMPct = 1.3, 5
	hedged := fullVariants(t, "hedged")[0]
	for _, c := range []struct {
		name string
		row  replay.FullStrangleRow
		want string
	}{
		{"today's own row", replay.FullStrangleRow{Result: base}, replay.VerdictBaseline},
		{"more net, gap no worse", replay.FullStrangleRow{Variant: hedged, Result: res(0.02, 30, 0.02, -0.02, 0.02)}, replay.VerdictCandidate},
		{"more net, worse gap", replay.FullStrangleRow{Variant: hedged, Result: res(0.02, 31, 0.02, -0.02, 0.02)}, replay.VerdictWorseGap},
		{"more net, worse week", replay.FullStrangleRow{Variant: hedged, Result: res(0.03, 20, 0.01, -0.04, 0.02)}, replay.VerdictWorseWeek},
		{"same net", replay.FullStrangleRow{Variant: hedged, Result: res(0.01, 20, 0.01, -0.02, 0.02)}, replay.VerdictNoGain},
		{"a much smaller book, better on every line", replay.FullStrangleRow{Variant: hedged, Result: small}, replay.VerdictNotComparable},
		{"half of today's book still compares", replay.FullStrangleRow{Variant: hedged, Result: half}, replay.VerdictCandidate},
	} {
		if got := replay.FullStrangleVerdict(c.row, base, 35); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseFullStrangle(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"today", "today", true}, {"hedged", "hedged", true}, {" hedged ", "hedged", true}, {"hedged:0", "hedged:0", true},
		{"hedged:24", "hedged", true}, {"hedged:72", "hedged:72", true},
		{"hedged:-1", "", false}, {"hedged:x", "", false}, {"hedged:", "", false}, {"today:1", "", false}, {"full", "", false}, {"", "", false},
	} {
		v, err := replay.ParseFullStrangle(c.in)
		if (err == nil) != c.ok || (c.ok && v.String() != c.want) {
			t.Errorf("%q: %v (%v), want %q ok=%v", c.in, v, err, c.want, c.ok)
		}
	}
	if v, err := replay.ParseFullStrangle("hedged"); err != nil || v.CooldownHours != replay.DefaultHedgedCooldownHours {
		t.Errorf("hedged = %+v (%v), want the proposal's cooldown", v, err)
	}
	if _, err := replay.ParseFullStrangles("today,bogus"); err == nil {
		t.Error("a bad rule set in the list: want an error")
	}
}
