package tests

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The weekly evidence report's account reconciliation (#566): the margin
// balance's change over the week next to the journal's booked P&L, the gap
// in coin and % of the starting balance, flagged over the tolerance.

// balancePoint is a history point carrying a margin balance in unit.
func balancePoint(at time.Duration, balance, spot float64, unit string) history.Point {
	b := balance
	return history.Point{T: evWeek.From.Add(at), Spot: spot, MarginBalance: &b, Unit: unit}
}

func reconClose(at time.Duration, pnl float64) orders.Trade {
	return orders.Trade{At: evWeek.From.Add(at), Kind: "close", Reason: "roi_target", PnL: pnl, Fees: 0.0001}
}

// reconSection is the section's Markdown, up to the next section.
func reconSection(t *testing.T, r evidence.Report) string {
	t.Helper()
	md := covMarkdown(t, r)
	i := strings.Index(md, "## Account reconciliation")
	if i < 0 {
		t.Fatalf("no reconciliation section:\n%s", md)
	}
	md = md[i:]
	if j := strings.Index(md[1:], "\n## "); j >= 0 {
		md = md[:j+1]
	}
	return md
}

// everyDay is one balance point a day over the week, from start rising by step.
func everyDay(start, step float64) []history.Point {
	var pts []history.Point
	for d := 0; d < 7; d++ {
		pts = append(pts, balancePoint(time.Duration(d)*24*time.Hour+time.Hour, start+float64(d)*step, 100000, "BTC"))
	}
	return pts
}

// The tolerance and the cause list are part of the report's contract.
func TestEvidenceReconcile_ToleranceAndCauses(t *testing.T) {
	t.Parallel()
	if evidence.ReconcileTolerancePct != 1.0 {
		t.Errorf("tolerance %.2f %%, want 1 %% of the starting balance", evidence.ReconcileTolerancePct)
	}
	want := []string{
		"a deposit or withdrawal on the exchange",
		"unrealised P&L of open legs, in the account's balance but not yet booked",
		"funding or fees the journal did not record",
		"a position the bot's book missed",
	}
	if !reflect.DeepEqual(evidence.ReconcileCauses, want) {
		t.Errorf("causes %q", evidence.ReconcileCauses)
	}
	label := strings.ToLower(evidence.ReconcileLabel)
	if !strings.Contains(label, "measured on the account") || strings.Contains(label, "expected") || strings.Contains(label, "profit") {
		t.Errorf("label %q", evidence.ReconcileLabel)
	}
}

// Scenario: equity change matches booked P&L.
func TestEvidenceReconcile_EquityChangeMatchesBookedPnL(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Points = everyDay(2, 0.0625)                        // 2 → 2.375 BTC
	in.Trades = []orders.Trade{reconClose(-time.Hour, -1), // before the week: not booked in it
		reconClose(30*time.Hour, 0.25), reconClose(100*time.Hour, 0.125),
		{At: evWeek.From.Add(50 * time.Hour), Kind: "open", Price: 0.01}}
	r := evidence.Build(in)
	c := r.Recon
	if !c.Known || c.Flagged || c.Closes != 2 || c.Gap == nil || *c.Gap != 0 || c.GapPct != 0 || len(c.Causes) != 0 {
		t.Fatalf("reconciliation %+v", c)
	}
	if *c.EquityStart != 2 || *c.EquityEnd != 2.375 || *c.Change != 0.375 || *c.Booked != 0.375 || c.Unit != "BTC" || c.EquityPoints != 7 {
		t.Errorf("figures %+v", c)
	}
	md := reconSection(t, r)
	for _, want := range []string{"Measured on the account, not a forecast", "days without a balance point: none",
		"margin balance 2.0000 BTC → 2.3750 BTC: change +0.3750 BTC (+18.75 %)", "booked P&L, net of fees: +0.3750 BTC",
		"**gap +0.0000 BTC = +0.00 % of the starting balance** — within the 1.0 % tolerance"} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "**flagged") || strings.Contains(strings.ToLower(md), "expected") || strings.Contains(strings.ToLower(md), "profit") {
		t.Errorf("section:\n%s", md)
	}
}

// Scenario: an unexplained gap is flagged — in coin and % of the starting
// equity, with the causes in plain words; a gap at the tolerance is not.
func TestEvidenceReconcile_AnUnexplainedGapIsFlagged(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Points = []history.Point{balancePoint(time.Hour, 2, 100000, "BTC"), balancePoint(6*24*time.Hour, 1.5, 100000, "BTC")}
	in.Trades = []orders.Trade{reconClose(48*time.Hour, 0.01)}
	r := evidence.Build(in)
	c := r.Recon
	if !c.Known || !c.Flagged || math.Abs(*c.Gap+0.51) > 1e-12 || math.Abs(c.GapPct+25.5) > 1e-9 ||
		!reflect.DeepEqual(c.Causes, evidence.ReconcileCauses) {
		t.Fatalf("reconciliation %+v", c)
	}
	md := reconSection(t, r)
	for _, want := range []string{"**gap -0.5100 BTC = -25.50 % of the starting balance**", "**flagged: over the 1.0 % tolerance**",
		"Possible causes: " + strings.Join(evidence.ReconcileCauses, "; ")} {
		if !strings.Contains(md, want) {
			t.Errorf("section lacks %q:\n%s", want, md)
		}
	}

	at := evidence.BuildReconciliation([]history.Point{balancePoint(0, 100, 0, "BTC"), balancePoint(time.Hour, 101, 0, "BTC")},
		nil, evWeek, "", "h", "j")
	if !at.Known || at.Flagged || at.GapPct != 1 {
		t.Errorf("a gap of exactly the tolerance is not flagged: %+v", at)
	}
	past := evidence.BuildReconciliation([]history.Point{balancePoint(0, 100, 0, "BTC"), balancePoint(time.Hour, 98.9, 0, "BTC")},
		nil, evWeek, "", "h", "j")
	if !past.Flagged {
		t.Errorf("a gap of -1.1 %% is flagged: %+v", past)
	}
}

