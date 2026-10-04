package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gw "optionsbot/internal/gateway"
	"optionsbot/internal/strategy"
)

// Regression for 2026-10-03: the BTC bot's connection dropped, and dropped
// again while the reconnect was restoring its subscriptions. The second
// drop was ignored (a reconnect was "already running"), the first gave up
// after the failed restore, and every call then waited forever.

func TestGateway_DropDuringResubscribeStillReconnects(t *testing.T) {
	m := newMockDeribit(t)
	var subscribes atomic.Int32
	m.on("public/subscribe", func(gw.JSONRPCRequest) mockReply {
		if subscribes.Add(1) == 2 { // the restore after the first drop
			return mockReply{closeConn: true}
		}
		return mockReply{}
	})
	cfg := testGatewayConfig()
	cfg.Heartbeat.ReconnectMaxAttempts = 5
	g := connectGateway(t, m, cfg)
	ctx := context.Background()
	channels := []string{"ticker.BTC-PERPETUAL.100ms", "deribit_price_index.btc_usd"}
	if err := g.Subscribe(ctx, channels); err != nil {
		t.Fatal(err)
	}

	m.dropConnections() // first drop; the restore then hits a second drop

	eventually(t, 3*time.Second, "subscriptions restored on a third connection", func() bool { return subscribes.Load() >= 3 })
	last := m.requests("public/subscribe")[subscribes.Load()-1]
	params, _ := json.Marshal(last.Params)
	for _, ch := range channels {
		if !strings.Contains(string(params), ch) {
			t.Errorf("restore after the second drop is missing %s: %s", ch, params)
		}
	}
	if _, err := g.Call(ctx, "public/test", map[string]any{}, gw.PriorityLow); err != nil {
		t.Fatalf("the gateway must be usable after the double drop: %v", err)
	}
	select {
	case err := <-g.Fatal():
		t.Fatalf("no fatal expected: %v", err)
	default:
	}
}

func TestGateway_FailedResubscribeCountsAsFailedAttempt(t *testing.T) {
	m := newMockDeribit(t)
	var subscribes atomic.Int32
	m.on("public/subscribe", func(gw.JSONRPCRequest) mockReply {
		if subscribes.Add(1) == 2 {
			return mockReply{err: &gw.RPCError{Code: 10028, Message: "too_many_requests"}}
		}
		return mockReply{}
	})
	cfg := testGatewayConfig()
	cfg.Retry.MaxRetries = 0 // the restore fails once, for good
	g := connectGateway(t, m, cfg)
	if err := g.Subscribe(context.Background(), []string{"deribit_price_index.btc_usd"}); err != nil {
		t.Fatal(err)
	}

	m.dropConnections()

	eventually(t, 3*time.Second, "restore retried on a fresh connection", func() bool { return subscribes.Load() >= 3 })
	if n := m.count("public/auth"); n < 3 {
		t.Errorf("a failed restore must reconnect again: %d auths", n)
	}
}

func TestGateway_CallNeverWaitsForeverWithoutAConnection(t *testing.T) {
	// Never connected: nothing will ever send the request. The caller must
	// still get an answer, bounded by the request timeout.
	g := gw.New(testGatewayConfig(), gw.WithRequestTimeout(100*time.Millisecond))
	start := time.Now()
	_, err := g.Call(context.Background(), "private/get_order_state", map[string]any{}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrRequestTimeout) {
		t.Fatalf("want ErrRequestTimeout, got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("caller waited %v", d)
	}
}

// ── Decision-loop watchdog ──────────────────────────────────────────────────

func TestWatchProgress(t *testing.T) {
	run := func(progress func() (time.Time, bool)) (fired bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		strategy.WatchProgress(ctx, progress, 50*time.Millisecond, 10*time.Millisecond, func(time.Duration) { fired = true })
		return fired
	}
	stuck := time.Now()
	if !run(func() (time.Time, bool) { return stuck, false }) {
		t.Error("a loop with no progress past the limit must be reported")
	}
	if run(func() (time.Time, bool) { return time.Now(), false }) {
		t.Error("a cycling loop must not be reported")
	}
	if run(func() (time.Time, bool) { return stuck, true }) {
		t.Error("a halted bot (kill switch) is idle on purpose: restarting it would resume trading")
	}
	if run(func() (time.Time, bool) { return time.Time{}, false }) {
		t.Error("not started yet is not a stall")
	}
}

func TestStrategy_ProgressAdvancesWithCycles(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()
	first, halted := time.Time{}, false
	eventually(t, 2*time.Second, "started", func() bool { first, halted = f.strat.Progress(); return !first.IsZero() })
	eventually(t, 2*time.Second, "advanced", func() bool { last, _ := f.strat.Progress(); return last.After(first) })
	if halted {
		t.Error("not halted")
	}
}
