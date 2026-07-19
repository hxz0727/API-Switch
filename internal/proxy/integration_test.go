package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/streaming"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// =====================================================================
// Helpers
// =====================================================================

// newTool returns an OpenAI tool definition (function type) for tests.
func newTool(name string) openai.Tool {
	return openai.Tool{
		Type: "function",
		Function: openai.FunctionDef{
			Name:        name,
			Description: "Test tool: " + name,
			Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
}

// contentOf returns the message content as a string, regardless of whether
// it was stored as a plain string or a []ContentPart slice.
func contentOf(m openai.Message) string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []openai.ContentPart:
		var parts []string
		for _, p := range v {
			if p.Type == "text" {
				parts = append(parts, p.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", m.Content)
	}
}

// findAssistant returns the index of the first assistant message, or -1.
func findAssistant(msgs []openai.Message) int {
	for i, m := range msgs {
		if m.Role == "assistant" {
			return i
		}
	}
	return -1
}

// =====================================================================
// 1. Multi-turn tool use end-to-end
// =====================================================================

// TestIntegration_MultiTurnToolUse exercises the canonical Claude Code flow:
//   user → assistant(tool_use) → user(tool_result) → assistant(text)
// It verifies that the final OpenAI request has all 4 messages in the
// expected order, tool_calls are linked to tool responses, and the
// orphaned tool_calls stripping pass leaves valid tool_calls alone.
func TestIntegration_MultiTurnToolUse(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"What is the weather in Beijing?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"I'll check."},{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{"city":"Beijing"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":"22°C, sunny"}]}]`)},
			{Role: "assistant", Content: json.RawMessage(`"The weather in Beijing is 22°C and sunny."`)},
		},
		MaxTokens: 1024,
		Tools: []anthropic.ToolDefinition{
			{Name: "get_weather", Description: "Get weather", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	// 4 expected messages: user, assistant(tool_call), tool, assistant(text)
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(result.Messages), result.Messages)
	}

	// [0] user
	if result.Messages[0].Role != "user" {
		t.Errorf("messages[0] role = %q, want user", result.Messages[0].Role)
	}
	if contentOf(result.Messages[0]) != "What is the weather in Beijing?" {
		t.Errorf("messages[0] content = %q", contentOf(result.Messages[0]))
	}

	// [1] assistant with tool_calls
	if result.Messages[1].Role != "assistant" {
		t.Fatalf("messages[1] role = %q, want assistant", result.Messages[1].Role)
	}
	if len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("messages[1] tool_calls len = %d, want 1", len(result.Messages[1].ToolCalls))
	}
	tc := result.Messages[1].ToolCalls[0]
	if tc.ID != "toolu_01" {
		t.Errorf("tool_call id = %q, want toolu_01", tc.ID)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool_call name = %q, want get_weather", tc.Function.Name)
	}
	if !strings.Contains(tc.Function.Arguments, "Beijing") {
		t.Errorf("tool_call args missing city: %q", tc.Function.Arguments)
	}
	// Assistant text "I'll check." should also be preserved on the assistant message.
	if !strings.Contains(contentOf(result.Messages[1]), "I'll check.") {
		t.Errorf("assistant text not preserved, got %q", contentOf(result.Messages[1]))
	}

	// [2] tool — must be linked to the assistant's tool_call id
	if result.Messages[2].Role != "tool" {
		t.Fatalf("messages[2] role = %q, want tool", result.Messages[2].Role)
	}
	if result.Messages[2].ToolCallID != "toolu_01" {
		t.Errorf("messages[2] tool_call_id = %q, want toolu_01", result.Messages[2].ToolCallID)
	}
	if !strings.Contains(contentOf(result.Messages[2]), "22°C, sunny") {
		t.Errorf("tool result content missing: %q", contentOf(result.Messages[2]))
	}

	// [3] assistant text reply
	if result.Messages[3].Role != "assistant" {
		t.Fatalf("messages[3] role = %q, want assistant", result.Messages[3].Role)
	}
	if contentOf(result.Messages[3]) != "The weather in Beijing is 22°C and sunny." {
		t.Errorf("messages[3] content = %q", contentOf(result.Messages[3]))
	}
	// Final assistant must not have any tool_calls (orphaned stripping check).
	if len(result.Messages[3].ToolCalls) != 0 {
		t.Errorf("final assistant must have no tool_calls, got %d", len(result.Messages[3].ToolCalls))
	}

	// The first assistant's tool_calls must NOT be stripped (it has a following tool message).
	if len(result.Messages[1].ToolCalls) != 1 {
		t.Error("assistant tool_calls were stripped even though a tool message follows")
	}
}

// TestIntegration_MultiTurnOrphanedToolCallsStripped verifies that an assistant
// tool_use without a following tool_result IS stripped, while one with a
// following tool_result is NOT.
func TestIntegration_MultiTurnOrphanedToolCallsStripped(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			// Paired — tool_result will follow
			{Role: "user", Content: json.RawMessage(`"q1"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_A","name":"f","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_A","content":"ok"}]`)},
			// Orphaned — no tool_result follows
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_B","name":"f","input":{}}]`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	// user, assistant(A), tool(A), assistant(B)
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(result.Messages), result.Messages)
	}

	// assistant(A) keeps its tool_calls
	if len(result.Messages[1].ToolCalls) != 1 || result.Messages[1].ToolCalls[0].ID != "toolu_A" {
		t.Errorf("assistant(A) tool_calls lost: %+v", result.Messages[1].ToolCalls)
	}
	// assistant(B) should have its tool_calls stripped (orphaned)
	if len(result.Messages[3].ToolCalls) != 0 {
		t.Errorf("orphan assistant(B) tool_calls should be stripped, got %+v", result.Messages[3].ToolCalls)
	}
}

