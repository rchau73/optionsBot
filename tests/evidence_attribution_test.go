package tests

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The weekly evidence report's attribution section (#581): the week's booked
// result split into premium, buy-back cost by exit reason and fees, the rows
// summing to the realised result, any difference shown as unexplained.

// attrClose is a close of qty at price whose P&L adds up (orders.ClosedNetPnL).
func attrClose(at time.Duration, reason string, premium, price, qty, fees float64) orders.Trade {
	pnl := premium - price*qty - fees
	return orders.Trade{At: evWeek.From.Add(at), Kind: "close", Reason: reason, Premium: premium, Price: price, Qty: qty,
		Fees: fees, PnL: pnl, PnLUSD: pnl * 100000, Spot: 100000}
}

func attrRow(t *testing.T, a evidence.Attribution, key string) evidence.AttributionRow {
	t.Helper()
	for _, r := range a.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", key, a.Rows)
	return evidence.AttributionRow{}
}

// attrSection is the section's Markdown, up to the next section.
func attrSection(t *testing.T, r evidence.Report) string {
	t.Helper()
	md := covMarkdown(t, r)
	i := strings.Index(md, "## Where the result came from")
	if i < 0 {
		t.Fatalf("no attribution section:\n%s", md)
	}
	md = md[i:]
	if j := strings.Index(md[1:], "\n## "); j >= 0 {
		md = md[:j+1]
	}
	return md
}

func attrWeek() []orders.Trade {
	return []orders.Trade{
		attrClose(-time.Hour, "stop_loss", 9, 9, 9, 9), // before the week: not in it
		{At: evWeek.From.Add(time.Hour), Kind: "open", Premium: 0.04, OpenFee: 0.001, Spot: 100000},
		attrClose(10*time.Hour, "stop_loss", 0.02, 0.03, 2, 0.001),
		attrClose(20*time.Hour, "delta_exit", 0.02, 0.02, 1, 0.001),
		attrClose(30*time.Hour, "dte_rollout", 0.02, 0.01, 1, 0.001),
		attrClose(40*time.Hour, "delta_drift", 0.02, 0.005, 1, 0.001),
		attrClose(50*time.Hour, "roi_target", 0.02, 0.004, 1, 0.001),
		attrClose(60*time.Hour, "margin_mm_limit", 0.02, 0.003, 1, 0.001),
		attrClose(70*time.Hour, "gamma_regime", 0.02, 0.002, 1, 0.001),
		attrClose(80*time.Hour, "rebalance_legs", 0.02, 0.001, 1, 0.001),
		attrClose(7*24*time.Hour, "stop_loss", 9, 9, 9, 9), // the week's end is exclusive
	}
}

// Scenario: the parts add up — the rows sum to the report's net result.
func TestEvidenceAttribution_ThePartsAddUp(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Equity = 2
	in.Trades = attrWeek()
	r := evidence.Build(in)
	a := r.Attr
	if !a.Known || a.Closes != 8 || *a.Net.Coin != r.Live.Realised || a.Net.Closes != r.Live.Closes {
		t.Fatalf("attribution %+v (realised %v)", a, r.Live.Realised)
	}
	sum := 0.0
	for _, row := range a.Rows {
		sum += *row.Coin
	}
	if math.Abs(sum-r.Live.Realised) > 1e-12 {
		t.Errorf("rows sum to %v, realised %v", sum, r.Live.Realised)
	}
	for key, want := range map[string]float64{evidence.AttrPremium: 0.16, "stop_loss": -0.06, "delta_exit": -0.02, "dte_rollout": -0.01,
		"delta_drift": -0.005, "roi_target": -0.004, "margin_mm_limit": -0.003, evidence.AttrOther: -0.003, evidence.AttrFees: -0.008} {
		row := attrRow(t, a, key)
		if math.Abs(*row.Coin-want) > 1e-12 || math.Abs(*row.Pct-want/2*100) > 1e-9 || math.Abs(*row.USD-want*100000) > 1e-6 {
			t.Errorf("%s: %+v, want %v", key, row, want)
		}
	}
	if u := attrRow(t, a, evidence.AttrUnexplained); *u.Coin != 0 || u.Closes != 0 || a.Flagged {
		t.Errorf("unexplained %+v flagged %v", u, a.Flagged)
	}
	if strings.Join(a.OtherReasons, ",") != "gamma_regime,rebalance_legs" {
		t.Errorf("other reasons %v", a.OtherReasons)
	}
	if a.StopSharePct == nil || math.Abs(*a.StopSharePct-100*0.06/0.105) > 1e-9 {
		t.Errorf("stop share %v", a.StopSharePct)
	}
	md := attrSection(t, r)
	for _, want := range []string{"| premium of the legs closed | 8 | +0.1600 | +16000 | +8.00 |", "| buy-back: stop-loss | 1 | -0.0600 | -6000 | -3.00 |",
		"| unexplained | 0 |", "| **net: the realised result** | 8 |", "other buy-backs: gamma_regime, rebalance_legs",
		"stop-losses' share of the buy-back cost: **57.1 %**", "1 leg(s), premium +0.0400 BTC (+2.00 % of equity), opening fees -0.0010 BTC (-0.05 %)"} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "flagged") {
		t.Errorf("section:\n%s", md)
	}
}

