package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithRetry_Success(t *testing.T) {
	cfg := DefaultRetryConfig()
	attempts := 0

	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	attempts := 0

	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		if attempts < 3 {
			return NewRetryableError(errors.New("temporary failure"), true)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	cfg := DefaultRetryConfig()
	attempts := 0

	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		return NewRetryableError(errors.New("client error"), false)
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestWithRetry_AllAttemptsFail(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	attempts := 0

	err := WithRetry(context.Background(), cfg, func() error {
		attempts++
		return NewRetryableError(errors.New("persistent failure"), true)
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := WithRetry(ctx, cfg, func() error {
		attempts++
		return NewRetryableError(errors.New("failure"), true)
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"explicit retryable", NewRetryableError(errors.New("test"), true), true},
		{"explicit non-retryable", NewRetryableError(errors.New("test"), false), false},
		{"timeout error", errors.New("request timeout"), true},
		{"connection refused", errors.New("connection refused"), true},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.retryable {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.retryable)
			}
		})
	}
}
