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

// The income page's "% of start" view (#629, internal/api/income_start.go).

// startPoints is an hourly P&L history from first to last at spot(t) (and
// total(t) when given), with a margin balance of bal BTC from balanceFrom on.
func startPoints(first, last, balanceFrom time.Time, bal float64, spot, total func(time.Time) float64) []history.Point {
	var points []history.Point
	for t := first; !t.After(last); t = t.Add(time.Hour) {
		p := history.Point{T: t, Spot: spot(t)}
		if total != nil {
			p.Total = total(t)
		}
		if !t.Before(balanceFrom) {
			b := bal
			p.MarginBalance, p.Unit = &b, "BTC"
		}
		points = append(points, p)
	}
	return points
}

func incomeMonths(inc api.IncomeSummary) []string {
	var out []string
	for _, m := range inc.Months {
		out = append(out, m.Month)
	}
	return out
}

func flatSpot(time.Time) float64 { return 60000 }

// Scenario: % of start view — the start balance with its time, and the bot
// result, holding alone and bot + holding as signed % of it, all from one start.
func TestIncome_PercentOfStartView(t *testing.T) {
	balanceFrom := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	// Spot 70,000 until the balance (never used), then 60,000 → 66,000 (+10 %) by now.
	spot := func(t time.Time) float64 {
		if t.Before(balanceFrom) {
			return 70000
		}
		return 60000 + 6000*t.Sub(balanceFrom).Hours()/now.Sub(balanceFrom).Hours()
	}
	points := startPoints(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), now, balanceFrom, 0.5, spot, nil)
	trades := []orders.Trade{
		incomeClose(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), 0.02, 0.001, 0.3, 70000), // before the start: not counted
		incomeClose(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), 0.02, 0.001, 0.01, 62000),
		incomeClose(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), 0.02, 0.001, -0.005, 64000),
	}
	st := api.IncomeStartOf(incomeMonths(api.IncomeOf(trades, now, 6)), trades, points, time.Time{}, now)
	if !st.Known || st.At == nil || !st.At.Equal(balanceFrom) || st.Requested != nil || !strings.Contains(st.Source, "margin balance in BTC") {
		t.Fatalf("start %+v", st)
	}
	perfApprox(t, "balance_coin", st.BalanceCoin, 0.5)
	perfApprox(t, "balance_usd", st.BalanceUSD, 0.5*60000)
	if st.Closes != 2 || !incNear(st.ResultCoin, 0.005) {
		t.Errorf("result %v over %d closes", st.ResultCoin, st.Closes)
	}
	perfApprox(t, "result_pct", st.ResultPct, 1)
	perfApprox(t, "hold_pct", st.HoldPct, 10)
	perfApprox(t, "combined_pct", st.CombinedPct, (0.505*66000/(0.5*60000)-1)*100)

	// The months: October from 28 Oct (partial), November from its first line.
	oct, nov := st.Months[4], st.Months[5]
	if oct.Month != "2026-10" || !oct.Partial || oct.From == nil || !oct.From.Equal(balanceFrom) || oct.Closes != 1 {
		t.Errorf("October %+v", oct)
	}
	perfApprox(t, "October result_pct", oct.ResultPct, 2)
	if nov.Partial || nov.Closes != 1 || nov.From == nil || !nov.From.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("November %+v", nov)
	}
	perfApprox(t, "November result_pct", nov.ResultPct, -1)
}

// Scenario: month that began before the first balance — measured from that
// balance and marked partial; the coin view's worst week that began before
// the balance gets no % of it.
func TestIncome_MonthThatBeganBeforeTheFirstBalance(t *testing.T) {
	balanceFrom := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	week := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	// The total falls 0.01 BTC/day 8–15 Oct (the worst week), before the balance.
	total := func(t time.Time) float64 {
		return -0.01 * math.Min(math.Max(0, t.Sub(week).Hours()/24), 7)
	}
	points := startPoints(week, now, balanceFrom, 1.17, flatSpot, total)
	inc := api.IncomeWithHistory(api.IncomeOf(nil, now, 6), points, now, "testnet")
	st := api.IncomeStartOf(incomeMonths(inc), nil, points, time.Time{}, now)
	oct := st.Months[5]
	if !oct.Partial || oct.From == nil || !oct.From.Equal(balanceFrom) || oct.ResultPct == nil || *oct.ResultPct != 0 {
		t.Errorf("October %+v", oct)
	}
	// Under 7 days since the balance: the % view's worst week is not yet known.
	if st.WorstWeek.Known || st.WorstWeek.KnownAt == nil || !st.WorstWeek.KnownAt.Equal(balanceFrom.Add(7*24*time.Hour)) {
		t.Errorf("start worst week %+v", st.WorstWeek)
	}
	ww := inc.Summary.WorstWeek
	if !ww.Known || ww.Pct != nil || !strings.Contains(ww.PctNote, "28 Oct 2026") {
		t.Errorf("coin worst week %+v", ww)
	}
}

