package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
)

// journalEvent is a recent event whose data is a real orders.log line.
func explainJournalEvent(t *testing.T, seq uint64, at time.Time, rec orders.OrderLog) orders.RecentEvent {
	t.Helper()
	rec.Timestamp, rec.Status = at, rec.Event
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return orders.RecentEvent{Seq: seq, Event: rec.Event, At: at, Data: data}
}

// eventsAndTrades serves fixed events and trades (orders.Logger's two views).
type eventsAndTrades struct {
	fixedEvents
	trades []orders.Trade
}

func (e eventsAndTrades) Trades(after uint64, limit int) []orders.Trade {
	out := []orders.Trade{}
	for _, t := range e.trades {
		if t.Seq > after && (limit <= 0 || len(out) < limit) {
			out = append(out, t)
		}
	}
	return out
}
func (e eventsAndTrades) HistorySince() time.Time { return time.Time{} }

const stopPut = "BTC-27NOV26-60000-P"

// stopLossJournal: a delta move on the put (its stop order sent), the
// stop-loss close as seq 812, events on another instrument and long before.
func stopLossJournal(t *testing.T) (fixedEvents, time.Time) {
	t0 := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	slot := &orders.SlotRef{DTE: 45, Delta: 0.16}
	market := orders.MarketSnapshot{AsOf: t0, Spot: 61200, DVOL: 58, IVPercentile: 71, GEXRegime: "negative",
		GammaFlip: 64000, FlipBufferPct: 4.5, Delta: -0.41, Bid: 0.031, Ask: 0.034, Mid: 0.0325}
	port := orders.MarketContext{Trend: "bear", SMATrend: "bear", Bias: "down"}
	return fixedEvents{
		explainJournalEvent(t, 700, t0.Add(-2*time.Hour), orders.OrderLog{Event: orders.EventFilled, Instrument: stopPut,
			OptionType: "put", Slot: slot, TriggerReason: orders.TriggerEntry, Qty: 1}),
		explainJournalEvent(t, 810, t0.Add(-3*time.Minute), orders.OrderLog{Event: orders.EventSubmitted, Instrument: stopPut,
			OptionType: "put", Direction: orders.DirectionBuy, TriggerReason: orders.TriggerStopLoss200Pct, Qty: 1,
			Detail: "stop-loss: loss 2.10× the premium ≥ 2×", Market: market}),
		explainJournalEvent(t, 811, t0.Add(-1*time.Minute), orders.OrderLog{Event: orders.EventSkipped,
			SkipReason: "no_expiry", Slot: &orders.SlotRef{DTE: 7, Delta: 0.1}}),
		explainJournalEvent(t, 812, t0, orders.OrderLog{Event: orders.EventClosed, Instrument: stopPut, OptionType: "put",
			Slot: slot, Direction: orders.DirectionBuy, TriggerReason: orders.TriggerStopLoss200Pct, CloseReason: "stop_loss",
			Detail: "stop-loss: loss 2.10× the premium ≥ 2×", Qty: 1, FillPrice: 0.034, Market: market, Portfolio: port}),
		explainJournalEvent(t, 813, t0.Add(5*time.Minute), orders.OrderLog{Event: orders.EventSkipped, Slot: slot,
			SkipReason: "repair_held: put stopped out, cooldown 72h"}),
	}, t0
}

func explainGet(t *testing.T, h http.Handler, query string) (int, api.Explanation, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/explain?"+query, nil))
	var out api.Explanation
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
	}
	return rec.Code, out, rec.Body.String()
}

func relatedSeqs(ev []api.PlainEvent) []uint64 {
	out := make([]uint64, len(ev))
	for i, e := range ev {
		out[i] = e.Seq
	}
	return out
}

// Scenario: a stop-loss close explained.
func TestAPIExplainEndpoint_AStopLossCloseExplained(t *testing.T) {
	events, t0 := stopLossJournal(t)
	h := api.New(staticView{sampleView()}, events).Handler()
	code, ex, body := explainGet(t, h, "seq=812")
	if code != http.StatusOK {
		t.Fatalf("→ %d %s", code, body)
	}
	if ex.Event.Seq != 812 || ex.Event.Event != orders.EventClosed || !strings.Contains(ex.Event.Plain, "stop-loss") {
		t.Errorf("event = %+v", ex.Event)
	}
	if ex.Rule != "stop_loss" || ex.Detail != "stop-loss: loss 2.10× the premium ≥ 2×" || ex.Source != "journal" {
		t.Errorf("rule %q detail %q source %q", ex.Rule, ex.Detail, ex.Source)
	}
	m := ex.Market
	if m == nil || m.Spot != 61200 || m.DVOL != 58 || m.GEXRegime != "negative" || m.GammaFlip != 64000 ||
		m.Trend != "bear" || m.Bias != "down" || m.Delta != -0.41 || !m.AsOf.Equal(t0) {
		t.Errorf("market = %+v", m)
	}
	// The stop order sent before it and the repair hold after it; not the
	// entry two hours earlier nor another slot's skip.
	if got := relatedSeqs(ex.Related); len(got) != 2 || got[0] != 810 || got[1] != 813 {
		t.Fatalf("related = %v, want [810 813]", got)
	}
	for _, r := range ex.Related {
		if r.Plain == "" {
			t.Errorf("related %d has no plain sentence", r.Seq)
		}
	}
}

