package tests

import (
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"optionsbot/internal/account"
	"optionsbot/internal/api"
	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

var updateAccountGolden = flag.Bool("update-account-golden", false, "rewrite tests/testdata/account_api/*.json")

// newAccountPoller is the poller main builds: Deribit's account source on gw.
func newAccountPoller(gw *scriptedRPC) *account.Poller {
	return account.NewPoller(deribit.NewAccountSource(gw))
}

const btcPositionsJSON = `[
  {"instrument_name":"BTC-25DEC26-60000-P","kind":"option","direction":"buy","size":1.7,"average_price":0.067,"mark_price":0.0045,"total_profit_loss":-0.106,"delta":-0.13,"initial_margin":0,"maintenance_margin":0},
  {"instrument_name":"BTC-27NOV26-98000-C","kind":"option","direction":"sell","size":-0.8,"average_price":0.02,"mark_price":0.018,"total_profit_loss":0.0016,"delta":-0.18,"initial_margin":0.03,"maintenance_margin":0.02},
  {"instrument_name":"BTC-PERPETUAL","kind":"future","direction":"zero","size":0}]`

// accountCases are recorded replies (made-up amounts) for each path of the
// poller: the account-wide read, a failed positions read, the per-currency
// fallback with an undecodable positions reply, and everything failing.
var accountCases = []struct {
	name    string
	replies map[string]string
}{
	{"summaries", map[string]string{
		"private/get_account_summaries": summariesJSON,
		"private/get_positions:BTC":     btcPositionsJSON,
		"private/get_positions:USDC":    `[]`,
	}},
	{"positions_error", map[string]string{
		"private/get_account_summaries": summariesJSON,
	}},
	{"fallback", map[string]string{
		"private/get_account_summary:BTC": `{"currency":"BTC","balance":0.5,"equity":0.5,"margin_balance":0.5,"initial_margin":0.1,"maintenance_margin":0.05,"margin_model":"segregated_pm","portfolio_margining_enabled":true}`,
		"private/get_account_summary:ETH": `{"balance":0.2,"equity":0.2,"margin_balance":0.2}`,
		"private/get_positions:BTC":       `{"not":"a list"}`,
	}},
	{"failure", map[string]string{}},
}

// timestamps are the only bytes that differ between runs.
var accountTimestamps = regexp.MustCompile(`"(as_of|error_at)":"[^"]*"`)

// Scenario (#746): the account view is unchanged — /api/account returns the
// same JSON, byte for byte, as the poller gave before it read through the
// Deribit source (the golden files were written by the code before #746).
func TestAccountSource_AccountViewIsUnchanged(t *testing.T) {
	for _, c := range accountCases {
		t.Run(c.name, func(t *testing.T) {
			p := newAccountPoller(&scriptedRPC{replies: c.replies})
			p.Refresh(context.Background())
			h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), api.WithAccount(p)).Handler()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/account", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			got := accountTimestamps.ReplaceAll(rec.Body.Bytes(), []byte(`"$1":"T"`))

			path := filepath.Join("testdata", "account_api", c.name+".json")
			if *updateAccountGolden || os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("/api/account changed:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// Scenario (#746): same requests, same lane — the Deribit account source
// sends the methods and params the poller sent, on the low-priority lane.
func TestAccountSource_SameRequestsSameLane(t *testing.T) {
	low := gateway.PriorityLow
	summaries := feedCall{"private/get_account_summaries", map[string]any{"extended": true}, low}
	positions := func(c string) feedCall {
		return feedCall{"private/get_positions", map[string]any{"currency": c}, low}
	}
	summary := func(c string) feedCall {
		return feedCall{"private/get_account_summary", map[string]any{"currency": c, "extended": true}, low}
	}
	want := map[string][]feedCall{
		"summaries":       {summaries, positions("BTC"), positions("USDC")},
		"positions_error": {summaries, positions("BTC")},
		// Scenario: a failing summaries call still falls back per currency.
		"fallback": {summaries, summary("BTC"), summary("ETH"), summary("USDC"), summary("USDT"), positions("BTC")},
		"failure":  {summaries, summary("BTC"), summary("ETH"), summary("USDC"), summary("USDT")},
	}
	for _, c := range accountCases {
		rpc := &scriptedRPC{replies: c.replies}
		newAccountPoller(rpc).Refresh(context.Background())
		if !reflect.DeepEqual(rpc.requests, want[c.name]) {
			t.Errorf("%s: requests =\n %+v\nwant\n %+v", c.name, rpc.requests, want[c.name])
		}
	}
}

// Negative: each source method surfaces the gateway's error and a reply it
// cannot decode, with the messages the monitor showed before.
func TestAccountSource_Errors(t *testing.T) {
	ctx := context.Background()
	failing := deribit.NewAccountSource(&scriptedRPC{replies: map[string]string{}})
	if _, err := failing.Summaries(ctx); err == nil || err.Error() != "get_account_summaries: bad_request (code 11050)" {
		t.Errorf("Summaries error = %v", err)
	}
	if _, err := failing.Summary(ctx, "BTC"); err == nil {
		t.Error("Summary must fail when the call fails")
	}
	if _, err := failing.Positions(ctx, "BTC"); err == nil || err.Error() != "get_positions BTC: bad_request (code 11050)" {
		t.Errorf("Positions error = %v", err)
	}

	garbled := deribit.NewAccountSource(&scriptedRPC{replies: map[string]string{
		"private/get_account_summaries":   `[1]`,
		"private/get_account_summary:BTC": `[1]`,
		"private/get_positions:BTC":       `{}`,
	}})
	if _, err := garbled.Summaries(ctx); err == nil || !regexp.MustCompile(`^decode account summaries: `).MatchString(err.Error()) {
		t.Errorf("Summaries decode error = %v", err)
	}
	if _, err := garbled.Summary(ctx, "BTC"); err == nil {
		t.Error("Summary must fail on an undecodable reply")
	}
	if _, err := garbled.Positions(ctx, "BTC"); err == nil || !regexp.MustCompile(`^decode positions BTC: `).MatchString(err.Error()) {
		t.Errorf("Positions decode error = %v", err)
	}
}
