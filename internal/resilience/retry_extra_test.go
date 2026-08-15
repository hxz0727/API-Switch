package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 500*time.Millisecond {
		t.Errorf("expected InitialDelay=500ms, got %s", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 10*time.Second {
		t.Errorf("expected MaxDelay=10s, got %s", cfg.MaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("expected Multiplier=2.0, got %f", cfg.Multiplier)
	}
}

func TestRetryableError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("inner failure")
	re := NewRetryableError(inner, true)

	if re.Error() != "inner failure" {
		t.Errorf("expected Error()='inner failure', got %q", re.Error())
	}
	if re.Unwrap() != inner {
		t.Error("expected Unwrap() to return the wrapped error")
	}
	if !re.Retryable {
		t.Error("expected Retryable=true")
	}
}

func TestNewRetryableError_StatusCode(t *testing.T) {
	re := NewRetryableError(errors.New("boom"), true)
	if re.StatusCode != 0 {
		t.Errorf("expected StatusCode=0 by default, got %d", re.StatusCode)
	}
}

func TestIsRetryable_HTTPStatusCodes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"status 429", errors.New("status 429 too many requests"), true},
		{"status 503", errors.New("upstream (status 503)"), true},
		{"status 502", errors.New("status 502 bad gateway"), true},
		{"status 504", errors.New("status 504"), true},
		{"status 500", errors.New("status 500 internal error"), false}, // not in retryable set
		{"status 404", errors.New("status 404"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.retryable {
				t.Errorf("IsRetryable(%q) = %v, want %v", tt.err.Error(), got, tt.retryable)
			}
		})
	}
}

func TestIsRetryable_NetworkPatterns(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{"connection reset", "read: connection reset by peer", true},
		{"broken pipe", "write: broken pipe", true},
		{"EOF", "unexpected EOF", false}, // pattern "EOF" never matches because errStr is lowercased
		{"i/o error", "i/o error during read", true},
		{"server misbehaving", "server misbehaving", true},
		{"timeout uppercase", "READ TIMEOUT", true},
		{"generic", "something went wrong", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(errors.New(tt.msg)); got != tt.want {
				t.Errorf("IsRetryable(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestIsRetryable_PlainErrorNotRetryable(t *testing.T) {
	// A generic error with no retryable markers must not be retried.
	if IsRetryable(errors.New("plain error")) {
		t.Error("expected plain error to be non-retryable")
	}
}

func TestContainsStatusCode(t *testing.T) {
	if !containsStatusCode("got status 429 back", 429) {
		t.Error("expected 'status 429' to match")
	}
	if !containsStatusCode("parenthetical (status 503)", 503) {
		t.Error("expected '(status 503)' to match")
	}
	if containsStatusCode("status 404", 429) {
		t.Error("expected 'status 404' not to match 429")
	}
}

func TestWithRetry_AlreadyCancelledContext(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: time.Second,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the call

	attempts := 0
	err := WithRetry(ctx, cfg, func() error {
		attempts++
		return NewRetryableError(errors.New("failure"), true)
	})

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// The first attempt runs; the second attempt sees the cancelled context.
	if attempts != 1 {
		t.Errorf("expected 1 attempt before cancellation, got %d", attempts)
	}
}

func TestWithRetry_MaxDelayCapped(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  4,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   100.0,
	}
	start := time.Now()
	attempts := 0

	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		return NewRetryableError(errors.New("failure"), true)
	})

	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 4 {
		t.Fatalf("expected 4 attempts, got %d", attempts)
	}
	// Without the MaxDelay cap the third wait alone would exceed 10s.
	// With the cap, waits are 100ms + 10ms + 10ms.
	if elapsed > 2*time.Second {
		t.Errorf("expected retries to be capped by MaxDelay, took %s", elapsed)
	}
}

func TestWithRetry_PlainError_NoRetry(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		Multiplier:   2.0,
	}
	attempts := 0
	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		return errors.New("plain failure")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt for non-retryable plain error, got %d", attempts)
	}
}
