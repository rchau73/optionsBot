package tests

import (
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// incomeNow is the fixed clock of the income tests: the window is May–Oct 2026.
var incomeNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// incomeClose is a close of premium, fees and P&L (coin) valued at spot.
func incomeClose(at time.Time, premium, fees, pnl, spot float64) orders.Trade {
	return orders.Trade{At: at, Kind: "close", Premium: premium, Fees: fees, PnL: pnl, PnLUSD: pnl * spot, Spot: spot}
}

func incNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Scenario: the columns add up — paid to you − paid to buy back − fees equals
// the result, every month, in coin and in USD.
func TestIncome_TheColumnsAddUp(t *testing.T) {
	trades := []orders.Trade{
		incomeClose(time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), 0.02, 0.001, 0.015, 60000),   // bought back for 0.004
		incomeClose(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), 0.01, 0.0005, -0.02, 65000), // stop: bought back for 0.0295
		incomeClose(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), 0.03, 0.002, 0.028, 0),       // expired worthless, no spot
		{At: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Kind: "close", Premium: 0.01, Fees: 0.001, PnL: 0, Spot: 70000},
		{At: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), Kind: "open", Premium: 0.05},
	}
	inc := api.IncomeOf(trades, incomeNow, 6)
	for _, m := range inc.Months {
		if got := m.PremiumCoin - m.BuybackCoin - m.FeesCoin; !incNear(got, m.RealisedCoin) {
			t.Errorf("%s coin: %v − %v − %v = %v, result %v", m.Month, m.PremiumCoin, m.BuybackCoin, m.FeesCoin, got, m.RealisedCoin)
		}
		if got := m.PremiumUSD - m.BuybackUSD - m.FeesUSD; math.Abs(got-m.RealisedUSD) > 1e-6 {
			t.Errorf("%s USD: %v, result %v", m.Month, got, m.RealisedUSD)
		}
	}
	aug := inc.Months[3]
	if aug.Month != "2026-08" || aug.Closes != 2 || !incNear(aug.PremiumCoin, 0.03) || !incNear(aug.BuybackCoin, 0.0335) || !incNear(aug.FeesCoin, 0.0015) {
		t.Errorf("August %+v", aug)
	}
	// Each close at its own spot.
	if !incNear(aug.PremiumUSD, 0.02*60000+0.01*65000) {
		t.Errorf("August premium USD %v", aug.PremiumUSD)
	}
	sep := inc.Months[4]
	if sep.Closes != 2 || !incNear(sep.BuybackCoin, 0+0.009) || !incNear(sep.PremiumUSD, 0.01*70000) {
		t.Errorf("September %+v", sep)
	}
}

// Scenario: a losing month is as visible as a winning one — the worst month
// names it, and it counts as a month down.
func TestIncome_ALosingMonthIsTheWorstMonth(t *testing.T) {
	var trades []orders.Trade
	for i, pnl := range []float64{0.01, 0.02, -0.03, 0.005, 0.01, 0.004} {
		at := time.Date(2026, time.Month(5+i), 10, 0, 0, 0, 0, time.UTC)
		trades = append(trades, incomeClose(at, 0.04, 0.001, pnl, 60000))
	}
	// July: the coin fell from 60,000 to 51,600 (−14 %); equity 2 BTC.
	var points []history.Point
	for d := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC); d.Before(incomeNow); d = d.Add(24 * time.Hour) {
		spot := 60000.0
		if d.Month() == time.July {
			spot = 60000 - float64(d.Day()-1)*280
		}
		b := 2.0
		points = append(points, history.Point{T: d, Spot: spot, MarginBalance: &b, Unit: "BTC"})
	}
	inc := api.IncomeWithHistory(api.IncomeOf(trades, incomeNow, 6), points, incomeNow, "testnet")
	s := inc.Summary
	if s == nil {
		t.Fatal("no summary")
	}
	if s.WorstMonth == nil || s.WorstMonth.Month != "2026-07" || !incNear(s.WorstMonth.Coin, -0.03) || !incNear(s.WorstMonth.USD, -0.03*60000) {
		t.Fatalf("worst month %+v", s.WorstMonth)
	}
	if s.WorstMonth.SpotMovePct == nil || math.Abs(*s.WorstMonth.SpotMovePct-(51600.0/60000-1)*100) > 1e-9 {
		t.Errorf("worst month move %v", s.WorstMonth.SpotMovePct)
	}
	if s.UpMonths != 5 || s.DownMonths != 1 || s.ActiveMonths != 6 {
		t.Errorf("months up %d down %d active %d", s.UpMonths, s.DownMonths, s.ActiveMonths)
	}
	if !incNear(s.Total.Coin, 0.019) || s.AverageMonth == nil || !incNear(s.AverageMonth.Coin, 0.019/6) {
		t.Errorf("total %v average %+v", s.Total, s.AverageMonth)
	}
	perfApprox(t, "return_pct", s.ReturnPct, 0.019/2*100)
	if s.Label != "testnet" || len(s.MissingDays) != 0 {
		t.Errorf("label %q missing %v", s.Label, s.MissingDays)
	}
}

