package resilience

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type State string

const (
	StateClosed State = "closed"
	StateOpen   State = "open"
	StateHalf   State = "half_open"
)

type CircuitBreaker struct {
	mu         sync.Mutex
	state      State
	failures   int
	threshold  int
	openFor    time.Duration
	openedAt   time.Time
	probeTaken bool
}

func NewCircuitBreaker(threshold int, openFor time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if openFor <= 0 {
		openFor = 10 * time.Second
	}
	return &CircuitBreaker{state: StateClosed, threshold: threshold, openFor: openFor}
}

func (breaker *CircuitBreaker) Allow(now time.Time) error {
	if breaker == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	switch breaker.state {
	case StateClosed:
		return nil
	case StateOpen:
		if now.Sub(breaker.openedAt) < breaker.openFor {
			return ErrCircuitOpen
		}
		if breaker.probeTaken {
			return ErrCircuitOpen
		}
		breaker.state = StateHalf
		breaker.probeTaken = true
		return nil
	case StateHalf:
		if breaker.probeTaken {
			return ErrCircuitOpen
		}
		breaker.probeTaken = true
		return nil
	default:
		breaker.state = StateClosed
		return nil
	}
}

func (breaker *CircuitBreaker) Success() {
	if breaker == nil {
		return
	}
	breaker.mu.Lock()
	breaker.state = StateClosed
	breaker.failures = 0
	breaker.openedAt = time.Time{}
	breaker.probeTaken = false
	breaker.mu.Unlock()
}

func (breaker *CircuitBreaker) Failure(now time.Time) {
	if breaker == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	if breaker.state == StateHalf || breaker.failures+1 >= breaker.threshold {
		breaker.state = StateOpen
		breaker.openedAt = now
		breaker.probeTaken = false
		breaker.failures = breaker.threshold
		return
	}
	breaker.failures++
}

func (breaker *CircuitBreaker) State() State {
	if breaker == nil {
		return StateClosed
	}
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	return breaker.state
}

