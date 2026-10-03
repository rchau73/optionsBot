package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
)

type fakeBookSummary struct {
	mu     sync.Mutex
	result string
	err    error
	calls  int
}

func (f *fakeBookSummary) Call(_ context.Context, method string, _ any, _ int) (gateway.JSONRPCResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if method != "public/get_book_summary_by_currency" {
		return gateway.JSONRPCResponse{}, fmt.Errorf("unexpected method %s", method)
	}
	if f.err != nil {
		return gateway.JSONRPCResponse{}, f.err
	}
	return gateway.JSONRPCResponse{Result: json.RawMessage(f.result)}, nil
}

func (f *fakeBookSummary) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type staticChain []*marketdata.Instrument

func (c staticChain) AllInstruments() []*marketdata.Instrument { return c }

// gexFixture builds a chain with strikes 80k–120k on one expiry and open
// interest concentrated in puts below spot, which yields negative gamma
// below the strikes and a flip near spot.
func gexFixture() (staticChain, string) {
	expiry := time.Now().AddDate(0, 0, 20)
	var chain staticChain
	var rows []string
	for _, k := range []float64{80000, 90000, 100000, 110000, 120000} {
		for _, typ := range []string{"call", "put"} {
			name := fmt.Sprintf("BTC-T-%.0f-%s", k, strings.ToUpper(typ[:1]))
			chain = append(chain, &marketdata.Instrument{Name: name, Strike: k, Expiry: expiry, OptionType: typ})
			oi := 100.0
			if typ == "put" && k < 100000 {
				oi = 2000
			}
			rows = append(rows, fmt.Sprintf(`{"instrument_name":"%s","open_interest":%.0f,"underlying_price":100000,"mark_iv":60,"mark_price":0.01}`, name, oi))
		}
	}
	// An instrument with no open interest must be ignored.
	rows = append(rows, `{"instrument_name":"BTC-T-80000-C-EXTRA","open_interest":0,"underlying_price":100000,"mark_iv":60}`)
	return chain, "[" + strings.Join(rows, ",") + "]"
}

func TestGEXManager_RefreshPublishesSnapshot(t *testing.T) {
	chain, summary := gexFixture()
	gw := &fakeBookSummary{result: summary}
	m := gex.NewManager(gw, chain, "BTC", 0, 0.01, 0.25)

	if m.Snapshot() != nil {
		t.Fatal("no snapshot before the first refresh")
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	snap := m.Snapshot()
	if snap == nil {
		t.Fatal("snapshot not published")
	}
	if snap.Spot != 100000 {
		t.Errorf("spot = %v, want 100000", snap.Spot)
	}
	if snap.Regime == "" {
		t.Error("regime must be classified")
	}
}

func TestGEXManager_RefreshErrors(t *testing.T) {
	chain, _ := gexFixture()

	failing := &fakeBookSummary{err: errors.New("down")}
	if err := gex.NewManager(failing, chain, "BTC", 5, 0.01, 0).Refresh(context.Background()); err == nil {
		t.Error("a failed fetch must be reported")
	}

	garbage := &fakeBookSummary{result: `{"not":"a list"}`}
	if err := gex.NewManager(garbage, chain, "BTC", 5, 0.01, 0).Refresh(context.Background()); err == nil {
		t.Error("an undecodable summary must be reported")
	}

	noOI := &fakeBookSummary{result: `[]`}
	m := gex.NewManager(noOI, chain, "BTC", 5, 0.01, 0)
	if err := m.Refresh(context.Background()); err != nil {
		t.Errorf("no open interest is not an error: %v", err)
	}
	if m.Snapshot() != nil {
		t.Error("no snapshot without open interest")
	}
}

func TestGEXManager_StartBackgroundRefreshesUntilCancelled(t *testing.T) {
	chain, summary := gexFixture()
	gw := &fakeBookSummary{result: summary}
	m := gex.NewManager(gw, chain, "BTC", 5, 0.01, 0)

	ctx, cancel := context.WithCancel(context.Background())
	m.StartBackground(ctx, 10*time.Millisecond)
	eventually(t, time.Second, "several refreshes", func() bool { return gw.callCount() >= 3 })
	cancel()
	time.Sleep(30 * time.Millisecond)
	after := gw.callCount()
	time.Sleep(50 * time.Millisecond)
	if gw.callCount() != after {
		t.Error("refreshes must stop after cancel")
	}
	if m.Snapshot() == nil {
		t.Error("background refresh should publish a snapshot")
	}
}