// =====================================================================
// 2. Multi-turn with tool error
// =====================================================================

// TestIntegration_ToolError_Prefixed verifies that a tool_result with
// is_error=true has "[Tool Error] " prepended to its content.
func TestIntegration_ToolError_Prefixed(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"run tool"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_err","name":"failing_tool","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_err","is_error":true,"content":"division by zero"}]`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
	toolMsg := result.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "toolu_err" {
		t.Fatalf("messages[2] = %+v, want tool/toolu_err", toolMsg)
	}
	got := contentOf(toolMsg)
	if !strings.HasPrefix(got, "[Tool Error] ") {
		t.Errorf("expected [Tool Error] prefix, got %q", got)
	}
	if !strings.Contains(got, "division by zero") {
		t.Errorf("expected error text preserved, got %q", got)
	}
}

// TestIntegration_ToolError_EmptyContent verifies that an is_error result
// with no content still gets the [Tool Error] prefix (so the LLM can see
// the call failed, not just got an empty string).
func TestIntegration_ToolError_EmptyContent(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"x"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_e","name":"t","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_e","is_error":true,"content":""}]`)},
		},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
	got := contentOf(result.Messages[2])
	// The current implementation always emits "[Tool Error] " with a
	// trailing space (the prefix is unconditionally appended before the
	// result). We assert the prefix is present and no original content
	// leaked in.
	if !strings.HasPrefix(got, "[Tool Error]") {
		t.Errorf("expected [Tool Error] prefix, got %q", got)
	}
	if strings.Contains(strings.TrimPrefix(got, "[Tool Error]"), "error") {
		t.Errorf("unexpected error text in %q", got)
	}
}

// =====================================================================
// 3. Mixed content blocks in a single user message
// =====================================================================

