package tests

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/orders"
)

// The weekly evidence report's "replay vs live" section (#488): the replay of
// the week next to the journal's result, the gap in coin and as a % of the
// live result, not comparable when the books leave a gap, % only for a live
// account, causes only when the journal shows them.

// rvlSection is the section's Markdown, up to the next section.
func rvlSection(t *testing.T, r evidence.Report) string {
	t.Helper()
	md := covMarkdown(t, r)
	i := strings.Index(md, "## Replay vs live")
	if i < 0 {
		t.Fatalf("no replay vs live section:\n%s", md)
	}
	md = md[i:]
	if j := strings.Index(md, "\n## Stress"); j >= 0 {
		md = md[:j]
	}
	return md
}

// Scenario: a full week with recorded books — the report shows replay
// result, live result, fees and the gap in coin and percent.
func TestEvidenceReplayVsLive_AFullWeekWithRecordedBooks(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	s := r.RVL

	if s.Label != "replay vs live" || s.Account != evidence.AccountTestnet || !s.AmountsShown || !s.Comparable || s.Note != "" {
		t.Fatalf("section %+v", s)
	}
	if s.Replay.Net == nil || *s.Replay.Net != r.Replay.Net || s.Live.Net == nil || *s.Live.Net != r.Gap.Live.Net {
		t.Errorf("results: replay %v live %v, want the Replay and Gap sections' %.5f and %.5f", s.Replay.Net, s.Live.Net, r.Replay.Net, r.Gap.Live.Net)
	}
	if s.GapCoin == nil || *s.GapCoin != r.Gap.Gap {
		t.Errorf("gap %v, want %.5f", s.GapCoin, r.Gap.Gap)
	}
	if s.GapPct == nil || math.Abs(*s.GapPct-100*r.Gap.Gap/math.Abs(r.Gap.Live.Net)) > 1e-9 {
		t.Errorf("gap %% of live %v (live %.5f, gap %.5f)", s.GapPct, r.Gap.Live.Net, r.Gap.Gap)
	}
	if s.GapEquityPP == nil || math.Abs(*s.GapEquityPP-100*r.Gap.Gap/in.Equity) > 1e-9 || math.Abs(s.Live.NetPct-100*r.Gap.Live.Net) > 1e-9 {
		t.Errorf("percentages of equity: gap %v live %.4f", s.GapEquityPP, s.Live.NetPct)
	}
	if s.Replay.Fees == nil || *s.Replay.Fees != r.Replay.Fees || s.Live.Fees == nil || math.Abs(*s.Live.Fees-0.0006) > 1e-12 {
		t.Errorf("fees: replay %v live %v, want %.5f and the stop's 0.0006", s.Replay.Fees, s.Live.Fees, r.Replay.Fees)
	}
	if s.Live.Trades != 2 || s.Replay.Trades != r.Replay.Trades {
		t.Errorf("trades: live %d (want the week's open and stop) replay %d (want %d)", s.Live.Trades, s.Replay.Trades, r.Replay.Trades)
	}
	if s.Replay.WorstDay == nil || s.Live.WorstDay == nil || s.Replay.WorstDayDate == "" || s.Live.WorstDayDate == "" {
		t.Errorf("worst days: replay %v %q live %v %q", s.Replay.WorstDay, s.Replay.WorstDayDate, s.Live.WorstDay, s.Live.WorstDayDate)
	}
	if s.SpotUSD == nil || *s.SpotUSD != 100000 || s.GapUSD == nil || math.Abs(*s.GapUSD-r.Gap.Gap*100000) > 1e-6 {
		t.Errorf("USD: spot %v gap %v", s.SpotUSD, s.GapUSD)
	}
	if len(s.Causes) != 0 {
		t.Errorf("causes %v: the journal shows no freeze, restart or manual action", s.Causes)
	}
	md := rvlSection(t, r)
	for _, want := range []string{"Method:", "| replay |", "| live |", "Result BTC", "Result USD", "Fees BTC",
		"**gap (replay − live)**:", "% of the live result**", "likely causes in the journal: none shown", "not modelled by the replay"} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}
}

// A manual close in the week is named as a cause; nothing else is invented.
func TestEvidenceReplayVsLive_CausesOnlyFromTheJournal(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	in.Trades = append(in.Trades, orders.Trade{At: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC), Kind: "close",
		Instrument: "BTC-30OCT26-90000-P", OptionType: "put", Qty: 1, Reason: "manual_close", Actor: &orders.Actor{User: "bob", Role: "manager"}})
	s := evidence.Build(in).RVL
	if len(s.Causes) != 1 || !strings.Contains(s.Causes[0], "manual action: 1 close") {
		t.Errorf("causes %v, want the one manual close", s.Causes)
	}
}