// Scenario: no balance — a month with no balance says why and shows no %.
func TestIncome_NoBalance(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	// September has spot but no balance; October has the balance.
	points := startPoints(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), now, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 2, flatSpot, nil)
	months := incomeMonths(api.IncomeOf(nil, now, 6))
	st := api.IncomeStartOf(months, nil, points, time.Time{}, now)
	for _, m := range st.Months[:5] { // May–September
		if !strings.Contains(m.Reason, "no margin balance") || m.ResultPct != nil || m.HoldPct != nil || m.CombinedPct != nil || m.BalanceCoin != nil {
			t.Errorf("%s %+v", m.Month, m)
		}
	}
	if oct := st.Months[5]; oct.Reason != "" || oct.Partial || oct.ResultPct == nil {
		t.Errorf("October %+v", oct)
	}
	// No balance anywhere: the view is unknown and every % null.
	none := api.IncomeStartOf(months, nil, nil, time.Time{}, now)
	if none.Known || none.Reason == "" || none.ResultPct != nil || none.HoldPct != nil || none.At != nil || none.WorstWeek.KnownAt != nil {
		t.Errorf("no history %+v", none)
	}
}

// Scenario: same start on both environments — ?from= measures from the first
// balance at or after that time, and months before it say so.
func TestIncome_SameStartOnBothEnvironments(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	from, err := api.ParseIncomeFrom("2026-10-28T11:45Z")
	if err != nil {
		t.Fatal(err)
	}
	months := incomeMonths(api.IncomeOf(nil, now, 6))
	for _, env := range []struct {
		name        string
		balanceFrom time.Time
		bal         float64
	}{
		{"stable", time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), 1.17},
		{"mvp", time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), 0.5},
	} {
		points := startPoints(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), now, env.balanceFrom, env.bal, flatSpot, nil)
		st := api.IncomeStartOf(months, nil, points, from, now)
		// The first hourly line at or after 11:45 is 12:00 (months: June–November).
		if !st.Known || st.Requested == nil || !st.Requested.Equal(from) || !st.At.Equal(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)) {
			t.Errorf("%s start %+v", env.name, st)
		}
		if sep := st.Months[3]; !strings.Contains(sep.Reason, "before the start") || sep.ResultPct != nil {
			t.Errorf("%s September %+v", env.name, sep)
		}
		if oct := st.Months[4]; !oct.Partial || oct.From == nil || !oct.From.Equal(*st.At) {
			t.Errorf("%s October %+v", env.name, oct)
		}
	}
	for in, want := range map[string]time.Time{
		"":                       {},
		"2026-10-28":             time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
		"2026-10-28T11:45:30Z":   time.Date(2026, 10, 28, 11, 45, 30, 0, time.UTC),
		"2026-10-28T13:45+02:00": time.Date(2026, 10, 28, 11, 45, 0, 0, time.UTC),
	} {
		if got, err := api.ParseIncomeFrom(in); err != nil || !got.Equal(want) {
			t.Errorf("from=%q: %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"yesterday", "2026-13-01", "28/10/2026"} {
		if _, err := api.ParseIncomeFrom(bad); err == nil {
			t.Errorf("from=%q accepted", bad)
		}
	}
}

// The % view's worst week: the worst 7 days since the start balance, as a %
// of it, with open positions at their marks (the history's total).
func TestIncome_StartWorstWeek(t *testing.T) {
	balanceFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	fall := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	// The total loses 0.01 BTC a day 5–12 Oct, flat otherwise.
	total := func(t time.Time) float64 {
		return -0.01 * math.Min(math.Max(0, t.Sub(fall).Hours()/24), 7)
	}
	points := startPoints(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), now, balanceFrom, 2, flatSpot, total)
	st := api.IncomeStartOf(incomeMonths(api.IncomeOf(nil, now, 6)), nil, points, time.Time{}, now)
	ww := st.WorstWeek
	if !ww.Known || !incNear(ww.Coin, -0.07) || ww.KnownAt != nil || ww.Start == nil || ww.Start.Before(balanceFrom) {
		t.Fatalf("worst week %+v", ww)
	}
	perfApprox(t, "worst week pct", ww.Pct, -3.5)
}

// The route serves start, takes ?from= and refuses a bad one; /api/summary
// carries no start.
func TestAPI_IncomeRouteFrom(t *testing.T) {
	store, err := history.Open(filepath.Join(t.TempDir(), "pnl_history.jsonl"), 366*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		store.RecordPnL(now.Add(time.Duration(i-10)*time.Hour), 0, 0, 60000, &history.Margin{Known: true, IMLimitPct: 20, MarginBalance: 2, Unit: "BTC"})
	}
	h := api.New(staticView{summaryView(now)}, orders.NewWriterLogger(io.Discard, 0), api.WithPnLHistory(store)).Handler()
	code, body := getJSON(t, h, "/api/income?from="+now.Add(-5*time.Hour).Format(time.RFC3339))
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	st, ok := body["start"].(map[string]any)
	if !ok || st["known"] != true || st["requested"] == nil || st["balance_coin"] != 2.0 {
		t.Fatalf("start %v", body["start"])
	}
	if code, _ := getJSON(t, h, "/api/income?from=nope"); code != http.StatusBadRequest {
		t.Errorf("bad from: status %d", code)
	}
	_, sum := getJSON(t, h, "/api/summary")
	if inc := sum["income"].(map[string]any); inc["start"] != nil {
		t.Errorf("/api/summary carries start: %v", inc["start"])
	}
}
