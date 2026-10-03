package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	gw "optionsbot/internal/gateway"
)

func TestGatewaySubscribeDeduplication(t *testing.T) {
	reg := gw.NewSubscriptionRegistry(10)

	if !reg.Add("ticker.BTC-PERPETUAL.100ms") {
		t.Fatal("first add should succeed")
	}
	if reg.Add("ticker.BTC-PERPETUAL.100ms") {
		t.Fatal("duplicate add should return false")
	}
	if reg.Count() != 1 {
		t.Fatalf("expected count 1, got %d", reg.Count())
	}
}

func TestGatewaySubscribeCapacity(t *testing.T) {
	reg := gw.NewSubscriptionRegistry(3)
	for i := 0; i < 3; i++ {
		ch := strings.Repeat("x", i+1)
		if !reg.Add(ch) {
			t.Fatalf("add %d failed unexpectedly", i)
		}
	}
	if reg.Add("overflow") {
		t.Fatal("should reject subscription over capacity")
	}
}

func TestRetryOnRateLimitError(t *testing.T) {
	// Verify that WithRetry retries on rate-limit codes and succeeds on 3rd attempt
	attempt := 0
	cfg := gw.RetryConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := gw.WithRetry(ctx, cfg, func() error {
		attempt++
		if attempt < 3 {
			return &gw.RPCError{Code: 10028, Message: "rate limited"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempt)
	}
}
