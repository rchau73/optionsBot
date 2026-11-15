package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/hedge"
	"optionsbot/internal/history"
)

// The weekly evidence report's futures hedge trial (#678): the benchmark's
// ledger through hedge.Summary, labelled testnet and shadow-based, with no
// figure until it has HedgeTrialMinDays days.

var trialStart = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// trialLedger has real and paper trades and two days of shed decisions: the
// paper hedge bought 90,000 USD at 90,000 (fee 0.0005) and sold at 100,000;
// the real one sold 10,000 at 100,000 (fee 0.0001); legs worth +0.01, −0.03
// and, on another day, +0.002.
func trialLedger(legScale float64) hedge.Ledger {
	l := hedge.Ledger{Since: trialStart}
	l.Virtual.Book(90000, 90000, 0.0005)
	l.Virtual.Book(-90000, 100000, 0.0005)
	l.Real.Book(-10000, 100000, 0.0001)
	day := func(d int) time.Time { return trialStart.AddDate(0, 0, d) }
	leg := func(d int, shed, last float64, done bool) {
		l.Legs = append(l.Legs, hedge.Counterfactual{At: day(d), Mode: hedge.CounterHedged, Instrument: "BTC-27NOV26-90000-P",
			Qty: legScale, ShedPrice: shed, LastPrice: last, LastAt: day(d + 1), Done: done})
	}
	leg(2, 0.03, 0.02, true)
	leg(2, 0.03, 0.06, false)
	leg(5, 0.012, 0.01, true)
	return l
}

func trialState(l hedge.Ledger, enabled bool) hedge.ModeState {
	return hedge.ModeState{Enabled: enabled, Since: l.Since, By: hedge.Actor{User: "tester", Role: "admin"}, Ledger: l}
}

func trialPoints() []history.Point {
	return []history.Point{{T: trialStart, Spot: 90000}, {T: trialStart.AddDate(0, 0, 10), Spot: 100000}}
}

// trialReport builds the report with the section as of trialStart + days.
func trialReport(st hedge.ModeState, days float64, account string) evidence.Report {
	now := trialStart.Add(time.Duration(days * 24 * float64(time.Hour)))
	return evidence.Build(evidence.Inputs{Env: "mvp", Bot: "btc", Account: account, Window: evWeek, Points: trialPoints(),
		Hedge: evidence.HedgeInputs{Path: "data/btc/data/hedge_mode.json", State: st, Now: now}})
}

// trialSection is the section's Markdown.
func trialSection(t *testing.T, r evidence.Report) string {
	t.Helper()
	md := covMarkdown(t, r)
	i := strings.Index(md, "## Futures hedge trial")
	if i < 0 {
		t.Fatalf("no futures hedge trial section:\n%s", md)
	}
	return md[i:]
}

func wantIn(t *testing.T, md string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(md, w) {
			t.Errorf("no %q in:\n%s", w, md)
		}
	}
}

func wantNotIn(t *testing.T, md string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(md, w) {
			t.Errorf("%q in:\n%s", w, md)
		}
	}
}

