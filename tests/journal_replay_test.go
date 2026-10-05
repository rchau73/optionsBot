package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The journal (orders.log) is replayed at startup so realised P&L, closes,
// event counts, the activity feed and the trade history continue across
// restarts — before, they all started again from zero.

var (
	slot25 = &orders.SlotRef{DTE: 25, Delta: 0.16}
	slot60 = &orders.SlotRef{DTE: 60, Delta: 0.18}
)

func jctx(slot *orders.SlotRef, at time.Time) orders.EventContext {
	return orders.EventContext{StrategyID: "short-strangle", Slot: slot, Market: orders.MarketSnapshot{AsOf: at, Spot: 85000, DVOL: 36}}
}

// writeJournal journals a session: two opens, a skip, a take-profit close
// (+0.0036) and a stop-loss close (−0.0060).
func writeJournal(l *orders.Logger, start time.Time) {
	call := &orders.Position{Instrument: "BTC-27NOV26-98000-C", OptionType: "call", Qty: 0.8, PremiumReceived: 0.0124, EntryTime: start}
	put := &orders.Position{Instrument: "BTC-23OCT26-78000-P", OptionType: "put", Qty: 1.2, PremiumReceived: 0.006, EntryTime: start}
	l.LogOpen(call, orders.Fill{OrderID: "1", FillPrice: 0.0155, Qty: 0.8}, jctx(slot60, start))
	l.LogOpen(put, orders.Fill{OrderID: "2", FillPrice: 0.005, Qty: 1.2}, jctx(slot25, start.Add(time.Minute)))
	l.LogSkipped("margin_limit", jctx(slot25, start.Add(2*time.Minute)))
	l.LogClose(call, orders.Fill{OrderID: "3", FillPrice: 0.011, Qty: 0.8}, orders.TriggerRolloutROI, orders.TypeLimit, jctx(slot60, start.Add(time.Hour)))
	l.LogClose(put, orders.Fill{OrderID: "4", FillPrice: 0.01, Qty: 1.2}, orders.TriggerStopLoss200Pct, orders.TypeMarket, jctx(slot25, start.Add(2*time.Hour)))
}

func TestReplay_RestoresPnLCountsTradesAndFeed(t *testing.T) {
	var buf bytes.Buffer
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	writeJournal(orders.NewWriterLogger(&buf, 0), start)
	buf.WriteString("{\"event\":\"closed\",\"pnl\":99, torn line\n") // crash mid-write

	rp, err := orders.ReplayJournal(&buf, 3)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Events != 5 || rp.Skipped != 1 {
		t.Errorf("events %d (want 5), skipped %d (want 1: the torn line)", rp.Events, rp.Skipped)
	}
	if rp.Counts[orders.EventFilled] != 2 || rp.Counts[orders.EventClosed] != 2 || rp.Counts[orders.EventSkipped] != 1 {
		t.Errorf("counts = %v", rp.Counts)
	}
	if got := rp.Realised[orders.SlotKey(slot60)]; !near(got, 0.0124-0.011*0.8, 1e-12) {
		t.Errorf("slot 60 realised = %v", got)
	}
	if got := rp.Realised[orders.SlotKey(slot25)]; !near(got, 0.006-0.012, 1e-12) || rp.Closed[orders.SlotKey(slot25)] != 1 {
		t.Errorf("slot 25 realised = %v, closes %d", got, rp.Closed[orders.SlotKey(slot25)])
	}
	if len(rp.Trades) != 4 || rp.Trades[0].Kind != "open" || rp.Trades[3].Kind != "close" || rp.Trades[3].Reason != "stop_loss" || rp.Trades[2].Reason != "roi_target" {
		t.Errorf("trades = %+v", rp.Trades)
	}
	if !rp.FirstAt.Equal(start) {
		t.Errorf("history since %v, want %v", rp.FirstAt, start)
	}
	if len(rp.Recent) != 3 || rp.Recent[0].Seq != 3 || rp.Recent[2].Seq != 5 || !rp.Recent[2].At.Equal(start.Add(2*time.Hour)) {
		t.Errorf("recent keeps the last 3 with their original seq and time: %+v", rp.Recent)
	}
}