// Scenario: the parts add up — and any gap is listed as unexplained with its
// size; flagged only past the tolerance.
func TestEvidenceAttribution_AGapIsListedAsUnexplained(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		gap     float64
		flagged bool
	}{"inside": {0.009, false}, "at the tolerance": {0.01, false}, "past": {-0.02, true}} {
		in := evInputs(t, nil)
		in.Trades = []orders.Trade{attrClose(time.Hour, "stop_loss", 0.02, 0.01, 1, 0.001), attrClose(2*time.Hour, "roi_target", 0.02, 0.01, 1, 0.001)}
		in.Trades[1].PnL += c.gap
		r := evidence.Build(in)
		a := r.Attr
		u := attrRow(t, a, evidence.AttrUnexplained)
		if math.Abs(*u.Coin-c.gap) > 1e-12 || u.Closes != 1 || math.Abs(*u.Pct-c.gap*100) > 1e-9 || a.Flagged != c.flagged || *a.Net.Coin != r.Live.Realised {
			t.Errorf("%s: unexplained %+v flagged %v", name, u, a.Flagged)
		}
		if md := attrSection(t, r); strings.Contains(md, "**flagged: unexplained over the 1.0 % tolerance**") != c.flagged {
			t.Errorf("%s: section:\n%s", name, md)
		}
	}
}

// Scenario: live account shows percentages only.
func TestEvidenceAttribution_LiveAccountShowsPercentagesOnly(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Account = evidence.AccountLive
	in.Trades = attrWeek()
	r := evidence.Build(in)
	a := r.Attr
	if a.AmountsShown || a.Account != evidence.AccountLive {
		t.Fatalf("attribution %+v", a)
	}
	for _, row := range append(a.Rows, a.Net, a.Sold, a.OpenFees) {
		if row.Coin != nil || row.USD != nil || row.Pct == nil {
			t.Errorf("row %+v", row)
		}
	}
	data, _ := json.Marshal(a)
	for _, k := range []string{`"coin"`, `"usd"`} {
		if strings.Contains(string(data), k) {
			t.Errorf("JSON carries %s: %s", k, data)
		}
	}
	md := attrSection(t, r)
	if strings.Contains(md, "BTC") || strings.Contains(md, "USD") || !strings.Contains(md, "| buy-back: stop-loss | 1 | -6.00 |") {
		t.Errorf("section:\n%s", md)
	}
}

// Scenario: a week with no closes.
func TestEvidenceAttribution_AWeekWithNoCloses(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Trades = []orders.Trade{{At: evWeek.From.Add(time.Hour), Kind: "open", Premium: 0.04, OpenFee: 0.001, Spot: 100000}}
	r := evidence.Build(in)
	a := r.Attr
	if a.Closes != 0 || *a.Sold.Coin != 0.04 || *a.OpenFees.Coin != -0.001 || *a.Net.Coin != 0 || a.StopSharePct != nil {
		t.Fatalf("attribution %+v", a)
	}
	md := attrSection(t, r)
	for _, want := range []string{"**No closes in the week**", "| premium of the legs closed | 0 | +0.0000 |", "| fees (opening share + closing fee) | 0 | +0.0000 |",
		"premium +0.0400 BTC (+4.00 % of equity), opening fees -0.0010 BTC (-0.10 %)", "no buy-back cost in the week"} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}
}

