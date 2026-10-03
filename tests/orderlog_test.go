package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/orders"
)

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("orders.log line is not JSON: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func samplePosition() *orders.Position {
	return &orders.Position{
		ID: "pos-1", Instrument: "BTC-27DEC26-110000-C", OptionType: "call",
		Strike: 110000, Qty: 0.1, EntryPrice: 0.02, PremiumReceived: 0.002,
		UnderlyingPrice: 100000, CurrentMid: 0.005, EntryTime: time.Now().AddDate(0, 0, -10),
		CurrentGreeks: orders.Greeks{Delta: 0.16, Theta: -20},
	}
}

// sampleContext is an event context with a market snapshot at spot 90k
// (different from the 100k entry spot, to check which one P&L uses).
func sampleContext() orders.EventContext {
	return orders.EventContext{
		StrategyID: "short-strangle",
		Slot:       &orders.SlotRef{DTE: 45, Delta: 0.16},
		Market: orders.MarketSnapshot{
			AsOf: time.Now(), Spot: 90000, DVOL: 58, IVPercentile: 70,
			Moneyness: "OTM", DistanceToStrikePct: 22.2, StrikeOI: 1500, StrikeOIRank: 2,
			GEXRegime: "POSITIVE/PINNING", Bid: 0.004, Ask: 0.006, Mid: 0.005, SpreadPct: 40,
		},
		Portfolio: orders.MarketContext{Trend: "bull", NetDelta: -0.1},
	}
}

func TestOrderLog_CloseRecordsPnLWithMarketSnapshot(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0.05)
	fill := orders.Fill{OrderID: "o-9", FillPrice: 0.005, Qty: 0.1, Timestamp: time.Now()}

	l.LogClose(samplePosition(), fill, orders.TriggerRolloutROI, orders.TypeLimit, sampleContext())

	recs := decodeLogLines(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	// premium 0.002 − close 0.0005 = 0.0015 BTC; USD at the close-time spot (90k) = 135.
	if got := r["pnl"].(float64); math.Abs(got-0.0015) > 1e-12 {
		t.Errorf("pnl (coin) = %v, want 0.0015", got)
	}
	if got := r["pnl_usd"].(float64); math.Abs(got-135) > 1e-6 {
		t.Errorf("pnl_usd = %v, want 135 (spot at close, not entry)", got)
	}
	if r["pnl_usd_fmt"] != "135.00" || r["roi_pct_fmt"] != "75.0000%" {
		t.Errorf("formatted fields = %v / %v", r["pnl_usd_fmt"], r["roi_pct_fmt"])
	}
	if r["event"] != "closed" || r["status"] != "closed" || r["close_reason"] != "roi_target" ||
		r["order_type"] != "limit" || r["direction"] != "buy" || r["strategy_id"] != "short-strangle" {
		t.Errorf("core fields wrong: %v", r)
	}
	slot := r["slot"].(map[string]any)
	if slot["dte"].(float64) != 45 || slot["delta"].(float64) != 0.16 {
		t.Errorf("slot = %v", slot)
	}
	m := r["market"].(map[string]any)
	if m["dvol"].(float64) != 58 || m["moneyness"] != "OTM" || m["strike_oi"].(float64) != 1500 ||
		m["gex_regime"] != "POSITIVE/PINNING" || m["iv_percentile"].(float64) != 70 {
		t.Errorf("market snapshot not recorded: %v", m)
	}
	if r["portfolio"].(map[string]any)["trend"] != "bull" {
		t.Errorf("portfolio context missing: %v", r["portfolio"])
	}
	if r["hold_days"].(float64) != 10 {
		t.Errorf("hold_days = %v, want 10", r["hold_days"])
	}
}

func TestOrderLog_MarketOrderTypeAndLargeUSD(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0)
	pos := samplePosition()
	pos.Qty, pos.PremiumReceived = 10, 0.2
	ctx := sampleContext()
	ctx.Market.Spot = 100000
	// 0.2 − 0.0123×10 = 0.077 BTC × 100k = 7,700; 0.2 − 0.5 = −0.3 BTC = −30,000.
	l.LogClose(pos, orders.Fill{FillPrice: 0.0123, Qty: 10}, orders.TriggerStopLoss200Pct, orders.TypeMarket, ctx)
	l.LogClose(pos, orders.Fill{FillPrice: 0.05, Qty: 10}, orders.TriggerKillSwitch, orders.TypeMarket, ctx)

	recs := decodeLogLines(t, &buf)
	if recs[0]["pnl_usd_fmt"] != "7,700.00" || recs[0]["close_reason"] != "stop_loss" || recs[0]["order_type"] != "market" {
		t.Errorf("record 0: %v", recs[0])
	}
	if recs[1]["pnl_usd_fmt"] != "-30,000.00" || recs[1]["close_reason"] != "kill_switch" {
		t.Errorf("record 1: %v", recs[1])
	}
}

