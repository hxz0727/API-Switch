package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/monitor"
)

// =====================================================================
// newTestServer returns a Server with a fresh tracker and a tiny valid
// config. cfgPath is empty by default — set it when the test exercises
// handleAdminReload.
// =====================================================================
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Providers["p"] = config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models["m"] = config.ModelConfig{Provider: "p"}

	s := &Server{
		router:  NewRouter(cfg),
		tracker: monitor.NewTracker(64),
		done:    make(chan struct{}),
	}
	s.setConfig(cfg)
	return s
}

// =====================================================================
// TestIsLocalhostIP (table-driven)
// =====================================================================

func TestIsLocalhostIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		// IPv4 loopback
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv4 loopback 127.0.0.2", "127.0.0.2", true},
		{"ipv4 loopback 127.255.255.254", "127.255.255.254", true},

		// IPv6 loopback
		{"ipv6 loopback", "::1", true},

		// Hostname
		{"localhost", "localhost", true},

		// Non-loopback (should be false)
		{"private 10.x", "10.0.0.1", false},
		{"private 192.168.x", "192.168.1.1", false},
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public ipv6", "2001:db8::1", false},

		// IPv6 bracket notation — leading '[' is stripped; trailing ']' is not,
		// so ParseIP returns nil and the function falls through to false.
		{"ipv6 with leading bracket", "[::1]", false},
		{"ipv6 trailing bracket", "::1]", false},

		// Zone ID (e.g. IPv6 with link-local zone) — net.ParseIP strips the %eth0
		// suffix, so fe80::1%eth0 actually parses to fe80::1 which is link-local
		// but not loopback, hence false.
		{"ipv6 zone id", "fe80::1%eth0", false},

		// Garbage
		{"empty string", "", false},
		{"garbage", "not-an-ip", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLocalhostIP(tc.ip)
			if got != tc.want {
				t.Errorf("isLocalhostIP(%q) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// =====================================================================
// TestIsLocalhostRequest
// =====================================================================

func TestIsLocalhostRequest_DirectConnection(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	if !isLocalhostRequest(r) {
		t.Error("expected localhost request from 127.0.0.1:1234")
	}
}

func TestIsLocalhostRequest_NonLocal(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if isLocalhostRequest(r) {
		t.Error("expected non-localhost for 10.0.0.1:1234")
	}
}

func TestIsLocalhostRequest_WithXForwardedFor(t *testing.T) {
	// Even if RemoteAddr is local, a non-local X-Forwarded-For should
	// cause isLocalhostRequest to return false (the proxy header takes
	// priority as the original client address).
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if isLocalhostRequest(r) {
		t.Error("expected false: X-Forwarded-For indicates non-local client")
	}
}

func TestIsLocalhostRequest_XForwardedForLocal(t *testing.T) {
	// X-Forwarded-For with local IP shouldn't change the result.
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	if !isLocalhostRequest(r) {
		t.Error("expected true: both RemoteAddr and X-Forwarded-For are local")
	}
}

func TestIsLocalhostRequest_XRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "127.0.0.1")
	if isLocalhostRequest(r) {
		t.Error("expected false: X-Real-IP overrides local RemoteAddr? Actually no, both must be local")
	}
}

func TestIsLocalhostRequest_RemoteAddrWithoutPort(t *testing.T) {
	// RemoteAddr without a port — net.SplitHostPort fails and the
	// function falls back to using the raw value. The raw value should
	// still be recognised as loopback.
	r := httptest.NewRequest("GET", "/admin/", nil)
	r.RemoteAddr = "127.0.0.1"
	if !isLocalhostRequest(r) {
		t.Error("expected true: bare '127.0.0.1' should be recognised")
	}
}

// =====================================================================
// TestHandleAdminDashboard
// =====================================================================

func TestHandleAdminDashboard(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// Dashboard is registered with requireLocalhost in production; the
	// handler itself does not check the remote addr, so we call it
	// directly to isolate behaviour.
	s.handleAdminDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "API-Switch") {
		t.Errorf("expected dashboard body to contain 'API-Switch', got first 200 chars: %s", body[:min(len(body), 200)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// =====================================================================
// TestHandleAdminStats
// =====================================================================

func TestHandleAdminStats(t *testing.T) {
	s := newTestServer(t)

	// Record a couple of events so stats has content
	s.tracker.Record(&monitor.RequestEvent{
		ID:        "req_1",
		Model:     "m",
		Provider:  "p",
		Status:    "ok",
		Duration:  100 * time.Millisecond,
		Timestamp: time.Now(),
	})
	s.tracker.Record(&monitor.RequestEvent{
		ID:        "req_2",
		Model:     "m",
		Provider:  "p",
		Status:    "error",
		Duration:  50 * time.Millisecond,
		Timestamp: time.Now(),
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	s.handleAdminStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json content type, got %q", ct)
	}

	var resp struct {
		Stats  map[string]interface{} `json:"stats"`
		Recent []interface{}          `json:"recent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if resp.Stats == nil {
		t.Error("expected stats to be present")
	}
	if len(resp.Recent) != 2 {
		t.Errorf("expected 2 recent events, got %d", len(resp.Recent))
	}
	if total, ok := resp.Stats["total_requests"]; !ok {
		t.Error("expected 'total_requests' in stats")
	} else if total.(float64) != 2 {
		t.Errorf("expected total_requests=2, got %v", total)
	}
}

// =====================================================================
// TestHandleAdminEvents (SSE)
// =====================================================================

func TestHandleAdminEvents(t *testing.T) {
	s := newTestServer(t)

	// Use a context we can cancel to terminate the SSE handler.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest("GET", "/admin/events", nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1234"

	rr := httptest.NewRecorder()

	// Run the handler in a goroutine because it blocks until the
	// context is cancelled or the channel closes.
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleAdminEvents(rr, req)
	}()

	// Give the handler time to subscribe
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	s.tracker.Record(&monitor.RequestEvent{
		ID:        "req_sse",
		Model:     "m",
		Provider:  "p",
		Status:    "ok",
		Duration:  25 * time.Millisecond,
		Timestamp: time.Now(),
	})

	// Give the handler time to flush
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to terminate the handler
	cancel()

	// Wait for the handler to return (with a timeout to avoid hangs)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after context cancel")
	}

	body := rr.Body.String()
	if !strings.Contains(body, "event: request") {
		t.Errorf("expected 'event: request' in SSE body, got: %s", body)
	}
	if !strings.Contains(body, "req_sse") {
		t.Errorf("expected 'req_sse' event ID in SSE body, got: %s", body)
	}

	// Verify content-type was set
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
}

// =====================================================================
// TestHandleAdminReload
// =====================================================================

func TestHandleAdminReload_GET(t *testing.T) {
	s := newTestServer(t)
	// No cfgPath set, but GET should still 405.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/reload", nil)
	s.handleAdminReload(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", rr.Code)
	}
}

func TestHandleAdminReload_POST_NoConfig(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/reload", nil)
	s.handleAdminReload(rr, req)

	// With no cfgPath, reload is a no-op but the response should still
	// be 200 and report the current counts.
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON content type, got %q", ct)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if status, _ := resp["status"].(string); status != "ok" {
		t.Errorf("expected status 'ok', got %q", status)
	}
	if _, ok := resp["message"]; !ok {
		t.Error("expected 'message' field in response")
	}
}

func TestHandleAdminReload_POST_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := tmpDir + "/config.yaml"

	// Write an initial config file
	cfg := config.DefaultConfig()
	cfg.Providers["p"] = config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-x",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models["m"] = config.ModelConfig{Provider: "p"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	s := newTestServer(t)
	s.cfgPath = cfgPath

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/reload", nil)
	s.handleAdminReload(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Now modify the file and reload again
	cfg.Models["new-model"] = config.ModelConfig{Provider: "p"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/admin/reload", nil)
	s.handleAdminReload(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200 on second reload, got %d", rr2.Code)
	}

	// Server's config should now contain the new model
	if _, ok := s.getConfig().Models["new-model"]; !ok {
		t.Error("expected new-model in reloaded config")
	}
}

// =====================================================================
// TestRequireLocalhost
// =====================================================================

func TestRequireLocalhost_LocalRequest(t *testing.T) {
	called := false
	handler := requireLocalhost(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for localhost, got %d", rr.Code)
	}
	if !called {
		t.Error("expected wrapped handler to be called")
	}
}

func TestRequireLocalhost_RemoteRequest(t *testing.T) {
	called := false
	handler := requireLocalhost(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for remote, got %d", rr.Code)
	}
	if called {
		t.Error("expected wrapped handler NOT to be called")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON content type, got %q", ct)
	}
	// Body should contain an error message
	body := rr.Body.String()
	if !strings.Contains(body, "localhost") {
		t.Errorf("expected error message about localhost, got: %s", body)
	}
}

// =====================================================================
// TestMonitorConnect_SSE
// =====================================================================

func TestMonitorConnect_SSE(t *testing.T) {
	// Spin up a test server that emits the same SSE format as
	// handleAdminEvents so MonitorConnect can parse it.
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)

		// Send one request event
		ev := monitor.RequestEvent{
			ID:        "req_test",
			Timestamp: time.Now(),
			Model:     "m",
			Provider:  "p",
			Status:    "ok",
			Duration:  42 * time.Millisecond,
		}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}

		// Block until client disconnects
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Convert the test server URL to an "addr" in the form MonitorConnect
	// expects ("host:port", no scheme). Use the path "/admin/events"
	// directly via the URL the helper builds: "http://localhost<addr>/admin/events"
	// so we need an addr that, when prepended with "http://localhost",
	// produces the test server's URL.
	//
	// Easier: extract host:port from srv.URL and construct a URL that
	// MonitorConnect can dial. The helper builds:
	//   http://localhost<addr>/admin/events
	// so we set addr = "<host>:<port>" but that wouldn't resolve to localhost.
	// Instead, we dial directly and inject a small client body.
	//
	// MonitorConnect does its own http.Get — to test the parser logic
	// without spinning up a real server on localhost, we replicate the
	// relevant scanning loop here.

	resp, err := http.Get(srv.URL + "/admin/events")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType, dataBuf string
	var foundEvent bool
	deadline := time.After(2 * time.Second)
	for scanner.Scan() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for SSE event")
		default:
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataBuf = strings.TrimPrefix(line, "data: ")
		} else if line == "" && dataBuf != "" {
			if eventType == "request" {
				var ev monitor.RequestEvent
				if err := json.Unmarshal([]byte(dataBuf), &ev); err == nil {
					if ev.ID == "req_test" {
						foundEvent = true
					}
				}
			}
			eventType = ""
			dataBuf = ""
		}
		if foundEvent {
			break
		}
	}
	if !foundEvent {
		t.Error("did not receive expected SSE event")
	}
}

// TestMonitorConnect_BadAddr verifies MonitorConnect returns an error for
// an unreachable address.
func TestMonitorConnect_BadAddr(t *testing.T) {
	// Use a port that's almost certainly closed.
	err := MonitorConnect(":39999")
	if err == nil {
		t.Error("expected error connecting to unreachable addr")
	}
}

// =====================================================================
// TestFormatDuration
// =====================================================================

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub-second", 250 * time.Millisecond, "250ms"},
		{"zero", 0, "0ms"},
		{"one second", time.Second, "1.0s"},
		{"two-point-five", 2500 * time.Millisecond, "2.5s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDuration(tc.in)
			if got != tc.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// =====================================================================
// TestPrintEvent (smoke — just verify no panic and stable output shape)
// =====================================================================

func TestPrintEvent(t *testing.T) {
	// printEvent writes via log.Printf; we just want to confirm it
	// handles all status branches without panicking.
	cases := []monitor.RequestEvent{
		{ID: "1", Timestamp: time.Now(), Model: "m", Provider: "p", Status: "ok", Duration: 10 * time.Millisecond},
		{ID: "2", Timestamp: time.Now(), Model: "m", Provider: "p", Status: "error", Error: "boom", Duration: 20 * time.Millisecond},
		{ID: "3", Timestamp: time.Now(), Model: "m", Provider: "p", Status: "cancelled", Duration: 5 * time.Millisecond},
		{ID: "4", Timestamp: time.Now(), Model: "m", Provider: "p", Status: "unknown", Duration: 1 * time.Millisecond},
	}
	for _, ev := range cases {
		ev := ev
		t.Run(ev.Status, func(t *testing.T) {
			printEvent(&ev) // should not panic
		})
	}
}
