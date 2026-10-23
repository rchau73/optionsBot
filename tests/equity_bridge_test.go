package tests

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
	"optionsbot/internal/strategy"
)

// #277: each P&L history line records the account's margin balance and
// equity, and the evidence report bridges the week's equity change: the
// bot's net P&L and the residual the bot does not explain.

// equityPoint is a history point carrying equity in unit.
func equityPoint(at time.Duration, total, spot, equity float64, unit string) history.Point {
	bal := equity
	return history.Point{T: evWeek.From.Add(at), Total: total, Spot: spot, MarginBalance: &bal, Equity: &equity, Unit: unit}
}

// runRecordingStrategy runs the strategy fixture with a P&L history fake
// written every second.
func runRecordingStrategy(t *testing.T) (*strategyFixture, *recordedPnL) {
	t.Helper()
	f := newStrategyFixture(t)
	f.cfg.ReportIntervalSec = 1
	f.exch.mu.Lock()
	f.exch.summary.Equity = 10.5 // differs from the 10 BTC margin balance
	f.exch.mu.Unlock()
	rec := &recordedPnL{}
	f.strat = strategy.New(f.cfg, strategy.Deps{
		Market: f.market, Exchange: f.exch, State: f.state, Journal: f.journal, Hedge: nopHedge{}, History: rec,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.strat.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return f, rec
}

// Scenario: a history line records the balance.
func TestEquityBridge_AHistoryLineRecordsTheBalance(t *testing.T) {
	_, rec := runRecordingStrategy(t)
	eventually(t, 3*time.Second, "point with a known margin", func() bool {
		m := rec.lastMargin()
		return m != nil && m.Known
	})
	m := rec.lastMargin()
	if m.MarginBalance != 10 || m.Equity != 10.5 || m.Unit != "BTC" {
		t.Errorf("recorded balance %v equity %v unit %q; want the account summary's 10, 10.5, BTC", m.MarginBalance, m.Equity, m.Unit)
	}

	// The store writes them on the line, and they load back.
	path := filepath.Join(t.TempDir(), "pnl_history.jsonl")
	h := openHistory(t, path, t0)
	h.RecordPnL(t0, 0.001, 0, 100000, m)
	h.Close()
	h = openHistory(t, path, t0)
	defer h.Close()
	pts := h.Range(t0.Add(-time.Minute), t0.Add(time.Minute), 10)
	if len(pts) != 1 || pts[0].MarginBalance == nil || *pts[0].MarginBalance != 10 || pts[0].Equity == nil ||
		*pts[0].Equity != 10.5 || pts[0].Unit != "BTC" {
		t.Fatalf("reloaded point = %+v", pts)
	}
}

// Under cross collateral the policy reads Deribit's USD totals; the equity
// recorded is the account-wide USD equity, in the same unit.
func TestEquityBridge_CrossCollateralEquityIsUSD(t *testing.T) {
	sum := orders.AccountSummary{Currency: "BTC", Equity: 1.2, MarginBalance: 1.1, CrossCollateralEnabled: true,
		TotalMarginBalanceUSD: 150000, TotalEquityUSD: 152000, TotalInitialMarginUSD: 30000}
	u := sum.MarginUsage()
	if u.Unit != "USD" || u.MarginBalance != 150000 || u.Equity != 152000 {
		t.Errorf("cross collateral usage = %+v", u)
	}
	sum.CrossCollateralEnabled = false
	if u := sum.MarginUsage(); u.Unit != "BTC" || u.MarginBalance != 1.1 || u.Equity != 1.2 {
		t.Errorf("own-currency usage = %+v", u)
	}
	rv := strategy.RiskView{Unit: "USD", MarginBalance: 150000, Equity: 152000, AsOf: t0}
	if m := strategy.HistoryMargin(rv, t0); m == nil || m.Unit != "USD" || m.MarginBalance != 150000 || m.Equity != 152000 {
		t.Errorf("history margin = %+v", m)
	}
}

// Scenario: margin unknown — the balance and equity are absent, not zero.
func TestEquityBridge_MarginUnknown(t *testing.T) {
	f, rec := runRecordingStrategy(t)
	eventually(t, 3*time.Second, "point with a known margin", func() bool {
		m := rec.lastMargin()
		return m != nil && m.Known
	})
	f.exch.mu.Lock()
	f.exch.summaryErr = errors.New("account summary timed out")
	f.exch.mu.Unlock()
	eventually(t, 4*time.Second, "point with the margin unknown", func() bool {
		m := rec.lastMargin()
		return m != nil && !m.Known
	})

	path := filepath.Join(t.TempDir(), "pnl_history.jsonl")
	h := openHistory(t, path, t0)
	h.RecordPnL(t0, 0, 0, 100000, rec.lastMargin())
	h.RecordPnL(t0.Add(time.Minute), 0, 0, 100000, nil) // before the first margin read
	h.RecordPnL(t0.Add(2*time.Minute), 0, 0, 100000,    // Deribit reported no equity
		&history.Margin{Known: true, MarginBalance: 10, Unit: "BTC"})
	h.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, l := range lines[:2] {
		for _, key := range []string{`"margin_balance"`, `"equity"`, `"unit"`} {
			if strings.Contains(l, key) {
				t.Errorf("line %d with the margin unknown carries %s: %s", i, key, l)
			}
		}
	}
	if !strings.Contains(lines[2], `"margin_balance":10`) || strings.Contains(lines[2], `"equity"`) {
		t.Errorf("no equity reported → balance only: %s", lines[2])
	}
}

// Scenario: old history still loads — history, perf and evidence read it,
// and the bridge says no equity was recorded.
func TestEquityBridge_OldHistoryStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnl_history.jsonl")
	old := `{"t":"2026-10-12T12:00:00Z","realised":0.001,"unrealised":0,"total":0.001,"spot":100000,"realised_usd":100,"total_usd":100,"im_pct":10,"im_limit_pct":20,"mm_pct":5,"entries_allowed":true}
{"t":"2026-10-13T12:00:00Z","realised":0.002,"unrealised":0,"total":0.002,"spot":100000,"realised_usd":200,"total_usd":200}
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	h := openHistory(t, path, evWeek.To)
	defer h.Close()
	pts := h.Range(evWeek.From, evWeek.To, 1000)
	if len(pts) != 2 || pts[0].Equity != nil || pts[0].MarginBalance != nil {
		t.Fatalf("old lines = %+v", pts)
	}
	if s := perf.Summarize(pts, 1, evWeek.From, evWeek.Last()); s.Points != 2 || math.Abs(s.NetPnL-0.001) > 1e-12 {
		t.Errorf("perf on old lines = %+v", s)
	}
	b := evidence.BuildBridge(pts, evWeek.From, evWeek.To, path)
	if b.Known || !strings.Contains(b.Note, "no equity recorded") {
		t.Errorf("bridge on old lines = %+v", b)
	}

	in := evInputs(t, nil) // its points carry no equity either
	r := evidence.Build(in)
	if r.Bridge.Known || !strings.Contains(r.Bridge.Note, "no equity recorded") {
		t.Errorf("report bridge = %+v", r.Bridge)
	}
	var md bytes.Buffer
	_ = evidence.WriteMarkdown(&md, r)
	if !strings.Contains(md.String(), "## Equity bridge\n\nSource: data/btc/data/pnl_history.jsonl") ||
		!strings.Contains(md.String(), "Not available: no equity recorded") {
		t.Errorf("markdown bridge:\n%s", md.String())
	}
}

// Scenario: a transfer shows as residual — equity fell by more than the
// bot's net P&L explains.
func TestEquityBridge_ATransferShowsAsResidual(t *testing.T) {
	in := evInputs(t, nil)
	in.Points = []history.Point{
		equityPoint(-time.Hour, 0, 100000, 9, "BTC"), // before the week: ignored
		equityPoint(time.Hour, 0, 100000, 2, "BTC"),
		{T: evWeek.From.Add(2 * time.Hour), Total: 0.004, Spot: 100000}, // margin unknown that cycle
		equityPoint(3*24*time.Hour, 0.01, 100000, 1.5, "BTC"),           // 0.51 BTC transferred out
		{T: evWeek.From.Add(4 * 24 * time.Hour), Total: 0.02, Spot: 100000},
	}
	r := evidence.Build(in)
	b := r.Bridge
	if !b.Known || b.Unit != "BTC" || b.EquityStart != 2 || b.EquityEnd != 1.5 || math.Abs(b.BotNet-0.01) > 1e-12 ||
		math.Abs(b.Residual+0.51) > 1e-12 || math.Abs(b.ResidualPct+25.5) > 1e-9 || b.Label != evidence.ResidualLabel {
		t.Fatalf("bridge = %+v", b)
	}
	if !b.Start.Equal(evWeek.From.Add(time.Hour)) || !b.End.Equal(evWeek.From.Add(3*24*time.Hour)) {
		t.Errorf("span %s → %s: want the first and last point carrying equity", b.Start, b.End)
	}
	var md bytes.Buffer
	_ = evidence.WriteMarkdown(&md, r)
	s := md.String()
	for _, want := range []string{"## Equity bridge", "equity 2.0000 BTC → 1.5000 BTC, change **-0.5000 BTC**",
		"net of fees): +0.0100 BTC", "**-0.5100 BTC = -25.50 % of the starting equity** — " + evidence.ResidualLabel} {
		if !strings.Contains(s, want) {
			t.Errorf("markdown lacks %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "## Equity bridge") < strings.Index(s, "## Live") || strings.Index(s, "## Equity bridge") > strings.Index(s, "## Replay") {
		t.Error("the bridge comes after Live and before Replay")
	}
}

// Scenario: the bot explains everything — the residual is zero, in coin
// and in USD (each step of the coin P&L valued at its own point's spot).
func TestEquityBridge_TheBotExplainsEverything(t *testing.T) {
	coin := evidence.BuildBridge([]history.Point{
		equityPoint(0, 0, 100000, 2, "BTC"),
		equityPoint(time.Hour, 0.0625, 100000, 2.0625, "BTC"),
	}, evWeek.From, evWeek.To, "h")
	if !coin.Known || coin.Residual != 0 || coin.ResidualPct != 0 {
		t.Errorf("coin bridge = %+v", coin)
	}

	usd := evidence.BuildBridge([]history.Point{
		equityPoint(0, 0, 100000, 1000000, "USD"),
		{T: evWeek.From.Add(time.Hour), Total: 0.25, Spot: 100000}, // +25,000 USD
		equityPoint(2*time.Hour, 0.5, 120000, 1055000, "USD"),      // +30,000 USD
	}, evWeek.From, evWeek.To, "h")
	if !usd.Known || usd.BotNet != 55000 || usd.Residual != 0 || !strings.Contains(usd.Label, "USD value of the collateral") {
		t.Errorf("USD bridge = %+v", usd)
	}
}

// Fewer than two points with equity, or a unit that changed: not known,
// with the reason.
func TestEquityBridge_NotKnownSaysWhy(t *testing.T) {
	one := evidence.BuildBridge([]history.Point{equityPoint(0, 0, 100000, 2, "BTC")}, evWeek.From, evWeek.To, "h")
	if one.Known || !strings.Contains(one.Note, "only one") {
		t.Errorf("one point = %+v", one)
	}
	switched := evidence.BuildBridge([]history.Point{
		equityPoint(0, 0, 100000, 2, "BTC"), equityPoint(time.Hour, 0, 100000, 200000, "USD"),
	}, evWeek.From, evWeek.To, "h")
	if switched.Known || !strings.Contains(switched.Note, "BTC → USD") {
		t.Errorf("unit change = %+v", switched)
	}
	noSpot := evidence.BuildBridge([]history.Point{
		equityPoint(0, 0, 0, 1000, "USD"), equityPoint(time.Hour, 0.1, 0, 1100, "USD"),
	}, evWeek.From, evWeek.To, "h")
	if noSpot.Known || !strings.Contains(noSpot.Note, "no spot") {
		t.Errorf("no spot = %+v", noSpot)
	}
	if none := evidence.BuildBridge(nil, evWeek.From, evWeek.To, "h"); none.Known || !strings.Contains(none.Note, "no equity recorded") {
		t.Errorf("no points = %+v", none)
	}
}
