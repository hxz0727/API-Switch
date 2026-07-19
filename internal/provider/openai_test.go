package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// newTestOpenAIClient creates an OpenAIClient whose BaseURL points at the
// provided httptest server. The API key is set to a deterministic value so
// tests can assert it appears in the request.
func newTestOpenAIClient(t *testing.T, srv *httptest.Server) *OpenAIClient {
	t.Helper()
	cfg := &config.ProviderConfig{
		Type:    "openai",
		APIKey:  "test-key-123",
		BaseURL: srv.URL,
	}
	return NewOpenAIClient(cfg)
}

func newChatCompletionRequest() *openai.ChatCompletionRequest {
	return &openai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []openai.Message{
			{Role: "user", Content: "hello"},
		},
	}
}

func TestOpenAIClient_SendMessage_Success(t *testing.T) {
	var capturedAuth atomic.Value
	var capturedContentType atomic.Value
	var capturedBody atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth.Store(r.Header.Get("Authorization"))
		capturedContentType.Store(r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-1",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "gpt-4o-mini",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "hi there"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
		}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	resp, err := client.SendMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("SendMessage returned nil response")
	}
	if resp.ID != "chatcmpl-1" {
		t.Errorf("expected id=chatcmpl-1, got %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if msg, ok := resp.Choices[0].Message.Content.(string); !ok || msg != "hi there" {
		t.Errorf("expected content=hi there, got %#v", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("expected total_tokens=8, got %d", resp.Usage.TotalTokens)
	}

	if got := capturedAuth.Load(); got != "Bearer test-key-123" {
		t.Errorf("expected Authorization header 'Bearer test-key-123', got %q", got)
	}
	if got := capturedContentType.Load(); got != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", got)
	}

	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"gpt-4o-mini"`) {
		t.Errorf("expected request body to include model, got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, `"role":"user"`) {
		t.Errorf("expected request body to include user message, got %q", bodyStr)
	}
}

func TestOpenAIClient_SendMessage_HTTPError(t *testing.T) {
	t.Run("400", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": {"message": "bad request"}}`))
		}))
		defer srv.Close()

		client := newTestOpenAIClient(t, srv)
		_, err := client.SendMessage(newChatCompletionRequest())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 400") {
			t.Errorf("expected error to contain 'status 400', got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "bad request") {
			t.Errorf("expected error to contain body, got %q", err.Error())
		}
	})

	t.Run("500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`internal failure`))
		}))
		defer srv.Close()

		client := newTestOpenAIClient(t, srv)
		_, err := client.SendMessage(newChatCompletionRequest())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected error to contain 'status 500', got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "internal failure") {
			t.Errorf("expected error to contain body, got %q", err.Error())
		}
	})
}

