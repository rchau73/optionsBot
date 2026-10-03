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

func TestOrderLog_CloseRecordsPnLInBothUnits(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0.05)
	fill := orders.Fill{OrderID: "o-9", FillPrice: 0.005, Qty: 0.1, Timestamp: time.Now()}

	l.LogClose(samplePosition(), fill, 42, orders.TriggerRolloutROI,
		orders.MarketContext{Trend: "bull", NetDelta: -0.1},
		orders.GEXContext{Regime: "POSITIVE/PINNING", GammaFlip: 95000, FlipFound: true})

	recs := decodeLogLines(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	// premium 0.002 − close 0.0005 = 0.0015 BTC; × 100k spot = 150 USD.
	if got := r["pnl"].(float64); math.Abs(got-0.0015) > 1e-12 {
		t.Errorf("pnl (BTC) = %v, want 0.0015", got)
	}
	if got := r["pnl_usd"].(float64); math.Abs(got-150) > 1e-6 {
		t.Errorf("pnl_usd = %v, want 150", got)
	}
	if r["pnl_usd_fmt"] != "150.00" || r["roi_pct_fmt"] != "75.0000%" {
		t.Errorf("formatted fields = %v / %v", r["pnl_usd_fmt"], r["roi_pct_fmt"])
	}
	if r["close_reason"] != "roi_target" || r["gamma_regime"] != "POSITIVE/PINNING" || r["market_trend"] != "bull" {
		t.Errorf("context fields missing: %v", r)
	}
	if r["hold_days"].(float64) != 10 {
		t.Errorf("hold_days = %v, want 10", r["hold_days"])
	}
}

func TestOrderLog_FormatsLargeAndNegativeUSD(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0.05)
	pos := samplePosition()
	pos.Qty = 10
	pos.PremiumReceived = 0.2
	// Close at 0.0123 × 10 = 0.123 BTC → pnl 0.077 BTC × 100k = 7,700.00 USD.
	l.LogClose(pos, orders.Fill{FillPrice: 0.0123, Qty: 10}, 0, orders.TriggerStopLoss200Pct, orders.MarketContext{}, orders.GEXContext{})
	// Loss: close at 0.05 × 10 = 0.5 → pnl −0.3 BTC = −30,000.00 USD.
	l.LogClose(pos, orders.Fill{FillPrice: 0.05, Qty: 10}, 0, orders.TriggerKillSwitch, orders.MarketContext{}, orders.GEXContext{})

	recs := decodeLogLines(t, &buf)
	if recs[0]["pnl_usd_fmt"] != "7,700.00" || recs[0]["close_reason"] != "stop_loss" {
		t.Errorf("record 0: %v %v", recs[0]["pnl_usd_fmt"], recs[0]["close_reason"])
	}
	if recs[1]["pnl_usd_fmt"] != "-30,000.00" || recs[1]["close_reason"] != "kill_switch" {
		t.Errorf("record 1: %v %v", recs[1]["pnl_usd_fmt"], recs[1]["close_reason"])
	}
}

func TestOrderLog_LifecycleEvents(t *testing.T) {
	var buf bytes.Buffer
	l := orders.NewWriterLogger(&buf, 0.05)
	rec := orders.PendingOrderRecord{
		OrderID: "o-1", Instrument: "BTC-X-110000-C", OptionType: "call",
		Direction: orders.DirectionSell, TriggerReason: orders.TriggerEntry,
		Qty: 0.1, LimitPrice: 0.021, Strike: 110000, UnderlyingPrice: 100000,
		Bid: 0.019, Ask: 0.021,
	}
	l.LogSubmit(rec, orders.MarketContext{}, orders.GEXContext{})
	l.LogCancelled(rec, orders.MarketContext{}, orders.GEXContext{})
	l.LogOpen(samplePosition(), orders.Fill{OrderID: "o-1", FillPrice: 0.02, Qty: 0.1}, 50, 0.05, orders.MarketContext{}, orders.GEXContext{})
	l.LogReconciled(samplePosition(), 50, orders.MarketContext{}, orders.GEXContext{})

	recs := decodeLogLines(t, &buf)
	if len(recs) != 4 {
		t.Fatalf("want 4 records, got %d", len(recs))
	}
	var statuses []string
	for _, r := range recs {
		statuses = append(statuses, r["status"].(string))
		if r["instrument"] == "" || r["timestamp"] == "" {
			t.Errorf("record missing core fields: %v", r)
		}
	}
	if got := strings.Join(statuses, ","); !strings.Contains(got, "submitted") || !strings.Contains(got, "cancelled") {
		t.Errorf("statuses = %s", got)
	}
}

func TestOrderLog_FileLoggerAppendsAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.log")
	for i := 0; i < 2; i++ {
		l, err := orders.NewLogger(path, 0.05)
		if err != nil {
			t.Fatal(err)
		}
		l.LogReconciled(samplePosition(), 50, orders.MarketContext{}, orders.GEXContext{})
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
