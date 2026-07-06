package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	mu           sync.Mutex
	requests     map[string]*clientBucket
	maxRequests  int           // max requests per window
	window       time.Duration // time window
	cleanupEvery time.Duration // cleanup interval
	lastCleanup  time.Time
}

type clientBucket struct {
	count     int
	expiresAt time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests:     make(map[string]*clientBucket),
		maxRequests:  maxRequests,
		window:       window,
		cleanupEvery: window * 2,
		lastCleanup:  time.Now(),
	}
}

// Allow checks if a request from the given client IP is allowed.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Periodic cleanup of expired buckets
	if now.Sub(rl.lastCleanup) > rl.cleanupEvery {
		for ip, bucket := range rl.requests {
			if now.After(bucket.expiresAt) {
				delete(rl.requests, ip)
			}
		}
		rl.lastCleanup = now
	}

	bucket, exists := rl.requests[clientIP]
	if !exists || now.After(bucket.expiresAt) {
		// New window
		rl.requests[clientIP] = &clientBucket{
			count:     1,
			expiresAt: now.Add(rl.window),
		}
		return true
	}

	if bucket.count >= rl.maxRequests {
		return false
	}

	bucket.count++
	return true
}

// rateLimitMiddleware returns a middleware that rate limits requests.
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// Default: 100 requests per minute per IP
	limiter := NewRateLimiter(100, time.Minute)

	return func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting if not configured
		if s.cfg == nil || s.cfg.Server.RateLimit == 0 {
			next(w, r)
			return
		}

		// Get client IP
		clientIP := getClientIP(r)

		if !limiter.Allow(clientIP) {
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

	// Fall back to RemoteAddr
	return r.RemoteAddr
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
