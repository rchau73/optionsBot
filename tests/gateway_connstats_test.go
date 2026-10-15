package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	gw "optionsbot/internal/gateway"
)

// Scenario: a drop is visible — the gateway loses the connection, reconnects,
// and its stats show the drop and the reconnect (#87).
func TestGateway_DropIsVisibleAfterReconnect(t *testing.T) {
	m := newMockDeribit(t)
	g := connectGateway(t, m, testGatewayConfig())
	if s := g.ConnStats(); s.DropsToday != 0 || s.ReconnectsToday != 0 || s.LastDropAt != nil {
		t.Fatalf("fresh gateway: %+v", s)
	}

	before := time.Now()
	m.dropConnections()
	eventually(t, 3*time.Second, "reconnect counted", func() bool { return g.ConnStats().ReconnectsToday == 1 })

	s := g.ConnStats()
	if s.DropsToday != 1 {
		t.Errorf("drops_today = %d, want 1", s.DropsToday)
	}
	if s.LastDropAt == nil || s.LastDropAt.Before(before) {
		t.Errorf("last_drop_at = %v, want after %v", s.LastDropAt, before)
	}
	if s.LastDropReason == "" {
		t.Error("last_drop_reason is empty; the monitor should say why it dropped")
	}
	if s.LastReconnectAt == nil || s.LastReconnectAt.Before(*s.LastDropAt) {
		t.Errorf("last_reconnect_at = %v, want after the drop %v", s.LastReconnectAt, s.LastDropAt)
	}
	if s.Day != time.Now().UTC().Format(time.DateOnly) {
		t.Errorf("day = %q", s.Day)
	}
}

func TestGateway_TimeoutIsCounted(t *testing.T) {
	m := newMockDeribit(t)
	m.on("private/get_account_summary", func(gw.JSONRPCRequest) mockReply { return mockReply{noReply: true} })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	g := gw.New(testGatewayConfig(), gw.WithEndpoint(m.url()), gw.WithRequestTimeout(100*time.Millisecond))
	if err := g.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	_, err := g.Call(ctx, "private/get_account_summary", map[string]any{}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrRequestTimeout) {
		t.Fatalf("want ErrRequestTimeout, got %v", err)
	}
	s := g.ConnStats()
	if s.TimeoutsToday < 1 || s.LastTimeoutAt == nil {
		t.Errorf("timeout not counted: %+v", s)
	}
	if s.DropsToday != 0 {
		t.Errorf("a timeout is not a drop: drops_today = %d", s.DropsToday)
	}
}

func TestConnTracker_CountsResetAtUTCMidnight(t *testing.T) {
	var tr gw.ConnTracker
	day1 := time.Date(2026, 10, 14, 23, 50, 0, 0, time.UTC)
	tr.Drop(day1, "EOF")
	tr.Reconnect(day1.Add(time.Second))
	tr.Timeout(day1.Add(2 * time.Second))
	tr.Drop(day1.Add(5*time.Minute), "reset by peer")

	s := tr.Stats(day1.Add(6 * time.Minute))
	if s.Day != "2026-10-14" || s.DropsToday != 2 || s.ReconnectsToday != 1 || s.TimeoutsToday != 1 {
		t.Fatalf("day 1: %+v", s)
	}
	if s.LastDropReason != "reset by peer" || !s.LastDropAt.Equal(day1.Add(5*time.Minute)) {
		t.Errorf("last drop = %v %q", s.LastDropAt, s.LastDropReason)
	}

	s = tr.Stats(day1.Add(20 * time.Minute)) // 00:10 UTC next day
	if s.Day != "2026-10-15" || s.DropsToday != 0 || s.ReconnectsToday != 0 || s.TimeoutsToday != 0 {
		t.Errorf("counts must restart on a new UTC day: %+v", s)
	}
	if s.LastDropAt == nil || s.LastReconnectAt == nil || s.LastTimeoutAt == nil {
		t.Errorf("the last times stay visible across the day change: %+v", s)
	}
}

func TestConnTracker_ZeroValue(t *testing.T) {
	var tr gw.ConnTracker
	s := tr.Stats(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	if s.Day != "2026-10-15" || s.DropsToday != 0 || s.LastDropAt != nil || s.LastReconnectAt != nil || s.LastTimeoutAt != nil {
		t.Errorf("zero tracker: %+v", s)
	}
}
