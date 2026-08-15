package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// =====================================================================
// Helpers
// =====================================================================

// proxyTestConfig builds a config pointing at the given upstream base URL.
func proxyTestConfig(upstreamURL string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 0},
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				Type:    "openai",
				APIKey:  "test-key",
				BaseURL: upstreamURL,
			},
		},
		Models: map[string]config.ModelConfig{
			"test-model": {Provider: "test-provider"},
		},
	}
}

// startProxyServer starts a real proxy on a free port and waits until ready.
func startProxyServer(t *testing.T, cfg *config.Config) int {
	t.Helper()
	port := freePort(t)
	srv := NewServer(cfg)
	addr := fmt.Sprintf(":%d", port)
	go func() {
		_ = srv.StartWithConfigFile(addr, "")
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	waitForProxyReady(t, port)
	return port
}

func waitForProxyReady(t *testing.T, port int) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("proxy on port %d did not become ready", port)
}

// postMessages sends a raw JSON body to /v1/messages.
func postMessages(t *testing.T, port int, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port),
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/messages: %v", err)
	}
	return resp
}

// validMessagesBody is a minimal valid Anthropic Messages request.
func validMessagesBody(stream bool) string {
	return fmt.Sprintf(`{"model":"test-model","stream":%t,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, stream)
}

// eventually polls cond until it returns true or the timeout elapses.
func eventually(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// =====================================================================
// handleMessages — method / decode / routing errors
// =====================================================================

func TestHandleMessages_MethodNotAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for GET")
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port))
	if err != nil {
		t.Fatalf("GET /v1/messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
	if errType := parseAnthropicError(t, readAll(t, resp)); errType != "method_not_allowed" {
		t.Errorf("expected method_not_allowed, got %q", errType)
	}
}

func TestHandleMessages_InvalidJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for invalid JSON")
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	resp := postMessages(t, port, `{not valid json`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	if errType := parseAnthropicError(t, readAll(t, resp)); errType != "invalid_request" {
		t.Errorf("expected invalid_request, got %q", errType)
	}
}

func TestHandleMessages_ModelNotFound(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for unknown model")
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	body := `{"model":"no-such-model","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	resp := postMessages(t, port, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	raw := readAll(t, resp)
	if errType := parseAnthropicError(t, raw); errType != "not_found" {
		t.Errorf("expected not_found, got %q", errType)
	}
	if !strings.Contains(raw, "no-such-model") {
		t.Errorf("expected error to mention model name, got: %s", raw)
	}
}

// =====================================================================
// handleMessages — success paths
// =====================================================================

func TestHandleMessages_NonStreaming_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id":"chatcmpl-1",
			"object":"chat.completion",
			"created":1,
			"model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello back"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`)
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	resp := postMessages(t, port, validMessagesBody(false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, readAll(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON content type, got %q", ct)
	}

	var ant anthropic.MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&ant); err != nil {
		t.Fatalf("failed to decode Anthropic response: %v", err)
	}
	if len(ant.Content) == 0 || ant.Content[0].Text != "hello back" {
		t.Errorf("unexpected content: %+v", ant.Content)
	}
	if ant.Usage.InputTokens != 3 || ant.Usage.OutputTokens != 2 {
		t.Errorf("unexpected usage: %+v", ant.Usage)
	}
}

func TestHandleMessages_Streaming_Success(t *testing.T) {
	chunks := []string{
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	resp := postMessages(t, port, validMessagesBody(true))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, readAll(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}

	raw := readAll(t, resp)
	if !strings.Contains(raw, "event: message_start") {
		t.Errorf("missing message_start in stream: %s", raw)
	}
	if !strings.Contains(raw, "event: message_stop") {
		t.Errorf("missing message_stop in stream: %s", raw)
	}
	if !strings.Contains(raw, "hello") {
		t.Errorf("missing delta text in stream: %s", raw)
	}
}

// =====================================================================
// handleMessages — upstream failure → 502
// =====================================================================

func TestHandleMessages_NonStreaming_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 400 is non-retryable → single attempt, fast test.
		http.Error(w, "bad request from upstream", http.StatusBadRequest)
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	resp := postMessages(t, port, validMessagesBody(false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d: %s", resp.StatusCode, readAll(t, resp))
	}
	if errType := parseAnthropicError(t, readAll(t, resp)); errType != "api_error" {
		t.Errorf("expected api_error, got %q", errType)
	}
}

func TestHandleMessages_Streaming_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "stream unavailable", http.StatusBadRequest)
	}))
	defer upstream.Close()

	port := startProxyServer(t, proxyTestConfig(upstream.URL))
	resp := postMessages(t, port, validMessagesBody(true))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d: %s", resp.StatusCode, readAll(t, resp))
	}
	if errType := parseAnthropicError(t, readAll(t, resp)); errType != "api_error" {
		t.Errorf("expected api_error, got %q", errType)
	}
}

// =====================================================================
// handleMessages — auth through the HTTP stack
// =====================================================================

func TestHandleMessages_AuthRequired(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called without auth")
	}))
	defer upstream.Close()

	cfg := proxyTestConfig(upstream.URL)
	cfg.Server.AuthToken = "super-secret"
	port := startProxyServer(t, cfg)

	resp := postMessages(t, port, validMessagesBody(false))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
	if errType := parseAnthropicError(t, readAll(t, resp)); errType != "authentication_error" {
		t.Errorf("expected authentication_error, got %q", errType)
	}
}

// =====================================================================
// StartWithConfigFile — failure paths
// =====================================================================

func TestStartWithConfigFile_PortConflict(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	cfg := proxyTestConfig("http://127.0.0.1:9")
	srv := NewServer(cfg)
	err = srv.StartWithConfigFile(fmt.Sprintf(":%d", port), "")
	if err == nil {
		t.Fatal("expected error when port is already in use")
	}
}

func TestStartWithConfigFile_TLSInvalidCert(t *testing.T) {
	port := freePort(t)
	cfg := proxyTestConfig("http://127.0.0.1:9")
	cfg.Server.TLSCert = "/nonexistent/cert.pem"
	cfg.Server.TLSKey = "/nonexistent/key.pem"
	srv := NewServer(cfg)
	err := srv.StartWithConfigFile(fmt.Sprintf(":%d", port), "")
	if err == nil {
		t.Fatal("expected error for invalid TLS cert/key paths")
	}
}

// =====================================================================
// watchConfigFile — hot reload
// =====================================================================

func TestWatchConfigFile_HotReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := config.DefaultConfig()
	cfg.Providers["p"] = config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models["m"] = config.ModelConfig{Provider: "p"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save initial config: %v", err)
	}

	port := freePort(t)
	srv := NewServer(cfg)
	addr := fmt.Sprintf(":%d", port)
	go func() {
		_ = srv.StartWithConfigFile(addr, cfgPath)
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	waitForProxyReady(t, port)

	// Give fsnotify time to establish the watcher.
	time.Sleep(200 * time.Millisecond)

	// Modify the config file and keep re-saving until the server reloads.
	deadline := time.Now().Add(5 * time.Second)
	reloaded := false
	for time.Now().Before(deadline) {
		cfg.Models["hot-new-model"] = config.ModelConfig{Provider: "p"}
		if err := config.Save(cfgPath, cfg); err != nil {
			t.Fatalf("failed to save updated config: %v", err)
		}
		if _, ok := srv.getConfig().Models["hot-new-model"]; ok {
			reloaded = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !reloaded {
		t.Fatal("config was not hot-reloaded after file change")
	}
}

// =====================================================================
// reloadConfigFromFile — error cases
// =====================================================================

func TestReloadConfigFromFile_NoPath(t *testing.T) {
	srv := NewServer(config.DefaultConfig())
	// cfgPath empty → no-op, must not panic.
	srv.reloadConfigFromFile()
}

func TestReloadConfigFromFile_InvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("::: not valid yaml :::"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	srv := NewServer(config.DefaultConfig())
	srv.cfgPath = cfgPath
	before := srv.getConfig()
	srv.reloadConfigFromFile()
	after := srv.getConfig()
	if before != after {
		t.Error("expected config to remain unchanged when reload fails")
	}
}

// =====================================================================
// Shutdown
// =====================================================================

func TestShutdown_WithoutHTTPServer(t *testing.T) {
	srv := NewServer(config.DefaultConfig())
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil error when no HTTP server is running, got %v", err)
	}
}

// =====================================================================
// sanitizeErrorMessage
// =====================================================================

func TestSanitizeErrorMessage(t *testing.T) {
	short := "short message"
	if got := sanitizeErrorMessage(short); got != short {
		t.Errorf("short message should pass through unchanged, got %q", got)
	}

	long := strings.Repeat("a", 600)
	got := sanitizeErrorMessage(long)
	if len(got) != 500+len("...(truncated)") {
		t.Errorf("expected truncated length, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("expected truncation suffix, got %q", got[:min(len(got), 20)])
	}
}

// =====================================================================
// Deprecated converter wrappers
// =====================================================================

func TestWrapper_convertAnthropicMessage(t *testing.T) {
	var msgs []openai.Message
	n := convertAnthropicMessage(anthropic.Message{Role: "user", Content: json.RawMessage(`"hi"`)}, &msgs)
	if n != 1 {
		t.Errorf("expected 1 message appended, got %d", n)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestWrapper_toolResultContent(t *testing.T) {
	block := anthropic.ContentBlock{Type: "tool_result", Content: json.RawMessage(`"the result"`)}
	if got := toolResultContent(block); got != "the result" {
		t.Errorf("toolResultContent = %q, want 'the result'", got)
	}
}

func TestWrapper_hasOnlyText(t *testing.T) {
	if !hasOnlyText([]anthropic.ContentBlock{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}) {
		t.Error("expected all-text blocks to report true")
	}
	if hasOnlyText([]anthropic.ContentBlock{{Type: "text"}, {Type: "image"}}) {
		t.Error("expected mixed blocks to report false")
	}
}

// =====================================================================
// initUsageTracker
// =====================================================================

func TestInitUsageTracker_Error(t *testing.T) {
	// Make MkdirAll fail by creating a regular file where the
	// .api-switch directory is expected.
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".api-switch"), []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("HOME", home)

	if ut := initUsageTracker(); ut != nil {
		t.Errorf("expected nil tracker when usage dir cannot be created, got %v", ut)
	}
}

// =====================================================================
// Local helpers
// =====================================================================

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
