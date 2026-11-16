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

// The weekly evidence report's gap split (#802): the Gap section's replay −
// live cut into fill price, fees, trades one side made, marks and
// unexplained, each in coin and % of the equity, with the legs behind it.

// splitSection is the section's Markdown, up to the next section.
func splitSection(t *testing.T, r evidence.Report) string {
	t.Helper()
	md := covMarkdown(t, r)
	i := strings.Index(md, "## Gap split (replay − live)")
	if i < 0 {
		t.Fatalf("no gap split section:\n%s", md)
	}
	md = md[i:]
	if j := strings.Index(md[1:], "\n## "); j >= 0 {
		md = md[:j+1]
	}
	return md
}

func splitPart(t *testing.T, s evidence.GapSplit, name string) evidence.GapPart {
	t.Helper()
	for _, p := range s.Parts {
		if p.Part == name {
			return p
		}
	}
	t.Fatalf("no %q part in %+v", name, s.Parts)
	return evidence.GapPart{}
}

func splitSum(s evidence.GapSplit) float64 {
	sum := 0.0
	for _, p := range s.Parts {
		sum += *p.Coin
	}
	return sum
}

// Scenario: a week where live and replay differ — the report lists fill
// price, fees, one-sided trades, marks and unexplained, each in coin and in
// % of the equity, and they add up to the Gap section's gap.
func TestEvidenceGapSplit_AWeekWhereLiveAndReplayDiffer(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	s := r.Split

	if s.Label != evidence.GapSplitLabel || !s.Known || s.Note != "" || s.Partial != "" || !s.AmountsShown {
		t.Fatalf("section %+v", s)
	}
	want := []string{evidence.PartFillPrice, evidence.PartFees, evidence.PartOneSided, evidence.PartMarks, evidence.PartUnexplained}
	if len(s.Parts) != len(want) {
		t.Fatalf("parts %+v, want %v", s.Parts, want)
	}
	pctSum := 0.0
	for i, p := range s.Parts {
		if p.Part != want[i] || p.Coin == nil || math.Abs(p.EquityPP-100**p.Coin/in.Equity) > 1e-9 {
			t.Errorf("part %d %+v, want %s in coin and pp of equity", i, p, want[i])
		}
		if p.GapPct != nil {
			pctSum += *p.GapPct
		}
	}
	if s.Gap == nil || *s.Gap != r.Gap.Gap || math.Abs(splitSum(s)-r.Gap.Gap) > 1e-12 {
		t.Errorf("parts add to %.6f, gap %v, want the Gap section's %.6f", splitSum(s), s.Gap, r.Gap.Gap)
	}
	if r.Gap.Gap != 0 && math.Abs(pctSum-100) > 1e-6 {
		t.Errorf("shares of the gap add to %.4f %%, want 100", pctSum)
	}
	if r.Replay.Trades > 0 && splitPart(t, s, evidence.PartFees).Legs == 0 {
		t.Errorf("fees part counts no legs although the replay traded %d", r.Replay.Trades)
	}
	// The live stop (the journal's only close in the week) has a fee the
	// replay does not pay on that leg: the fees part names live's 0.0006.
	if f := splitPart(t, s, evidence.PartFees); math.Abs(*f.Coin-(0.0006-r.Replay.Fees)) > 1e-12 {
		t.Errorf("fees part %.6f, want live 0.0006 − replay %.6f", *f.Coin, r.Replay.Fees)
	}
	// The fixture's stop has no fill price, so its buy-back cost is what the
	// four parts cannot place: unexplained, and the gap reads "not explained".
	if u := splitPart(t, s, evidence.PartUnexplained); math.Abs(*u.Coin-0.0119) > 1e-9 || s.Explained {
		t.Errorf("unexplained %.6f explained %v, want the stop's missing 0.0119 buy-back", *u.Coin, s.Explained)
	}
	// With the price its P&L implies (0.006 premium − 0.0119 − 0.0006 fees =
	// −0.0065) the journal is consistent, and the parts explain the whole gap.
	in.Trades[2].Price = 0.0119
	if c := evidence.Build(in).Split; math.Abs(*splitPart(t, c, evidence.PartUnexplained).Coin) > 1e-9 || !c.Explained {
		t.Errorf("a consistent journal leaves %.9f unexplained (explained %v): %+v", *splitPart(t, c, evidence.PartUnexplained).Coin, c.Explained, c.Parts)
	}
	md := splitSection(t, r)
	for _, w := range []string{"Method:", "| Part | BTC | pp of equity | % of the gap | Sample |", "| fill price |", "| fees |",
		"| trades one side made |", "| marks |", "| **unexplained** |", "| gap |", "outside the sum"} {
		if !strings.Contains(md, w) {
			t.Errorf("section lacks %q:\n%s", w, md)
		}
	}
	// The Gap and RVL figures do not change.
	if r.RVL.GapCoin == nil || *r.RVL.GapCoin != r.Gap.Gap {
		t.Errorf("RVL gap %v, Gap %.6f", r.RVL.GapCoin, r.Gap.Gap)
	}
}

