package gateway

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"optionsbot/internal/config"
)

// Deribit error codes the gateway reacts to.
const (
	codeTooManyRequests      = 10028
	codeRetry                = 10040
	codeSettlementInProgress = 10041
	codeSystemMaintenance    = 11051
	codeTimedOut             = 13888
)

// isRateLimitError reports whether Deribit asked us to slow down.
func isRateLimitError(err error) bool {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc.Code == codeTooManyRequests || rpc.Code == codeRetry
	}
	return false
}

// isExchangeUnhealthy reports whether an RPC error means the exchange itself
// is struggling, as opposed to rejecting our request on its merits (bad price,
// not enough funds). Only the former should count towards the circuit breaker.
func isExchangeUnhealthy(rpc *RPCError) bool {
	switch rpc.Code {
	case codeTooManyRequests, codeRetry, codeSettlementInProgress, codeSystemMaintenance, codeTimedOut:
		return true
	}
	return false
}

// isIdempotent reports whether a method is safe to send twice. Order placement
// is not: a "failed" sell may have reached the book, and a retry would double it.
func isIdempotent(method string) bool {
	return strings.HasPrefix(method, "public/") || strings.HasPrefix(method, "private/get_")
}

// WithRetry executes fn with exponential backoff and full jitter on rate-limit errors.
func WithRetry(ctx context.Context, cfg config.RetryConfig, fn func() error) error {
	backoff := time.Duration(cfg.InitialMS) * time.Millisecond
	maxBackoff := time.Duration(cfg.MaxMS) * time.Millisecond
	if backoff <= 0 {
		backoff = time.Millisecond // rand.Int63n panics on 0
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRateLimitError(err) {
			return err
		}

		// Full jitter: sleep a random duration in [0, backoff).
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
