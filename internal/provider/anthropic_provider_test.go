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

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// Compile-time interface conformance checks. If either type stops satisfying
// the Provider interface, this file will not compile.
var (
	_ Provider = (*AnthropicProvider)(nil)
	_ Provider = (*OpenAIProvider)(nil)
)

func TestAnthropicProvider_NameAndType(t *testing.T) {
	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: "http://127.0.0.1:9"}
	p := NewAnthropicProvider("claude-1", cfg)

	if p.Name() != "claude-1" {
		t.Errorf("expected Name()=claude-1, got %q", p.Name())
	}
	if p.Type() != "anthropic" {
		t.Errorf("expected Type()=anthropic, got %q", p.Type())
	}
}

func TestAnthropicProvider_ListModels(t *testing.T) {
	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: "http://127.0.0.1:9"}
	p := NewAnthropicProvider("claude-1", cfg)

	models, err := p.ListModels()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if models != nil {
		t.Errorf("expected nil models for Anthropic, got %v", models)
	}
}

func TestAnthropicProvider_GetClient(t *testing.T) {
	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: "http://127.0.0.1:9"}
	p := NewAnthropicProvider("claude-1", cfg)

	if p.GetClient() == nil {
		t.Fatal("expected non-nil client from GetClient")
	}
}

func TestAnthropicProvider_Ping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Errorf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "ant-key", BaseURL: srv.URL}
	p := NewAnthropicProvider("claude-1", cfg)

	if err := p.Ping(); err != nil {
		t.Fatalf("expected Ping to succeed, got %v", err)
	}
}

func TestAnthropicProvider_Ping_Failure(t *testing.T) {
	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: "http://127.0.0.1:1"}
	p := NewAnthropicProvider("claude-1", cfg)

	if err := p.Ping(); err == nil {
		t.Fatal("expected Ping to fail on unreachable host, got nil")
	}
}

func TestAnthropicProvider_SendMessage_OverridesModel(t *testing.T) {
	var capturedBody atomic.Value
	var capturedPath atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath.Store(r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_01", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "hi"}],
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: srv.URL, APIVersion: "2023-06-01"}
	p := NewAnthropicProvider("claude-1", cfg)

	req := &anthropic.MessagesRequest{
		Model:     "original-model",
		MaxTokens: 100,
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}
	resp, err := p.SendMessage(context.Background(), req, "override-model", 200)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp == nil || resp.ID != "msg_01" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if got := capturedPath.Load(); got != "/v1/messages" {
		t.Errorf("expected path /v1/messages, got %q", got)
	}
	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"override-model"`) {
		t.Errorf("expected model override in request body, got %q", bodyStr)
	}

	// The caller's request must not be mutated.
	if req.Model != "original-model" {
		t.Errorf("caller's request was mutated: Model=%q", req.Model)
	}
}

func TestAnthropicProvider_SendMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: srv.URL}
	p := NewAnthropicProvider("claude-1", cfg)

	_, err := p.SendMessage(context.Background(), &anthropic.MessagesRequest{
		Model: "x", MaxTokens: 10, Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, "x", 10)
	if err == nil {
		t.Fatal("expected error from HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected 'status 401' in error, got %q", err.Error())
	}
}

func TestAnthropicProvider_StreamMessage(t *testing.T) {
	var capturedBody atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{Type: "anthropic", APIKey: "k", BaseURL: srv.URL}
	p := NewAnthropicProvider("claude-1", cfg)

	req := &anthropic.MessagesRequest{
		Model:     "original-model",
		MaxTokens: 100,
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}
	rc, err := p.StreamMessage(context.Background(), req, "override-model", 200)
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(raw), "message_start") {
		t.Errorf("expected stream content, got %q", string(raw))
	}

	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"override-model"`) {
		t.Errorf("expected model override in stream request, got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, `"stream":true`) {
		t.Errorf("expected stream=true in request, got %q", bodyStr)
	}
	if req.Model != "original-model" {
		t.Errorf("caller's request was mutated: Model=%q", req.Model)
	}
}
