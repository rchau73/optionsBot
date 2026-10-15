package tests

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// ── Manual rebalance (POST /api/rebalance) ───────────────────────────────────
//
// A deposit moves the margin balance, not the confirmed IM limit, so the
// automatic rebalance never runs for it. The manual one runs the same pass
// with the same gates.

func TestRebalanceBlockReason(t *testing.T) {
	ok := risk.Status{LimitIMPct: 20, MaxMMPct: 35, CanRebalance: true}
	use := risk.Usage{IM: 1, MM: 1, MarginBalance: 10}
	cases := []struct {
		name    string
		st      risk.Status
		u       risk.Usage
		err     error
		gexWait string
		pending bool
		want    string
	}{
		{"all clear", ok, use, nil, "", false, ""},
		{"margin unknown", ok, use, errors.New("timeout"), "", false, "margin data unavailable"},
		{"MM breach", ok, risk.Usage{MM: 4, MarginBalance: 10}, nil, "", false, "maintenance margin"},
		{"frozen", risk.Status{LimitIMPct: 20, MaxMMPct: 35, CanRebalance: true, Frozen: true, FreezeReason: "DVOL moved"}, use, nil, "", false, "new risk frozen: DVOL moved"},
		{"unconfirmed limit", risk.Status{LimitIMPct: 20, MaxMMPct: 35}, use, nil, "", false, "confirmed data"},
		{"stale GEX", ok, use, nil, "no GEX snapshot yet", false, "waiting for GEX"},
		{"complement working", ok, use, nil, "", true, "complement is still working"},
	}
	for _, c := range cases {
		got := strategy.RebalanceBlockReason(c.st, c.u, c.err, c.gexWait, c.pending)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestManualRebalanceWait(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if w := strategy.ManualRebalanceWait(time.Time{}, now, false); w != 0 {
		t.Errorf("never ran: no wait, got %v", w)
	}
	if w := strategy.ManualRebalanceWait(now.Add(-time.Minute), now, false); w != 4*time.Minute {
		t.Errorf("real pass 1 min ago: wait 4 min, got %v", w)
	}
	if w := strategy.ManualRebalanceWait(now.Add(-time.Minute), now, true); w != 0 {
		t.Errorf("dry run 1 min ago: 30 s cooldown has passed, got %v", w)
	}
	if w := strategy.ManualRebalanceWait(now.Add(-10*time.Second), now, true); w != 20*time.Second {
		t.Errorf("dry run 10 s ago: wait 20 s, got %v", w)
	}
}

// afterDeposit starts the bot with a one-lot strangle that fills its slot,
// waits for the startup rebalance, then doubles the margin balance: the slot
// target becomes two lots, but the confirmed limit (20 %) did not move.
func (f *strategyFixture) afterDeposit(t *testing.T) {
	t.Helper()
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 3*time.Second, "startup rebalance ran",
		func() bool { return len(f.journal.riskChanges(orders.RiskRebalance)) > 0 })
	f.exch.mu.Lock()
	f.exch.summary.MarginBalance *= 2
	f.exch.summary.Equity *= 2
	f.exch.mu.Unlock()
}

func topUps(rep strategy.RebalanceReport) []strategy.RebalanceAction {
	var out []strategy.RebalanceAction
	for _, a := range rep.Actions {
		if a.Action == strategy.ActionTopUp {
			out = append(out, a)
		}
	}
	return out
}

func TestManualRebalance_DryRunReportsTheTopUpAndSendsNothing(t *testing.T) {
	f := newStrategyFixture(t)
	f.afterDeposit(t)
	sells, rebalances := len(f.exch.sells()), len(f.journal.riskChanges(orders.RiskRebalance))

	rep, err := f.strat.RequestRebalance(context.Background(), true, "test")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	ups := topUps(rep)
	if !rep.DryRun || len(ups) != 1 || math.Abs(ups[0].Qty-0.1) > 1e-9 || math.Abs(ups[0].TargetQty-0.2) > 1e-9 || ups[0].Call == "" || ups[0].Put == "" {
		t.Fatalf("dry run must report one 0.1 top-up toward 0.2 with its strikes: %+v", rep)
	}
	time.Sleep(100 * time.Millisecond) // a few cycles
	if n := len(f.exch.sells()); n != sells {
		t.Errorf("a dry run sends nothing: sells %d → %d", sells, n)
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalance)); n != rebalances {
		t.Errorf("a dry run journals nothing: rebalance records %d → %d", rebalances, n)
	}
	if n := len(f.journal.riskChanges(orders.RiskRebalanceManual)); n != 0 {
		t.Errorf("a dry run is not a manual rebalance: %d records", n)
	}
	if _, err := f.strat.RequestRebalance(context.Background(), true, "test"); !errors.As(err, new(strategy.RebalanceCooldown)) {
		t.Errorf("a second dry run within 30 s must hit the cooldown, got %v", err)
	}
}