// Scenario: a week with missing books — the week is listed as not
// comparable with the missing hours, and no gap is computed.
func TestEvidenceReplayVsLive_AWeekWithMissingBooks(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	snaps := dropHours(evRecord(t), start, 6)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	s := r.RVL

	if s.Comparable || !strings.Contains(s.Note, "gap over 3 h") {
		t.Fatalf("comparable %v note %q: a 6 h recorder gap leaves the week not comparable", s.Comparable, s.Note)
	}
	if s.MissingHours == nil || *s.MissingHours != 6 || len(s.Gaps) != 1 || !s.Gaps[0].From.Equal(start) {
		t.Errorf("missing hours %v gaps %+v, want the 6 h from %s", s.MissingHours, s.Gaps, start)
	}
	if s.GapCoin != nil || s.GapPct != nil || s.GapEquityPP != nil || s.Replay.Net != nil || s.Live.Net != nil {
		t.Errorf("no gap may be computed: %+v", s)
	}
	md := rvlSection(t, r)
	for _, want := range []string{"market books missing: 6.0 h", "2026-10-14 03:00 → 2026-10-14 09:00 UTC", "**Not comparable**", "No gap computed"} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "gap (replay − live)") {
		t.Errorf("a not comparable week shows a gap:\n%s", md)
	}
	// The existing sections are unchanged: the replay still ran, the Gap section still reports.
	if !r.Replay.Ran || !r.Gap.Known {
		t.Errorf("replay %v gap %v: the Replay and Gap sections must not change", r.Replay.Ran, r.Gap.Known)
	}

	// Coverage under the share, coverage not measured, replay not run: not comparable either.
	for name, mod := range map[string]func(*evidence.Inputs){
		"under 90 %":   func(in *evidence.Inputs) { in.Snaps = evRecord(t, 14); in.Recorded = covSets(in.Snaps) },
		"not measured": func(in *evidence.Inputs) { in.Recorded = nil },
		"no replay":    func(in *evidence.Inputs) { in.Equity = 0 },
	} {
		in := evInputs(t, evRecord(t))
		in.Recorded = covSets(in.Snaps)
		mod(&in)
		if s := evidence.Build(in).RVL; s.Comparable || s.Note == "" || s.GapCoin != nil {
			t.Errorf("%s: comparable %v note %q gap %v", name, s.Comparable, s.Note, s.GapCoin)
		}
	}
}

// Scenario: a live bot — DERIBIT_ENV=live shows percentages only and no
// amounts, in the Markdown and in the JSON.
func TestEvidenceReplayVsLive_ALiveBot(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	in.Account = evidence.AccountLive
	r := evidence.Build(in)
	s := r.RVL

	if !s.Comparable || s.AmountsShown || s.Account != evidence.AccountLive {
		t.Fatalf("section %+v", s)
	}
	for name, v := range map[string]*float64{"replay net": s.Replay.Net, "replay usd": s.Replay.NetUSD, "replay fees": s.Replay.Fees,
		"replay worst": s.Replay.WorstDay, "live net": s.Live.Net, "live usd": s.Live.NetUSD, "live fees": s.Live.Fees,
		"live worst": s.Live.WorstDay, "gap": s.GapCoin, "gap usd": s.GapUSD, "spot": s.SpotUSD} {
		if v != nil {
			t.Errorf("%s = %v: a live account shows no amount", name, *v)
		}
	}
	if s.GapPct == nil || s.GapEquityPP == nil || s.Live.NetPct == 0 {
		t.Errorf("percentages missing: %+v", s)
	}
	js, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"net":`, `"net_usd"`, `"fees":`, `"worst_day":`, `"gap":`, `"gap_usd"`, `"spot_usd"`} {
		if strings.Contains(string(js), key) {
			t.Errorf("JSON carries %s for a live account: %s", key, js)
		}
	}
	md := rvlSection(t, r)
	if !strings.Contains(md, "Live account: percentages of the starting equity only") || !strings.Contains(md, "pp of equity") {
		t.Errorf("section:\n%s", md)
	}
	for _, bad := range []string{"Result BTC", "Result USD", " BTC (", "$"} {
		if strings.Contains(md, bad) {
			t.Errorf("live section shows an amount (%q):\n%s", bad, md)
		}
	}
}
