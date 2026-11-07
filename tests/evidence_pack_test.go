package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/helpkb"
	"optionsbot/internal/history"
)

// cmd/evidence -pack (#700): one document joining the reports already written.
// It copies figures, names what is missing and keeps caveats beside results.

// packWeeks are n ISO weeks from 2026-W32.
func packWeeks(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("2026-W%d", 32+i)
	}
	return out
}

// packPoints are six-hourly P&L history points over n ISO weeks from 2026-W32
// (Monday 2026-08-03): the total moves by slope each point, spot rises.
func packPoints(n int, slope float64) []history.Point {
	var pts []history.Point
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n*7*4; i++ {
		pts = append(pts, history.Point{T: start.Add(time.Duration(i) * 6 * time.Hour), Total: slope * float64(i), Spot: 100000 + 10*float64(i)})
	}
	return pts
}

// packFromClone writes the track record and trend the way cmd/evidence does,
// then loads and builds the pack.
func packFromClone(t *testing.T, root string) evidence.Pack {
	t.Helper()
	in := evidence.LoadTrack("stable", "btc", root, &bytes.Buffer{})
	md, js := evidence.TrackOutPaths(root, "btc")
	if err := evidence.WriteTrack(evidence.BuildTrackRecord(in), md, js); err != nil {
		t.Fatal(err)
	}
	md, js = evidence.TrendOutPaths(root, "btc")
	if err := evidence.WriteTrend(evidence.BuildTrend("stable", "btc", in.WeeklySource, in.Weekly, in.Unreadable), md, js); err != nil {
		t.Fatal(err)
	}
	return evidence.BuildPack(evidence.LoadPack("stable", "btc", root, &bytes.Buffer{}))
}

func packOutputs(t *testing.T, p evidence.Pack) (md, js string) {
	t.Helper()
	var b bytes.Buffer
	if err := evidence.WritePackMarkdown(&b, p); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b.String(), string(data)
}

func packSection(t *testing.T, p evidence.Pack, title string) evidence.PackSection {
	t.Helper()
	for _, s := range p.Sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no section %q in %+v", title, p.Sections)
	return evidence.PackSection{}
}

