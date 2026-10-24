package tests

import (
	"io"
	"math"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
)

// perfPoints is one point a day from start: total P&L from totals, spot from
// spots, and the margin balance (in unit) on every point when balance > 0.
func perfPoints(start time.Time, totals, spots []float64, balance float64, unit string) []history.Point {
	pts := make([]history.Point, len(totals))
	for i, total := range totals {
		pts[i] = history.Point{T: start.AddDate(0, 0, i), Realised: total, Total: total, Spot: spots[i]}
		if balance > 0 {
			b := balance
			pts[i].MarginBalance, pts[i].Unit = &b, unit
		}
	}
	return pts
}

func ramp(n int, from, step float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = from + float64(i)*step
	}
	return out
}

func perfApprox(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: null, want %v", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

// Scenario: a 30-day summary — the payload carries perf.Summarize's figures.
func TestPerformance_ThirtyDaySummary(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// Rises, falls 0.03 over a week (the drawdown and the worst week), rises.
	totals := make([]float64, 30)
	for i := range totals {
		switch {
		case i < 10:
			totals[i] = 0.002 * float64(i)
		case i < 17:
			totals[i] = 0.018 - 0.005*float64(i-9)
		default:
			totals[i] = -0.017 + 0.003*float64(i-16)
		}
	}
	pts := perfPoints(start, totals, ramp(30, 60000, 100), 2, "BTC")
	from, to := start.Add(-time.Hour), start.AddDate(0, 0, 29).Add(time.Hour)

	got := api.BuildPerformance(pts, from, to, "30d", "testnet")
	want := perf.Summarize(pts, 2, from, to)

	if got.Points != 30 || !got.NetPnLKnown || got.NetPnL != want.NetPnL {
		t.Fatalf("points %d net %v, want 30 and %v", got.Points, got.NetPnL, want.NetPnL)
	}
	perfApprox(t, "net_pnl_usd", got.NetPnLUSD, want.NetPnLUSD)
	if !got.EquityKnown {
		t.Fatalf("equity unknown: %s", got.EquityReason)
	}
	perfApprox(t, "equity", got.Equity, 2)
	perfApprox(t, "return_pct", got.ReturnPct, want.ReturnPct)
	perfApprox(t, "hold_return_usd_pct", got.HoldReturnUSDPct, want.HoldReturnUSDPct)
	perfApprox(t, "return_usd_pct", got.ReturnUSDPct, want.ReturnUSDPct)
	perfApprox(t, "excess_return_pct", got.ExcessReturnPct, want.ExcessReturnPct)
	if got.MaxDrawdown.Coin != want.MaxDrawdown || want.MaxDrawdown >= 0 {
		t.Errorf("max drawdown %v, want %v (< 0)", got.MaxDrawdown.Coin, want.MaxDrawdown)
	}
	perfApprox(t, "max_drawdown.pct", got.MaxDrawdown.Pct, want.MaxDrawdownPct)
	if !got.WorstWeek.Known || got.WorstWeek.Coin != want.WorstWeek || got.WorstWeek.Coin >= 0 {
		t.Errorf("worst week %+v, want %v (< 0)", got.WorstWeek, want.WorstWeek)
	}
	perfApprox(t, "worst_week.pct", got.WorstWeek.Pct, want.WorstWeekPct)
	if len(got.MissingDays) != 0 || got.Days != want.Days || got.DaysCovered != want.DaysCovered {
		t.Errorf("coverage %d/%d missing %v, want %d/%d none", got.DaysCovered, got.Days, got.MissingDays, want.DaysCovered, want.Days)
	}
	if got.Label != "testnet" {
		t.Errorf("label %q", got.Label)
	}
}

// Scenario: equity unknown — every percentage of equity is null.
func TestPerformance_EquityUnknown(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pts := perfPoints(start, ramp(10, 0, -0.001), ramp(10, 60000, 100), 0, "")
	got := api.BuildPerformance(pts, start, start.AddDate(0, 0, 9), "30d", "testnet")

	if got.EquityKnown || got.Equity != nil || got.EquityReason == "" {
		t.Fatalf("equity known %v %v reason %q, want unknown with a reason", got.EquityKnown, got.Equity, got.EquityReason)
	}
	for name, v := range map[string]*float64{
		"return_pct": got.ReturnPct, "return_usd_pct": got.ReturnUSDPct, "excess_return_pct": got.ExcessReturnPct,
		"max_drawdown.pct": got.MaxDrawdown.Pct, "worst_week.pct": got.WorstWeek.Pct,
	} {
		if v != nil {
			t.Errorf("%s = %v, want null", name, *v)
		}
	}
	// What does not need equity is still there.
	if !got.HoldKnown || got.HoldReturnUSDPct == nil || got.NetPnL >= 0 || got.MaxDrawdown.Coin >= 0 {
		t.Errorf("hold %v %v net %v dd %v", got.HoldKnown, got.HoldReturnUSDPct, got.NetPnL, got.MaxDrawdown.Coin)
	}
}

// Scenario: days without data — listed, nothing filled in.
func TestPerformance_MissingDaysListed(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pts := perfPoints(start, ramp(5, 0, 0.001), ramp(5, 60000, 0), 2, "BTC")
	pts = append(pts[:2], pts[3:]...) // drop 2026-09-03
	got := api.BuildPerformance(pts, start, start.AddDate(0, 0, 4), "7d", "testnet")

	if len(got.MissingDays) != 1 || got.MissingDays[0] != "2026-09-03" {
		t.Fatalf("missing days %v, want [2026-09-03]", got.MissingDays)
	}
	if got.Points != 4 || got.Days != 5 || got.DaysCovered != 4 {
		t.Errorf("points %d days %d covered %d, want 4, 5, 4", got.Points, got.Days, got.DaysCovered)
	}
}

func TestPerformance_EquityFromUSDBalanceAtThatSpot(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pts := perfPoints(start, ramp(3, 0, 0.001), []float64{50000, 51000, 52000}, 100000, "USD")
	pts[0].MarginBalance, pts[0].Unit = nil, "" // the first line has none: the next one is used
	eq, reason, ok := api.EquityAtStart(pts)
	if !ok || math.Abs(eq-100000.0/51000) > 1e-12 || reason == "" {
		t.Fatalf("equity %v ok %v reason %q, want 100000/51000", eq, ok, reason)
	}
	// A USD balance on a line without a spot cannot be converted.
	pts[1].Spot, pts[2].MarginBalance = 0, nil
	if _, _, ok := api.EquityAtStart(pts); ok {
		t.Error("equity known without a spot to convert at")
	}
}

func TestPerformance_LabelIsLiveOnlyForLive(t *testing.T) {
	for env, want := range map[string]string{"live": "live", "testnet": "testnet", "testnet (demo)": "testnet", "": "testnet"} {
		if got := api.PerformanceLabel(env); got != want {
			t.Errorf("%q → %q, want %q", env, got, want)
		}
	}
}

func TestPerformance_EmptyWindow(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	got := api.BuildPerformance(nil, now.AddDate(0, 0, -7), now, "7d", "testnet")
	if got.NetPnLKnown || got.NetPnLUSD != nil || got.EquityKnown || got.WorstWeek.Known || got.MissingDays == nil || len(got.MissingDays) != got.Days {
		t.Errorf("empty window: %+v", got)
	}
}

// The route end to end on a real history store.
func TestAPI_PerformanceRoute(t *testing.T) {
	store, err := history.Open(filepath.Join(t.TempDir(), "pnl_history.jsonl"), 366*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		store.RecordPnL(now.AddDate(0, 0, i-19).Add(-time.Second), 0.001*float64(i), 0, 60000+float64(i)*100,
			&history.Margin{Known: true, IMLimitPct: 20, MarginBalance: 2, Unit: "BTC"})
	}
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), api.WithPnLHistory(store)).Handler()

	code, body := getJSON(t, h, "/api/performance?range=30d")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if body["range"] != "30d" || body["label"] != "testnet" || body["equity_known"] != true || body["points"] != float64(20) {
		t.Errorf("body %v", body)
	}
	if math.Abs(body["net_pnl"].(float64)-0.019) > 1e-12 {
		t.Errorf("net_pnl %v, want 0.019", body["net_pnl"])
	}
	for _, k := range []string{"net_pnl_usd", "hold_return_usd_pct", "excess_return_pct", "max_drawdown", "worst_week", "missing_days"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	// 30 days back, 20 days of points: the first days of the window are listed.
	if md := body["missing_days"].([]any); len(md) < 9 {
		t.Errorf("missing days %v, want the ~10 days before the first point", md)
	}

	// "all" starts at the first point: no missing days before it.
	_, body = getJSON(t, h, "/api/performance?range=all")
	if md := body["missing_days"].([]any); len(md) != 0 {
		t.Errorf("all: missing days %v, want none", md)
	}
	_, body = getJSON(t, h, "/api/performance")
	if body["range"] != "30d" {
		t.Errorf("default range %v, want 30d", body["range"])
	}
}

// Scenario: bad range — 400.
func TestAPI_PerformanceBadRange(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	for _, r := range []string{"1d", "1w", "90d", "x"} {
		if code, _ := getJSON(t, h, "/api/performance?range="+r); code != http.StatusBadRequest {
			t.Errorf("range %q: status %d, want 400", r, code)
		}
	}
	// Without a history the route answers with an empty summary.
	if code, body := getJSON(t, h, "/api/performance?range=7d"); code != http.StatusOK || body["points"] != float64(0) || body["equity_known"] != false {
		t.Errorf("no history: %d %v", code, body)
	}
}
