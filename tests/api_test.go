package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

type staticView struct{ v strategy.View }

func (s staticView) View() strategy.View { return s.v }

func getJSON(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if rec.Code == http.StatusOK {
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content type %q", path, ct)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: invalid JSON: %v", path, err)
		}
	}
	return rec.Code, body
}

func sampleView() strategy.View {
	return strategy.View{
		AsOf: time.Now(), StrategyID: "short-strangle", Underlying: "BTC", Environment: "testnet",
		Market: orders.MarketSnapshot{Spot: 100000, DVOL: 55},
		Strangles: []strategy.StrangleView{{
			ID: "st-1", Slot: orders.SlotRef{DTE: 45, Delta: 0.16},
			Legs: []strategy.LegView{
				{Instrument: "BTC-X-110000-C", OptionType: "call", Strike: 110000, Qty: 0.1},
				{Instrument: "BTC-X-90000-P", OptionType: "put", Strike: 90000, Qty: 0.1},
			},
		}},
		Pending: []strategy.PendingView{{ID: "ps-1"}},
		PnL:     []strategy.PnLView{{Realised: 0.001, Total: 0.001}},
	}
}

func TestAPI_StatusSummarisesTheBot(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	logger.LogSkipped("no_expiry", orders.EventContext{})
	h := api.New(staticView{sampleView()}, logger).Handler()

	code, body := getJSON(t, h, "/api/status")
	if code != http.StatusOK {
		t.Fatalf("status code %d", code)
	}
	if body["strategy_id"] != "short-strangle" || body["environment"] != "testnet" ||
		body["open_strangles"].(float64) != 1 || body["open_legs"].(float64) != 2 || body["pending"].(float64) != 1 {
		t.Errorf("status = %v", body)
	}
	if body["event_counts"].(map[string]any)["skipped"].(float64) != 1 {
		t.Errorf("event counts = %v", body["event_counts"])
	}
	if body["market"].(map[string]any)["dvol"].(float64) != 55 {
		t.Errorf("market = %v", body["market"])
	}
}

func TestAPI_PositionsOrdersPnL(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()

	_, pos := getJSON(t, h, "/api/positions")
	legs := pos["strangles"].([]any)[0].(map[string]any)["legs"].([]any)
	if len(legs) != 2 || legs[0].(map[string]any)["strike"].(float64) != 110000 {
		t.Errorf("positions = %v", pos)
	}
	_, ord := getJSON(t, h, "/api/orders")
	if len(ord["pending"].([]any)) != 1 {
		t.Errorf("orders = %v", ord)
	}
	_, pnl := getJSON(t, h, "/api/pnl")
	if pnl["spot"].(float64) != 100000 || len(pnl["pnl"].([]any)) != 1 {
		t.Errorf("pnl = %v", pnl)
	}
	if code, _ := getJSON(t, h, "/api/health"); code != http.StatusOK {
		t.Error("health should be OK")
	}
}

func TestAPI_EmptyBotReturnsEmptyLists(t *testing.T) {
	h := api.New(staticView{strategy.View{Strangles: []strategy.StrangleView{}, Pending: []strategy.PendingView{}}},
		orders.NewWriterLogger(io.Discard, 0)).Handler()

	_, pos := getJSON(t, h, "/api/positions")
	if l, ok := pos["strangles"].([]any); !ok || len(l) != 0 {
		t.Errorf("empty positions must be [] not null: %v", pos["strangles"])
	}
	_, ev := getJSON(t, h, "/api/events")
	if l, ok := ev["events"].([]any); !ok || len(l) != 0 {
		t.Errorf("no events must be []: %v", ev["events"])
	}
}

func TestAPI_EventsSinceAndLimit(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	for i := 0; i < 5; i++ {
		logger.LogSkipped("no_expiry", orders.EventContext{})
	}
	h := api.New(staticView{sampleView()}, logger).Handler()

	_, all := getJSON(t, h, "/api/events")
	events := all["events"].([]any)
	if len(events) != 5 {
		t.Fatalf("want 5 events, got %d", len(events))
	}
	last := events[4].(map[string]any)
	if last["seq"].(float64) != 5 || last["event"] != "skipped" || last["data"].(map[string]any)["skip_reason"] != "no_expiry" {
		t.Errorf("event = %v", last)
	}

	_, newer := getJSON(t, h, "/api/events?since=3")
	if n := len(newer["events"].([]any)); n != 2 {
		t.Errorf("since=3 → %d events, want 2", n)
	}
	_, limited := getJSON(t, h, "/api/events?limit=2")
	first := limited["events"].([]any)[0].(map[string]any)
	if len(limited["events"].([]any)) != 2 || first["seq"].(float64) != 4 {
		t.Errorf("limit keeps the newest events, got %v", limited["events"])
	}

	for _, bad := range []string{"/api/events?since=-1", "/api/events?since=x", "/api/events?limit=0"} {
		if code, _ := getJSON(t, h, bad); code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", bad, code)
		}
	}
}

func TestAPI_IsReadOnly(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d, want 405", rec.Code)
	}
}

func TestRecentEvents_RingKeepsTheNewest(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	for i := 0; i < 600; i++ { // ring holds 500
		logger.LogSkipped("x", orders.EventContext{})
	}
	events := logger.Recent(0, 1000)
	if len(events) != 500 || events[0].Seq != 101 || events[499].Seq != 600 {
		t.Errorf("ring: %d events, first %d last %d", len(events), events[0].Seq, events[len(events)-1].Seq)
	}
	if logger.EventCounts()["skipped"] != 600 {
		t.Errorf("counts must cover all events since start: %v", logger.EventCounts())
	}
}

// The API reads live strategy state while the decision loop trades; under
// -race this proves the view is built without data races.
func TestAPI_LiveStrategyUnderConcurrentPolling(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenInterest()
	f.withOpenStrangle(0.1, 0.02)
	f.logger = orders.NewWriterLogger(io.Discard, 0.05)
	f.startRun()
	h := api.New(f.strat, f.logger).Handler()

	eventually(t, 2*time.Second, "positions visible", func() bool {
		_, body := getJSON(t, h, "/api/positions")
		st, _ := body["strangles"].([]any)
		return len(st) == 1
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				for _, p := range []string{"/api/status", "/api/positions", "/api/orders", "/api/pnl", "/api/events"} {
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
					if rec.Code != http.StatusOK {
						t.Errorf("%s → %d", p, rec.Code)
						return
					}
				}
			}
		}()
	}
	f.market.setQuote(f.call, 0.004, 0.006) // trigger a take-profit roll while polling
	wg.Wait()

	_, status := getJSON(t, h, "/api/status")
	if status["loop_at"] == "0001-01-01T00:00:00Z" {
		t.Error("loop_at should advance as the loop publishes")
	}
	_, pos := getJSON(t, h, "/api/positions")
	leg := pos["strangles"].([]any)[0].(map[string]any)["legs"].([]any)[0].(map[string]any)
	for _, k := range []string{"strike", "dte", "qty", "mark", "unrealised_pnl", "stop_loss_mark", "moneyness", "greeks"} {
		if _, ok := leg[k]; !ok {
			t.Errorf("leg missing %s: %v", k, leg)
		}
	}
	_, ev := getJSON(t, h, "/api/events")
	if len(ev["events"].([]any)) == 0 {
		t.Error("journal events should be visible")
	}
}