func packFigure(t *testing.T, s evidence.PackSection, name string) evidence.PackFigure {
	t.Helper()
	for _, f := range s.Figures {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("section %q has no figure %q: %+v", s.Title, name, s.Figures)
	return evidence.PackFigure{}
}

func wantValue(t *testing.T, f evidence.PackFigure, want float64) {
	t.Helper()
	if f.Value == nil || *f.Value != want {
		t.Errorf("%s = %v, want %v, the source's", f.Name, f.Value, want)
	}
}

// Scenario: pack from existing reports.
func TestEvidencePack_FromExistingReports(t *testing.T) {
	t.Parallel()
	weeks := packWeeks(12)
	root := trackClone(t, packPoints(12, 0.0002), weeks...)
	p := packFromClone(t, root)

	var titles []string
	for _, s := range p.Sections {
		titles = append(titles, s.Title)
	}
	if got := strings.Join(titles, ", "); got != "Headline, Twelve-week scoreboard, Replay vs live, Weeks missing, Futures hedge trial" {
		t.Fatalf("sections in the wrong order: %s", got)
	}
	if p.Label != "internal, not for external use before counsel approves (legal brief)" {
		t.Errorf("label %q", p.Label)
	}

	// Every headline figure equals the file it names, as that file was written.
	_, trackJS := evidence.TrackOutPaths(root, "btc")
	data, err := os.ReadFile(trackJS)
	if err != nil {
		t.Fatal(err)
	}
	var tr evidence.TrackRecord
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatal(err)
	}
	s, h := tr.Perf, packSection(t, p, "Headline")
	wantValue(t, packFigure(t, h, "Net return in coin"), s.NetPnL)
	wantValue(t, packFigure(t, h, "Net return, % of the starting equity, in coin"), s.ReturnPct)
	wantValue(t, packFigure(t, h, "Holding the coin over the same window, in USD"), s.HoldReturnUSDPct)
	wantValue(t, packFigure(t, h, "The strategy in USD"), s.ReturnUSDPct)
	wantValue(t, packFigure(t, h, "Excess over holding the coin"), s.ExcessReturnPct)
	wantValue(t, packFigure(t, h, "Worst 7 days in coin"), s.WorstWeek)
	wantValue(t, packFigure(t, h, "Worst 7 days, % of the starting equity"), s.WorstWeekPct)
	wantValue(t, packFigure(t, h, "Max drawdown in coin"), s.MaxDrawdown)
	wantValue(t, packFigure(t, h, "Max drawdown, % of the starting equity"), s.MaxDrawdownPct)
	wantValue(t, packFigure(t, h, "Worst ISO week, % of that week's starting equity"), *tr.Counts.WorstWeekPct)
	if f := packFigure(t, h, "Weeks covered"); f.Display != "12 of 12 ISO weeks have a report" {
		t.Errorf("weeks covered: %q", f.Display)
	}

	wantSource := filepath.Join("data", "evidence", "track-record", "btc.json")
	span := tr.From.Format(time.DateOnly) + " → " + tr.To.Format(time.DateOnly) + " UTC"
	if f := packFigure(t, h, "Net return in coin"); f.Source != wantSource || f.Window != span || f.Caveat != evidence.PackCaveat {
		t.Errorf("figure without its source, window or caveat: %+v (want %s, %s)", f, wantSource, span)
	}
	for _, sec := range p.Sections {
		for _, f := range sec.Figures {
			if f.Source == "" || f.Window == "" || f.Caveat == "" || f.Display == "" {
				t.Errorf("%s / %s: every figure names its source, window and caveat: %+v", sec.Title, f.Name, f)
			}
		}
	}

	md, js := packOutputs(t, p)
	if strings.Contains(md+js, root) || strings.Contains(md+js, filepath.Dir(root)) {
		t.Errorf("a machine path in the pack:\n%s", md)
	}
	wantIn(t, md, "**"+evidence.PackLabel+"**", "| Net return in coin | "+packFigure(t, h, "Net return in coin").Display+" | "+wantSource+" | "+span+" | "+evidence.PackCaveat+" |")

	if again := packFromClone(t, root); !bytes.Equal(mustJSON(t, again), mustJSON(t, p)) {
		t.Error("the same files must give the same pack")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The latest weekly report's sections are copied as written: scoreboard,
// replay vs live and the hedge trial.
func TestEvidencePack_LatestReportFiguresEqualTheirSource(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	if !r.RVL.Comparable {
		t.Fatalf("fixture not comparable: %+v", r.RVL)
	}
	engine, hold := 0.8, 0.3
	r.Score = evidence.Scoreboard{Label: evidence.ScoreboardLabel, Status: evidence.ScoreMeets, WeeksCounted: 12, WeeksNeeded: 12,
		FirstWeek: "2026-W32", LastWeek: "2026-W43", EngineRatio: &engine, HoldRatio: &hold, MissingWeeks: []string{"2026-W35"}}
	r.Hedge = trialReport(trialState(trialLedger(1), true), 10, evidence.AccountTestnet).Hedge
	path := "data/evidence/" + r.Week + "/btc.json"
	p := evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Latest: &r, LatestPath: path})

	sb := packSection(t, p, "Twelve-week scoreboard")
	if f := packFigure(t, sb, "Status"); f.Display != "meets" || f.Window != "2026-W32 → 2026-W43" || f.Source != path+" (scoreboard)" {
		t.Errorf("status %+v", f)
	}
	wantValue(t, packFigure(t, sb, "Engine ratio (weekly Sharpe)"), engine)
	wantValue(t, packFigure(t, sb, "Holding the coin ratio (weekly Sharpe)"), hold)
	if f := packFigure(t, sb, "Weeks missing from the window"); f.Display != "2026-W35" {
		t.Errorf("weeks missing %+v", f)
	}

	rv := packSection(t, p, "Replay vs live")
	wantValue(t, packFigure(t, rv, "Replay result in coin"), *r.RVL.Replay.Net)
	wantValue(t, packFigure(t, rv, "Live result in coin"), *r.RVL.Live.Net)
	wantValue(t, packFigure(t, rv, "Replay result, % of the starting equity"), r.RVL.Replay.NetPct)
	wantValue(t, packFigure(t, rv, "Live result, % of the starting equity"), r.RVL.Live.NetPct)
	wantValue(t, packFigure(t, rv, "Live worst day, % of the starting equity"), r.RVL.Live.WorstDayPct)
	wantValue(t, packFigure(t, rv, "Gap (replay − live) in coin"), *r.RVL.GapCoin)
	wantValue(t, packFigure(t, rv, "Gap (replay − live), of the starting equity"), *r.RVL.GapEquityPP)
	if f := packFigure(t, rv, "Replay vs live"); f.Display != evidence.PackComparable || f.Window != r.Week {
		t.Errorf("label %+v", f)
	}
	if !strings.Contains(rv.Risks[0], evidence.Unmodelled[0]) {
		t.Errorf("the replay's limits stand beside its figures: %q", rv.Risks)
	}

	ht := packSection(t, p, "Futures hedge trial")
	if f := packFigure(t, ht, "Status"); f.Display != evidence.HedgeTrialMeasured {
		t.Errorf("hedge status %+v", f)
	}
	wantValue(t, packFigure(t, ht, "Advantage of the hedge over the shed"), *r.Hedge.Advantage)
	wantValue(t, packFigure(t, ht, "Days measured"), float64(*r.Hedge.Days))
	if !strings.Contains(strings.Join(ht.Risks, " "), evidence.HedgeTrialLabel) {
		t.Errorf("hedge risks %q", ht.Risks)
	}
}