func TestOrderLog_CloseFallsBackToEntrySpot(t *testing.T) {
	var buf bytes.Buffer
	ctx := sampleContext()
	ctx.Market.Spot = 0 // no index price yet
	orders.NewWriterLogger(&buf, 0).LogClose(samplePosition(),
		orders.Fill{FillPrice: 0.005, Qty: 0.1}, orders.TriggerRolloutROI, orders.TypeLimit, ctx)
	if got := decodeLogLines(t, &buf)[0]["pnl_usd"].(float64); math.Abs(got-150) > 1e-6 {
		t.Errorf("pnl_usd = %v, want 150 at the entry spot", got)
	}
}

func TestOrderLog_EveryEventCarriesItsContext(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0.05)
	ctx := sampleContext()
	rec := orders.PendingOrderRecord{
		OrderID: "o-1", Instrument: "BTC-X-110000-C", OptionType: "call",
		Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: orders.TriggerEntry,
		Qty: 0.1, LimitPrice: 0.021, Greeks: orders.Greeks{Delta: 0.16},
	}
	l.LogSubmit(rec, ctx)
	rec.LimitPrice = 0.019
	l.LogAmend(rec, 0.021, ctx)
	l.LogCancelled(rec, ctx)
	l.LogOpen(samplePosition(), orders.Fill{OrderID: "o-1", FillPrice: 0.02, Qty: 0.1}, ctx)
	l.LogReconciled(samplePosition(), ctx)
	l.LogSkipped("no_expiry", orders.EventContext{StrategyID: "short-strangle", Slot: ctx.Slot, Market: orders.MarketSnapshot{Spot: 90000, DVOL: 58}})

	recs := decodeLogLines(t, &buf)
	want := []string{"submitted", "amended", "cancelled", "filled", "reconciled", "skipped"}
	if len(recs) != len(want) {
		t.Fatalf("want %d records, got %d", len(want), len(recs))
	}
	for i, r := range recs {
		if r["event"] != want[i] {
			t.Errorf("record %d event = %v, want %s", i, r["event"], want[i])
		}
		m, ok := r["market"].(map[string]any)
		if !ok || m["spot"].(float64) != 90000 || m["dvol"].(float64) != 58 {
			t.Errorf("%s: market snapshot missing: %v", want[i], r["market"])
		}
		if r["strategy_id"] != "short-strangle" || r["slot"] == nil {
			t.Errorf("%s: strategy/slot missing", want[i])
		}
	}
	if recs[1]["previous_price"].(float64) != 0.021 || recs[1]["limit_price"].(float64) != 0.019 {
		t.Errorf("amend should record old and new price: %v", recs[1])
	}
	if recs[3]["premium_received"].(float64) != 0.002 {
		t.Errorf("fill premium = %v, want 0.02 × 0.1", recs[3]["premium_received"])
	}
	if recs[5]["skip_reason"] != "no_expiry" {
		t.Errorf("skip reason = %v", recs[5]["skip_reason"])
	}
}

func TestOrderLog_PnLRecord(t *testing.T) {
	var buf bytes.Buffer
	orders.NewWriterLogger(&buf, 0).LogPnL(orders.PnLRecord{
		Timestamp: time.Now(), StrategyID: "short-strangle", Realised: 0.001, Unrealised: -0.0004,
		Total: 0.0006, TotalUSD: 60, Spot: 100000, OpenLegs: 2, ClosedLegs: 1,
	})
	r := decodeLogLines(t, &buf)[0]
	if r["event"] != "pnl" || r["total_usd"].(float64) != 60 || r["slot"] != nil {
		t.Errorf("pnl record = %v", r)
	}
}

func TestOrderLog_FileLoggerAppendsAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.log")
	for i := 0; i < 2; i++ {
		l, err := orders.NewLogger(path, 0.05)
		if err != nil {
			t.Fatal(err)
		}
		l.LogReconciled(samplePosition(), sampleContext())
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Errorf("restarts must append, not truncate: %d lines", n)
	}
	if _, err := orders.NewLogger(filepath.Join(t.TempDir(), "no", "such", "dir.log"), 0); err == nil {
		t.Error("an unwritable path must be an error")
	}
	if err := orders.NewWriterLogger(&bytes.Buffer{}, 0).Close(); err != nil {
		t.Error("closing a writer logger is a no-op")
	}
}
