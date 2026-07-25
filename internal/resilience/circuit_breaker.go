package resilience

import (
	"fmt"
	"sync"
	"time"

	"github.com/hxz0727/API-Switch/internal/logutil"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// StateClosed is the normal operating state. Requests pass through.
	StateClosed CircuitState = iota
	// StateOpen means the circuit is tripped. Requests are rejected.
	StateOpen
	//StateHalfOpen means the circuit is testing if the provider has recovered.
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the circuit breaker behavior.
type CircuitBreakerConfig struct {
	FailureThreshold int64         // Number of consecutive failures to trip the circuit. Default: 5
	SuccessThreshold int64         // Number of consecutive successes in half-open to close. Default: 3
	Timeout          time.Duration // How long the circuit stays open before half-open. Default: 30s
}

// DefaultCircuitBreakerConfig returns a CircuitBreakerConfig with sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu              sync.Mutex
	config          CircuitBreakerConfig
	state           CircuitState
	failureCount    int64
	successCount    int64
	lastFailureTime time.Time
	lastStateChange time.Time
	totalFailures   int64
	totalSuccesses  int64
	totalRejected   int64
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config:          cfg,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if open state has timed out
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.config.Timeout {
		cb.transitionTo(StateHalfOpen)
	}

	return cb.state
}

// Allow checks if a request is allowed through the circuit breaker.
// Returns true if the request should proceed, false if it should be rejected.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if open state has timed out
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.config.Timeout {
		cb.transitionTo(StateHalfOpen)
	}

	switch cb.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		return true // Allow one request to test
	case StateOpen:
		cb.totalRejected++
		return false
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++
	cb.failureCount = 0

	switch cb.state {
	case StateClosed:
		// Already closed, nothing to do
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures++
	cb.successCount = 0
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
			logutil.Warn("Circuit breaker tripped: %d consecutive failures", cb.failureCount)
		}
	case StateHalfOpen:
		// Any failure in half-open goes back to open
		cb.transitionTo(StateOpen)
		logutil.Warn("Circuit breaker re-opened: failure in half-open state")
	}
}

// transitionTo moves the circuit breaker to a new state.
func (cb *CircuitBreaker) transitionTo(state CircuitState) {
	if cb.state != state {
		logutil.Debug("Circuit breaker: %s -> %s", cb.state, state)
		cb.state = state
		cb.lastStateChange = time.Now()
		if state == StateClosed {
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

// Stats returns circuit breaker statistics.
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return map[string]interface{}{
		"state":            cb.state.String(),
		"failure_count":    cb.failureCount,
		"success_count":    cb.successCount,
		"total_failures":   cb.totalFailures,
		"total_successes":  cb.totalSuccesses,
		"total_rejected":   cb.totalRejected,
		"last_failure":     cb.lastFailureTime,
		"last_state_change": cb.lastStateChange,
	}
}

// String returns a human-readable representation of the circuit breaker state.
func (cb *CircuitBreaker) String() string {
	state := cb.State()
	return fmt.Sprintf("CircuitBreaker{state=%s, failures=%d, successes=%d}", state, cb.failureCount, cb.successCount)
}