// Scenario: a loss is as visible as a gain. With the numbers masked the
// headline of a losing window and of a winning one are the same text.
func TestEvidencePack_ALossIsAsVisibleAsAGain(t *testing.T) {
	t.Parallel()
	weeks := packWeeks(3)
	gain := packFromClone(t, trackClone(t, packPoints(3, 0.0002), weeks...))
	loss := packFromClone(t, trackClone(t, packPoints(3, -0.0002), weeks...))

	h := packSection(t, loss, "Headline")
	for _, name := range []string{"Net return in coin", "Net return, % of the starting equity, in coin", "Worst 7 days in coin"} {
		if f := packFigure(t, h, name); !strings.HasPrefix(f.Display, "-") {
			t.Errorf("%s: %q carries no minus sign", name, f.Display)
		}
	}
	if f := packFigure(t, h, "Net return in coin"); !strings.HasSuffix(f.Display, " BTC") {
		t.Errorf("no unit: %q", f.Display)
	}
	mask := regexp.MustCompile(`[+-]?\d+(\.\d+)?`)
	headline := func(p evidence.Pack) string {
		md, _ := packOutputs(t, p)
		md = md[strings.Index(md, "## Headline"):]
		md = md[:strings.Index(md, "## Twelve-week")]
		return mask.ReplaceAllString(md, "N")
	}
	if a, b := headline(gain), headline(loss); a != b {
		t.Errorf("gain and loss differ beyond their numbers:\n%s\n---\n%s", a, b)
	}
	md, _ := packOutputs(t, loss)
	if strings.Count(md, "**") != 2 { // the label's pair only
		t.Errorf("bold inside the pack's figures:\n%s", md)
	}
}

// Scenario: missing data is named, not filled.
func TestEvidencePack_MissingDataIsNamedNotFilled(t *testing.T) {
	t.Parallel()
	weeks := packWeeks(6)
	reported := []string{weeks[0], weeks[1], weeks[3], weeks[5]} // W34 and W36 have no report
	p := packFromClone(t, trackClone(t, packPoints(6, 0.0002), reported...))

	missing := packSection(t, p, "Weeks missing")
	if len(missing.Figures) != 2 {
		t.Fatalf("weeks missing %+v", missing.Figures)
	}
	for _, wk := range []string{weeks[2], weeks[4]} {
		if f := packFigure(t, missing, wk); f.Display != evidence.NotReported || f.Value != nil {
			t.Errorf("%s: %+v", wk, f)
		}
	}
	if f := packFigure(t, packSection(t, p, "Headline"), "Weeks covered"); f.Display != "4 of 6 ISO weeks have a report" {
		t.Errorf("headline: %q", f.Display)
	}
	md, _ := packOutputs(t, p)
	wantIn(t, md, "| "+weeks[2]+" | not reported |", "| "+weeks[4]+" | not reported |", "4 of 6 ISO weeks have a report")

	// Every week reported: said, not left blank.
	full := packFromClone(t, trackClone(t, packPoints(2, 0.0002), packWeeks(2)...))
	if f := packFigure(t, packSection(t, full, "Weeks missing"), "Weeks missing"); f.Display != "none" {
		t.Errorf("no week missing: %+v", f)
	}
}

