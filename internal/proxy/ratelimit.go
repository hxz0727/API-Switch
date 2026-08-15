package proxy

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a sliding-window rate limiter.
// It records request timestamps and counts those within the sliding window.
type RateLimiter struct {
	mu          sync.Mutex
	requests    map[string][]int64 // timestamps per client IP (UnixNano)
	maxRequests int                // max requests per window
	window      time.Duration      // sliding window duration
}

// NewRateLimiter creates a new sliding-window rate limiter.
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests:    make(map[string][]int64),
		maxRequests: maxRequests,
		window:      window,
	}
}

// Allow checks if a request from the given client IP is allowed.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UnixNano()
	cutoff := now - rl.window.Nanoseconds()

	timestamps, exists := rl.requests[clientIP]
	if !exists {
		rl.requests[clientIP] = []int64{now}
		return true
	}

	// Find the first non-expired index
	start := 0
	for start < len(timestamps) && timestamps[start] <= cutoff {
		start++
	}

	// Count requests in the sliding window
	count := len(timestamps) - start
	if count >= rl.maxRequests {
		// Keep the entry but don't add new timestamp; also keep the timestamps
		// so we can slide the window. If all timestamps are expired after this
		// decision, clean up.
		if start == len(timestamps) {
			delete(rl.requests, clientIP)
		}
		return false
	}

	// Append new timestamp and clean up expired entries
	if start == len(timestamps) {
		// All expired — reset with just the new one
		rl.requests[clientIP] = []int64{now}
	} else {
		rl.requests[clientIP] = append(timestamps[start:], now)
	}

	return true
}

// rateLimitMiddleware returns a middleware that rate limits requests.
// It uses the configured server.rate_limit value as the per-IP max.
// A config value of 0 disables rate limiting entirely.
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// Cached limiter rebuilt when the configured rate limit changes.
	var (
		mu            sync.Mutex
		limiter       *RateLimiter
		lastRateLimit int
	)

	return func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting if not configured
		cfg := s.getConfig()
		if cfg == nil || cfg.Server.RateLimit == 0 {
			next(w, r)
			return
		}

		// Rebuild limiter if the configured rate limit changed
		mu.Lock()
		if limiter == nil || lastRateLimit != cfg.Server.RateLimit {
			limiter = NewRateLimiter(cfg.Server.RateLimit, time.Minute)
			lastRateLimit = cfg.Server.RateLimit
		}
		rl := limiter
		mu.Unlock()

		// Get client IP
		clientIP := getClientIP(r)

		if !rl.Allow(clientIP) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"type":  "error",
				"error": "rate limit exceeded",
			})
			return
		}

		next(w, r)
	}
}

// getClientIP extracts the client IP from the request.
// It checks X-Forwarded-For, X-Real-IP, and falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For (leftmost is original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := splitAndTrim(xff, ",")
		if len(ips) > 0 && ips[0] != "" {
			return ips[0]
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (strip port for per-IP rate limiting)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