// Scenario: not enough history — under two balance points, no gap figure.
func TestEvidenceReconcile_NotEnoughHistory(t *testing.T) {
	t.Parallel()
	for name, pts := range map[string][]history.Point{
		"none":      nil,
		"one":       {balancePoint(time.Hour, 2, 100000, "BTC")},
		"no margin": {{T: evWeek.From.Add(time.Hour), Total: 0.1}, {T: evWeek.From.Add(2 * time.Hour), Total: 0.2}},
		"outside":   {balancePoint(-time.Hour, 2, 100000, "BTC"), balancePoint(7*24*time.Hour, 3, 100000, "BTC"), balancePoint(time.Hour, 2, 100000, "BTC")},
	} {
		in := evInputs(t, nil)
		in.Points = pts
		r := evidence.Build(in)
		c := r.Recon
		if c.Known || c.Gap != nil || c.Flagged || !strings.HasPrefix(c.Note, "not enough history") {
			t.Errorf("%s: %+v", name, c)
		}
		md := reconSection(t, r)
		if !strings.Contains(md, "**Not enough history") || !strings.Contains(md, "No gap figure") || strings.Contains(md, "**gap") {
			t.Errorf("%s: section:\n%s", name, md)
		}
		data, _ := json.Marshal(c)
		if strings.Contains(string(data), `"gap":`) {
			t.Errorf("%s: JSON carries a gap: %s", name, data)
		}
	}
}

// Scenario: missing days are named beside the figures.
func TestEvidenceReconcile_MissingDaysAreNamed(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	pts := everyDay(2, 0)
	in.Points = append(pts[:2:2], pts[4:]...) // Oct 14 and 15 missing
	r := evidence.Build(in)
	c := r.Recon
	if !c.Known || !reflect.DeepEqual(c.MissingDays, []string{"2026-10-14", "2026-10-15"}) {
		t.Fatalf("reconciliation %+v", c)
	}
	md := reconSection(t, r)
	if !strings.Contains(md, "days without a balance point: **2** — 2026-10-14, 2026-10-15") {
		t.Errorf("section:\n%s", md)
	}
}

// A live account shows percentages only, in the Markdown and the JSON.
func TestEvidenceReconcile_LiveAccountShowsPercentagesOnly(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Account = evidence.AccountLive
	in.Trades = nil
	in.Points = []history.Point{balancePoint(time.Hour, 2, 100000, "BTC"), balancePoint(6*24*time.Hour, 1.5, 100000, "BTC")}
	r := evidence.Build(in)
	c := r.Recon
	if !c.Known || c.AmountsShown || c.EquityStart != nil || c.Change != nil || c.Booked != nil || c.Gap != nil || math.Abs(c.GapPct+25) > 1e-9 || !c.Flagged {
		t.Fatalf("reconciliation %+v", c)
	}
	data, _ := json.Marshal(c)
	for _, k := range []string{`"equity_start"`, `"equity_end"`, `"change"`, `"booked"`, `"gap"`} {
		if strings.Contains(string(data), k+":") {
			t.Errorf("JSON carries %s: %s", k, data)
		}
	}
	md := reconSection(t, r)
	if strings.Contains(md, " BTC") || !strings.Contains(md, "**gap -25.00 % of the starting balance**") {
		t.Errorf("section:\n%s", md)
	}
}

// A USD balance (cross collateral) books each close at its own spot, and a
// switch of the unit in the week is not reconciled.
func TestEvidenceReconcile_USDBalanceAndUnitChange(t *testing.T) {
	t.Parallel()
	pts := []history.Point{balancePoint(0, 1000000, 100000, "USD"), balancePoint(2*time.Hour, 1055000, 120000, "USD")}
	trades := []orders.Trade{
		{At: evWeek.From.Add(time.Hour), Kind: "close", PnL: 0.25, PnLUSD: 25000},
		{At: evWeek.From.Add(2 * time.Hour), Kind: "close", PnL: 0.25}, // older line without pnl_usd: the history's spot
	}
	c := evidence.BuildReconciliation(pts, trades, evWeek, "", "h", "j")
	if !c.Known || c.Unit != "USD" || *c.Booked != 55000 || *c.Gap != 0 || c.Flagged {
		t.Errorf("USD reconciliation %+v", c)
	}
	pts[1].Unit = "BTC"
	c = evidence.BuildReconciliation(pts, trades, evWeek, "", "h", "j")
	if c.Known || !strings.Contains(c.Note, "unit changed") {
		t.Errorf("unit change %+v", c)
	}
}
