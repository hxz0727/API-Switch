package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hxz0727/API-Switch/internal/config"
)

// newAuthServer returns a Server with the given auth token configured.
func newAuthServer(t *testing.T, token string) *Server {
	t.Helper()
	s := &Server{}
	s.setConfig(&config.Config{
		Server: config.ServerConfig{AuthToken: token},
	})
	return s
}

// parseAnthropicError decodes the Anthropic-format error body and returns
// its "type" field (e.g. "authentication_error").
func parseAnthropicError(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("failed to parse error body %q: %v", body, err)
	}
	return resp.Error.Type
}

func TestAuthMiddleware_NoAuthConfigured(t *testing.T) {
	s := newAuthServer(t, "")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	handler(rr, req)

	if !called {
		t.Error("expected wrapped handler to be called when no auth token configured")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	s := newAuthServer(t, "secret-token")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	handler(rr, req)

	if called {
		t.Error("wrapped handler should NOT be called without Authorization header")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected JSON content type, got %q", ct)
	}
	if errType := parseAnthropicError(t, rr.Body.String()); errType != "authentication_error" {
		t.Errorf("expected authentication_error, got %q", errType)
	}
}

func TestAuthMiddleware_InvalidHeaderFormat(t *testing.T) {
	s := newAuthServer(t, "secret-token")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	cases := []struct {
		name    string
		header  string
	}{
		{"single word", "secret-token"},
		{"wrong scheme", "Token secret-token"},
		{"lowercase bearer wrong token", "bearer wrong-token"},
		{"empty token", "Bearer "},
		{"extra spaces", "Bearer  secret-token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/messages", nil)
			req.Header.Set("Authorization", tc.header)
			handler(rr, req)

			if called {
				t.Error("wrapped handler should NOT be called for invalid header format")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rr.Code)
			}
		})
	}
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	s := newAuthServer(t, "secret-token")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	handler(rr, req)

	if called {
		t.Error("wrapped handler should NOT be called with wrong token")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if errType := parseAnthropicError(t, rr.Body.String()); errType != "authentication_error" {
		t.Errorf("expected authentication_error, got %q", errType)
	}
}

func TestAuthMiddleware_CorrectToken(t *testing.T) {
	s := newAuthServer(t, "secret-token")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	handler(rr, req)

	if !called {
		t.Error("expected wrapped handler to be called with correct token")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_HeaderWithLeadingWhitespace(t *testing.T) {
	// "Bearer secret-token" with leading space — SplitN still yields 2 parts
	// with parts[0]=="Bearer", so it should pass.
	s := newAuthServer(t, "secret-token")
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer  secret-token")
	handler(rr, req)

	// "Bearer  secret-token" splits into ["Bearer", " secret-token"] — token
	// has a leading space, so constant-time compare fails → 401.
	if called {
		t.Error("expected rejection: token contains leading whitespace")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}