// A section that is absent reads "not reported": no file at all, or a weekly
// report from before that section existed.
func TestEvidencePack_MissingSectionsReadNotReported(t *testing.T) {
	t.Parallel()
	older := evidence.Report{Week: "2026-W40"} // written before the sections existed
	for name, in := range map[string]evidence.PackInputs{
		"no report": {Env: "stable", Bot: "btc"},
		"older":     {Env: "stable", Bot: "btc", Latest: &older, LatestPath: "w.json"},
	} {
		p := evidence.BuildPack(in)
		h := packSection(t, p, "Headline")
		for _, f := range []string{"Weeks covered", "Net return", "Holding the coin", "Worst 7 days", "Max drawdown"} {
			if got := packFigure(t, h, f); got.Display != evidence.NotReported || got.Value != nil {
				t.Errorf("%s: %s = %+v", name, f, got)
			}
		}
		if f := packFigure(t, packSection(t, p, "Twelve-week scoreboard"), "Status"); f.Display != evidence.NotReported {
			t.Errorf("%s: scoreboard %+v", name, f)
		}
		if f := packFigure(t, packSection(t, p, "Replay vs live"), "Replay vs live"); f.Display != evidence.NotReported {
			t.Errorf("%s: replay vs live %+v", name, f)
		}
		if f := packFigure(t, packSection(t, p, "Weeks missing"), "Weeks missing"); f.Display != evidence.NotReported {
			t.Errorf("%s: weeks %+v", name, f)
		}
		if f := packFigure(t, packSection(t, p, "Futures hedge trial"), "Status"); f.Display != evidence.NotReported {
			t.Errorf("%s: hedge %+v", name, f)
		}
	}
}

