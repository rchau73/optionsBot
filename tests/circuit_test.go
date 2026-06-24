package tests

import (
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
)

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cfg := config.CircuitConfig{Threshold: 3, OpenSec: 60}
	cb := gateway.NewCircuitBreaker(cfg)

	for i := 0; i < 3; i++ {
		if err := cb.Allow(); err != nil {
			t.Fatalf("circuit should be closed at failure %d", i)
		}
		cb.Failure()
	}

	if err := cb.Allow(); err == nil {
		t.Fatal("circuit should be open after threshold failures")
	}
	if cb.State() != "open" {
		t.Fatalf("expected state 'open', got %q", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterProbeSuccess(t *testing.T) {
	cfg := config.CircuitConfig{Threshold: 2, OpenSec: 1}
	cb := gateway.NewCircuitBreaker(cfg)

	cb.Failure()
	cb.Failure()

	if cb.State() != "open" {
		t.Fatal("should be open after 2 failures")
	}

	// Wait for open period to pass
	time.Sleep(1100 * time.Millisecond)

	if err := cb.Allow(); err != nil {
		t.Fatalf("should allow probe after open period: %v", err)
	}
	if cb.State() != "half_open" {
		t.Fatalf("expected half_open, got %q", cb.State())
	}

	cb.Success()
	if cb.State() != "closed" {
		t.Fatalf("expected closed after probe success, got %q", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnProbeFailure(t *testing.T) {
	cfg := config.CircuitConfig{Threshold: 2, OpenSec: 1}
	cb := gateway.NewCircuitBreaker(cfg)

	cb.Failure()
	cb.Failure()
	time.Sleep(1100 * time.Millisecond)

	_ = cb.Allow() // probe: transitions to half_open
	cb.Failure()   // probe fails: back to open

	if cb.State() != "open" {
		t.Fatalf("expected open after probe failure, got %q", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cfg := config.CircuitConfig{Threshold: 5, OpenSec: 60}
	cb := gateway.NewCircuitBreaker(cfg)

	cb.Failure()
	cb.Failure()
	cb.Success() // resets

	if cb.Failures() != 0 {
		t.Fatalf("expected 0 failures after success, got %d", cb.Failures())
	}
	if cb.State() != "closed" {
		t.Fatalf("expected closed, got %q", cb.State())
	}
}