func TestEvidenceHedgeTrial_LedgerWith10Days(t *testing.T) {
	t.Parallel()
	l := trialLedger(1)
	r := trialReport(trialState(l, true), 10, "testnet")
	h := r.Hedge
	if h.Status != evidence.HedgeTrialMeasured || h.Mode != evidence.HedgeModeHedge || *h.Days != 10 {
		t.Fatalf("status %q mode %q days %v", h.Status, h.Mode, h.Days)
	}
	// Read through hedge.Summary, at the history's last spot.
	want := hedge.PricingIndex.Summarise(l, 0, 100000, 100000)
	if !near(*h.Advantage, want.AdvantageCoin, 1e-12) || !near(*h.HedgePnL, want.HedgeCoin, 1e-12) ||
		!near(*h.RealPnL, want.RealCoin, 1e-12) || !near(*h.PaperPnL, want.VirtualCoin, 1e-12) || !near(*h.LegsValue, want.LegsCoin, 1e-12) {
		t.Errorf("trial %+v, summary %+v", h, want)
	}
	if !near(*h.Fees, 0.0011, 1e-12) || !near(*h.RealFees, 0.0001, 1e-12) || !near(*h.PaperFees, 0.001, 1e-12) {
		t.Errorf("fees %v real %v paper %v", *h.Fees, *h.RealFees, *h.PaperFees)
	}
	if *h.RealTrades != 1 || *h.PaperTrades != 2 || *h.Legs != 3 || *h.LegsOpen != 1 {
		t.Errorf("counts: real %d paper %d legs %d open %d", *h.RealTrades, *h.PaperTrades, *h.Legs, *h.LegsOpen)
	}
	if w := h.WorstDay; w == nil || w.Date != "2026-10-29" || !near(w.Value, -0.02, 1e-12) || w.Legs != 2 {
		t.Errorf("worst day %+v", h.WorstDay)
	}

	md := trialSection(t, r)
	wantIn(t, md, "Testnet figures", "shadow-based", "not a result", "mode now: **hedge**",
		"Advantage", "positive: the hedge did better than the shed", "Fees (real 0.0001 + paper 0.0010)", "0.0011 BTC | 10 |",
		"Real trades | 1 | 10 |", "Paper trades | 2 | 10 |", "still open | 1 of 3 | 10 |", "2026-10-29", hedge.BenchmarkNote, "funding left out")
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Figure") && !strings.HasSuffix(line, "| 10 |") {
			t.Errorf("a figure without its days: %s", line)
		}
	}
}

func TestEvidenceHedgeTrial_ModeOffIsShed(t *testing.T) {
	t.Parallel()
	if r := trialReport(trialState(trialLedger(1), false), 10, "testnet"); r.Hedge.Mode != evidence.HedgeModeShed {
		t.Errorf("mode %q", r.Hedge.Mode)
	}
}

func TestEvidenceHedgeTrial_NoLedger(t *testing.T) {
	t.Parallel()
	paperOnly := hedge.ModeState{Ledger: trialLedger(1)} // the paper hedge runs before the first switch
	for name, st := range map[string]hedge.ModeState{"no file": {}, "paper ledger, never switched on": paperOnly} {
		r := trialReport(st, 10, "testnet")
		h := r.Hedge
		if h.Status != evidence.HedgeTrialNotStarted || h.Advantage != nil || h.Days != nil || h.RealTrades != nil {
			t.Errorf("%s: %+v", name, h)
		}
		md := trialSection(t, r)
		wantIn(t, md, "The trial has not started", "No figure")
		wantNotIn(t, md, "| Figure", "Advantage", "mode now", "Verdict")
		if h.Verdict != nil {
			t.Errorf("%s: verdict %+v", name, h.Verdict)
		}
	}
}

func TestEvidenceHedgeTrial_FewerThan7Days(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		days    float64
		measure bool
		missing int
	}{{0, false, 7}, {3, false, 4}, {6.99, false, 1}, {7, true, 0}, {7.5, true, 0}} {
		r := trialReport(trialState(trialLedger(1), true), c.days, "testnet")
		h := r.Hedge
		if c.measure {
			if h.Status != evidence.HedgeTrialMeasured || h.Advantage == nil || h.DaysMissing != nil {
				t.Errorf("%.2f days: %+v", c.days, h)
			}
			continue
		}
		if h.Status != evidence.HedgeTrialTooFew || *h.DaysMissing != c.missing || h.Advantage != nil || h.Fees != nil || h.RealTrades != nil {
			t.Errorf("%.2f days: %+v", c.days, h)
		}
		md := trialSection(t, r)
		wantIn(t, md, "**not enough days**", " day(s) still missing", "mode now: **hedge**",
			fmt.Sprintf("**Verdict (docs/hedge-trial.md): needs more days**: %d day(s) missing of the 7", c.missing))
		wantNotIn(t, md, "| Figure", "Advantage", "BTC")
		if v := h.Verdict; v == nil || v.Outcome != evidence.HedgeVerdictMoreDays {
			t.Errorf("%.2f days: verdict %+v", c.days, v)
		}
	}
}

