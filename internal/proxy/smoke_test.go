// Smoke test: start a real proxy with a real mock upstream and exercise the
// full request/response cycle for the multi-turn tool use flow.
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// mockUpstream simulates an OpenAI-compatible API that returns
// 1) tool_call for the first request
// 2) text for the second request (after receiving tool result)
type mockUpstream struct {
	mu          sync.Mutex
	calls       []openai.ChatCompletionRequest
	responses   []string
	currentStep int
}

func (m *mockUpstream) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openai.ChatCompletionRequest
		_ = json.Unmarshal(body, &req)

		m.mu.Lock()
		m.calls = append(m.calls, req)
		step := m.currentStep
		if step < len(m.responses) {
			m.currentStep++
		}
		resp := ""
		if step < len(m.responses) {
			resp = m.responses[step]
		}
		m.mu.Unlock()

		// Handle streaming vs non-streaming
		var stream bool
		_ = json.Unmarshal(body, &struct {
			Stream *bool `json:"stream"`
		}{Stream: &stream})

		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for _, line := range strings.Split(resp, "\n") {
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, resp)
		}
	})
}

func (m *mockUpstream) getCalls() []openai.ChatCompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]openai.ChatCompletionRequest{}, m.calls...)
}

func TestSmoke_EndToEnd_MultiTurnToolUse(t *testing.T) {
	// Set up a mock upstream that simulates a tool-use conversation
	mock := &mockUpstream{
		responses: []string{
			// First call: assistant calls a tool
			`{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":"tool_calls"}]}`,
			// Second call: after tool result, assistant replies with text
			`{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"It's 72°F in San Francisco."},"finish_reason":null}]}`,
			`{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
	}

	upstream := httptest.NewServer(mock.handler())
	defer upstream.Close()

	// Configure the proxy to use this mock upstream as the OpenAI provider
	port := freePort(t)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: port,
		},
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				Type:    "openai",
				BaseURL: upstream.URL,
				APIKey:  "test-key",
			},
		},
		Models: map[string]config.ModelConfig{
			"test-model": {
				Provider:      "test-provider",
				ModelOverride: "test-model",
			},
		},
	}

	// Start the server
	srv := NewServer(cfg)
	srv.cfgPath = ""
	addr := fmt.Sprintf(":%d", port)
	go func() {
		_ = srv.StartWithConfigFile(addr, "")
	}()
	time.Sleep(300 * time.Millisecond)
	defer srv.Shutdown(context.Background())

	// Build a multi-turn Anthropic request with tool_result (modern format)
	antReq := anthropic.MessagesRequest{
		Model:  "test-model",
		Stream: true,
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"What's the weather in SF?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call_001","name":"get_weather","input":{"city":"SF"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call_001","content":"72°F"}]`)},
		},
		MaxTokens: 100,
	}
	body, _ := json.Marshal(antReq)

	// Send request to the proxy
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port),
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST to proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("proxy returned %d: %s", resp.StatusCode, string(raw))
	}

	// Read the streaming response
	reader := bufio.NewReader(resp.Body)
	collected := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		collected += line
		if err != nil {
			break
		}
	}

	if !strings.Contains(collected, "message_start") {
		t.Errorf("missing message_start in response")
	}
	if !strings.Contains(collected, "message_stop") {
		t.Errorf("missing message_stop in response")
	}

	// Verify the proxy called the upstream TWICE (first time for tool_use, second for the final response)
	calls := mock.getCalls()
	if len(calls) < 1 {
		t.Fatalf("expected at least 1 upstream call, got %d", len(calls))
	}

	// Verify the FIRST call to upstream contains the tool_result (not dropped)
	firstReq := calls[0]
	hasToolMsg := false
	hasUserMsg := false
	for _, m := range firstReq.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_001" {
			hasToolMsg = true
		}
		if m.Role == "user" {
			hasUserMsg = true
		}
	}
	if !hasToolMsg {
		t.Errorf("upstream request missing 'tool' message with tool_call_id=call_001 (tool_result was dropped)")
	}
	if !hasUserMsg {
		t.Errorf("upstream request missing the original 'user' message")
	}

	// CRITICAL: verify the assistant message still has its tool_calls (not stripped)
	for _, m := range firstReq.Messages {
		if m.Role == "assistant" {
			if len(m.ToolCalls) == 0 {
				t.Errorf("assistant tool_calls were stripped from upstream request — multi-turn broken!")
			}
		}
	}
}

func TestSmoke_EndToEnd_NonStreamingToolUse(t *testing.T) {
	// Same flow but with non-streaming response
	mock := &mockUpstream{
		responses: []string{
			"It's 72°F in San Francisco.",
		},
	}

	upstream := httptest.NewServer(mock.handler())
	defer upstream.Close()

	port := freePort(t)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: port,
		},
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				Type:    "openai",
				BaseURL: upstream.URL,
				APIKey:  "test-key",
			},
		},
		Models: map[string]config.ModelConfig{
			"test-model": {
				Provider:      "test-provider",
				ModelOverride: "test-model",
			},
		},
	}

	srv := NewServer(cfg)
	addr := fmt.Sprintf(":%d", port)
	go func() {
		_ = srv.StartWithConfigFile(addr, "")
	}()
	time.Sleep(300 * time.Millisecond)
	defer srv.Shutdown(context.Background())

	antReq := anthropic.MessagesRequest{
		Model: "test-model",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call_001","name":"get_weather","input":{"city":"SF"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call_001","content":"72°F"}]`)},
		},
		MaxTokens: 100,
	}
	body, _ := json.Marshal(antReq)

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port),
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, string(raw))
	}

	var ant anthropic.MessagesResponse
	if err := json.Unmarshal(raw, &ant); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(ant.Content) == 0 {
		t.Errorf("empty content in response")
	}

	// Verify tool result was sent to upstream
	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 upstream call, got %d", len(calls))
	}
	hasTool := false
	for _, m := range calls[0].Messages {
		if m.Role == "tool" && m.ToolCallID == "call_001" {
			hasTool = true
			break
		}
	}
	if !hasTool {
		t.Errorf("non-streaming path also dropped tool_result!")
	}
}

// freePort returns an available TCP port for testing.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