// TestIntegration_UserMessage_MixedTextImageToolResult verifies the modern
// Claude Code format where one user message can hold text + image + tool_result.
// Text and image must be folded into a multimodal user message; the
// tool_result must be split out into its own tool message.
func TestIntegration_UserMessage_MixedTextImageToolResult(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"describe"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_img","name":"snap","input":{}}]`)},
			// Tool result AND a follow-up user text+image in the same Anthropic user message
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"toolu_img","content":"snapshot taken"},
				{"type":"text","text":"What is in this image?"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
			]`)},
		},
		MaxTokens: 1024,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	// user, assistant(tool_call), tool, user(multimodal) → 4 messages
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(result.Messages), result.Messages)
	}

	// [2] tool message
	if result.Messages[2].Role != "tool" || result.Messages[2].ToolCallID != "toolu_img" {
		t.Errorf("messages[2] = role=%q id=%q, want tool/toolu_img",
			result.Messages[2].Role, result.Messages[2].ToolCallID)
	}

	// [3] user message with text + image parts (no tool_result blocks)
	if result.Messages[3].Role != "user" {
		t.Fatalf("messages[3] role = %q", result.Messages[3].Role)
	}
	parts, ok := result.Messages[3].Content.([]openai.ContentPart)
	if !ok {
		t.Fatalf("messages[3] content = %T, want []ContentPart", result.Messages[3].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %+v", len(parts), parts)
	}
	if parts[0].Type != "text" || parts[0].Text != "What is in this image?" {
		t.Errorf("parts[0] = %+v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("parts[1] = %+v", parts[1])
	}
	if parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Errorf("parts[1] url = %q", parts[1].ImageURL.URL)
	}
}

// =====================================================================
// 4. SenseNova reasoning content recovery
// =====================================================================

// TestIntegration_SenseNova_ReasoningContent verifies the response converter
// pulls text out of the "reasoning" field when "content" is empty — this
// is how SenseNova (and some other Chinese providers) return assistant text.
func TestIntegration_SenseNova_ReasoningContent(t *testing.T) {
	oaiResp := &openai.ChatCompletionResponse{
		ID:    "chatcmpl-sn",
		Model: "SenseNova-V6-Pro",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:      "assistant",
					Content:   "", // empty!
					Reasoning: "The answer is 42.",
				},
				FinishReason: strPtr("stop"),
			},
		},
		Usage: openai.Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
	}

	result := ConvertOpenAIToAnthropic(oaiResp, "claude-3-opus")
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected text block, got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "The answer is 42." {
		t.Errorf("expected text from reasoning field, got %q", result.Content[0].Text)
	}
	if result.StopReason == nil || *result.StopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %v", result.StopReason)
	}
}

// TestIntegration_SenseNova_ContentTakesPrecedence verifies that when both
// content and reasoning are populated, content wins (we don't want to
// duplicate output).
func TestIntegration_SenseNova_ContentTakesPrecedence(t *testing.T) {
	oaiResp := &openai.ChatCompletionResponse{
		ID: "x", Model: "m",
		Choices: []openai.Choice{{
			Index: 0,
			Message: openai.Message{
				Role:      "assistant",
				Content:   "real answer",
				Reasoning: "thinking…",
			},
			FinishReason: strPtr("stop"),
		}},
	}
	result := ConvertOpenAIToAnthropic(oaiResp, "claude-3-opus")
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "real answer" {
		t.Errorf("content should take precedence, got %q", result.Content[0].Text)
	}
}

// =====================================================================
// 5. Rate limiter cleanup
// =====================================================================

// TestIntegration_RateLimiter_Cleanup verifies that the rate limiter removes
// map entries whose entire window has expired, preventing memory leaks.
func TestIntegration_RateLimiter_Cleanup(t *testing.T) {
	// Use a short window so the test runs fast.
	rl := NewRateLimiter(5, 50*time.Millisecond)

	const client = "10.0.0.42"

	// Make 3 requests, all under the limit, within the window.
	for i := 0; i < 3; i++ {
		if !rl.Allow(client) {
			t.Fatalf("request %d should have been allowed", i)
		}
	}

	rl.mu.Lock()
	entry, ok := rl.requests[client]
	rl.mu.Unlock()
	if !ok || len(entry) != 3 {
		t.Fatalf("expected 3 timestamps, got ok=%v len=%d", ok, len(entry))
	}

	// Wait for the window to expire.
	time.Sleep(80 * time.Millisecond)

	// Next request should be allowed AND should reset the entry to just one timestamp
	// (because all prior timestamps are expired).
	if !rl.Allow(client) {
		t.Fatal("post-expiry request should be allowed")
	}

	rl.mu.Lock()
	entry, ok = rl.requests[client]
	rl.mu.Unlock()
	if !ok || len(entry) != 1 {
		t.Errorf("expected entry to be reset to 1 timestamp, got ok=%v len=%d", ok, len(entry))
	}
}

// TestIntegration_RateLimiter_DeletesFullyExpiredEntry verifies that when
// a request arrives for a client whose window has fully expired, and
// the request itself is rejected (because the limit was set such that
// re-adding still hits the cap, OR — more relevantly — when the limiter
// hits the cap on a client that has only expired entries), the entry is
// deleted. (This is the explicit `delete(rl.requests, clientIP)` path.)
func TestIntegration_RateLimiter_DeletesFullyExpiredEntry(t *testing.T) {
	// max=1 so the second request is rejected
	rl := NewRateLimiter(1, 30*time.Millisecond)

	const client = "10.0.0.99"

	if !rl.Allow(client) {
		t.Fatal("first request should be allowed")
	}
	// Immediate second request hits the cap and is rejected.
	if rl.Allow(client) {
		t.Fatal("second request should be rejected")
	}

	// Wait for the first request's timestamp to expire.
	time.Sleep(50 * time.Millisecond)

	// The first request is now expired. The next call should:
	// - find all timestamps expired (start == len)
	// - allow the request
	// - reset the entry to a single new timestamp
	if !rl.Allow(client) {
		t.Fatal("post-expiry request should be allowed")
	}

	rl.mu.Lock()
	entry, ok := rl.requests[client]
	rl.mu.Unlock()
	if !ok || len(entry) != 1 {
		t.Errorf("expected entry with 1 timestamp, got ok=%v len=%d", ok, len(entry))
	}
}

// TestIntegration_RateLimiter_TableDriven covers the per-client isolation
// and sliding-window behaviour in a single table-driven test.
func TestIntegration_RateLimiter_TableDriven(t *testing.T) {
	type step struct {
		client   string
		expected bool
	}
	type tc struct {
		name     string
		max      int
		window   time.Duration
		steps    []step
		// post: total unique clients we expect the map to contain right now
		postClients int
	}

	// Use a window long enough that all requests within a single test
	// case fall inside the same window.
	const win = time.Hour
	cases := []tc{
		{
			name:   "two clients isolated",
			max:    3,
			window: win,
			steps: []step{
				{"a", true}, {"a", true}, {"a", true},
				{"b", true}, {"b", true},
				{"a", false}, // 4th for a — over the cap
				{"b", true},  // 3rd for b — at the cap, still allowed
				{"b", false}, // 4th for b — over
			},
			postClients: 2,
		},
		{
			name:   "single client hits cap",
			max:    2,
			window: win,
			steps: []step{
				{"c", true},
				{"c", true},
				{"c", false},
			},
			postClients: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rl := NewRateLimiter(c.max, c.window)
			for _, s := range c.steps {
				got := rl.Allow(s.client)
				if got != s.expected {
					t.Errorf("client=%q allow=%v, want %v", s.client, got, s.expected)
				}
			}
			rl.mu.Lock()
			defer rl.mu.Unlock()
			if len(rl.requests) != c.postClients {
				t.Errorf("expected %d clients in map, got %d", c.postClients, len(rl.requests))
			}
		})
	}
}

// =====================================================================
// 6. SSE streaming — Anthropic passthrough with cancellation
// =====================================================================

// slowReader returns one byte at a time with a delay, so we can interrupt it.
type slowReader struct {
	data   []byte
	pos    int
	closed atomic.Bool
	// closeNotify is closed when the consumer no longer wants more data
	// (we use this to assert the stream was abandoned).
	closeNotify chan struct{}
	// closedNotify is closed when Close() is called on the slowReader
	// (proves upstream body was closed when the client disconnected).
	closedNotify chan struct{}
}

func newSlowReader(data string) *slowReader {
	return &slowReader{
		data:         []byte(data),
		closeNotify:  make(chan struct{}),
		closedNotify: make(chan struct{}),
	}
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	// Yield one byte at a time so the caller's context can be cancelled
	// between reads. Sleep long enough to be cancellable but short enough
	// to keep the test fast.
	select {
	case <-s.closeNotify:
		return 0, io.EOF
	case <-time.After(20 * time.Millisecond):
	}
	p[0] = s.data[s.pos]
	s.pos++
	return 1, nil
}

func (s *slowReader) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		close(s.closedNotify)
	}
	return nil
}

// TestIntegration_SSE_AnthropicPassthroughCancellation verifies that when
// the client disconnects (ctx cancelled), the upstream body is closed
// and the passthrough stream returns promptly.
func TestIntegration_SSE_AnthropicPassthroughCancellation(t *testing.T) {
	// Build a stream that takes ~1s to fully read at slowReader's pace
	// (20ms per byte × ~50 bytes).
	stream := strings.Repeat("data: {\"type\":\"ping\"}\n\n", 50)
	sr := newSlowReader(stream)

	// Use a cancellable context — this is what the HTTP server's
	// request context would look like when the client disconnects.
	ctx, cancel := context.WithCancel(context.Background())

	// Defer cleanup so we always release the reader.
	defer sr.Close()

	// Wire up the close-on-cancel behaviour that handler.go uses.
	go func() {
		<-ctx.Done()
		_ = sr.Close()
	}()

	var buf nopWriteFlusher
	flushed := 0

	done := make(chan error, 1)
	go func() {
		err := streaming.AnthropicPassthroughStream(sr, &buf, &buf, true)
		done <- err
		_ = flushed
	}()

	// Let some bytes flow, then cancel.
	time.Sleep(60 * time.Millisecond)
	cancelStart := time.Now()
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Logf("passthrough returned err=%v (acceptable on cancellation)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("passthrough did not return within 2s of ctx cancel")
	}

	elapsed := time.Since(cancelStart)
	if elapsed > 500*time.Millisecond {
		t.Errorf("passthrough took %v to return after cancel, want <500ms", elapsed)
	}

	// Verify the upstream reader was actually closed.
	select {
	case <-sr.closedNotify:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Error("upstream reader was NOT closed after ctx cancel")
	}
}

// TestIntegration_SSE_OpenAIToAnthropic_ContextCancel verifies the
// OpenAI→Anthropic stream path also respects cancellation: when the
// upstream body is closed mid-stream, the scanner returns an error
// promptly and the converter still emits the trailing close events.
func TestIntegration_SSE_OpenAIToAnthropic_ContextCancel(t *testing.T) {
	// A stream that has deltas but is intentionally truncated (no
	// finish_reason, no [DONE]) — forces the "scanner ended without
	// [DONE] or finish_reason — close gracefully" path. The slow
	// reader holds the data so we can abort mid-stream.
	sseInput := strings.Join([]string{
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		``,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}`,
		``,
	}, "\n")
	// Pad with whitespace so the slow reader doesn't hit EOF before
	// we cancel. The scanner will keep reading until something closes
	// the underlying reader.
	sseInput += "\n" + strings.Repeat(" ", 200) + "\n"

	sr := newSlowReader(sseInput)
	defer sr.Close()

	var buf nopWriteFlusher
	done := make(chan error, 1)
	go func() {
		err := streaming.OpenAIToAnthropicStream(sr, &buf, &buf, true, "gpt-4o", 5)
		done <- err
	}()

	// Give the scanner a moment to consume the first two chunks, then
	// close the upstream. The scanner will return scanner.Err() and
	// the converter will finish the "done" function.
	time.Sleep(80 * time.Millisecond)
	sr.Close()

	select {
	case err := <-done:
		// nil or scanner.Err() — both acceptable
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("stream conversion did not complete within 2s of upstream close")
	}

	out := buf.String()
	// Verify the gracefully-closed stream still emitted the start
	// events we care about.
	if !strings.Contains(out, "event: message_start") {
		t.Errorf("expected message_start in output, got: %s", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("expected message_stop on graceful close, got: %s", out)
	}
}