// A clock behind the ledger's start is no negative count of days.
func TestEvidenceHedgeTrial_ClockBeforeStart(t *testing.T) {
	t.Parallel()
	h := trialReport(trialState(trialLedger(1), true), -1, "testnet").Hedge
	if *h.Days != 0 || *h.DaysMissing != evidence.HedgeTrialMinDays {
		t.Errorf("days %d missing %d", *h.Days, *h.DaysMissing)
	}
}

func TestEvidenceHedgeTrial_LossesAreAsVisibleAsGains(t *testing.T) {
	t.Parallel()
	row := func(scale float64) (float64, string) {
		r := trialReport(trialState(trialLedger(scale), true), 10, "testnet")
		for _, line := range strings.Split(trialSection(t, r), "\n") {
			if strings.HasPrefix(line, "| Advantage") {
				return *r.Hedge.Advantage, line
			}
		}
		t.Fatal("no advantage row")
		return 0, ""
	}
	gain, gainRow := row(1)
	loss, lossRow := row(10) // ten times the legs outweigh the hedge's P&L
	if loss >= 0 || gain <= 0 {
		t.Fatalf("advantages %v and %v: want one of each sign", loss, gain)
	}
	wantIn(t, lossRow, "**-0.0")
	wantIn(t, gainRow, "**+0.0")
	cell := func(s string) string { return s[:strings.Index(s, "**")] }
	if cell(lossRow) != cell(gainRow) {
		t.Errorf("different weight:\n%s\n%s", lossRow, gainRow)
	}
}

func TestEvidenceHedgeTrial_LiveShowsDaysAndCountsOnly(t *testing.T) {
	t.Parallel()
	r := trialReport(trialState(trialLedger(1), false), 10, "live")
	h := r.Hedge
	if h.AmountsShown || h.Status != evidence.HedgeTrialMeasured || *h.PaperTrades != 2 {
		t.Fatalf("%+v", h)
	}
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	wantNotIn(t, string(data), `"advantage"`, `"hedge_pnl"`, `"real_pnl"`, `"paper_pnl"`, `"counterfactuals_value"`, `"fees"`, `"worst_day"`, `"spot_usd"`)
	md := trialSection(t, r)
	wantIn(t, md, "Days and counts only", "Paper trades | 2 | 10 |")
	wantNotIn(t, md, "Advantage", "BTC |", "Testnet figures")
}

func TestEvidenceHedgeTrial_NoSpotNoFigure(t *testing.T) {
	t.Parallel()
	in := evidence.HedgeInputs{Path: "m.json", State: trialState(trialLedger(1), true), Now: trialStart.AddDate(0, 0, 10)}
	h := evidence.BuildHedgeTrial(in, nil, "testnet")
	if h.Status != evidence.HedgeTrialNoSpot || h.Advantage != nil {
		t.Errorf("%+v", h)
	}
}

func TestEvidenceHedgeTrial_NoShedDecision(t *testing.T) {
	t.Parallel()
	l := hedge.Ledger{Since: trialStart}
	l.Virtual.Book(1000, 100000, 0.00001)
	md := trialSection(t, trialReport(trialState(l, false), 10, "testnet"))
	wantIn(t, md, "none: no shed decision | 10 |", "still open | 0 of 0 | 10 |")
}

func TestEvidenceHedgeTrial_LoadHedge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := trialStart.AddDate(0, 0, 10)
	if in := evidence.LoadHedge(filepath.Join(dir, "absent.json"), now); in.Err != "" || !in.State.Since.IsZero() {
		t.Errorf("no file: %+v", in)
	}
	good := filepath.Join(dir, "hedge_mode.json")
	if err := hedge.SaveMode(good, trialState(trialLedger(1), true)); err != nil {
		t.Fatal(err)
	}
	if in := evidence.LoadHedge(good, now); in.Err != "" || !in.State.Enabled || !in.Now.Equal(now) || len(in.State.Ledger.Legs) != 3 {
		t.Errorf("saved file: %+v", in)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := evidence.LoadHedge(bad, now)
	if in.Err == "" {
		t.Fatal("an unreadable file gave no error")
	}
	h := evidence.BuildHedgeTrial(in, trialPoints(), "testnet")
	if h.Status != evidence.HedgeTrialNotRead || h.Advantage != nil {
		t.Errorf("%+v", h)
	}
	r := trialReport(hedge.ModeState{}, 10, "testnet")
	r.Hedge = h
	wantIn(t, trialSection(t, r), "**Not read**", "No figure")
}

