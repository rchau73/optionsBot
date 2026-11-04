package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/config"
	gw "optionsbot/internal/gateway"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// captureLogs sends every slog line to a buffer until the test ends and
// returns a reader of what was logged so far.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	lw := &lockedWriter{w: &buf}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(lw, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string {
		lw.mu.Lock()
		defer lw.mu.Unlock()
		return buf.String()
	}
}

func TestLoopAge_WallClockAgeAndLateAfterTwoCycles(t *testing.T) {
	now := time.Date(2026, 11, 2, 11, 50, 55, 0, time.UTC)
	every := 40 * time.Second
	cases := []struct {
		name    string
		last    time.Time
		halted  bool
		age     time.Duration
		late    bool
		comment string
	}{
		{"no cycle yet", time.Time{}, false, 0, false, "zero loop_at has no age"},
		{"one cycle old", now.Add(-every), false, every, false, ""},
		{"exactly two cycles", now.Add(-2 * every), false, 2 * every, false, "the limit itself is on time"},
		{"one second past two cycles", now.Add(-2*every - time.Second), false, 2*every + time.Second, true, ""},
		{"stable ETH 2026-11-02", now.Add(-61*time.Minute - 4*time.Second), false, 61*time.Minute + 4*time.Second, true, "the issue's 61 min"},
		{"halted by the kill switch", now.Add(-time.Hour), true, time.Hour, false, "idle on purpose"},
		{"clock behind loop_at", now.Add(time.Second), false, 0, false, "never negative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strategy.LoopAge(c.last, now); got != c.age {
				t.Errorf("LoopAge = %v, want %v %s", got, c.age, c.comment)
			}
			if got := strategy.LoopLate(c.last, c.halted, now, every); got != c.late {
				t.Errorf("LoopLate = %v, want %v %s", got, c.late, c.comment)
			}
		})
	}
	if strategy.LoopLate(now.Add(-time.Hour), false, now, 0) {
		t.Error("no interval configured: never late")
	}
}

func TestLoopAge_SuspendedForNeedsAMinuteOfWallClockBeyondTheProcess(t *testing.T) {
	cases := []struct {
		wall, mono, want time.Duration
	}{
		{10 * time.Second, 10 * time.Second, 0},                             // awake
		{10*time.Second + 59*time.Second, 10 * time.Second, 0},              // under a minute: an NTP step, not a sleep
		{10*time.Second + time.Minute, 10 * time.Second, time.Minute},       // the limit
		{49*time.Minute + 6*time.Second, 6 * time.Second, 49 * time.Minute}, // stable BTC's 6 s backoff that took 49 min
		{5 * time.Second, 10 * time.Second, 0},                              // wall clock stepped back
	}
	for _, c := range cases {
		if got := strategy.SuspendedFor(c.wall, c.mono); got != c.want {
			t.Errorf("SuspendedFor(%v, %v) = %v, want %v", c.wall, c.mono, got, c.want)
		}
	}
}

// One call can never hold a cycle for two intervals: the gateway's bound
// (its reply timeout plus the caller's hard second) is under it.
func TestLoopAge_GatewayRequestTimeoutIsUnderTwoCycles(t *testing.T) {
	twoCycles := strategy.LoopLateCycles * time.Duration(config.FixedEvalIntervalMS) * time.Millisecond
	if bound := gw.RequestTimeout + time.Second; bound >= twoCycles {
		t.Errorf("a call may wait %v, not under 2 cycles (%v)", bound, twoCycles)
	}
}

// Scenario: a blocked cycle is bounded — a call in the cycle never answers;
// it times out, an ERROR line names it, and the cycle ends.
func TestLoopAge_BlockedCallTimesOutWithAnErrorNamingIt(t *testing.T) {
	logs := captureLogs(t) // before New: the gateway keeps the default logger
	m := newMockDeribit(t)
	m.on("private/get_account_summary", func(gw.JSONRPCRequest) mockReply { return mockReply{noReply: true} })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	timeout := 100 * time.Millisecond
	g := gw.New(testGatewayConfig(), gw.WithEndpoint(m.url()), gw.WithRequestTimeout(timeout))
	if err := g.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	start := time.Now()
	_, err := g.Call(ctx, "private/get_account_summary", map[string]any{"currency": "ETH"}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrRequestTimeout) {
		t.Fatalf("want ErrRequestTimeout, got %v", err)
	}
	if waited := time.Since(start); waited > 2*timeout+time.Second {
		t.Errorf("the call waited %v, more than its bound", waited)
	}
	var line string
	for _, l := range strings.Split(logs(), "\n") {
		if strings.Contains(l, `"msg":"request timed out"`) {
			line = l
		}
	}
	if !strings.Contains(line, `"level":"ERROR"`) || !strings.Contains(line, `"method":"private/get_account_summary"`) {
		t.Errorf("want an ERROR line naming the call, got %q", line)
	}
}

