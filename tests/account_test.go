package tests

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/account"
	"optionsbot/internal/api"
	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// scriptedRPC answers by method; a missing method is an error.
type scriptedRPC struct {
	mu      sync.Mutex
	replies map[string]string
	calls   []string
}

func (s *scriptedRPC) Call(_ context.Context, method string, params any, _ int) (gateway.JSONRPCResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method
	if p, ok := params.(map[string]any); ok && p["currency"] != nil {
		key = method + ":" + p["currency"].(string)
	}
	s.calls = append(s.calls, key)
	r, ok := s.replies[key]
	if !ok {
		return gateway.JSONRPCResponse{}, &gateway.RPCError{Code: 11050, Message: "bad_request"}
	}
	return gateway.JSONRPCResponse{Result: json.RawMessage(r)}, nil
}

const summariesJSON = `{"summaries":[
 {"currency":"BTC","balance":1.2,"equity":1.25,"margin_balance":1.25,"available_funds":0.95,"available_withdrawal_funds":0.9,
  "initial_margin":0.30,"maintenance_margin":0.20,"projected_initial_margin":0.25,"projected_maintenance_margin":0.18,
  "spot_reserve":0.01,"margin_model":"cross_pm","portfolio_margining_enabled":true,"cross_collateral_enabled":true,
  "total_equity_usd":150000,"total_margin_balance_usd":150000,"total_initial_margin_usd":30000,"total_maintenance_margin_usd":21000},
 {"currency":"USDC","balance":25000,"equity":25000,"margin_balance":25000,"available_funds":25000,"available_withdrawal_funds":25000,
  "margin_model":"cross_pm","cross_collateral_enabled":true},
 {"currency":"ETH","balance":0,"equity":0,"margin_balance":0,"margin_model":"cross_pm","cross_collateral_enabled":true}
]}`

func TestAccount_PollsAllCurrenciesInOneCall(t *testing.T) {
	rpc := &scriptedRPC{replies: map[string]string{"private/get_account_summaries": summariesJSON}}
	p := account.NewPoller(rpc)
	p.Refresh(context.Background())

	st := p.Status()
	s := st.Snapshot
	if s == nil || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	if s.MarginModel != "cross_pm" || !s.PortfolioMargining || !s.CrossCollateral || s.Source != "get_account_summaries" {
		t.Errorf("model flags = %+v", s)
	}
	if len(s.Assets) != 2 || s.Assets[0].Currency != "BTC" || s.Assets[1].Currency != "USDC" {
		t.Fatalf("empty ETH must be left out, assets sorted: %+v", s.Assets)
	}
	btc := s.Assets[0]
	if !near(btc.IMPct, 24, 1e-9) || !near(btc.MMPct, 16, 1e-9) || btc.AvailableWithdraw != 0.9 || btc.ProjectedMM != 0.18 {
		t.Errorf("BTC = %+v (IM 0.30 / MB 1.25 = 24 %%, MM 0.20 / 1.25 = 16 %%)", btc)
	}
	if s.Totals == nil || s.Totals.InitialMarginUSD != 30000 || !near(s.Totals.IMPct, 20, 1e-9) || !near(s.Totals.MMPct, 14, 1e-9) {
		t.Errorf("cross-collateral totals = %+v", s.Totals)
	}
	if len(rpc.calls) != 1 {
		t.Errorf("one call per poll, got %v", rpc.calls)
	}
}

func TestAccount_FallsBackToPerCurrencySummaries(t *testing.T) {
	rpc := &scriptedRPC{replies: map[string]string{ // no account-wide method on this account
		"private/get_account_summary:BTC": `{"currency":"BTC","balance":0.5,"equity":0.5,"margin_balance":0.5,"initial_margin":0.1,"maintenance_margin":0.05,"margin_model":"segregated_pm","portfolio_margining_enabled":true}`,
		"private/get_account_summary:ETH": `{"currency":"ETH","balance":0,"equity":0,"margin_balance":0}`,
	}}
	p := account.NewPoller(rpc)
	p.Refresh(context.Background())
	s := p.Status().Snapshot
	if s == nil || s.Source != "get_account_summary" || len(s.Assets) != 1 || s.MarginModel != "segregated_pm" || s.Totals != nil {
		t.Fatalf("fallback snapshot = %+v", s)
	}

	rpc.mu.Lock()
	rpc.calls = nil
	rpc.mu.Unlock()
	p.Refresh(context.Background())
	for _, c := range rpc.calls {
		if c == "private/get_account_summaries" {
			t.Error("after a failure the poller should not keep retrying the account-wide method")
		}
	}
}

func TestAccount_FailureKeepsLastSnapshot(t *testing.T) {
	rpc := &scriptedRPC{replies: map[string]string{"private/get_account_summaries": summariesJSON}}
	p := account.NewPoller(rpc)
	p.Refresh(context.Background())
	first := p.Status().Snapshot

	rpc.mu.Lock()
	rpc.replies = map[string]string{} // exchange now failing everything
	rpc.mu.Unlock()
	p.Refresh(context.Background())

	st := p.Status()
	if st.Snapshot != first || st.Error == "" || st.ErrorAt.IsZero() {
		t.Errorf("a failed poll must keep the last snapshot and report the error: %+v", st)
	}
}

func TestAccount_StartPollsUntilCancelled(t *testing.T) {
	rpc := &scriptedRPC{replies: map[string]string{"private/get_account_summaries": summariesJSON}}
	p := account.NewPoller(rpc)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx, 10*time.Millisecond)
	eventually(t, time.Second, "polled several times", func() bool {
		rpc.mu.Lock()
		defer rpc.mu.Unlock()
		return len(rpc.calls) >= 3
	})
	cancel()
	time.Sleep(30 * time.Millisecond)
	rpc.mu.Lock()
	n := len(rpc.calls)
	rpc.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	if len(rpc.calls) != n {
		t.Error("polling must stop on cancel")
	}
}

func TestAccount_Pct(t *testing.T) {
	cases := []struct{ part, whole, want float64 }{
		{30, 150, 20},
		{0, 0, 0},
		{0.1, 0, 100},  // margin required with no collateral → fully used, never 0 %
		{0.1, -1, 100}, // negative margin balance
	}
	for _, c := range cases {
		if got := account.Pct(c.part, c.whole); !near(got, c.want, 1e-12) {
			t.Errorf("Pct(%v, %v) = %v, want %v", c.part, c.whole, got, c.want)
		}
	}
}

type staticAccount struct{ st account.Status }

func (s staticAccount) Status() account.Status { return s.st }

func TestAPI_Account(t *testing.T) {
	snap := account.Build([]account.Summary{{Currency: "BTC", Balance: 1, Equity: 1, MarginBalance: 1, InitialMargin: 0.2, MarginModel: "segregated_pm"}}, "test", time.Now())
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0),
		api.WithAccount(staticAccount{account.Status{Snapshot: snap, Error: "timeout", ErrorAt: time.Now()}})).Handler()

	_, body := getJSON(t, h, "/api/account")
	s := body["snapshot"].(map[string]any)
	asset := s["assets"].([]any)[0].(map[string]any)
	if s["margin_model"] != "segregated_pm" || asset["im_pct"].(float64) != 20 || body["error"] != "timeout" {
		t.Errorf("account = %v", body)
	}

	none := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	if _, b := getJSON(t, none, "/api/account"); b["snapshot"] != nil {
		t.Errorf("no account source → null snapshot, got %v", b["snapshot"])
	}
}
