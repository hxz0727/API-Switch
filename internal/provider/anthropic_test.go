package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

func newTestAnthropicClient(t *testing.T, srv *httptest.Server) *AnthropicClient {
	t.Helper()
	cfg := &config.ProviderConfig{
		Type:       "anthropic",
		APIKey:     "ant-test-key",
		BaseURL:    srv.URL,
		APIVersion: "2023-06-01",
	}
	return NewAnthropicClient(cfg)
}

func newAnthropicMessagesRequest() *anthropic.MessagesRequest {
	return &anthropic.MessagesRequest{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 256,
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
}

func TestAnthropicClient_SendMessage_Success(t *testing.T) {
	var capturedAuth atomic.Value
	var capturedVersion atomic.Value
	var capturedBody atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		capturedAuth.Store(r.Header.Get("x-api-key"))
		capturedVersion.Store(r.Header.Get("anthropic-version"))
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_01",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-5-sonnet-20241022",
			"content": [{"type": "text", "text": "hello there"}],
			"stop_reason": "end_turn",
			"stop_sequence": null,
			"usage": {"input_tokens": 5, "output_tokens": 3}
		}`))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	resp, err := client.SendMessage(newAnthropicMessagesRequest())
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ID != "msg_01" {
		t.Errorf("expected id=msg_01, got %q", resp.ID)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Text != "hello there" {
		t.Errorf("expected text='hello there', got %q", resp.Content[0].Text)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}

	if got := capturedAuth.Load(); got != "ant-test-key" {
		t.Errorf("expected x-api-key=ant-test-key, got %q", got)
	}
	if got := capturedVersion.Load(); got != "2023-06-01" {
		t.Errorf("expected anthropic-version=2023-06-01, got %q", got)
	}
	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"claude-3-5-sonnet-20241022"`) {
		t.Errorf("expected model in body, got %q", bodyStr)
	}
}

func TestAnthropicClient_SendMessage_HTTPError(t *testing.T) {
	t.Run("400", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`))
		}))
		defer srv.Close()

		client := newTestAnthropicClient(t, srv)
		_, err := client.SendMessage(newAnthropicMessagesRequest())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 400") {
			t.Errorf("expected error to contain 'status 400', got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "bad input") {
			t.Errorf("expected error to include body, got %q", err.Error())
		}
	})

	t.Run("500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream down"))
		}))
		defer srv.Close()

		client := newTestAnthropicClient(t, srv)
		_, err := client.SendMessage(newAnthropicMessagesRequest())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected error to contain 'status 500', got %q", err.Error())
		}
	})
}

func TestAnthropicClient_StreamMessage_Success(t *testing.T) {
	var capturedBody atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	body, err := client.StreamMessage(newAnthropicMessagesRequest())
	if err != nil {
		t.Fatalf("StreamMessage returned error: %v", err)
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), "message_start") {
		t.Errorf("expected stream to contain message_start, got %q", string(raw))
	}
	if !strings.Contains(string(raw), "message_stop") {
		t.Errorf("expected stream to contain message_stop, got %q", string(raw))
	}
	if !strings.Contains(capturedBody.Load().(string), `"stream":true`) {
		t.Errorf("expected stream=true in request body, got %q", capturedBody.Load())
	}
}

func TestAnthropicClient_StreamMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream gateway down"))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	_, err := client.StreamMessage(newAnthropicMessagesRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("expected error to contain 'status 502', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "upstream gateway down") {
		t.Errorf("expected error to include body, got %q", err.Error())
	}
}

func TestAnthropicClient_StreamMessage_DoesNotMutateRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	req := newAnthropicMessagesRequest()
	if req.Stream {
		t.Fatal("request should not start with Stream=true")
	}

	body, err := client.StreamMessage(req)
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	body.Close()

	if req.Stream {
		t.Error("StreamMessage must not mutate the caller's request (Stream became true)")
	}
}

func TestAnthropicClient_StreamMessageWithContext_Canceled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release) // LIFO: release handlers before server.Close() blocks

	client := newTestAnthropicClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := client.StreamMessageWithContext(ctx, newAnthropicMessagesRequest())
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected context cancellation error, got %q", err.Error())
	}
}

func TestAnthropicClient_StreamMessageWithContext_AlreadyCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.StreamMessageWithContext(ctx, newAnthropicMessagesRequest())
	if err == nil {
		t.Fatal("expected error for already-canceled context, got nil")
	}
}

func TestAnthropicClient_Ping(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("x-api-key") != "ant-test-key" {
				t.Errorf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client := newTestAnthropicClient(t, srv)
		if err := client.Ping(); err != nil {
			t.Errorf("expected Ping to succeed, got %v", err)
		}
	})

	t.Run("network_error", func(t *testing.T) {
		cfg := &config.ProviderConfig{
			Type:    "anthropic",
			APIKey:  "ant-test-key",
			BaseURL: "http://127.0.0.1:1",
		}
		client := NewAnthropicClient(cfg)
		if err := client.Ping(); err == nil {
			t.Error("expected Ping to fail on unreachable host, got nil")
		}
	})
}

func TestAnthropicClient_SendMessageRaw_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_01","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	result, err := client.SendMessageRaw(map[string]interface{}{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 64,
		"messages":   []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("SendMessageRaw: %v", err)
	}
	if result["id"] != "msg_01" {
		t.Errorf("expected id=msg_01, got %v", result["id"])
	}
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
}

func TestAnthropicClient_SendMessageRaw_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	_, err := client.SendMessageRaw(map[string]interface{}{"model": "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected 'status 401' in error, got %q", err.Error())
	}
}

func TestAnthropicClient_SendMessage_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	_, err := client.SendMessage(newAnthropicMessagesRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %q", err.Error())
	}
}

func TestAnthropicClient_SendMessageRaw_MarshalFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when marshaling fails")
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	// Channels cannot be marshaled to JSON — this forces the marshal branch to fail.
	if _, err := client.SendMessageRaw(make(chan int)); err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

func TestAnthropicClient_EndpointPath(t *testing.T) {
	var pathHit atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathHit.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","type":"message","role":"assistant","content":[]}`))
	}))
	defer srv.Close()

	client := newTestAnthropicClient(t, srv)
	if _, err := client.SendMessage(newAnthropicMessagesRequest()); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got := pathHit.Load(); got != "/v1/messages" {
		t.Errorf("expected path /v1/messages, got %q", got)
	}
}

func TestAnthropicClient_ClientTimeout(t *testing.T) {
	client := NewAnthropicClient(&config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "x",
		BaseURL: "https://example.com",
	})
	if client.client.Timeout != 5*time.Minute {
		t.Errorf("expected Anthropic client timeout=5m, got %s", client.client.Timeout)
	}
}
