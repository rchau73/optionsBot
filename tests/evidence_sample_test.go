package tests

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// #772: every figure of the evidence pack and of the weekly report carries
// its sample (days, weeks, events) and reads "too few to conclude" under the
// floors, its number still shown.

// sampleTrack is a track record over pts with one weekly report per ISO week
// of weeks, each holding stopsPerWeek stop-loss closes.
func sampleTrack(t *testing.T, pts []history.Point, stopsPerWeek int, weeks ...string) *evidence.TrackRecord {
	t.Helper()
	var reports []evidence.Report
	for _, wk := range weeks {
		w, err := evidence.ParseWeek(wk)
		if err != nil {
			t.Fatal(err)
		}
		var trades []orders.Trade
		for i := 0; i < stopsPerWeek; i++ {
			trades = append(trades, orders.Trade{At: w.From.Add(time.Duration(i+1) * time.Hour), Kind: "close", Reason: "stop_loss", PnL: -0.001})
		}
		reports = append(reports, evidence.Build(evidence.Inputs{Env: "stable", Bot: "btc", Window: w, Points: pts, History: "h.jsonl",
			Trades: trades, Journal: "orders.log", Equity: 1, EquitySource: "account.json"}))
	}
	tr := evidence.BuildTrackRecord(evidence.TrackInputs{Env: "stable", Bot: "btc", Account: evidence.AccountTestnet,
		Points: pts, History: "h.jsonl", Weekly: reports})
	return &tr
}

// sampleDays are six-hourly points over n days from Monday 2026-08-03.
func sampleDays(n int, slope float64) []history.Point {
	var pts []history.Point
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n*4; i++ {
		pts = append(pts, history.Point{T: start.Add(time.Duration(i) * 6 * time.Hour), Total: slope * float64(i), Spot: 100000 + 10*float64(i)})
	}
	return pts
}

func evMarkdown(t *testing.T, r evidence.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func samplePack(track *evidence.TrackRecord, latest *evidence.Report) evidence.Pack {
	return evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Account: evidence.AccountTestnet,
		Track: track, TrackPath: "data/evidence/track-record/btc.json", Latest: latest, LatestPath: "data/evidence/2026-W43/btc.json"})
}

// Scenario: a figure over a short window is flagged.
func TestEvidenceSample_AFigureOverAShortWindowIsFlagged(t *testing.T) {
	t.Parallel()
	track := sampleTrack(t, sampleDays(9, 0.0002), 3, "2026-W32", "2026-W33")
	p := samplePack(track, nil)
	f := packFigure(t, packSection(t, p, "Headline"), "Net return in coin")
	if f.Sample == nil || f.Sample.Days != 9 || !f.Sample.Thin {
		t.Fatalf("net return over 9 days: sample %+v", f.Sample)
	}
	wantValue(t, f, track.Perf.NetPnL) // the number itself is still shown
	md, _ := packOutputs(t, p)
	wantIn(t, md, "| Net return in coin | "+f.Display+" | 9 days · 1 week · 6 "+evidence.TailEventsName+" · too few to conclude (9 days) |")
}

// Scenario: a figure over a long window is not flagged.
func TestEvidenceSample_AFigureOverALongWindowIsNotFlagged(t *testing.T) {
	t.Parallel()
	track := sampleTrack(t, sampleDays(84, 0.0002), 1, packWeeks(12)...)
	p := samplePack(track, nil)
	h := packSection(t, p, "Headline")
	for _, name := range []string{"Net return in coin", "Net return, % of the starting equity, in coin", "Max drawdown, % of the starting equity", "Worst 7 days, % of the starting equity"} {
		f := packFigure(t, h, name)
		if f.Sample == nil || f.Sample.Thin || f.Sample.Weeks != 12 || f.Sample.Events == nil || *f.Sample.Events != 12 {
			t.Errorf("%s: sample %+v", name, f.Sample)
		}
	}
	md, _ := packOutputs(t, p)
	wantIn(t, md, "| 84 days · 12 weeks · 12 "+evidence.TailEventsName+" |")
	headline := md[strings.Index(md, "## Headline"):strings.Index(md, "## Twelve-week")]
	wantNotIn(t, headline, evidence.ThinNote)

	// Twelve weeks but one week without a stop: under the events floor.
	short := sampleTrack(t, sampleDays(84, 0.0002), 1, packWeeks(12)[:9]...)
	if f := packFigure(t, packSection(t, samplePack(short, nil), "Headline"), "Net return in coin"); f.Sample == nil || !f.Sample.Thin {
		t.Errorf("9 stops: %+v", f.Sample)
	}
}

// The floors at their boundary and one under.
func TestEvidenceSample_Floors(t *testing.T) {
	t.Parallel()
	n := func(v int) *int { return &v }
	for _, c := range []struct {
		name string
		s    evidence.Sample
		thin bool
	}{
		{"at every floor", evidence.SpanSample(evidence.SampleMinDays, n(evidence.SampleMinEvents), evidence.TailEventsName), false},
		{"a day short", evidence.SpanSample(evidence.SampleMinDays-1, n(evidence.SampleMinEvents), evidence.TailEventsName), true},
		{"an event short", evidence.SpanSample(evidence.SampleMinDays, n(evidence.SampleMinEvents-1), evidence.TailEventsName), true},
		{"no events needed", evidence.SpanSample(evidence.SampleMinDays, nil, ""), false},
		{"no day", evidence.SpanSample(0, nil, ""), true},
		{"twelve weeks", evidence.WeeksSample(evidence.SampleMinWeeks, nil, ""), false},
		{"eleven weeks", evidence.WeeksSample(evidence.SampleMinWeeks-1, nil, ""), true},
	} {
		if c.s.Thin != c.thin {
			t.Errorf("%s: %+v, thin want %v", c.name, c.s, c.thin)
		}
		if got := strings.Contains(c.s.Text(), evidence.ThinNote); got != c.thin {
			t.Errorf("%s: text %q", c.name, c.s.Text())
		}
	}
	if evidence.SampleMinWeeks != evidence.ScoreboardWeeks || evidence.SampleMinDays != 7*evidence.SampleMinWeeks {
		t.Error("the floors are the scoreboard's twelve weeks")
	}
	if got := evidence.SpanSample(1, n(1), "stops").Text(); got != "1 day · 0 weeks · 1 stops · too few to conclude (1 day)" {
		t.Errorf("text %q", got)
	}
}

