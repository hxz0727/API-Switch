package resilience

import (
	"testing"
	"time"
)

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half_open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	if cfg.FailureThreshold != 5 {
		t.Errorf("expected FailureThreshold=5, got %d", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 3 {
		t.Errorf("expected SuccessThreshold=3, got %d", cfg.SuccessThreshold)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected Timeout=30s, got %s", cfg.Timeout)
	}
}

func TestCircuitBreaker_Allow_HalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: 10 * time.Millisecond}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // trip open
	time.Sleep(20 * time.Millisecond)

	// Allow() in half-open returns true to let one test request through.
	if !cb.Allow() {
		t.Fatal("expected Allow() to return true in half-open")
	}
}

func TestCircuitBreaker_Allow_Open_CountsRejected(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: time.Hour}
	cb := NewCircuitBreaker(cfg)
	cb.RecordFailure() // trip open

	if cb.Allow() {
		t.Fatal("expected Allow() to return false in open state")
	}
	if cb.Allow() {
		t.Fatal("expected Allow() to return false in open state (second call)")
	}

	stats := cb.Stats()
	if stats["total_rejected"].(int64) != 2 {
		t.Errorf("expected total_rejected=2, got %v", stats["total_rejected"])
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 2, SuccessThreshold: 2, Timeout: time.Hour}
	cb := NewCircuitBreaker(cfg)

	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()
	if stats["state"] != "open" {
		t.Errorf("expected state open, got %v", stats["state"])
	}
	if stats["failure_count"].(int64) != 2 {
		t.Errorf("expected failure_count=2, got %v", stats["failure_count"])
	}
	if stats["total_failures"].(int64) != 2 {
		t.Errorf("expected total_failures=2, got %v", stats["total_failures"])
	}
	if stats["total_successes"].(int64) != 1 {
		t.Errorf("expected total_successes=1, got %v", stats["total_successes"])
	}
	if stats["success_count"].(int64) != 0 {
		t.Errorf("expected success_count=0, got %v", stats["success_count"])
	}
	if stats["last_failure"].(time.Time).IsZero() {
		t.Error("expected last_failure to be set")
	}
	if stats["last_state_change"].(time.Time).IsZero() {
		t.Error("expected last_state_change to be set")
	}
}

func TestCircuitBreaker_String(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	s := cb.String()
	if s == "" {
		t.Fatal("expected non-empty String() output")
	}
}

func TestCircuitBreaker_ClosedSuccess_DoesNotAccumulateSuccessCount(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 3, SuccessThreshold: 2, Timeout: time.Hour}
	cb := NewCircuitBreaker(cfg)

	// Success in closed state must not increment success_count (used only in half-open).
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Fatalf("expected Closed, got %s", cb.State())
	}
	stats := cb.Stats()
	if stats["success_count"].(int64) != 0 {
		t.Errorf("expected success_count=0 in closed state, got %v", stats["success_count"])
	}
}

func TestCircuitBreaker_OpenTimeout_TransitionsInStateCall(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: 10 * time.Millisecond}
	cb := NewCircuitBreaker(cfg)
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected Open before timeout, got %s", cb.State())
	}

	time.Sleep(20 * time.Millisecond)

	// Calling State() after the timeout transitions open -> half-open.
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after timeout, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_SuccessCountsTowardThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 3, Timeout: 10 * time.Millisecond}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %s", cb.State())
	}

	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected still HalfOpen after 2 successes, got %s", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after threshold successes, got %s", cb.State())
	}

	// Transitioning to closed resets the counts.
	stats := cb.Stats()
	if stats["success_count"].(int64) != 0 || stats["failure_count"].(int64) != 0 {
		t.Errorf("expected counts reset to 0 after closing, got success_count=%v failure_count=%v",
			stats["success_count"], stats["failure_count"])
	}
}