// A repair hold: its rule word, the skip reason as detail, and the stop that
// preceded it on the same slot.
func TestAPIExplainEndpoint_RepairHold(t *testing.T) {
	events, _ := stopLossJournal(t)
	h := api.New(staticView{sampleView()}, events).Handler()
	code, ex, body := explainGet(t, h, "seq=813")
	if code != http.StatusOK {
		t.Fatalf("→ %d %s", code, body)
	}
	if ex.Rule != "repair_held" || ex.Detail != "repair_held: put stopped out, cooldown 72h" ||
		!strings.Contains(ex.Event.Plain, "cooldown") {
		t.Errorf("rule %q detail %q plain %q", ex.Rule, ex.Detail, ex.Event.Plain)
	}
	if got := relatedSeqs(ex.Related); len(got) != 1 || got[0] != 812 {
		t.Errorf("related = %v, want the stop-loss [812]", got)
	}
	if ex.Market != nil && ex.Market.Spot != 0 {
		t.Errorf("market = %+v, want what the skip recorded (none)", ex.Market)
	}
}

// Scenario: no invention — an event out of the recent window is a 404.
func TestAPIExplainEndpoint_NoInvention(t *testing.T) {
	events, _ := stopLossJournal(t)
	h := api.New(staticView{sampleView()}, events).Handler()
	code, _, body := explainGet(t, h, "seq=5")
	if code != http.StatusNotFound || !strings.Contains(body, "not in recent history") {
		t.Errorf("→ %d %s, want 404 not in recent history", code, body)
	}
}

func TestAPIExplainEndpoint_ByTrade(t *testing.T) {
	events, _ := stopLossJournal(t)
	src := eventsAndTrades{fixedEvents: events, trades: []orders.Trade{
		{Seq: 3, Kind: "close", Reason: "stop_loss"}, // in the trades, gone from the events
		{Seq: 812, Kind: "close", Reason: "stop_loss", Instrument: stopPut},
	}}
	h := api.New(staticView{sampleView()}, src).Handler()
	if code, ex, body := explainGet(t, h, "trade=812"); code != http.StatusOK || ex.Event.Seq != 812 || ex.Rule != "stop_loss" {
		t.Errorf("trade=812 → %d %s", code, body)
	}
	// Not a trade (a skip), and a trade whose event left memory: 404.
	for _, q := range []string{"trade=813", "trade=3", "trade=99"} {
		if code, _, body := explainGet(t, h, q); code != http.StatusNotFound || !strings.Contains(body, "not in recent history") {
			t.Errorf("%s → %d %s, want 404", q, code, body)
		}
	}
	// Without trades at all, a trade is never found.
	h = api.New(staticView{sampleView()}, events).Handler()
	if code, _, _ := explainGet(t, h, "trade=812"); code != http.StatusNotFound {
		t.Errorf("no trade source → %d, want 404", code)
	}
}

func TestAPIExplainEndpoint_BadQueries(t *testing.T) {
	events, _ := stopLossJournal(t)
	h := api.New(staticView{sampleView()}, events).Handler()
	for _, q := range []string{"", "seq=812&trade=812", "seq=abc", "seq=0", "seq=-1", "trade=x"} {
		if code, _, body := explainGet(t, h, q); code != http.StatusBadRequest {
			t.Errorf("%q → %d %s, want 400", q, code, body)
		}
	}
}

// A risk line names no instrument or slot: no related events, its flat
// market fields kept.
func TestAPIExplainEndpoint_RiskLine(t *testing.T) {
	data, _ := json.Marshal(orders.RiskRecord{Event: orders.EventRisk, Change: orders.RiskFrozen, Detail: "dvol band 2→1",
		DVOL: 60, IVPercentile: 80, Regime: "negative"})
	now := time.Now()
	events := fixedEvents{
		{Seq: 1, Event: orders.EventRisk, At: now, Data: data},
		{Seq: 2, Event: orders.EventSkipped, At: now, Data: json.RawMessage(`{"skip_reason":"risk_frozen"}`)},
	}
	ex := api.BuildExplanation(events[0], events)
	if ex.Rule != orders.RiskFrozen || ex.Detail != "dvol band 2→1" || len(ex.Related) != 0 || ex.Related == nil {
		t.Errorf("explanation = %+v", ex)
	}
	if ex.Market == nil || ex.Market.DVOL != 60 || ex.Market.IVPercentile != 80 || ex.Market.GEXRegime != "negative" {
		t.Errorf("market = %+v", ex.Market)
	}
}

// The real journal: an event logged by orders.Logger is explained by its seq.
func TestAPIExplainEndpoint_FromTheLogger(t *testing.T) {
	logger := orders.NewWriterLogger(&strings.Builder{}, 0)
	logger.LogSkipped("no_free_expiry: 27NOV held by slot 45", orders.EventContext{Slot: &orders.SlotRef{DTE: 25, Delta: 0.2}})
	seq := logger.Recent(0, 0)[0].Seq
	h := api.New(staticView{sampleView()}, logger).Handler()
	code, ex, body := explainGet(t, h, "seq="+strconv.FormatUint(seq, 10))
	if code != http.StatusOK || ex.Rule != "no_free_expiry" || ex.Market == nil {
		t.Errorf("→ %d %s", code, body)
	}
}
