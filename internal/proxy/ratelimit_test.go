package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hxz0727/API-Switch/internal/config"
)

// =====================================================================
// getClientIP
// =====================================================================

func TestGetClientIP_XForwardedFor_Single(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	r.Header.Set("X-Real-IP", "1.1.1.1") // must be ignored when XFF present
	got := getClientIP(r)
	if got != "8.8.8.8" {
		t.Errorf("getClientIP = %q, want 8.8.8.8", got)
	}
}

func TestGetClientIP_XForwardedFor_Multiple(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1, 10.0.0.2")
	got := getClientIP(r)
	if got != "203.0.113.5" {
		t.Errorf("getClientIP = %q, want leftmost 203.0.113.5", got)
	}
}

func TestGetClientIP_XForwardedFor_WithWhitespace(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Forwarded-For", "  198.51.100.7  , 10.0.0.1")
	got := getClientIP(r)
	if got != "198.51.100.7" {
		t.Errorf("getClientIP = %q, want 198.51.100.7", got)
	}
}

func TestGetClientIP_XForwardedFor_EmptyFirst(t *testing.T) {
	// A header like ", 10.0.0.1" — splitAndTrim drops the empty entries
	// so the first surviving IP is used.
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Forwarded-For", ", 10.0.0.1")
	got := getClientIP(r)
	if got != "10.0.0.1" {
		t.Errorf("getClientIP = %q, want 10.0.0.1", got)
	}
}

func TestGetClientIP_XForwardedFor_WhitespaceOnlyFallsThrough(t *testing.T) {
	// XFF of only whitespace produces no IPs → falls through to X-Real-IP.
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Forwarded-For", "   ")
	r.Header.Set("X-Real-IP", "9.9.9.9")
	got := getClientIP(r)
	if got != "9.9.9.9" {
		t.Errorf("getClientIP = %q, want X-Real-IP 9.9.9.9", got)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Real-IP", "172.16.0.1")
	got := getClientIP(r)
	if got != "172.16.0.1" {
		t.Errorf("getClientIP = %q, want 172.16.0.1", got)
	}
}

func TestGetClientIP_FallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.RemoteAddr = "192.0.2.55:4321"
	got := getClientIP(r)
	if got != "192.0.2.55" {
		t.Errorf("getClientIP = %q, want stripped port 192.0.2.55", got)
	}
}

func TestGetClientIP_RemoteAddrWithoutPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.RemoteAddr = "192.0.2.55"
	got := getClientIP(r)
	if got != "192.0.2.55" {
		t.Errorf("getClientIP = %q, want raw RemoteAddr", got)
	}
}

// =====================================================================
// splitAndTrim
// =====================================================================

func TestSplitAndTrim(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"whitespace only", "   ", []string{}},
		{"single", "a", []string{"a"}},
		{"single with spaces", "  a  ", []string{"a"}},
		{"multiple", "a,b,c", []string{"a", "b", "c"}},
		{"multiple with spaces", " a , b , c ", []string{"a", "b", "c"}},
		{"empty entries", "a,,b", []string{"a", "b"}},
		{"all empty", ",,,", []string{}},
		{"comma at ends", ",a,b,", []string{"a", "b"}},
		{"only commas and spaces", " , , ", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitAndTrim(tc.in, ",")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitAndTrim(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// =====================================================================
// rateLimitMiddleware
// =====================================================================

// newRateLimitServer returns a Server with the given rate limit configured.
func newRateLimitServer(t *testing.T, rateLimit int) *Server {
	t.Helper()
	s := &Server{}
	s.setConfig(&config.Config{
		Server: config.ServerConfig{RateLimit: rateLimit},
	})
	return s
}

func TestRateLimitMiddleware_NotConfigured(t *testing.T) {
	s := newRateLimitServer(t, 0) // 0 = disabled
	called := 0

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	// The limiter is created once per middleware build, so reuse handler.
	for i := 0; i < 200; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}
	if called != 200 {
		t.Errorf("expected 200 calls, got %d", called)
	}
}

func TestRateLimitMiddleware_Configured_Allowed(t *testing.T) {
	s := newRateLimitServer(t, 100)
	called := 0

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}
	if called != 3 {
		t.Errorf("expected 3 calls, got %d", called)
	}
}

func TestRateLimitMiddleware_Configured_Exceeded(t *testing.T) {
	// NOTE: the middleware's limiter is hardcoded to 100 requests/minute
	// (config.RateLimit only enables/disables limiting), so we must send
	// 101 requests to actually trip the 429 path.
	s := newRateLimitServer(t, 2)
	called := 0

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	clientAddr := "10.0.0.3:1234"

	// First 100 requests allowed
	for i := 0; i < 100; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.RemoteAddr = clientAddr
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 101st request exceeds the limit → 429
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.RemoteAddr = clientAddr
	handler(rr, req)

	if called != 100 {
		t.Errorf("expected wrapped handler called exactly 100 times, got %d", called)
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
	if retryAfter := rr.Header().Get("Retry-After"); retryAfter != "60" {
		t.Errorf("expected Retry-After: 60, got %q", retryAfter)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse 429 body: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("expected error body 'rate limit exceeded', got %q", body["error"])
	}
}

func TestRateLimitMiddleware_Configured_IsolatedPerIP(t *testing.T) {
	s := newRateLimitServer(t, 1)
	called := 0

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	// Client A consumes its 100 allowances.
	for i := 0; i < 100; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.RemoteAddr = "10.0.0.4:1111"
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("client A request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// Client A is now limited.
	rrA := httptest.NewRecorder()
	reqA := httptest.NewRequest("POST", "/v1/messages", nil)
	reqA.RemoteAddr = "10.0.0.4:1111"
	handler(rrA, reqA)
	if rrA.Code != http.StatusTooManyRequests {
		t.Errorf("client A 101st request: expected 429, got %d", rrA.Code)
	}

	// Client B is unaffected.
	rrB := httptest.NewRecorder()
	reqB := httptest.NewRequest("POST", "/v1/messages", nil)
	reqB.RemoteAddr = "10.0.0.5:2222"
	handler(rrB, reqB)
	if rrB.Code != http.StatusOK {
		t.Errorf("client B request: expected 200, got %d", rrB.Code)
	}
}