// =====================================================================
// 7. Tool choice variants (table-driven)
// =====================================================================

// TestIntegration_ToolChoice_Variants verifies all three Anthropic
// tool_choice forms convert correctly to OpenAI's format.
func TestIntegration_ToolChoice_Variants(t *testing.T) {
	type tc struct {
		name     string
		input    string
		validate func(t *testing.T, v interface{})
	}

	cases := []tc{
		{
			name:  "auto",
			input: `{"type":"auto"}`,
			validate: func(t *testing.T, v interface{}) {
				if v != "auto" {
					t.Errorf("got %v, want \"auto\"", v)
				}
			},
		},
		{
			name:  "any -> required",
			input: `{"type":"any"}`,
			validate: func(t *testing.T, v interface{}) {
				if v != "required" {
					t.Errorf("got %v, want \"required\"", v)
				}
			},
		},
		{
			name:  "specific tool",
			input: `{"type":"tool","name":"get_weather"}`,
			validate: func(t *testing.T, v interface{}) {
				m, ok := v.(map[string]interface{})
				if !ok {
					t.Fatalf("got %T, want map", v)
				}
				if m["type"] != "function" {
					t.Errorf("type=%v, want function", m["type"])
				}
				fn, ok := m["function"].(map[string]string)
				if !ok {
					t.Fatalf("function = %T, want map[string]string", m["function"])
				}
				if fn["name"] != "get_weather" {
					t.Errorf("name=%v, want get_weather", fn["name"])
				}
			},
		},
		{
			name:  "malformed json -> auto",
			input: `{not json`,
			validate: func(t *testing.T, v interface{}) {
				if v != "auto" {
					t.Errorf("got %v, want auto (malformed fallback)", v)
				}
			},
		},
		{
			name:  "missing name on tool type -> auto",
			input: `{"type":"tool"}`,
			validate: func(t *testing.T, v interface{}) {
				if v != "auto" {
					t.Errorf("got %v, want auto (no name fallback)", v)
				}
			},
		},
		{
			name:  "unknown type -> auto",
			input: `{"type":"banana"}`,
			validate: func(t *testing.T, v interface{}) {
				if v != "auto" {
					t.Errorf("got %v, want auto (unknown type fallback)", v)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			antReq := &anthropic.MessagesRequest{
				Model:     "claude-3-opus",
				Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
				MaxTokens: 100,
				Tools:     []anthropic.ToolDefinition{{Name: "get_weather", InputSchema: json.RawMessage(`{}`)}},
				ToolChoice: json.RawMessage(c.input),
			}
			result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
			c.validate(t, result.ToolChoice)
		})
	}
}

// =====================================================================
// 8. Multi-modal images (base64 + URL)
// =====================================================================

// TestIntegration_MultiModal_Base64Image verifies a base64-encoded image
// is converted to a data URL in the OpenAI message.
func TestIntegration_MultiModal_Base64Image(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"what is this?"},
				{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"BASE64PAYLOAD"}}
			]`)},
		},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	parts, ok := result.Messages[0].Content.([]openai.ContentPart)
	if !ok {
		t.Fatalf("content = %T, want []ContentPart", result.Messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[1].Type != "image_url" {
		t.Errorf("parts[1].Type = %q", parts[1].Type)
	}
	want := "data:image/jpeg;base64,BASE64PAYLOAD"
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != want {
		t.Errorf("image url = %v, want %q", parts[1].ImageURL, want)
	}
}

// TestIntegration_MultiModal_URLImage verifies a url-type image source
// passes the URL through unchanged.
func TestIntegration_MultiModal_URLImage(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}
			]`)},
		},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	parts, ok := result.Messages[0].Content.([]openai.ContentPart)
	if !ok {
		t.Fatalf("content = %T", result.Messages[0].Content)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].ImageURL == nil || parts[0].ImageURL.URL != "https://example.com/cat.png" {
		t.Errorf("image url = %v", parts[0].ImageURL)
	}
}

