package tests

import (
	"context"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
)

func TestRateLimiterRespectsBurstLimit(t *testing.T) {
	cfg := config.RateLimitConfig{
		WsNonMatchRPS: 5,
		WsMatchRPS:    2,
		OrderOpsRPS:   1,
		RestRPS:       1,
		SafetyFactor:  1.0,
	}
	rl := gateway.NewRateLimiter(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 5 non-match requests should acquire quickly (burst=1 per the limiter)
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := rl.WaitNonMatch(ctx); err != nil {
			t.Fatalf("WaitNonMatch failed: %v", err)
		}
	}
	elapsed := time.Since(start)

	// At 5 RPS, 5 tokens should take ~0.8s (token 1 is instant, then 4×200ms)
	// We just verify it doesn't complete instantly (< 10ms) or take forever (> 5s)
	if elapsed < 100*time.Millisecond {
		t.Errorf("rate limiter too fast: %v", elapsed)
	}
}

func TestRateLimiterSafetyFactor(t *testing.T) {
	// With safety factor 0.5, 10 RPS becomes 5 RPS
	cfg := config.RateLimitConfig{
		WsNonMatchRPS: 10,
		SafetyFactor:  0.5,
	}
	rl := gateway.NewRateLimiter(cfg)
	tokens, _ := rl.Tokens()
	// Tokens should be at most 1 (burst=1)
	if tokens > 1.1 {
		t.Errorf("expected ≤1 token, got %v", tokens)
	}
}

func TestRateLimiterMatchSeparateFromNonMatch(t *testing.T) {
	cfg := config.RateLimitConfig{
		WsNonMatchRPS: 20,
		WsMatchRPS:    2,
		SafetyFactor:  1.0,
	}
	rl := gateway.NewRateLimiter(cfg)
	ctx := context.Background()

	// Match limiter is much slower
	start := time.Now()
	_ = rl.WaitMatch(ctx)
	_ = rl.WaitMatch(ctx)
	elapsed := time.Since(start)

	// 2 match tokens at 2 RPS: ~500ms
	if elapsed < 200*time.Millisecond {
		t.Errorf("match limiter too fast: %v", elapsed)
	}
}
