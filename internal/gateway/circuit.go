package gateway

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"optionsbot/internal/config"
)

var ErrCircuitOpen = errors.New("circuit breaker open")

type cbState int

const (
	cbClosed   cbState = iota
	cbOpen
	cbHalfOpen
)

// CircuitBreaker implements a three-state circuit breaker.
type CircuitBreaker struct {
	mu         sync.Mutex
	state      cbState
	failures   int
	threshold  int
	openUntil  time.Time
	openDur    time.Duration
}

func NewCircuitBreaker(cfg config.CircuitConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:     cbClosed,
		threshold: cfg.Threshold,
		openDur:   time.Duration(cfg.OpenSec) * time.Second,
	}
}

func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbOpen:
		if time.Now().After(cb.openUntil) {
			cb.state = cbHalfOpen
			return nil
		}
		return ErrCircuitOpen
	default:
		return nil
	}
}

func (cb *CircuitBreaker) Success() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = cbClosed
}

func (cb *CircuitBreaker) Failure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == cbHalfOpen || cb.failures >= cb.threshold {
		cb.state = cbOpen
		cb.openUntil = time.Now().Add(cb.openDur)
		slog.Warn("circuit breaker opened",
			"failures", cb.failures,
			"open_until", cb.openUntil)
	}
}

func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

func (cb *CircuitBreaker) Failures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}