// The trend stands in for the weeks when the track record has none; a file
// that does not parse is listed and its figures read "not reported".
func TestEvidencePack_UnreadableTrackRecordFallsBackToTheTrend(t *testing.T) {
	t.Parallel()
	root := trackClone(t, packPoints(3, -0.0002), packWeeks(3)[0], packWeeks(3)[2])
	packFromClone(t, root)
	_, trackJS := evidence.TrackOutPaths(root, "btc")
	if err := os.WriteFile(trackJS, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := evidence.BuildPack(evidence.LoadPack("stable", "btc", root, &bytes.Buffer{}))

	rel := filepath.Join("data", "evidence", "track-record", "btc.json")
	if len(p.Unreadable) != 1 || p.Unreadable[0] != rel {
		t.Fatalf("unreadable %v", p.Unreadable)
	}
	h := packSection(t, p, "Headline")
	if f := packFigure(t, h, "Weeks covered"); f.Display != "2 of 3 ISO weeks have a report" || !strings.Contains(f.Source, "trend") {
		t.Errorf("weeks from the trend: %+v", f)
	}
	if f := packFigure(t, h, "Net return"); f.Display != evidence.NotReported {
		t.Errorf("net %+v", f)
	}
	if f := packFigure(t, packSection(t, p, "Weeks missing"), packWeeks(3)[1]); f.Display != evidence.NotReported {
		t.Errorf("week missing %+v", f)
	}
	md, _ := packOutputs(t, p)
	wantIn(t, md, "Files not read", rel)
}

// packAmountReport is a comparable report with a measured hedge, each amount a
// number that appears nowhere else.
func packAmountReport(account string) *evidence.Report {
	coin := func(v float64) *float64 { return &v }
	since, read := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	days, trades := 19, 4
	return &evidence.Report{Week: "2026-W42", Equity: 7.1111,
		RVL: evidence.ReplayVsLive{Label: evidence.ReplayVsLiveLabel, Account: account, AmountsShown: account != evidence.AccountLive, Comparable: true,
			Replay: evidence.RVLSide{Net: coin(7.2222), NetUSD: coin(7333), NetPct: 1.5}, Live: evidence.RVLSide{Net: coin(7.4444), NetUSD: coin(7555), NetPct: -2.5},
			GapCoin: coin(7.6666), GapUSD: coin(7777), GapEquityPP: coin(4), SpotUSD: coin(78888)},
		Hedge: evidence.HedgeTrial{Label: evidence.HedgeTrialLabel, Account: account, AmountsShown: account != evidence.AccountLive, Status: evidence.HedgeTrialMeasured,
			Since: &since, ReadAt: &read, Days: &days, RealTrades: &trades, Advantage: coin(7.8181), HedgePnL: coin(7.9191),
			WorstDay: &evidence.HedgeWorstDay{Date: "2026-10-05", Value: 7.0101, Legs: 1}}}
}

// Scenario: live accounts show % only. A report written while testnet still
// holds amounts; the pack drops them on the account, whichever file says live.
func TestEvidencePack_LiveAccountsShowPercentOnly(t *testing.T) {
	t.Parallel()
	amounts := []string{"7.1111", "7.2222", "7333", "7.4444", "7555", "7.6666", "7777", "78888", "7.8181", "7.9191", "7.0101"}
	track := &evidence.TrackRecord{Account: evidence.AccountTestnet, Known: true}
	track.Perf.NetPnL, track.Perf.MaxDrawdown, track.Perf.WorstWeek, track.Perf.WorstWeekKnown = 7.1212, -7.1313, -7.1414, true
	track.Perf.EquityKnown, track.Perf.ReturnPct = true, 3.5
	amounts = append(amounts, "7.1212", "7.1313", "7.1414")

	liveTrack := *track
	liveTrack.Account = evidence.AccountLive
	for name, in := range map[string]evidence.PackInputs{
		"the clone says live":       {Account: evidence.AccountLive, Track: track, Latest: packAmountReport(evidence.AccountTestnet)},
		"the track record says":     {Account: evidence.AccountTestnet, Track: &liveTrack, Latest: packAmountReport(evidence.AccountTestnet)},
		"the weekly report says it": {Account: evidence.AccountTestnet, Track: track, Latest: packAmountReport(evidence.AccountLive)},
	} {
		in.Env, in.Bot = "stable", "btc"
		p := evidence.BuildPack(in)
		if p.Account != evidence.AccountLive || p.AmountsShown {
			t.Fatalf("%s: account %q shown %v", name, p.Account, p.AmountsShown)
		}
		md, js := packOutputs(t, p)
		wantNotIn(t, md+js, amounts...)
		wantNotIn(t, md, evidence.PackCaveat)
		wantIn(t, md, evidence.PackCaveatLive, "+3.50 %", "Live account: percentages, ratios and counts only.")
		for _, s := range p.Sections {
			for _, f := range s.Figures {
				if f.Unit == "BTC" || strings.HasSuffix(f.Display, " BTC") {
					t.Errorf("%s: an amount in %q: %+v", name, s.Title, f)
				}
			}
		}
	}

	// The same inputs on testnet do show them: the check above is not hollow.
	md, _ := packOutputs(t, evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Account: evidence.AccountTestnet, Track: track, Latest: packAmountReport(evidence.AccountTestnet)}))
	wantIn(t, md, "+7.1212 BTC", "+7.2222 BTC", "+7.8181 BTC")
}

// A live pack read from the clone: DERIBIT_ENV=live in its .env.
func TestEvidencePack_LiveCloneFromFiles(t *testing.T) {
	t.Parallel()
	pts := packPoints(3, 0.0002)
	root := trackClone(t, pts, packWeeks(3)...)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DERIBIT_ENV=live\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := packFromClone(t, root)
	md, js := packOutputs(t, p)
	net := fmt.Sprintf("%.4f", pts[len(pts)-1].Total-pts[0].Total)
	wantNotIn(t, md+js, net, " BTC |")
	wantIn(t, md, "Account: live.")
}

// packPromiseRe adds the forecasting words to helpkb.Promises' list.
var packPromiseRe = regexp.MustCompile(`(?i)expected (return|profit|income|yield)|projected|will (return|make|yield|gain|beat)|promised? |target return|outperform|consistent (return|profit)|passive income`)

func packPromises(text string) []string {
	out := packPromiseRe.FindAllString(text, -1)
	return append(out, helpkb.Promises([]helpkb.Passage{{ID: "pack", Text: text}})...)
}