// fullPack has every section with figures: a track record, a comparable
// replay vs live, a scoreboard and a measured hedge trial.
func fullPack(t *testing.T) (evidence.Pack, evidence.Report) {
	t.Helper()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	engine, hold := 0.8, 0.3
	r.Score = evidence.Scoreboard{Label: evidence.ScoreboardLabel, Status: evidence.ScoreMeets, WeeksCounted: 12, WeeksNeeded: 12,
		FirstWeek: "2026-W32", LastWeek: "2026-W43", EngineRatio: &engine, HoldRatio: &hold}
	r.Hedge = trialReport(trialState(trialLedger(1), true), 10, evidence.AccountTestnet).Hedge
	if !r.RVL.Comparable || r.Hedge.Advantage == nil {
		t.Fatalf("fixture: comparable %v, hedge %+v", r.RVL.Comparable, r.Hedge)
	}
	return samplePack(sampleTrack(t, sampleDays(20, 0.0002), 1, "2026-W32", "2026-W33", "2026-W34"), &r), r
}

// Scenario: no figure without a sample — in the pack and the weekly report.
func TestEvidenceSample_NoFigureWithoutASample(t *testing.T) {
	t.Parallel()
	p, r := fullPack(t)
	numbers := 0
	for _, s := range p.Sections {
		for _, f := range s.Figures {
			if f.Value == nil {
				continue
			}
			numbers++
			if f.Sample == nil {
				t.Errorf("pack %s / %s has no sample", s.Title, f.Name)
			}
		}
	}
	if numbers < 25 { // every section reached its figures: the check is not hollow
		t.Fatalf("only %d figures with a number", numbers)
	}

	// The weekly report: every headline figure the issue names, in its table.
	want := []string{evidence.FigureNetReturn, evidence.FigureMaxDrawdown, evidence.FigureWorst7Days, evidence.FigureHold,
		evidence.FigureExcess, evidence.FigureGap, evidence.FigureHedge, evidence.FigureScoreboard}
	got := map[string]evidence.FigureSample{}
	for _, f := range r.Samples {
		got[f.Figure] = f
	}
	md := evMarkdown(t, r)
	for _, name := range want {
		f, ok := got[name]
		if !ok {
			t.Errorf("weekly report: %s has no sample", name)
			continue
		}
		wantIn(t, md, "| "+name+" | "+f.Display+" | "+f.Sample.Text()+" |")
	}
	if len(r.Samples) != len(want) {
		t.Errorf("weekly samples %d, want %d: a new one needs its name here", len(r.Samples), len(want))
	}
	// The values are the report's own.
	if f := got[evidence.FigureNetReturn]; !strings.HasPrefix(f.Display, "+") || f.Sample.Days != r.Live.Perf.DaysCovered ||
		f.Sample.Events == nil || *f.Sample.Events != 1 || !f.Sample.Thin {
		t.Errorf("net return %+v (perf %+v)", f, r.Live.Perf)
	}
	if f := got[evidence.FigureGap]; f.Sample.Days != 7 {
		t.Errorf("gap over the replay's week: %+v", f)
	}
}

// Scenario: honesty rules hold — a loss reads like a gain with its sample,
// nothing is bold, nothing promises a return, and a live account shows no
// amount in the samples either.
func TestEvidenceSample_HonestyRulesHold(t *testing.T) {
	t.Parallel()
	loss := samplePack(sampleTrack(t, sampleDays(9, -0.0002), 1, "2026-W32", "2026-W33"), nil)
	f := packFigure(t, packSection(t, loss, "Headline"), "Max drawdown, % of the starting equity")
	if !strings.HasPrefix(f.Display, "-") || f.Sample == nil || !f.Sample.Thin {
		t.Errorf("drawdown %+v", f)
	}
	md, js := packOutputs(t, loss)
	if strings.Count(md, "**") != 2 {
		t.Errorf("bold inside the pack:\n%s", md)
	}
	if got := packPromises(md + js); len(got) != 0 {
		t.Errorf("promised-return words %q", got)
	}

	_, r := fullPack(t)
	week := evMarkdown(t, r)
	section := week[strings.Index(week, "## Sample size"):]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	if strings.Contains(section, "**") || len(packPromises(section)) != 0 {
		t.Errorf("sample section:\n%s", section)
	}
	// A measured hedge trial shows its advantage in coin on testnet, never live.
	for account, coin := range map[string]bool{evidence.AccountTestnet: true, evidence.AccountLive: false} {
		rep := trialReport(trialState(trialLedger(1), true), 10, account)
		for _, s := range rep.Samples {
			if s.Figure == evidence.FigureHedge && strings.HasSuffix(s.Display, " BTC") != coin {
				t.Errorf("%s: hedge sample %+v", account, s)
			}
		}
	}
}