func TestEvidenceHedgeTrial_BotPaths(t *testing.T) {
	t.Parallel()
	p := evidence.BotPaths("/env", "/env", "btc")
	if want := filepath.Join("/env", "data", "btc", "data", "hedge_mode.json"); p.HedgeMode != want {
		t.Errorf("%s, want %s", p.HedgeMode, want)
	}
}

// The verdict (#780): docs/hedge-trial.md's rule over the section's figures.
// trialLedger's advantage is about +0.081 BTC at scale 1 (fees 0.0011, worst
// day −0.02), −0.081 at scale 10, about 0 at 5.5, and at 3 the worst day
// (−0.06) costs more than half the advantage (+0.045).
func verdictReport(scale, days float64, ev *evidence.HedgeEvents) evidence.Report {
	now := trialStart.Add(time.Duration(days * 24 * float64(time.Hour)))
	return evidence.Build(evidence.Inputs{Env: "mvp", Bot: "btc", Account: "testnet", Window: evWeek, Points: trialPoints(),
		Hedge: evidence.HedgeInputs{Path: "m.json", State: trialState(trialLedger(scale), true), Now: now, Events: ev}})
}

func TestEvidenceHedgeTrial_Verdict(t *testing.T) {
	t.Parallel()
	clean := &evidence.HedgeEvents{}
	for _, c := range []struct {
		name    string
		scale   float64
		days    float64
		events  *evidence.HedgeEvents
		outcome string
		why     string
		cites   string // a figure the line must cite
	}{
		{"clear advantage, enough days", 1, 90, clean, evidence.HedgeVerdictPropose, "beat the shed net of fees", "0 hedge_unavailable, 0 hedge_rate_limited, 90 days"},
		{"clear advantage, fewer days than a proposal needs", 1, 10, clean, evidence.HedgeVerdictKeep, "10 of the 84 days", "advantage +0.08"},
		{"clear advantage, events not read", 1, 90, nil, evidence.HedgeVerdictKeep, "were not read", "hedge events not read"},
		{"clear advantage, the cap held a hedge", 1, 90, &evidence.HedgeEvents{RateLimited: 2}, evidence.HedgeVerdictKeep, "2 hedge(s) waited for the 12 orders/h cap", "2 hedge_rate_limited"},
		{"clear disadvantage", 10, 10, clean, evidence.HedgeVerdictStop, "worse than the shed by more than its fees", "advantage -0.08"},
		{"often unavailable", 1, 10, &evidence.HedgeEvents{Unavailable: 11}, evidence.HedgeVerdictStop, "unavailable more than 1 time(s) a day", "11 hedge_unavailable"},
		{"mixed: worst day past the guard", 3, 90, clean, evidence.HedgeVerdictKeep, "mixed", "worst day -0.0600 BTC"},
		{"no difference: within the fees", 5.5, 90, clean, evidence.HedgeVerdictKeep, "within the fees", "fees 0.0011 BTC"},
	} {
		r := verdictReport(c.scale, c.days, c.events)
		v := r.Hedge.Verdict
		if v == nil || v.Outcome != c.outcome || !strings.Contains(v.Why, c.why) || v.Rule != evidence.HedgeTrialRule {
			t.Errorf("%s: verdict %+v, want %q (%q)", c.name, v, c.outcome, c.why)
			continue
		}
		md := trialSection(t, r)
		wantIn(t, md, "**Verdict (docs/hedge-trial.md): "+c.outcome+"**: ", "Figures used: ", c.cites, "A verdict changes no hedge mode, setting or config")
		if n := strings.Count(md, "**Verdict"); n != 1 {
			t.Errorf("%s: %d verdict lines", c.name, n)
		}
	}
}

