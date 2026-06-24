package gateway

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"optionsbot/internal/config"
)

// isRateLimitError returns true for Deribit rate-limit and overload error codes.
func isRateLimitError(err error) bool {
	if rpc, ok := err.(*RPCError); ok {
		return rpc.Code == 10028 || rpc.Code == 10040
	}
	return false
}

// WithRetry executes fn with exponential backoff and full jitter on rate-limit errors.
func WithRetry(ctx context.Context, cfg config.RetryConfig, fn func() error) error {
	backoff := time.Duration(cfg.InitialMS) * time.Millisecond
	maxBackoff := time.Duration(cfg.MaxMS) * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
			if !isRateLimitError(err) {
				return err
			}
		}

		// Full jitter: sleep random duration in [0, backoff)
		jitter := time.Duration(rand.Int63n(int64(backoff)))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return ctx.Err()
		}

		backoff = min(time.Duration(float64(backoff)*cfg.Multiplier), maxBackoff)
	}
	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