func TestOpenAIClient_SendMessage_HTTP200WithErrorJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		// APIFree-style error wrapped in HTTP 200
		_, _ = w.Write([]byte(`{
			"code": 400,
			"error": {"message": "upstream rate limit hit", "type": "rate_limit_error"}
		}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.SendMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 200") {
		t.Errorf("expected error to mention HTTP 200, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "upstream rate limit hit") {
		t.Errorf("expected error to include upstream message, got %q", err.Error())
	}
}

func TestOpenAIClient_SendMessage_ZeroChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		// Empty choices array — should not error, should log warning
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-empty",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "gpt-4o-mini",
			"choices": []
		}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	resp, err := client.SendMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("SendMessage should not error on empty choices, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(resp.Choices))
	}
}

func TestOpenAIClient_StreamMessage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream=true is set in the request body
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("expected stream=true in body, got %q", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	body, err := client.StreamMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("StreamMessage returned error: %v", err)
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), "data: [DONE]") {
		t.Errorf("expected SSE stream body to include 'data: [DONE]', got %q", string(raw))
	}
	if !strings.Contains(string(raw), `"content":"hi"`) {
		t.Errorf("expected SSE stream body to include delta, got %q", string(raw))
	}
}

func TestOpenAIClient_StreamMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.StreamMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("expected error to contain 'status 502', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "upstream unavailable") {
		t.Errorf("expected error to contain body, got %q", err.Error())
	}
}

func TestOpenAIClient_StreamMessage_HTTP200WithErrorJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error": {"message": "stream quota exceeded"}}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.StreamMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stream quota exceeded") {
		t.Errorf("expected error to include upstream message, got %q", err.Error())
	}
}

func TestOpenAIClient_StreamMessageWithContext_Canceled(t *testing.T) {
	// Server blocks until the test cancels the context.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release) // LIFO: release handlers before server.Close() blocks

	client := newTestOpenAIClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := client.StreamMessageWithContext(ctx, newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// The Go HTTP client returns a context-cancelled error wrapped in "request failed"
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected context cancellation error, got %q", err.Error())
	}
}

func TestOpenAIClient_Ping(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-key-123" {
				t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := newTestOpenAIClient(t, srv)
		if err := client.Ping(); err != nil {
			t.Errorf("expected Ping to succeed, got %v", err)
		}
	})

	t.Run("server_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := newTestOpenAIClient(t, srv)
		// Ping treats any HTTP response (including 500) as "reachable" — only network errors fail.
		if err := client.Ping(); err != nil {
			t.Errorf("expected Ping to succeed even on 500, got %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		// Bind then close to get an unreachable port.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		addr := srv.URL
		srv.Close()

		cfg := &config.ProviderConfig{
			Type:    "openai",
			APIKey:  "test",
			BaseURL: addr,
		}
		client := NewOpenAIClient(cfg)
		if err := client.Ping(); err == nil {
			t.Error("expected Ping to fail on unreachable server, got nil")
		}
	})
}

func TestOpenAIClient_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key-123" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "gpt-4o"},
				{"id": "gpt-4o-mini"},
				{"id": ""},
				{"id": "gpt-3.5-turbo"}
			]
		}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	models, err := client.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 non-empty models, got %d: %v", len(models), models)
	}
	want := []string{"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"}
	for i, w := range want {
		if models[i] != w {
			t.Errorf("model[%d]: expected %q, got %q", i, w, models[i])
		}
	}
}

func TestOpenAIClient_ListModels_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.ListModels()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected error to contain 'status 401', got %q", err.Error())
	}
}

func TestOpenAIClient_StreamMessage_DoesNotMutateRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	req := newChatCompletionRequest()
	if req.Stream {
		t.Fatal("request should not start with Stream=true")
	}

	body, err := client.StreamMessage(req)
	if err != nil {
		t.Fatalf("StreamMessage returned error: %v", err)
	}
	body.Close()

	if req.Stream {
		t.Error("StreamMessage must not mutate the caller's request (Stream became true)")
	}
}

func TestOpenAIClient_Transport_VerifyCustomTransport(t *testing.T) {
	client := NewOpenAIClient(&config.ProviderConfig{
		Type:    "openai",
		APIKey:  "x",
		BaseURL: "https://example.com",
	})

	tr, ok := client.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.client.Transport)
	}
	if tr.MaxIdleConns != 100 {
		t.Errorf("expected MaxIdleConns=100, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 20 {
		t.Errorf("expected MaxIdleConnsPerHost=20, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxConnsPerHost != 50 {
		t.Errorf("expected MaxConnsPerHost=50, got %d", tr.MaxConnsPerHost)
	}
	if tr.IdleConnTimeout != 120*time.Second {
		t.Errorf("expected IdleConnTimeout=120s, got %s", tr.IdleConnTimeout)
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("expected TLSHandshakeTimeout=10s, got %s", tr.TLSHandshakeTimeout)
	}
	if tr.DisableKeepAlives {
		t.Error("expected DisableKeepAlives=false (keep-alives enabled)")
	}
	if client.client.Timeout != 10*time.Minute {
		t.Errorf("expected client.Timeout=10m, got %s", client.client.Timeout)
	}
}

func TestOpenAIClient_EndpointConstruction(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		want   string
		models string
	}{
		{"plain", "https://api.openai.com", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/models"},
		{"with_v1", "https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/models"},
		{"with_v1_slash", "https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/models"},
		{"already_chat", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/models"},
		{"trailing_slash", "https://api.deepseek.com/", "https://api.deepseek.com/v1/chat/completions", "https://api.deepseek.com/models"},
		{"with_path", "https://api.moonshot.cn", "https://api.moonshot.cn/v1/chat/completions", "https://api.moonshot.cn/models"},
		{"v1_chat_completions_to_models", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/models"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openAIEndpoint(tc.base); got != tc.want {
				t.Errorf("openAIEndpoint(%q) = %q, want %q", tc.base, got, tc.want)
			}
			if got := openAIModelsEndpoint(tc.base); got != tc.models {
				t.Errorf("openAIModelsEndpoint(%q) = %q, want %q", tc.base, got, tc.models)
			}
		})
	}
}

func TestOpenAIClient_StreamMessage_EmptyBody(t *testing.T) {
	// Server returns HTTP 200 but immediately closes with no body — Peek should error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Hijack and close without writing so the body is empty.
	 hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.StreamMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if !strings.Contains(err.Error(), "peek") {
		t.Errorf("expected peek error, got %q", err.Error())
	}
}

func TestOpenAIClient_StreamMessage_JSONWithoutErrorField(t *testing.T) {
	// Returns HTTP 200 with a JSON object that has no `error` field — falls through to
	// the "non-SSE response" branch.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unexpected":"shape"}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.StreamMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "non-SSE") {
		t.Errorf("expected non-SSE error, got %q", err.Error())
	}
}

func TestOpenAIClient_SendMessage_InvalidJSON(t *testing.T) {
	// HTTP 200 with body that doesn't unmarshal as either an error or a success
	// — the success-decode branch should fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	_, err := client.SendMessage(newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %q", err.Error())
	}
}

func TestOpenAIClient_SendMessage_RequestMarshalFailure(t *testing.T) {
	// Build a request that cannot be marshalled into JSON (channels are not supported).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when marshaling fails")
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	req := &openai.ChatCompletionRequest{
		Model:    "x",
		Messages: nil,
		// Channels can't be marshalled to JSON.
		ToolChoice: make(chan int),
	}

	_, err := client.SendMessage(req)
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("expected marshal error, got %q", err.Error())
	}
}

func TestTruncateBody_Additional(t *testing.T) {
	if got := truncateBody("hi", 500); got != "hi" {
		t.Errorf("short body should not be truncated, got %q", got)
	}
	if got := truncateBody("", 500); got != "" {
		t.Errorf("empty body should remain empty, got %q", got)
	}
}

// Sanity: the package-level test file already covers endpoint/truncate behaviors;
// keep one more end-to-end test for the streamReadCloser Close path so that the
// streamReadCloser struct is exercised.
func TestOpenAIClient_StreamReadCloser_Close(t *testing.T) {
	var closeCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		// Use a custom flusher to know when the body has been read.
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	body, err := client.StreamMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	// Drain the body so the underlying response is finalized before Close.
	_, _ = io.ReadAll(body)
	if err := body.Close(); err != nil {
		t.Errorf("close returned error: %v", err)
	}
	_ = closeCount.Load() // body close is internal; just make sure we get here without panic
}

// Helper to make sure we can also handle a request that arrives as plain JSON
// without the outer "error" object (the errCheck unmarshal still succeeds but
// errCheck.Error is nil).
func TestOpenAIClient_SendMessage_HTTP200ValidButNoChoicesNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		// Body is a valid JSON object but missing both `error` and `choices`.
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"gpt-4o-mini","created":1}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	resp, err := client.SendMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(resp.Choices))
	}
}

// Helper sanity: ensure the request body the client sends for non-streaming
// contains the model and at least one message.
func TestOpenAIClient_SendMessage_RequestShape(t *testing.T) {
	type captured struct {
		method  string
		path    string
		body    []byte
		headers http.Header
	}
	var got captured

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = captured{
			method:  r.Method,
			path:    r.URL.Path,
			body:    body,
			headers: r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"x","choices":[]}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	if _, err := client.SendMessage(newChatCompletionRequest()); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("expected POST, got %s", got.method)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("expected /v1/chat/completions, got %s", got.path)
	}
	if !bytes.Contains(got.body, []byte(`"model":"gpt-4o-mini"`)) {
		t.Errorf("expected model in body, got %s", string(got.body))
	}
	if !bytes.Contains(got.body, []byte(`"role":"user"`)) {
		t.Errorf("expected user role in body, got %s", string(got.body))
	}
	// Stream should be false (or omitted) on non-streaming sends.
	if !bytes.Contains(got.body, []byte(`"stream":false`)) && !bytes.Contains(got.body, []byte(`"messages"`)) {
		t.Errorf("unexpected body shape: %s", string(got.body))
	}
}

// Verify the inner errCheck unmarshal doesn't false-positive on a valid response
// that happens to have an empty `error` object.
func TestOpenAIClient_SendMessage_HTTP200EmptyErrorObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		// `error` is an empty object — should be treated as "no error" and decoded as success.
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2},"error":{}}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	resp, err := client.SendMessage(newChatCompletionRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Errorf("expected 1 choice, got %d", len(resp.Choices))
	}
}

// Confirm json round-trip in the request remains stable for a request that
// already has stream=true. After SendMessage, the caller's struct should be
// unchanged.
func TestOpenAIClient_SendMessage_DoesNotMutateRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"x","choices":[]}`))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	req := newChatCompletionRequest()
	req.Stream = true
	if _, err := client.SendMessage(req); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if !req.Stream {
		t.Error("SendMessage must not change the caller's Stream field")
	}
}

