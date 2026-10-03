package gateway

import (
	"context"
	"strings"

	"golang.org/x/time/rate"

	"optionsbot/internal/config"
)

// RateLimiter mirrors Deribit's two credit pools: matching-engine requests
// (orders, edits, cancels) and everything else. Each pool is a token bucket
// scaled down by SafetyFactor to stay clear of the exchange's hard limit.
type RateLimiter struct {
	nonMatch *rate.Limiter
	match    *rate.Limiter
}

func NewRateLimiter(cfg config.RateLimitConfig) *RateLimiter {
	f := cfg.SafetyFactor
	return &RateLimiter{
		nonMatch: rate.NewLimiter(rate.Limit(cfg.WsNonMatchRPS*f), 1),
		match:    rate.NewLimiter(rate.Limit(cfg.WsMatchRPS*f), 1),
	}
}

// Wait blocks until the pool that method draws from has a token.
func (r *RateLimiter) Wait(ctx context.Context, method string) error {
	if IsMatchingEngine(method) {
		return r.match.Wait(ctx)
	}
	return r.nonMatch.Wait(ctx)
}

func (r *RateLimiter) WaitNonMatch(ctx context.Context) error {
	return r.nonMatch.Wait(ctx)
}

func (r *RateLimiter) WaitMatch(ctx context.Context) error {
	return r.match.Wait(ctx)
}

// Tokens returns available tokens for metrics reporting.
func (r *RateLimiter) Tokens() (nonMatch, match float64) {
	return r.nonMatch.Tokens(), r.match.Tokens()
}

// IsMatchingEngine reports whether method is charged against Deribit's
// matching-engine credit pool (anything that creates, edits or cancels orders).
func IsMatchingEngine(method string) bool {
	switch method {
	case "private/buy", "private/sell", "private/edit", "private/close_position":
		return true
	}
	return strings.HasPrefix(method, "private/cancel")
}