// Scenario: USD toggle — holding alone in USD is the coin's change over the
// month for the equity at the month's start; a month with no history says why.
func TestIncome_HoldingAloneInUSD(t *testing.T) {
	bal := func(v float64) *float64 { return &v }
	points := []history.Point{
		{T: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Spot: 60000, MarginBalance: bal(120000), Unit: "USD"}, // 2 BTC
		{T: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), Spot: 0},                                             // no spot: skipped
		{T: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Spot: 66000},
		{T: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Spot: 66000}, // no balance
		{T: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Spot: 63000},
	}
	inc := api.IncomeWithHistory(api.IncomeOf(nil, incomeNow, 6), points, incomeNow, "live")
	sep, oct, aug := inc.Months[4], inc.Months[5], inc.Months[3]
	perfApprox(t, "September hold_usd", sep.HoldUSD, 2*6000)
	perfApprox(t, "September spot_days", sep.SpotDays, 29)
	if oct.HoldUSD != nil || oct.HoldReason == "" || oct.SpotEnd == nil || *oct.SpotEnd != 63000 {
		t.Errorf("October (no balance) %+v", oct)
	}
	if aug.HoldUSD != nil || aug.SpotStart != nil || !strings.Contains(aug.HoldReason, "no spot") {
		t.Errorf("August (no history) %+v", aug)
	}
	if inc.ThisMonth.SpotEnd == nil {
		t.Errorf("this_month not completed: %+v", inc.ThisMonth)
	}
	s := inc.Summary
	if s.Label != "live" || s.AverageMonth != nil || s.WorstMonth != nil || s.UpMonths != 0 {
		t.Errorf("no closes: %+v", s)
	}
	// Missing days are listed, never filled: May to August have no point.
	if len(s.MissingDays) < 120 || s.MissingDays[0] != "2026-05-01" {
		t.Errorf("missing days %d, first %v", len(s.MissingDays), s.MissingDays[:1])
	}
}

// Without the account's balance every % of equity is null, never 0.
func TestIncome_UnknownEquityLeavesPercentagesNull(t *testing.T) {
	var points []history.Point
	for i := 0; i < 20; i++ {
		points = append(points, history.Point{T: incomeNow.AddDate(0, 0, i-19), Total: -0.001 * float64(i), Spot: 60000})
	}
	trades := []orders.Trade{incomeClose(incomeNow.Add(-time.Hour), 0.01, 0.001, 0.005, 60000)}
	s := api.IncomeWithHistory(api.IncomeOf(trades, incomeNow, 6), points, incomeNow, "testnet").Summary
	if s.Equity != nil || s.ReturnPct != nil || s.EquityNote == "" {
		t.Errorf("equity %v return %v note %q", s.Equity, s.ReturnPct, s.EquityNote)
	}
	if !s.WorstWeek.Known || s.WorstWeek.Pct != nil || !incNear(s.WorstWeek.Coin, -0.007) {
		t.Errorf("worst week %+v", s.WorstWeek)
	}
}

// The route end to end on a real history store; /api/summary keeps its
// income table (with the new columns) and no summary.
func TestAPI_IncomeRoute(t *testing.T) {
	store, err := history.Open(filepath.Join(t.TempDir(), "pnl_history.jsonl"), 366*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		store.RecordPnL(now.Add(time.Duration(i-10)*time.Hour), 0, 0, 60000+float64(i)*100,
			&history.Margin{Known: true, IMLimitPct: 20, MarginBalance: 2, Unit: "BTC"})
	}
	closed := incomeClose(now, 0.02, 0.001, 0.015, 60000)
	closed.Seq = 1
	journal := summaryLogger(closed)
	h := api.New(staticView{summaryView(now)}, journal, api.WithPnLHistory(store)).Handler()

	code, body := getJSON(t, h, "/api/income")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	months := body["months"].([]any)
	last := months[len(months)-1].(map[string]any)
	for _, k := range []string{"premium_coin", "buyback_coin", "fees_coin", "premium_usd", "buyback_usd", "fees_usd", "hold_usd", "spot_start", "spot_end", "realised_coin", "closes"} {
		if _, ok := last[k]; !ok {
			t.Errorf("months[].%s missing", k)
		}
	}
	if !incNear(last["buyback_coin"].(float64), 0.004) {
		t.Errorf("buyback %v", last["buyback_coin"])
	}
	s, ok := body["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary %v", body["summary"])
	}
	for _, k := range []string{"total_6m", "average_month", "up_months", "down_months", "worst_month", "worst_week", "missing_days", "return_pct", "label"} {
		if _, ok := s[k]; !ok {
			t.Errorf("summary.%s missing", k)
		}
	}
	if s["up_months"] != float64(1) || s["label"] != "testnet" || s["return_pct"] == nil {
		t.Errorf("summary %v", s)
	}

	_, sum := getJSON(t, h, "/api/summary")
	income := sum["income"].(map[string]any)
	if income["summary"] != nil {
		t.Errorf("/api/summary carries the income summary: %v", income["summary"])
	}
	if m := income["months"].([]any)[5].(map[string]any); !incNear(m["premium_coin"].(float64), 0.02) || m["hold_usd"] != nil {
		t.Errorf("/api/summary month %v", m)
	}
}

// No history wired: the page still has its table and an unknown worst week.
func TestAPI_IncomeRouteWithoutHistory(t *testing.T) {
	h := api.New(staticView{summaryView(time.Now())}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	code, body := getJSON(t, h, "/api/income")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	s := body["summary"].(map[string]any)
	if s["worst_week"].(map[string]any)["known"] != false || s["worst_month"] != nil || s["average_month"] != nil {
		t.Errorf("summary %v", s)
	}
}