// Scenario: a blocked cycle is bounded — every account summary waits for the
// gateway's timeout and fails; the cycles still end, loop_at advances, and
// the watchdog logs the late loop as an ERROR.
func TestLoopAge_LoopAdvancesWhileEveryCallTimesOut(t *testing.T) {
	logs := captureLogs(t)
	f := newStrategyFixture(t)
	f.exch.summaryDelay = 30 * time.Millisecond // 3 intervals of the fixture's 10 ms
	f.exch.summaryErr = fmt.Errorf("private/get_account_summary: %w (never answered)", gw.ErrRequestTimeout)
	f.startRun()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go strategy.WatchLoopAge(ctx, f.strat.Progress, time.Duration(f.cfg.EvalIntervalMS)*time.Millisecond, 5*time.Millisecond)

	seen := map[time.Time]bool{}
	eventually(t, 3*time.Second, "loop_at advances over several cycles", func() bool {
		if at := f.strat.View().LoopAt; !at.IsZero() {
			seen[at] = true
		}
		return len(seen) >= 3
	})
	eventually(t, 2*time.Second, "late loop logged", func() bool {
		return strings.Contains(logs(), `"msg":"decision loop late: no cycle completed, exits are not being checked"`)
	})
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no margin data, no entry: got %d sells", n)
	}
}

// The watchdog logs a late loop once per episode, then once when it is on
// time again; a halted loop is never late.
func TestLoopAge_WatchdogLogsOnceAndRecovers(t *testing.T) {
	logs := captureLogs(t)
	mu := make(chan struct{}, 1) // a lock the progress reader and set share
	mu <- struct{}{}
	last, halted := time.Now().Add(-time.Hour), false
	progress := func() (time.Time, bool) {
		<-mu
		defer func() { mu <- struct{}{} }()
		return last, halted
	}
	set := func(l time.Time, h bool) {
		<-mu
		last, halted = l, h
		mu <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go strategy.WatchLoopAge(ctx, progress, 40*time.Second, 2*time.Millisecond)

	lateMsg := `"msg":"decision loop late: no cycle completed, exits are not being checked"`
	eventually(t, 2*time.Second, "late logged", func() bool { return strings.Contains(logs(), lateMsg) })
	time.Sleep(20 * time.Millisecond) // many more checks
	if n := strings.Count(logs(), lateMsg); n != 1 {
		t.Errorf("late logged %d times in one episode, want 1", n)
	}
	set(time.Now(), false)
	eventually(t, 2*time.Second, "recovery logged", func() bool {
		return strings.Contains(logs(), `"msg":"decision loop on time again"`)
	})
	set(time.Now().Add(-time.Hour), true)
	time.Sleep(20 * time.Millisecond)
	if n := strings.Count(logs(), lateMsg); n != 1 {
		t.Errorf("a halted loop is not late: late logged %d times", n)
	}
}

// /api/status carries loop_age_sec and loop_stale for the monitor header and
// the SME collection.
func TestLoopAge_StatusShowsAgeAndStale(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.EvalIntervalMS = 10_000 // no cycle after startup during this test
	f.startRun()
	eventually(t, 2*time.Second, "startup published", func() bool { return !f.strat.View().LoopAt.IsZero() })
	v := f.strat.View()
	if v.LoopStale || v.LoopAgeSec != 0 {
		t.Errorf("fresh loop: loop_stale=%v loop_age_sec=%d", v.LoopStale, v.LoopAgeSec)
	}
	h := api.New(f.strat, orders.NewWriterLogger(io.Discard, 0.05)).Handler()
	_, body := getJSON(t, h, "/api/status")
	if _, ok := body["loop_age_sec"].(float64); !ok {
		t.Errorf("loop_age_sec = %v", body["loop_age_sec"])
	}
	if stale, ok := body["loop_stale"].(bool); !ok || stale {
		t.Errorf("loop_stale = %v", body["loop_stale"])
	}
}

// Scenario: exits are never skipped — a short leg reaches its stop-loss while
// the gateway is reconnecting; once the connection returns the stop-loss is
// sent in the next cycles and the leg is closed.
func TestLoopAge_StopLossFiresAfterReconnect(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.01) // mark 0.02: 1× premium, no exit yet
	f.market.setQuote(f.call, 0.019, 0.021)
	f.startRun()
	eventually(t, 2*time.Second, "book loaded", func() bool { return f.position(f.call) != nil })
	time.Sleep(30 * time.Millisecond)
	if n := len(f.exch.buys()); n != 0 {
		t.Fatalf("no exit expected before the move, got %d buys", n)
	}

	f.exch.setDown(true)
	f.market.setQuote(f.call, 0.039, 0.041) // mid 0.04 = 4× premium: stop-loss
	time.Sleep(50 * time.Millisecond)       // several cycles while every call fails
	if f.position(f.call) == nil {
		t.Fatal("a stop that could not be sent must leave the leg tracked")
	}

	f.exch.setDown(false)
	eventually(t, 3*time.Second, "stop-loss sent and the call closed after the reconnect", func() bool {
		return f.position(f.call) == nil
	})
	var stop *orders.Order
	for _, o := range f.exch.buys() {
		if o.Instrument == f.call {
			stop = &o
			break
		}
	}
	if stop == nil || stop.OrderType != orders.TypeMarket || stop.TriggerReason != orders.TriggerStopLoss200Pct {
		t.Errorf("the call must be closed by a stop-loss market order, got %+v", stop)
	}
}
