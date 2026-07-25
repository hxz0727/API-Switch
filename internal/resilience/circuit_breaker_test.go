package resilience

import (
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	if cb.State() != StateClosed {
		t.Fatalf("expected initial state Closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_Trip(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Should stay closed after 2 failures
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after 2 failures, got %s", cb.State())
	}

	// Should trip to open after 3rd failure
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open after 3 failures, got %s", cb.State())
	}

	// Should reject requests when open
	if cb.Allow() {
		t.Fatal("expected Allow() to return false when open")
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Trip the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after timeout, got %s", cb.State())
	}

	// Should allow requests in half-open
	if !cb.Allow() {
		t.Fatal("expected Allow() to return true in half-open")
	}
}

func TestCircuitBreaker_Recovery(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Trip the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond) // Wait for timeout

	// Trigger transition to half-open by calling State()
	cb.State()
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after timeout, got %s", cb.State())
	}

	// Record successes in half-open
	cb.RecordSuccess()
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after 1 success, got %s", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after 2 successes, got %s", cb.State())
	}
}

func TestCircuitBreaker_ReTripFromHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Trip the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond) // Wait for timeout

	// Fail in half-open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_ResetOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Record some failures but not enough to trip
	cb.RecordFailure()
	cb.RecordFailure()

	// Record success - should reset failure count
	cb.RecordSuccess()

	// Should need 3 more failures to trip
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after 2 failures (reset by success), got %s", cb.State())
	}
}