// Scenario: caveats travel with the figures, in every state of the sources;
// no output holds a promised or expected return.
func TestEvidencePack_CaveatsTravelWithTheFigures(t *testing.T) {
	t.Parallel()
	if len(packPromises("an expected return of 3 %, guaranteed")) != 2 {
		t.Fatal("the phrase check misses a promise")
	}
	partial := packAmountReport(evidence.AccountTestnet)
	partial.Replay.Partial = "partial: the market books cover 70.0 % of the week's hours, under 90 %"
	notComparable := packAmountReport(evidence.AccountTestnet)
	notComparable.RVL.Comparable, notComparable.RVL.Note = false, "the replay did not run: no config"

	track := &evidence.TrackRecord{Known: true}
	track.Perf.EquityKnown, track.Perf.NetPnL, track.Perf.ReturnPct = true, -0.5, -5
	for _, c := range []struct {
		name, label string
		in          evidence.PackInputs
	}{
		{"comparable", evidence.PackComparable, evidence.PackInputs{Track: track, Latest: packAmountReport(evidence.AccountTestnet)}},
		{"partial", evidence.PackPartial, evidence.PackInputs{Track: track, Latest: partial}},
		{"not comparable", evidence.PackNotComparable, evidence.PackInputs{Track: track, Latest: notComparable}},
		{"no weekly report", evidence.NotReported, evidence.PackInputs{Track: track}},
		{"live", evidence.PackComparable, evidence.PackInputs{Account: evidence.AccountLive, Track: track, Latest: packAmountReport(evidence.AccountLive)}},
	} {
		c.in.Env, c.in.Bot = "stable", "btc"
		p := evidence.BuildPack(c.in)
		caveat := evidence.PackCaveat
		if c.name == "live" {
			caveat = evidence.PackCaveatLive
		}
		md, js := packOutputs(t, p)
		parts := strings.Split(md, "\n## ")[1:]
		if len(parts) != 5 {
			t.Fatalf("%s: %d sections", c.name, len(parts))
		}
		for _, part := range parts {
			wantIn(t, part, caveat+" · replay vs live: "+c.label+"\n")
			rows := 0
			for _, line := range strings.Split(part, "\n") {
				if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Figure ") {
					rows++
					if !strings.HasSuffix(line, "| "+caveat+" |") {
						t.Errorf("%s: a row without its caveat: %s", c.name, line)
					}
				}
			}
			if rows < 2 { // at least a figure and a risk, in one table
				t.Errorf("%s: %d rows in section\n%s", c.name, rows, part)
			}
		}
		if got := packPromises(md + js); len(got) != 0 {
			t.Errorf("%s: promised-return words %q", c.name, got)
		}
	}
}

func TestEvidencePack_NoteKeepsMachinePathsOut(t *testing.T) {
	t.Parallel()
	r := packAmountReport(evidence.AccountTestnet)
	r.RVL.Comparable, r.RVL.Note = false, "the replay did not run: no recorded book in the week (/home/me/envs/stable/data/btc/data/market)"
	p := evidence.BuildPack(evidence.PackInputs{Env: "mvp", Bot: "btc", Latest: r, EnvsDir: "/home/me/envs"})
	f := packFigure(t, packSection(t, p, "Replay vs live"), "Why not comparable")
	if f.Display != "the replay did not run: no recorded book in the week (stable/data/btc/data/market)" {
		t.Errorf("note %q", f.Display)
	}
}

func TestEvidencePack_OutPathsAndWrite(t *testing.T) {
	t.Parallel()
	root := trackClone(t, packPoints(2, 0.0002), packWeeks(2)...)
	p := packFromClone(t, root)
	md, js := evidence.PackOutPaths(root, "btc")
	if want := filepath.Join(root, "data", "evidence", "pack", "btc.md"); md != want {
		t.Fatalf("%s, want %s", md, want)
	}
	if err := evidence.WritePack(p, md, js); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var back evidence.Pack
	if err := json.Unmarshal(data, &back); err != nil || back.Label != evidence.PackLabel {
		t.Fatalf("pack JSON: %v, %+v", err, back)
	}
	// A pack folder is no ISO week: the weekly reader skips it.
	reports, unreadable, err := evidence.ReadWeekly(root, "btc")
	if err != nil || len(reports) != 2 || len(unreadable) != 0 {
		t.Fatalf("weekly %d, unreadable %v, %v", len(reports), unreadable, err)
	}
}