func TestManualRebalance_RealRunTopsUpJournalsAndCoolsDown(t *testing.T) {
	f := newStrategyFixture(t)
	f.afterDeposit(t)
	sells := len(f.exch.sells())

	rep, err := f.strat.RequestRebalance(context.Background(), false, "test")
	if err != nil {
		t.Fatalf("manual rebalance: %v", err)
	}
	if rep.DryRun || len(topUps(rep)) != 1 {
		t.Fatalf("one top-up expected: %+v", rep)
	}
	eventually(t, 2*time.Second, "complement sold on both legs",
		func() bool { return len(f.exch.sells()) >= sells+2 })
	if n := len(f.journal.riskChanges(orders.RiskRebalanceManual)); n != 1 {
		t.Errorf("the manual pass is journaled once, got %d", n)
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("a top-up never buys back, got %d buys", n)
	}
	_, err = f.strat.RequestRebalance(context.Background(), false, "test")
	var cool strategy.RebalanceCooldown
	if !errors.As(err, &cool) || cool.RetryAfter <= 0 {
		t.Errorf("a second real pass within 5 min must hit the cooldown, got %v", err)
	}
}

func TestManualRebalance_RefusedWithoutMarginData(t *testing.T) {
	f := newStrategyFixture(t)
	f.afterDeposit(t)
	sells := len(f.exch.sells())
	f.exch.mu.Lock()
	f.exch.summaryErr = errors.New("request timed out")
	f.exch.mu.Unlock()

	_, err := f.strat.RequestRebalance(context.Background(), false, "test")
	var refused strategy.RebalanceRefused
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "margin data unavailable") {
		t.Fatalf("must be refused with the gate's reason, got %v", err)
	}
	if n := len(f.exch.sells()); n != sells {
		t.Errorf("a refused rebalance sends nothing: sells %d → %d", sells, n)
	}
	// A refusal does not start the cooldown: once margin data is back it runs.
	f.exch.mu.Lock()
	f.exch.summaryErr = nil
	f.exch.mu.Unlock()
	if _, err := f.strat.RequestRebalance(context.Background(), false, "test"); err != nil {
		t.Errorf("after a refusal the next request runs, got %v", err)
	}
}

type fakeRebalancer struct {
	dryRun bool
	calls  int
	err    error
}

func (r *fakeRebalancer) RequestRebalance(_ context.Context, dryRun bool, _ string) (strategy.RebalanceReport, error) {
	r.calls++
	r.dryRun = dryRun
	if r.err != nil {
		return strategy.RebalanceReport{}, r.err
	}
	return strategy.RebalanceReport{DryRun: dryRun, Done: true, LimitIMPct: 20, Unit: "ETH",
		Actions: []strategy.RebalanceAction{{Action: strategy.ActionTopUp, Qty: 1}}}, nil
}

func TestAPI_ManualRebalance(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	post := func(h http.Handler, auth, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/rebalance"+query, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	off := api.New(staticView{sampleView()}, logger, api.WithManualRebalance(&fakeRebalancer{}, "")).Handler()
	if rec := post(off, "Bearer ", ""); rec.Code == http.StatusOK {
		t.Errorf("no token configured: the route must not exist, got %d", rec.Code)
	}

	r := &fakeRebalancer{}
	h := api.New(staticView{sampleView()}, logger, api.WithManualRebalance(r, "s3cret")).Handler()
	if rec := post(h, "Bearer wrong", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d", rec.Code)
	}
	if rec := post(h, "Bearer s3cret", "?dry_run=maybe"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad dry_run: %d", rec.Code)
	}
	if r.calls != 0 {
		t.Fatalf("nothing may reach the strategy before auth and validation, got %d calls", r.calls)
	}
	if rec := post(h, "Bearer s3cret", "?dry_run=true"); rec.Code != http.StatusOK || !r.dryRun || !strings.Contains(rec.Body.String(), `"dry_run":true`) {
		t.Errorf("dry run: %d %s", rec.Code, rec.Body)
	}
	if rec := post(h, "Bearer s3cret", ""); rec.Code != http.StatusOK || r.dryRun || !strings.Contains(rec.Body.String(), `"action":"top_up"`) {
		t.Errorf("real run: %d %s", rec.Code, rec.Body)
	}

	for _, c := range []struct {
		err  error
		code int
		body string
	}{
		{strategy.RebalanceRefused{Reason: "new risk frozen: DVOL moved"}, http.StatusConflict, "frozen"},
		{strategy.RebalanceCooldown{RetryAfter: 90 * time.Second}, http.StatusTooManyRequests, "try again"},
		{strategy.ErrRebalanceBusy, http.StatusGatewayTimeout, "did not take"},
	} {
		r.err = c.err
		rec := post(h, "Bearer s3cret", "")
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body)
		}
		if c.code == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "90" {
			t.Errorf("429 must carry Retry-After 90, got %q", rec.Header().Get("Retry-After"))
		}
	}
}
