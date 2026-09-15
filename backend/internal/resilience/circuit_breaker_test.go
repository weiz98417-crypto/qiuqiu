package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerOpensAndAllowsOneProbe(t *testing.T) {
	start := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker(2, time.Second)
	breaker.Failure(start)
	breaker.Failure(start)
	if breaker.State() != StateOpen {
		t.Fatalf("state = %q, want open", breaker.State())
	}
	if err := breaker.Allow(start.Add(500 * time.Millisecond)); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Allow before timeout = %v", err)
	}
	if err := breaker.Allow(start.Add(2 * time.Second)); err != nil {
		t.Fatalf("probe Allow = %v", err)
	}
	if err := breaker.Allow(start.Add(2 * time.Second)); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("second probe Allow = %v", err)
	}
	breaker.Success()
	if breaker.State() != StateClosed {
		t.Fatalf("state after success = %q, want closed", breaker.State())
	}
}