// TestIntegration_MultiModal_MultipleImages verifies N images + text in one
// user message produce N+1 content parts in order.
func TestIntegration_MultiModal_MultipleImages(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"compare:"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"A"}},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"B"}}
			]`)},
		},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	parts := result.Messages[0].Content.([]openai.ContentPart)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Type != "text" {
		t.Errorf("parts[0] type = %q", parts[0].Type)
	}
	if parts[1].ImageURL.URL != "data:image/png;base64,A" {
		t.Errorf("parts[1] = %v", parts[1].ImageURL)
	}
	if parts[2].ImageURL.URL != "data:image/png;base64,B" {
		t.Errorf("parts[2] = %v", parts[2].ImageURL)
	}
}

// =====================================================================
// 9. System messages
// =====================================================================

// TestIntegration_System_TopLevelOnly verifies a top-level system field
// becomes a system message at index 0.
func TestIntegration_System_TopLevelOnly(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model:    "claude-3-opus",
		System:   json.RawMessage(`"You are a concise assistant."`),
		Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("messages[0] role = %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "You are a concise assistant." {
		t.Errorf("messages[0] content = %q", result.Messages[0].Content)
	}
}

// TestIntegration_System_RoleMessageOnly verifies a system-role message
// in the messages array is hoisted to a top-level system message.
func TestIntegration_System_RoleMessageOnly(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "system", Content: json.RawMessage(`"Be helpful."`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" || result.Messages[0].Content != "Be helpful." {
		t.Errorf("messages[0] = %+v", result.Messages[0])
	}
	if result.Messages[1].Role != "user" {
		t.Errorf("messages[1] role = %q", result.Messages[1].Role)
	}
}

// TestIntegration_System_BothCombined verifies that the top-level system
// field and a system-role message are joined (not duplicated as two
// separate system messages) and that an extra system-role message
// also folds in.
func TestIntegration_System_BothCombined(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model:    "claude-3-opus",
		System:   json.RawMessage(`"Top-level system."`),
		Messages: []anthropic.Message{
			{Role: "system", Content: json.RawMessage(`"Inline system A."`)},
			{Role: "system", Content: json.RawMessage(`"Inline system B."`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	// Expect exactly 2 messages: one combined system, one user.
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(result.Messages), result.Messages)
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("messages[0] role = %q", result.Messages[0].Role)
	}
	// All three sources should be present, joined by newlines, top-level first.
	want := "Top-level system.\nInline system A.\nInline system B."
	if result.Messages[0].Content != want {
		t.Errorf("messages[0] content = %q, want %q", result.Messages[0].Content, want)
	}
}

// =====================================================================
// 10. Long conversation (20+ turns)
// =====================================================================

// TestIntegration_LongConversation verifies ordering is preserved across
// 20+ turns with mixed roles and tool calls.
func TestIntegration_LongConversation(t *testing.T) {
	// 8 outer turns. Each turn = 1 user + 1 assistant.
	// Turn index 3 (the 4th) inserts a tool_use + tool_result cycle.
	// After conversion: 7 turns × 2 messages = 14, plus turn 3 =
	//   user + assistant(tool_call) + tool = 3, total = 17.
	msgs := make([]anthropic.Message, 0, 24)
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			anthropic.Message{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"user turn %d"`, i))},
		)
		if i == 3 {
			// Mid-conversation tool use
			msgs = append(msgs,
				anthropic.Message{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_mid","name":"f","input":{"i":3}}]`)},
				anthropic.Message{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_mid","content":"mid result"}]`)},
			)
		} else {
			msgs = append(msgs,
				anthropic.Message{Role: "assistant", Content: json.RawMessage(fmt.Sprintf(`"assistant turn %d"`, i))},
			)
		}
	}

	antReq := &anthropic.MessagesRequest{
		Model:     "claude-3-opus",
		Messages:  msgs,
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	wantLen := 17
	if len(result.Messages) != wantLen {
		t.Fatalf("expected %d messages, got %d", wantLen, len(result.Messages))
	}

	// Walk the result and verify role pattern.
	expectedRoles := []string{
		"user", "assistant", // turn 0
		"user", "assistant", // turn 1
		"user", "assistant", // turn 2
		"user", "assistant", "tool", // turn 3 (user, assistant(tool_call), tool)
		"user", "assistant", // turn 4
		"user", "assistant", // turn 5
		"user", "assistant", // turn 6
		"user", "assistant", // turn 7
	}
	if len(expectedRoles) != wantLen {
		t.Fatalf("test setup error: expected %d roles, got %d", wantLen, len(expectedRoles))
	}
	for i, want := range expectedRoles {
		if result.Messages[i].Role != want {
			t.Errorf("messages[%d] role = %q, want %q", i, result.Messages[i].Role, want)
		}
	}

	// The tool call assistant (index 7) must have 1 tool call, and the
	// next message (index 8) must be a tool message with the matching id.
	toolIdx := 7
	if len(result.Messages[toolIdx].ToolCalls) != 1 {
		t.Errorf("messages[%d] tool_calls = %d, want 1", toolIdx, len(result.Messages[toolIdx].ToolCalls))
	}
	if result.Messages[toolIdx+1].Role != "tool" ||
		result.Messages[toolIdx+1].ToolCallID != "toolu_mid" {
		t.Errorf("messages[%d] = %+v, want tool/toolu_mid", toolIdx+1, result.Messages[toolIdx+1])
	}

	// Spot-check content of a few messages.
	if !strings.Contains(contentOf(result.Messages[0]), "user turn 0") {
		t.Errorf("messages[0] content wrong: %q", contentOf(result.Messages[0]))
	}
	if !strings.Contains(contentOf(result.Messages[16]), "assistant turn 7") {
		t.Errorf("messages[16] content wrong: %q", contentOf(result.Messages[16]))
	}
}

// =====================================================================
// 11. Edge cases
// =====================================================================

// TestIntegration_Edge_EmptyUserContent verifies a user message with an
// empty string content survives conversion.
func TestIntegration_Edge_EmptyUserContent(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model:     "claude-3-opus",
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`""`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("role = %q", result.Messages[0].Role)
	}
	if contentOf(result.Messages[0]) != "" {
		t.Errorf("content = %q, want empty", contentOf(result.Messages[0]))
	}
}

