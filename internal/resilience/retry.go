package resilience

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/hxz0727/API-Switch/internal/logutil"
)

// RetryConfig configures the retry behavior.
type RetryConfig struct {
	MaxAttempts  int           // Maximum number of attempts (including the first try). Default: 3
	InitialDelay time.Duration // Initial delay before first retry. Default: 500ms
	MaxDelay     time.Duration // Maximum delay between retries. Default: 10s
	Multiplier   float64       // Backoff multiplier. Default: 2.0
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryableError is an error that can be wrapped to explicitly mark it as retryable or non-retryable.
type RetryableError struct {
	Err        error
	Retryable  bool
	StatusCode int // HTTP status code, if applicable
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// NewRetryableError creates a RetryableError.
func NewRetryableError(err error, retryable bool) *RetryableError {
	return &RetryableError{Err: err, Retryable: retryable}
}

// IsRetryable determines if an error is retryable.
// Default rules:
// - Network errors (timeout, connection refused) → retryable
// - HTTP 429 (Too Many Requests) → retryable
// - HTTP 5xx (Server Error) → retryable
// - HTTP 4xx (Client Error) → NOT retryable
// - Explicit RetryableError → use the Retryable field
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for explicit retryable marker
	var re *RetryableError
	if As(err, &re) {
		return re.Retryable
	}

	errStr := err.Error()

	// Network errors are generally retryable
	retryablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"broken pipe",
		"EOF",
		"i/o error",
		"server misbehaving",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	// Check for HTTP status codes in error message
	// Format: "status 429" or "(status 503)"
	retryableCodes := []int{429, 502, 503, 504}
	for _, code := range retryableCodes {
		if containsStatusCode(errStr, code) {
			return true
		}
	}

	return false
}

// containsStatusCode checks if an error string contains an HTTP status code.
func containsStatusCode(errStr string, code int) bool {
	codeStr := http.StatusText(code)
	if codeStr == "" {
		return false
	}
	// Check for patterns like "status 429" or "(status 503)"
	statusPatterns := []string{
		string(rune('0'+code/100)) + string(rune('0'+(code/10)%10)) + string(rune('0'+code%10)),
	}
	for _, p := range statusPatterns {
		if strings.Contains(errStr, "status "+p) || strings.Contains(errStr, "(status "+p+")") {
			return true
		}
	}
	return false
}

// As is a wrapper around errors.As to avoid importing errors package in this file.
func As(err error, target interface{}) bool {
	// Simple type assertion for RetryableError
	if re, ok := err.(*RetryableError); ok {
		if ptr, ok := target.(**RetryableError); ok {
			*ptr = re
			return true
		}
	}
	return false
}

// WithRetry executes fn with exponential backoff retry.
// It returns the first success, or the last error if all attempts fail.
func WithRetry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			logutil.Debug("Retry attempt %d/%d after %v", attempt+1, cfg.MaxAttempts, delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if the error is retryable
		if !IsRetryable(err) {
			logutil.Debug("Non-retryable error, not retrying: %v", err)
			return err
		}
	}

	return lastErr
}