func TestEvidenceHedgeTrial_VerdictNoneOnLiveOrWithoutSpot(t *testing.T) {
	t.Parallel()
	live := trialReport(trialState(trialLedger(1), true), 10, "live")
	if live.Hedge.Verdict != nil {
		t.Errorf("live: %+v", live.Hedge.Verdict)
	}
	wantIn(t, trialSection(t, live), "No verdict: a live account")
	in := evidence.HedgeInputs{Path: "m.json", State: trialState(trialLedger(1), true), Now: trialStart.AddDate(0, 0, 10)}
	if h := evidence.BuildHedgeTrial(in, nil, "testnet"); h.Verdict != nil {
		t.Errorf("no spot: %+v", h.Verdict)
	}
}

func TestEvidenceHedgeTrial_EventRows(t *testing.T) {
	t.Parallel()
	md := trialSection(t, verdictReport(1, 10, &evidence.HedgeEvents{Unavailable: 3, RateLimited: 1}))
	wantIn(t, md, "| Real hedge orders an hour, mean (cap 12) | 0.00 | 10 |", "(hedge_rate_limited) | 1 | 10 |", "(hedge_unavailable) | 3 | 10 |")
	md = trialSection(t, verdictReport(1, 10, nil))
	wantIn(t, md, "| not read: the journal was not read | 10 |")
}

func TestEvidenceHedgeTrial_ReadHedgeEvents(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "orders.log")
	line := func(at time.Time, event, change string) string {
		return fmt.Sprintf(`{"timestamp":%q,"event":%q,"change":%q}`, at.Format(time.RFC3339), event, change)
	}
	lines := []string{
		line(trialStart.Add(-time.Hour), "hedge", "hedge_unavailable"), // before the ledger: not counted
		line(trialStart, "hedge", "hedge_unavailable"),
		line(trialStart.Add(time.Hour), "hedge", "hedge_rate_limited"),
		line(trialStart.Add(2*time.Hour), "hedge", "hedge_rate_limited"),
		line(trialStart.Add(3*time.Hour), "hedge", "hedge_fill"),
		line(trialStart.Add(4*time.Hour), "skipped", "hedge_unavailable"), // not a hedge line
		"not json",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, err := evidence.ReadHedgeEvents(path, trialStart)
	if err != nil || ev.Unavailable != 1 || ev.RateLimited != 2 {
		t.Errorf("events %+v, err %v", ev, err)
	}
	if _, err := evidence.ReadHedgeEvents(filepath.Join(t.TempDir(), "absent.log"), trialStart); err == nil {
		t.Error("an absent journal gave no error")
	}
}

// Given any verdict, no hedge setting, mode file or config is changed.
func TestEvidenceHedgeTrial_VerdictChangesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mode := filepath.Join(dir, "hedge_mode.json")
	for _, scale := range []float64{1, 3, 10} {
		if err := hedge.SaveMode(mode, trialState(trialLedger(scale), false)); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(mode)
		if err != nil {
			t.Fatal(err)
		}
		in := evidence.LoadHedge(mode, trialStart.AddDate(0, 0, 90))
		in.Events = &evidence.HedgeEvents{}
		r := evidence.Build(evidence.Inputs{Env: "mvp", Bot: "btc", Account: "testnet", Window: evWeek, Points: trialPoints(), Hedge: in})
		if r.Hedge.Verdict == nil {
			t.Fatalf("scale %v: no verdict", scale)
		}
		_ = covMarkdown(t, r)
		after, err := os.ReadFile(mode)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("scale %v (%s): the mode file changed", scale, r.Hedge.Verdict.Outcome)
		}
		if st, _ := hedge.LoadMode(mode); st.Enabled {
			t.Errorf("scale %v: the verdict switched the hedge on", scale)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("scale %v: files written next to the mode file: %v", scale, entries)
		}
	}
}
