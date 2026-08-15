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

func newOpenAIProvider(name string, srv *httptest.Server) *OpenAIProvider {
	cfg := &config.ProviderConfig{
		Type:             "openai",
		APIKey:           "oai-key",
		BaseURL:          srv.URL,
		DefaultMaxTokens: 4096,
	}
	return NewOpenAIProvider(name, cfg)
}

func TestOpenAIProvider_NameAndType(t *testing.T) {
	p := NewOpenAIProvider("deepseek-1", &config.ProviderConfig{Type: "openai", APIKey: "k", BaseURL: "http://127.0.0.1:9"})

	if p.Name() != "deepseek-1" {
		t.Errorf("expected Name()=deepseek-1, got %q", p.Name())
	}
	if p.Type() != "openai" {
		t.Errorf("expected Type()=openai, got %q", p.Type())
	}
}

func TestOpenAIProvider_GetClient(t *testing.T) {
	p := NewOpenAIProvider("deepseek-1", &config.ProviderConfig{Type: "openai", APIKey: "k", BaseURL: "http://127.0.0.1:9"})
	if p.GetClient() == nil {
		t.Fatal("expected non-nil client from GetClient")
	}
}

func TestOpenAIProvider_Ping(t *testing.T) {
	var pathHit atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		pathHit.Store(r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer oai-key" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	if err := p.Ping(); err != nil {
		t.Fatalf("expected Ping to succeed, got %v", err)
	}
	if got := pathHit.Load(); got != "/models" {
		t.Errorf("expected Ping to hit /models, got %q", got)
	}
}

func TestOpenAIProvider_Ping_Unreachable(t *testing.T) {
	p := NewOpenAIProvider("deepseek-1", &config.ProviderConfig{Type: "openai", APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err := p.Ping(); err == nil {
		t.Fatal("expected Ping to fail on unreachable host, got nil")
	}
}

func TestOpenAIProvider_ListModels(t *testing.T) {
	var pathHit atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathHit.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"gpt-4o"},
			{"id":"gpt-4o-mini"},
			{"id":""}
		]}`))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	models, err := p.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models (empty id filtered), got %v", models)
	}
	if models[0] != "gpt-4o" || models[1] != "gpt-4o-mini" {
		t.Errorf("unexpected models: %v", models)
	}
	if got := pathHit.Load(); got != "/models" {
		t.Errorf("expected ListModels to hit /models, got %q", got)
	}
}

func TestOpenAIProvider_ListModels_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	_, err := p.ListModels()
	if err == nil {
		t.Fatal("expected error from ListModels, got nil")
	}
	if !strings.Contains(err.Error(), "list models API error") {
		t.Errorf("expected 'list models API error' in error, got %q", err.Error())
	}
}

func TestOpenAIProvider_SendMessage_ConvertsResponse(t *testing.T) {
	var capturedBody atomic.Value
	var capturedPath atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath.Store(r.URL.Path)
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

	p := newOpenAIProvider("deepseek-1", srv)

	req := &anthropic.MessagesRequest{
		Model:     "claude-test",
		MaxTokens: 128,
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}
	resp, err := p.SendMessage(context.Background(), req, "gpt-4o-mini", 256)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hi there" {
		t.Errorf("expected converted content 'hi there', got %+v", resp.Content)
	}

	if got := capturedPath.Load(); got != "/v1/chat/completions" {
		t.Errorf("expected path /v1/chat/completions, got %q", got)
	}
	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"gpt-4o-mini"`) {
		t.Errorf("expected converted model in request body, got %q", bodyStr)
	}
}

func TestOpenAIProvider_SendMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	_, err := p.SendMessage(context.Background(), &anthropic.MessagesRequest{
		Model: "x", MaxTokens: 10, Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, "x", 10)
	if err == nil {
		t.Fatal("expected error from HTTP 429, got nil")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Errorf("expected 'status 429' in error, got %q", err.Error())
	}
}

func TestOpenAIProvider_StreamMessage(t *testing.T) {
	var capturedBody atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody.Store(string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	req := &anthropic.MessagesRequest{
		Model:     "original-model",
		MaxTokens: 128,
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	}
	rc, err := p.StreamMessage(context.Background(), req, "gpt-4o", 256)
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "content_block_delta") || !strings.Contains(out, "Hello") {
		t.Errorf("expected converted anthropic stream events, got %q", out)
	}

	// The request sent upstream must be the OpenAI-converted one.
	bodyStr := capturedBody.Load().(string)
	if !strings.Contains(bodyStr, `"model":"gpt-4o"`) {
		t.Errorf("expected converted model in stream request, got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, `"stream":true`) {
		t.Errorf("expected stream=true in request, got %q", bodyStr)
	}
	if req.Model != "original-model" {
		t.Errorf("caller's request was mutated: Model=%q", req.Model)
	}
}

func TestOpenAIProvider_StreamMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	_, err := p.StreamMessage(context.Background(), &anthropic.MessagesRequest{
		Model: "x", MaxTokens: 10, Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, "x", 10)
	if err == nil {
		t.Fatal("expected error from StreamMessage, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("expected 'status 502' in error, got %q", err.Error())
	}
}

func TestOpenAIProvider_StreamToWriter_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()

	p := newOpenAIProvider("deepseek-1", srv)
	err := p.StreamToWriter(context.Background(), io.Discard, nil, false, &anthropic.MessagesRequest{
		Model: "x", MaxTokens: 10, Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, "x", 10)
	if err == nil {
		t.Fatal("expected error from StreamToWriter, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("expected 'status 502' in error, got %q", err.Error())
	}
}