// Scenario: a one-sided trade — a leg live made and the replay did not is
// listed under "trades one side made" with its journal reason; matched to a
// replay fill, it moves to the fill price part by exactly the price difference.
func TestEvidenceGapSplit_AOneSidedTrade(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	base := evidence.Build(in).Split

	// The live put re-sold on 16 Oct (reason "entry" in the journal).
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	in.Trades[3].Reason = "repair"
	found := false
	for _, l := range evidence.Build(in).Split.OneSided {
		if l.Side == "live" && l.Instrument == "BTC-30OCT26-90000-P" && l.Kind == "open" && l.Reason == "repair" {
			found = true
		}
	}
	if !found {
		t.Errorf("the live repair is not listed with its journal reason: %+v", base.OneSided)
	}

	// A leg the replay sold and live did not: listed as replay-only, with the rule.
	var rep *evidence.OneSidedLeg
	for i, l := range base.OneSided {
		if l.Side == "replay" && l.Kind == "open" {
			rep = &base.OneSided[i]
			break
		}
	}
	if rep == nil {
		t.Fatalf("the replay sold nothing live did not: %+v", base.OneSided)
	}
	if rep.Reason == "" || rep.Qty == nil || rep.Price == nil || rep.Coin == nil {
		t.Errorf("replay-only leg %+v: want its rule, quantity, price and share", *rep)
	}

	// Live sells the same leg 10 minutes later, 0.001 higher: now matched.
	in2 := evInputs(t, snaps)
	in2.Recorded = covSets(snaps)
	in2.Trades = append(in2.Trades, orders.Trade{At: rep.At.Add(10 * time.Minute), Kind: "open", Slot: slot, Instrument: rep.Instrument,
		Qty: *rep.Qty, Price: *rep.Price + 0.001, Reason: "entry"})
	r2 := evidence.Build(in2)
	s2 := r2.Split
	for _, l := range s2.OneSided {
		if l.Instrument == rep.Instrument && l.At.Equal(rep.At) && l.Side == "replay" {
			t.Errorf("the matched leg is still listed as one-sided: %+v", l)
		}
	}
	got := *splitPart(t, s2, evidence.PartFillPrice).Coin - *splitPart(t, base, evidence.PartFillPrice).Coin
	if want := -0.001 * *rep.Qty; math.Abs(got-want) > 1e-12 {
		t.Errorf("fill price part moved by %.6f, want %.6f (live sold 0.001 higher)", got, want)
	}
	if math.Abs(splitSum(s2)-r2.Gap.Gap) > 1e-12 {
		t.Errorf("parts %.6f, gap %.6f", splitSum(s2), r2.Gap.Gap)
	}

	// A live leg on an instrument with no recorded quote leaves the
	// unexplained line as it was: its premium is in the one-sided part, its
	// value (the premium, as the Gap marks it) in the marks part.
	in3 := evInputs(t, snaps)
	in3.Recorded = covSets(snaps)
	in3.Trades = append(in3.Trades, orders.Trade{At: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC), Kind: "open", Slot: slot,
		Instrument: "BTC-30OCT26-250000-C", Qty: 1, Price: 0.002, Reason: "entry"})
	s3 := evidence.Build(in3).Split
	if d := *splitPart(t, s3, evidence.PartUnexplained).Coin - *splitPart(t, base, evidence.PartUnexplained).Coin; math.Abs(d) > 1e-12 {
		t.Errorf("unexplained moved by %.6f for an unquoted one-sided leg", d)
	}
	if d := *splitPart(t, s3, evidence.PartOneSided).Coin - *splitPart(t, base, evidence.PartOneSided).Coin; math.Abs(d+0.002) > 1e-12 {
		t.Errorf("one-sided moved by %.6f, want −0.002 (live's premium)", d)
	}
}

