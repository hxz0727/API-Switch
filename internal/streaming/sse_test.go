package streaming

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hxz0727/API-Switch/internal/converter"
)

// mockFlusher implements http.Flusher for testing.
type mockFlusher struct {
	http.ResponseWriter
	flushed bool
}

func (m *mockFlusher) Header() http.Header       { return http.Header{} }
func (m *mockFlusher) Write(b []byte) (int, error) { return len(b), nil }
func (m *mockFlusher) WriteHeader(statusCode int) {}
func (m *mockFlusher) Flush()                     { m.flushed = true }

// nopWriteFlusher is a writer that also implements http.Flusher.
type nopWriteFlusher struct {
	bytes.Buffer
}

func (n *nopWriteFlusher) Header() http.Header       { return http.Header{} }
func (n *nopWriteFlusher) WriteHeader(statusCode int) {}
func (n *nopWriteFlusher) Flush()                     {}

func TestOpenAIToAnthropicStream_TextDeltas(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(output, "event: ping") {
		t.Error("missing ping event")
	}
	if !strings.Contains(output, "event: content_block_start") {
		t.Error("missing content_block_start event")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Error("missing content_block_delta event")
	}
	if !strings.Contains(output, `"text_delta"`) {
		t.Error("missing text_delta in output")
	}
	if !strings.Contains(output, `"Hello"`) {
		t.Error("missing 'Hello' in output")
	}
	if !strings.Contains(output, `" world"`) {
		t.Error("missing ' world' in output")
	}
	if !strings.Contains(output, "event: content_block_stop") {
		t.Error("missing content_block_stop event")
	}
	if !strings.Contains(output, "event: message_delta") {
		t.Error("missing message_delta event")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestOpenAIToAnthropicStream_ToolCallDeltas(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-002","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-002","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-002","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-002","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "event: content_block_start") {
		t.Error("missing content_block_start for tool_use")
	}
	if !strings.Contains(output, `"tool_use"`) {
		t.Error("missing tool_use type in output")
	}
	if !strings.Contains(output, `"get_weather"`) {
		t.Error("missing tool name 'get_weather'")
	}
	if !strings.Contains(output, `"input_json_delta"`) {
		t.Error("missing input_json_delta")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestOpenAIToAnthropicStream_EmptyInput(t *testing.T) {
	reader := strings.NewReader("")
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("missing message_start event for empty input")
	}
	if !strings.Contains(output, "event: ping") {
		t.Error("missing ping event for empty input")
	}
}

func TestOpenAIToAnthropicStream_InvalidJSON(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {invalid json`,
		``,
		`data: {"id":"chatcmpl-003","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"ok"`) {
		t.Error("expected valid chunk to be processed after malformed one")
	}
}

func TestOpenAIToAnthropicStream_NonSSELines(t *testing.T) {
	sseInput := strings.Join([]string{
		``, // blank line
		`not a data: line`,
		`data: {"id":"chatcmpl-004","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"test"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"test"`) {
		t.Error("expected test content in output")
	}
}

func TestOpenAIToAnthropicStream_NoChoices(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-005","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicPassthroughStream(t *testing.T) {
	sseInput := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_001"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := AnthropicPassthroughStream(reader, &buf, &buf, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("missing message_start in passthrough")
	}
	if !strings.Contains(output, `"text_delta"`) {
		t.Error("missing text_delta in passthrough")
	}
}

// Test converter.WriteAnthropicEvent directly
func TestWriteAnthropicEvent(t *testing.T) {
	var buf nopWriteFlusher
	payload := map[string]string{"type": "test_event", "data": "hello"}

	converter.WriteAnthropicEvent(&buf, &buf, true, "test", payload)

	output := buf.String()
	if !strings.Contains(output, "event: test") {
		t.Error("missing event line")
	}
	if !strings.Contains(output, "data: ") {
		t.Error("missing data line")
	}
	if !strings.Contains(output, `"type":"test_event"`) {
		t.Error("missing payload")
	}
}

// Test converter.MapFinishReason
func TestMapFinishReason_Nil(t *testing.T) {
	result := converter.MapFinishReason(nil)
	if result == nil || *result != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", result)
	}
}

func TestMapFinishReason_Stop(t *testing.T) {
	s := "stop"
	result := converter.MapFinishReason(&s)
	if result == nil || *result != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", result)
	}
}

func TestMapFinishReason_Length(t *testing.T) {
	s := "length"
	result := converter.MapFinishReason(&s)
	if result == nil || *result != "max_tokens" {
		t.Errorf("expected 'max_tokens', got %v", result)
	}
}

func TestOpenAIToAnthropicStream_SenseNovaReasoningField(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"sensenova-6.7","choices":[{"index":0,"delta":{"reasoning":"你好"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"sensenova-6.7","choices":[{"index":0,"delta":{"reasoning":"世界"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":1234567890,"model":"sensenova-6.7","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "sensenova-6.7", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"你好"`) {
		t.Errorf("missing '你好' in output (reasoning field should be used as content)\nOutput:\n%s", output)
	}
	if !strings.Contains(output, `"世界"`) {
		t.Errorf("missing '世界' in output (reasoning field should be used as content)\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestOpenAIToAnthropicStream_ContentOverReasoning(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello","reasoning":"thinking..."},"finish_reason":null}]}`,
		``,
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	reader := strings.NewReader(sseInput)
	var buf nopWriteFlusher

	err := OpenAIToAnthropicStream(reader, &buf, &buf, true, "test-model", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"Hello"`) {
		t.Errorf("missing 'Hello' in output (content should take priority over reasoning)")
	}
	if strings.Contains(output, `"thinking..."`) {
		t.Error("reasoning content leaked into output when content was present")
	}
}

func TestMapFinishReason_ToolCalls(t *testing.T) {
	s := "tool_calls"
	result := converter.MapFinishReason(&s)
	if result == nil || *result != "tool_use" {
		t.Errorf("expected 'tool_use', got %v", result)
	}
}

// Test converter.ToolCallAcc
func TestToolCallAcc(t *testing.T) {
	acc := &converter.ToolCallAcc{
		ID:         "call_001",
		Name:       "test_func",
		BlockIndex: 1,
	}
	acc.Arguments.WriteString(`{"key":"value"}`)

	if acc.ID != "call_001" {
		t.Errorf("expected id 'call_001', got %q", acc.ID)
	}
	if acc.Name != "test_func" {
		t.Errorf("expected name 'test_func', got %q", acc.Name)
	}
	if acc.BlockIndex != 1 {
		t.Errorf("expected blockIndex 1, got %d", acc.BlockIndex)
	}
	if acc.Arguments.String() != `{"key":"value"}` {
		t.Errorf("unexpected arguments: %q", acc.Arguments.String())
	}
}

// Ensure nopWriteFlusher satisfies the interfaces
var _ io.Writer = (*nopWriteFlusher)(nil)
var _ http.Flusher = (*nopWriteFlusher)(nil)
