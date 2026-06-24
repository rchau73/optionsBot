package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"optionsbot/internal/config"
	gw "optionsbot/internal/gateway"
)

func TestWithRetry_SucceedFirstAttempt(t *testing.T) {
	calls := 0
	err := gw.WithRetry(context.Background(), gw.RetryConfig(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWithRetry_NonRetryableErrorPropagatesImmediately(t *testing.T) {
	sentinel := errors.New("non-retryable")
	calls := 0
	err := gw.WithRetry(context.Background(), gw.RetryConfig(), func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
}

func TestWithRetry_MaxRetriesExceeded(t *testing.T) {
	cfg := config.RetryConfig{InitialMS: 5, Multiplier: 1.5, MaxMS: 50, MaxRetries: 3}
	calls := 0
	err := gw.WithRetry(context.Background(), cfg, func() error {
		calls++
		return &gw.RPCError{Code: 10028, Message: "rate limited"}
	})
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if calls != 4 { // initial + 3 retries
		t.Fatalf("expected 4 calls, got %d", calls)
	}
}

func TestWithRetry_BackoffIntervals(t *testing.T) {
	cfg := config.RetryConfig{InitialMS: 20, Multiplier: 2.0, MaxMS: 200, MaxRetries: 3}
	var timestamps []time.Time
	gw.WithRetry(context.Background(), cfg, func() error {
		timestamps = append(timestamps, time.Now())
		return &gw.RPCError{Code: 10028, Message: "rate limited"}
	})

	if len(timestamps) < 2 {
		t.Fatal("need at least 2 timestamps to measure intervals")
	}
	// Each interval should be >= 0 (jitter can be 0) and <= max_backoff
	for i := 1; i < len(timestamps); i++ {
		interval := timestamps[i].Sub(timestamps[i-1])
		if interval > 200*time.Millisecond+50*time.Millisecond {
			t.Errorf("interval %d too long: %v", i, interval)
		}
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := gw.WithRetry(ctx, gw.RetryConfig(), func() error {
		return &gw.RPCError{Code: 10028}
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}