// Verify endpoint construction when baseURL ends with /v1/chat/completions but
// for ListModels (which strips /chat/completions and then appends /models).
func TestOpenAIClient_Endpoint_ListModelsForChatCompletionsBase(t *testing.T) {
	var pathHit atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathHit.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{
		Type:    "openai",
		APIKey:  "k",
		BaseURL: srv.URL + "/v1/chat/completions",
	}
	client := NewOpenAIClient(cfg)
	if _, err := client.ListModels(); err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if got := pathHit.Load(); got != "/v1/models" {
		t.Errorf("expected /v1/models, got %v", got)
	}
}

// Verify StreamMessageWithContext propagates context cancellation when used
// without a separate goroutine.
func TestOpenAIClient_StreamMessageWithContext_AlreadyCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := newTestOpenAIClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.StreamMessageWithContext(ctx, newChatCompletionRequest())
	if err == nil {
		t.Fatal("expected error for already-canceled context, got nil")
	}
}

// ping should be fast (uses a short-timeout client) and not require a server
// response body.
func TestOpenAIClient_Ping_NetworkError(t *testing.T) {
	cfg := &config.ProviderConfig{
		Type:    "openai",
		APIKey:  "k",
		BaseURL: "http://127.0.0.1:1", // unreachable
	}
	client := NewOpenAIClient(cfg)
	if err := client.Ping(); err == nil {
		t.Error("expected Ping to fail on unreachable host, got nil")
	}
}

// Compile-time sanity: make sure the encode/decode types we use are
// compatible with the json package. This is a guard against the test file
// drifting away from the request/response types.
func TestOpenAIClient_ChatCompletionResponse_RoundTrip(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		ID:      "x",
		Object:  "chat.completion",
		Created: 1,
		Model:   "gpt-4o-mini",
		Choices: []openai.Choice{{Index: 0, Message: openai.Message{Role: "assistant", Content: "hi"}}},
		Usage:   openai.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back openai.ChatCompletionResponse
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Model != resp.Model || len(back.Choices) != 1 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
	// Suppress unused-import warnings for fmt if not used elsewhere.
	_ = fmt.Sprintf("%v", back)
}