// Scenario: partial books — under 90 % of the week's hours the split is
// labelled partial, like the Replay and Gap sections.
func TestEvidenceGapSplit_PartialBooks(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t, 14)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	if r.Gap.Partial == "" || r.Split.Partial != r.Gap.Partial || !r.Split.Known {
		t.Fatalf("split partial %q known %v, gap partial %q", r.Split.Partial, r.Split.Known, r.Gap.Partial)
	}
	if md := splitSection(t, r); !strings.Contains(md, "**"+r.Gap.Partial+"**") {
		t.Errorf("section not labelled partial:\n%s", md)
	}
}

// Scenario: no replay for the week — the section says why and shows no
// figures, and the report still builds.
func TestEvidenceGapSplit_NoReplayForTheWeek(t *testing.T) {
	t.Parallel()
	in := evInputs(t, evRecord(t))
	in.Equity = 0
	r := evidence.Build(in)
	s := r.Split
	if s.Known || !strings.Contains(s.Note, "the replay did not run") || len(s.Parts) != 0 || s.Gap != nil || s.Exchange != nil {
		t.Fatalf("section %+v", s)
	}
	md := splitSection(t, r)
	if !strings.Contains(md, "**Not available**: the replay did not run") || strings.Contains(md, "| fill price |") {
		t.Errorf("section:\n%s", md)
	}
}

// Scenario: sample size — every figure names the legs behind it.
func TestEvidenceGapSplit_SampleSize(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	md := splitSection(t, r)
	for _, p := range r.Split.Parts {
		if p.Sample.Events == nil || *p.Sample.Events != p.Legs || p.Sample.EventsName != evidence.LegsName || p.Sample.Days != 7 || !p.Sample.Thin {
			t.Errorf("%s: sample %+v, want its %d legs over the replay's 7 days, thin", p.Part, p.Sample, p.Legs)
		}
		if !strings.Contains(md, p.Sample.Text()) {
			t.Errorf("%s: section lacks its sample %q", p.Part, p.Sample.Text())
		}
	}
	if x := r.Split.Exchange; x == nil || x.Sample.Events == nil || *x.Sample.Events != x.Legs {
		t.Errorf("exchange marks %+v: want its legs as its sample", x)
	}
	// The pack's split rows carry the same samples.
	p := evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Latest: &r, LatestPath: "data/evidence/W/btc.json"})
	n := 0
	for _, sec := range p.Sections {
		for _, f := range sec.Figures {
			if strings.HasPrefix(f.Name, "Gap split, ") {
				n++
				if f.Sample == nil || f.Sample.EventsName != evidence.LegsName {
					t.Errorf("pack row %q sample %+v", f.Name, f.Sample)
				}
			}
		}
	}
	if n != 3*len(r.Split.Parts) {
		t.Errorf("%d pack split rows, want coin, pp and %% of the gap for each of %d parts", n, len(r.Split.Parts))
	}
}

// A live account: percentages only, in the report and the pack, where the
// split is a share of the gap only.
func TestEvidenceGapSplit_ALiveAccount(t *testing.T) {
	t.Parallel()
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	in.Account = evidence.AccountLive
	r := evidence.Build(in)
	s := r.Split
	if !s.Known || s.AmountsShown || s.Gap != nil {
		t.Fatalf("section %+v", s)
	}
	js, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"coin":`, `"gap":`, `"qty":`, `"price":`, `"diff":`, `"recorded_mids":`} {
		if strings.Contains(string(js), key) {
			t.Errorf("JSON carries %s for a live account: %s", key, js)
		}
	}
	md := splitSection(t, r)
	if !strings.Contains(md, "Live account: percentages only") || strings.Contains(md, "| BTC |") || strings.Contains(md, " BTC)") {
		t.Errorf("section:\n%s", md)
	}
	p := evidence.BuildPack(evidence.PackInputs{Env: "stable", Bot: "btc", Account: evidence.AccountLive, Latest: &r, LatestPath: "x.json"})
	for _, sec := range p.Sections {
		for _, f := range sec.Figures {
			if strings.HasPrefix(f.Name, "Gap split, ") && !strings.HasSuffix(f.Name, "% of the gap") {
				t.Errorf("live pack row %q: only shares of the gap", f.Name)
			}
		}
	}
}
