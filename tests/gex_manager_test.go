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

// gexFixture builds a book summary with strikes 80k–120k on one expiry 20
// days out and open interest concentrated in puts below spot, which yields
// negative gamma below spot and a flip near it. Names are real Deribit names:
// the manager reads strike, expiry and type from them.
func gexFixture() string {
	label := strings.ToUpper(time.Now().UTC().AddDate(0, 0, 20).Format("2Jan06"))
	var rows []string
	for _, k := range []float64{80000, 90000, 100000, 110000, 120000} {
		for _, typ := range []string{"call", "put"} {
			name := fmt.Sprintf("BTC-%s-%.0f-%s", label, k, strings.ToUpper(typ[:1]))
			oi := 100.0
			if typ == "put" && k < 100000 {
				oi = 2000
			}
			rows = append(rows, fmt.Sprintf(`{"instrument_name":"%s","open_interest":%.0f,"underlying_price":100000,"mark_iv":60,"mark_price":0.01}`, name, oi))
		}
	}
	// Other underlyings and unparsable names are ignored.
	rows = append(rows, `{"instrument_name":"ETH-27DEC30-3000-C","open_interest":5,"underlying_price":3000,"mark_iv":60}`,
		`{"instrument_name":"BTC-PERPETUAL","open_interest":5,"underlying_price":100000,"mark_iv":0}`)
	return "[" + strings.Join(rows, ",") + "]"
}

func gexParams(method string) gex.Params {
	return gex.Params{Underlying: "BTC", NExpiries: 5, StrikeRangePct: 0.25, Method: method}
}

func TestGEXManager_RefreshPublishesSnapshot(t *testing.T) {
	gw := &fakeBookSummary{result: gexFixture()}
	m := gex.NewManager(gw, gex.Params{Underlying: "BTC"}, 0.01) // defaults: 5 expiries, script method

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
	failing := &fakeBookSummary{err: errors.New("down")}
	if err := gex.NewManager(failing, gexParams(gex.MethodScript), 0.01).Refresh(context.Background()); err == nil {
		t.Error("a failed fetch must be reported")
	}

	garbage := &fakeBookSummary{result: `{"not":"a list"}`}
	if err := gex.NewManager(garbage, gexParams(gex.MethodScript), 0.01).Refresh(context.Background()); err == nil {
		t.Error("an undecodable summary must be reported")
	}

	noOI := &fakeBookSummary{result: `[]`}
	m := gex.NewManager(noOI, gexParams(gex.MethodScript), 0.01)
	if err := m.Refresh(context.Background()); err != nil {
		t.Errorf("an empty book is not an error: %v", err)
	}
	if m.Snapshot() != nil {
		t.Error("no snapshot from an empty book")
	}
}

func TestGEXManager_StartBackgroundRefreshesUntilCancelled(t *testing.T) {
	gw := &fakeBookSummary{result: gexFixture()}
	m := gex.NewManager(gw, gexParams(gex.MethodScript), 0.01)

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

// Open interest is published even when no GEX profile can be built (here no
// IV, which the original method requires), because the journal uses it on its own.
func TestGEXManager_PublishesOpenInterest(t *testing.T) {
	gw := &fakeBookSummary{result: `[{"instrument_name":"BTC-27DEC30-90000-P","open_interest":42,"mark_iv":0}]`}
	m := gex.NewManager(gw, gexParams(gex.MethodNearestFlip), 0.01)

	if m.OpenInterest() != nil {
		t.Fatal("no open interest before the first refresh")
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	oi := m.OpenInterest()
	if oi == nil || oi.ByInstrument["BTC-27DEC30-90000-P"] != 42 || oi.AsOf.IsZero() {
		t.Errorf("open interest = %+v", oi)
	}
	if m.Snapshot() != nil {
		t.Error("no GEX snapshot without IV")
	}
}