// A restart: the new logger continues the old one's sequence, counts,
// feed and trades, and P&L adds up across both sessions.
func TestReplay_RestartContinuesTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.log")
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	first, err := orders.NewLogger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeJournal(first, start)
	first.Close()

	rp, err := orders.ReplayFile(path, orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	second, err := orders.NewLogger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.Restore(rp)
	pos := &orders.Position{Instrument: "ETH-X", OptionType: "call", Qty: 1, PremiumReceived: 0.01, EntryTime: start}
	second.LogClose(pos, orders.Fill{OrderID: "9", FillPrice: 0.004, Qty: 1}, orders.TriggerRolloutROI, orders.TypeLimit, jctx(slot60, start.Add(3*time.Hour)))

	if c := second.EventCounts(); c[orders.EventClosed] != 3 || c[orders.EventFilled] != 2 {
		t.Errorf("counts must include the earlier session: %v", c)
	}
	recent := second.Recent(0, 0)
	if len(recent) != 6 || recent[5].Seq != 6 || recent[0].Seq != 1 {
		t.Errorf("feed continues: seqs %v", seqs(recent))
	}
	trades := second.Trades(0, 0)
	if len(trades) != 5 || trades[4].Seq != 6 || trades[4].PnL == 0 {
		t.Errorf("trades continue: %+v", trades)
	}
	if got := second.Trades(4, 0); len(got) != 2 {
		t.Errorf("paging after seq 4: %d trades", len(got))
	}
	if !second.HistorySince().Equal(start) {
		t.Errorf("history since = %v", second.HistorySince())
	}

	// And a full replay of both sessions agrees with the sum of the closes.
	rp2, _ := orders.ReplayFile(path, 10)
	total := 0.0
	for _, v := range rp2.Realised {
		total += v
	}
	if !near(total, (0.0124-0.0088)+(0.006-0.012)+(0.01-0.004), 1e-12) || rp2.Events != 6 {
		t.Errorf("replayed realised %v over %d events", total, rp2.Events)
	}
}

func seqs(es []orders.RecentEvent) []uint64 {
	out := make([]uint64, len(es))
	for i, e := range es {
		out[i] = e.Seq
	}
	return out
}

func TestReplay_MissingJournalIsAnEmptyHistory(t *testing.T) {
	rp, err := orders.ReplayFile(filepath.Join(t.TempDir(), "none.log"), 10)
	if err != nil || rp.Events != 0 || len(rp.Trades) != 0 || rp.Realised == nil {
		t.Errorf("replay = %+v, err %v", rp, err)
	}
}

// A long journal replays quickly: only fills and closes are fully decoded.
func TestReplay_LongJournal(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 50000; i++ {
		l.LogSkipped("margin_limit", jctx(slot25, at.Add(time.Duration(i)*time.Minute)))
	}
	writeJournal(l, at)
	size := buf.Len()
	began := time.Now()
	rp, err := orders.ReplayJournal(&buf, orders.RecentEvents)
	took := time.Since(began)
	if err != nil || rp.Events != 50005 || len(rp.Trades) != 4 || len(rp.Recent) != orders.RecentEvents {
		t.Fatalf("events %d trades %d recent %d err %v", rp.Events, len(rp.Trades), len(rp.Recent), err)
	}
	t.Logf("replayed %d events (%.1f MB) in %v", rp.Events, float64(size)/1e6, took)
	if took > 10*time.Second {
		t.Errorf("replay too slow: %v", took)
	}
}

func TestAPI_TradesAndHistorySince(t *testing.T) {
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	logger := orders.NewWriterLogger(io.Discard, 0)
	var buf bytes.Buffer
	writeJournal(orders.NewWriterLogger(&buf, 0), start)
	rp, _ := orders.ReplayJournal(&buf, 10)
	logger.Restore(rp)
	h := api.New(staticView{sampleView()}, logger).Handler()

	get := func(url string) map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", url, rec.Code)
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	all := get("/api/trades")["trades"].([]any)
	if len(all) != 4 {
		t.Fatalf("want 4 trades, got %d", len(all))
	}
	page := get("/api/trades?after=1&limit=2")["trades"].([]any)
	if len(page) != 2 || page[0].(map[string]any)["seq"].(float64) != 2 {
		t.Errorf("page after seq 1, limit 2 = %v", page)
	}
	if since := get("/api/status")["history_since"]; since == nil || !strings.HasPrefix(since.(string), "2026-10-04T12:00:00") {
		t.Errorf("status history_since = %v", since)
	}
}

// The strategy's P&L (status, KPI strip, P&L chart) starts from the
// journal's realised P&L, per slot, instead of zero.
func TestStrategy_RestorePnLCarriesRealisedAcrossRestart(t *testing.T) {
	f := newStrategyFixture(t)
	slot := orders.SlotRef{DTE: 45, Delta: 0.16}
	f.strat = strategy.New(f.cfg, strategy.Deps{ // built, not run: as main does before Run
		Market: f.market, Exchange: f.exch, State: f.state, Journal: f.journal, Hedge: nopHedge{}, GEX: f.gex, OI: f.oi, Regimes: f.regimes,
	})
	f.strat.RestorePnL(
		map[orders.SlotRef]float64{slot: 0.004, {}: -0.001}, // a close without a known slot counts in the total
		map[orders.SlotRef]int{slot: 3, {}: 1},
	)
	lines := f.strat.PnLReport()
	total := lines[len(lines)-1]
	if !near(total.Realised, 0.003, 1e-12) || total.ClosedLegs != 4 {
		t.Errorf("total = %+v, want realised 0.003 over 4 closes", total)
	}
	if !near(lines[0].Realised, 0.004, 1e-12) || lines[0].ClosedLegs != 3 {
		t.Errorf("slot 45/0.16 = %+v", lines[0])
	}
}