// Inputs missing: no journal, no starting equity, no spot. Each says so.
func TestEvidenceAttribution_MissingInputs(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Trades, in.JournalNote = nil, "open orders.log: no such file"
	r := evidence.Build(in)
	if r.Attr.Known || !strings.Contains(attrSection(t, r), "**Not available**: the journal was not read: open orders.log") {
		t.Errorf("no journal: %+v", r.Attr)
	}

	in = evInputs(t, nil)
	in.Equity = 0
	in.Trades = []orders.Trade{attrClose(time.Hour, "stop_loss", 0.02, 0.01, 1, 0.001)}
	in.Trades[0].PnL, in.Trades[0].PnLUSD = 5, 0 // unexplained, but no equity to judge it against
	r = evidence.Build(in)
	if row := attrRow(t, r.Attr, "stop_loss"); row.Pct != nil || row.Coin == nil || r.Attr.Flagged {
		t.Errorf("no equity: %+v flagged %v", row, r.Attr.Flagged)
	}
	if !strings.Contains(attrSection(t, r), "| buy-back: stop-loss | 1 | -0.0100 | -1000 | unknown |") {
		t.Errorf("section:\n%s", attrSection(t, r))
	}

	in = evInputs(t, nil)
	in.Points = []history.Point{}
	in.Trades = []orders.Trade{{At: evWeek.From.Add(time.Hour), Kind: "close", Reason: "stop_loss", Premium: 0.02, Price: 0.03, Qty: 1, PnL: -0.01}}
	in.Trades[0].PnLUSD = 0
	r = evidence.Build(in)
	if row := attrRow(t, r.Attr, "stop_loss"); row.USD != nil || row.Coin == nil {
		t.Errorf("no spot: %+v", row)
	}
	if !strings.Contains(attrSection(t, r), "| buy-back: stop-loss | 1 | -0.0300 | unknown |") {
		t.Errorf("section:\n%s", attrSection(t, r))
	}
}

// An open's fee comes back from the journal's fill line.
func TestEvidenceAttribution_OpenFeeReadFromTheJournal(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	l := orders.NewWriterLogger(&b, 0)
	pos := &orders.Position{Instrument: "BTC-30OCT26-90000-P", OptionType: "put", Qty: 1}
	l.LogOpen(pos, orders.Fill{Qty: 1, FillPrice: 0.01, Fee: 0.0003}, orders.TriggerEntry, orders.EventContext{})
	rp, err := orders.ReplayJournal(strings.NewReader(b.String()), 0)
	if err != nil || len(rp.Trades) != 1 || rp.Trades[0].OpenFee != 0.0003 {
		t.Fatalf("trades %+v, err %v", rp.Trades, err)
	}
}

// The stop share reaches the track record and the trend; a report older than
// the section reads "not measured".
func TestEvidenceAttribution_StopShareInTheWeekRows(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Trades = attrWeek()
	r := evidence.Build(in)
	old := r
	old.Week = "2026-W41"
	old.Attr = evidence.Attribution{}
	tr := evidence.BuildTrend("stable", "btc", "weekly", []evidence.Report{old, r}, nil)
	if len(tr.Weeks) != 2 {
		t.Fatalf("weeks %+v", tr.Weeks)
	}
	if w := tr.Weeks[1]; w.StopShare == nil || *w.StopShare != *r.Attr.StopSharePct {
		t.Errorf("W42 %+v", w)
	}
	if w := tr.Weeks[0]; w.StopShare != nil || !strings.Contains(strings.Join(w.NotMeasured, ","), "stop_share") {
		t.Errorf("W41 %+v", w)
	}
}