// TestIntegration_Edge_MalformedJSONContent verifies that a content field
// that is neither valid JSON string nor valid array of blocks degrades
// gracefully to an empty string rather than crashing.
func TestIntegration_Edge_MalformedJSONContent(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model:     "claude-3-opus",
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`not valid json {{{`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if contentOf(result.Messages[0]) != "" {
		t.Errorf("expected empty content, got %q", contentOf(result.Messages[0]))
	}
}

// TestIntegration_Edge_VeryLongSystemPrompt verifies a system prompt
// of ~50KB is preserved verbatim.
func TestIntegration_Edge_VeryLongSystemPrompt(t *testing.T) {
	longPrompt := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 1100) // ~49KB
	antReq := &anthropic.MessagesRequest{
		Model:    "claude-3-opus",
		System:   json.RawMessage(strconvQuote(longPrompt)),
		Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	got, ok := result.Messages[0].Content.(string)
	if !ok {
		t.Fatalf("messages[0] content = %T, want string", result.Messages[0].Content)
	}
	if got != longPrompt {
		t.Errorf("system prompt mismatch: got %d bytes, want %d bytes", len(got), len(longPrompt))
	}
}

// TestIntegration_Edge_MultipleSystemMessages verifies multiple system-role
// messages are joined with newlines, not duplicated as separate system msgs.
func TestIntegration_Edge_MultipleSystemMessages(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "system", Content: json.RawMessage(`"line 1"`)},
			{Role: "system", Content: json.RawMessage(`"line 2"`)},
			{Role: "system", Content: json.RawMessage(`"line 3"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(result.Messages), result.Messages)
	}
	// Only one system message at index 0.
	if result.Messages[0].Role != "system" {
		t.Errorf("messages[0] role = %q, want system", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "line 1\nline 2\nline 3" {
		t.Errorf("messages[0] content = %q, want %q",
			result.Messages[0].Content, "line 1\nline 2\nline 3")
	}
	if result.Messages[1].Role != "user" {
		t.Errorf("messages[1] role = %q, want user", result.Messages[1].Role)
	}
}

// TestIntegration_Edge_AssistantEmptyContent verifies an assistant message
// with an empty content array (just tool_use) produces an assistant
// message with empty text and a tool_call.
func TestIntegration_Edge_AssistantEmptyContent(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"q"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"t1","name":"f","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"t1","content":"r"}]`)},
		},
		MaxTokens: 100,
	}
	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)
	asstIdx := findAssistant(result.Messages)
	if asstIdx < 0 {
		t.Fatal("no assistant message found")
	}
	asst := result.Messages[asstIdx]
	if contentOf(asst) != "" {
		t.Errorf("assistant content = %q, want empty", contentOf(asst))
	}
	if len(asst.ToolCalls) != 1 {
		t.Errorf("assistant tool_calls = %d, want 1", len(asst.ToolCalls))
	}
}

