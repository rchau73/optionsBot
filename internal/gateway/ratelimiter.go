package gateway

import (
	"context"

	"golang.org/x/time/rate"

	"optionsbot/internal/config"
)

// RateLimiter wraps separate token-bucket limiters for each API scope.
type RateLimiter struct {
	nonMatch *rate.Limiter
	match    *rate.Limiter
	orderOps *rate.Limiter
	rest     *rate.Limiter
}

func NewRateLimiter(cfg config.RateLimitConfig) *RateLimiter {
	f := cfg.SafetyFactor
	return &RateLimiter{
		nonMatch: rate.NewLimiter(rate.Limit(cfg.WsNonMatchRPS*f), 1),
		match:    rate.NewLimiter(rate.Limit(cfg.WsMatchRPS*f), 1),
		orderOps: rate.NewLimiter(rate.Limit(cfg.OrderOpsRPS*f), 1),
		rest:     rate.NewLimiter(rate.Limit(cfg.RestRPS*f), 1),
	}
}

func (r *RateLimiter) WaitNonMatch(ctx context.Context) error {
	return r.nonMatch.Wait(ctx)
}

func (r *RateLimiter) WaitMatch(ctx context.Context) error {
	return r.match.Wait(ctx)
}

func (r *RateLimiter) WaitOrderOps(ctx context.Context) error {
	return r.orderOps.Wait(ctx)
}

func (r *RateLimiter) WaitRest(ctx context.Context) error {
	return r.rest.Wait(ctx)
}

// Tokens returns available tokens for metrics reporting.
func (r *RateLimiter) Tokens() (nonMatch, match float64) {
	return r.nonMatch.Tokens(), r.match.Tokens()
}