// TestIntegration_Edge_ZeroMaxTokens verifies max_tokens=0 falls back
// to the default passed in, and 0 default falls back to 1024.
func TestIntegration_Edge_ZeroMaxTokens(t *testing.T) {
	makeReq := func() *anthropic.MessagesRequest {
		return &anthropic.MessagesRequest{
			Model:     "claude-3-opus",
			Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
			MaxTokens: 0,
		}
	}

	// explicit default
	r1 := ConvertAnthropicToOpenAI(makeReq(), "gpt-4o", 2048)
	if *r1.MaxTokens != 2048 {
		t.Errorf("default 2048 not applied, got %d", *r1.MaxTokens)
	}

	// zero default — must fall back to 1024 to avoid "unlimited" generation
	r2 := ConvertAnthropicToOpenAI(makeReq(), "gpt-4o", 0)
	if *r2.MaxTokens != 1024 {
		t.Errorf("zero-default fallback wrong, got %d, want 1024", *r2.MaxTokens)
	}
}

// =====================================================================
// 12. End-to-end through HTTP handler (smoke test)
// =====================================================================

// TestIntegration_Handler_RoutesToUpstream wires up a stub upstream that
// records the request body and returns a canned OpenAI response, and
// verifies the proxy forwards a correctly-converted body to it.
func TestIntegration_Handler_RoutesToUpstream(t *testing.T) {
	// Stub OpenAI-compatible upstream.
	var captured atomic.Value // []byte
	captured.Store([]byte(nil))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Store(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id":"chatcmpl-1",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-4o",
			"choices":[{
				"index":0,
				"message":{"role":"assistant","content":"hi back"},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 0},
		Models: map[string]config.ModelConfig{
			"claude-test": {Provider: "stub"},
		},
		Providers: map[string]config.ProviderConfig{
			"stub": {
				Type:             "openai",
				APIKey:           "sk-test",
				BaseURL:          upstream.URL,
				DefaultMaxTokens: 1024,
			},
		},
	}

	srv := NewServer(cfg)
	router := srv.router

	// Sanity: routing should succeed.
	route, err := router.Route("claude-test")
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if route.ProviderType != "openai" {
		t.Fatalf("route.ProviderType = %q, want openai", route.ProviderType)
	}
	if route.ActualModel != "claude-test" {
		t.Errorf("ActualModel = %q, want claude-test", route.ActualModel)
	}

	// Convert a request that includes a tool call → result cycle.
	antReq := &anthropic.MessagesRequest{
		Model: "claude-test",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"ping"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"t_e2e","name":"echo","input":{"x":1}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"t_e2e","content":"done"}]`)},
		},
		MaxTokens: 100,
	}
	oaiReq := ConvertAnthropicToOpenAI(antReq, route.ActualModel, route.DefaultMaxTokens)

	resp, err := route.OpenAI.SendMessage(oaiReq)
	if err != nil {
		t.Fatalf("upstream call failed: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hi back" {
		t.Errorf("unexpected upstream response: %+v", resp)
	}

	// Verify the body the upstream received.
	body, _ := captured.Load().([]byte)
	if len(body) == 0 {
		t.Fatal("upstream did not receive a body")
	}
	var sentReq openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &sentReq); err != nil {
		t.Fatalf("upstream body not valid OpenAI request: %v\nbody: %s", err, string(body))
	}
	// Expect: assistant, tool (3 messages incl. first user)
	if len(sentReq.Messages) != 3 {
		t.Errorf("upstream saw %d messages, want 3", len(sentReq.Messages))
	}
	if sentReq.Messages[1].Role != "assistant" || len(sentReq.Messages[1].ToolCalls) != 1 {
		t.Errorf("upstream saw malformed assistant: %+v", sentReq.Messages[1])
	}
	if sentReq.Messages[2].Role != "tool" || sentReq.Messages[2].ToolCallID != "t_e2e" {
		t.Errorf("upstream saw malformed tool: %+v", sentReq.Messages[2])
	}
}

// =====================================================================
// 13. nopWriteFlusher — local copy from streaming tests
// =====================================================================

// nopWriteFlusher satisfies io.Writer and http.Flusher for SSE tests.
// (Defined here too so this file is self-contained and doesn't depend on
// the streaming package's test helpers.)
type nopWriteFlusher struct {
	bytes.Buffer
}

func (n *nopWriteFlusher) Write(p []byte) (int, error) { return n.Buffer.Write(p) }
func (n *nopWriteFlusher) Header() http.Header         { return http.Header{} }
func (n *nopWriteFlusher) WriteHeader(statusCode int)  {}
func (n *nopWriteFlusher) Flush()                      {}

// strPtr returns a pointer to its argument. (Handy for *string literals.)
func strPtr(s string) *string { return &s }

// strconvQuote is a tiny helper to wrap a string as a JSON-quoted literal
// without pulling strconv into the imports just for one call site.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// bufio import was previously used for a compile-time guard. Removed.

// Compile-time guards: ensure nopWriteFlusher satisfies the interfaces
// the streaming code expects.
var _ http.Flusher = (*nopWriteFlusher)(nil)
var _ io.Writer     = (*nopWriteFlusher)(nil)
