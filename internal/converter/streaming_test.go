package converter

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a thread-safe writer that also implements http.Flusher,
// required because the heartbeat goroutine writes concurrently.
type safeBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Buffer.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Buffer.String()
}

func (s *safeBuffer) Header() http.Header       { return http.Header{} }
func (s *safeBuffer) WriteHeader(statusCode int) {}
func (s *safeBuffer) Flush()                     {}

// blockReader blocks on Read until released, then returns io.EOF.
type blockReader struct {
	release chan struct{}
}

func (b *blockReader) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

// errorWriter always fails writes.
type errorWriter struct{}

func (errorWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

var _ io.Writer = (*safeBuffer)(nil)
var _ http.Flusher = (*safeBuffer)(nil)

// --- OpenAIToAnthropicStream tests ---

func TestOpenAIToAnthropicStream_TextDeltas(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"event: message_start",
		"event: ping",
		"event: content_block_start",
		"event: content_block_delta",
		`"text_delta"`,
		`"Hello"`,
		`" world"`,
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestOpenAIToAnthropicStream_ToolCallDeltas(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c2","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"c2","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"c2","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"c2","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		`"tool_use"`,
		`"get_weather"`,
		`"input_json_delta"`,
		`"{\"city\":`,
		"event: content_block_start",
		"event: content_block_stop",
		"event: message_stop",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestOpenAIToAnthropicStream_ToolCallNilFunction(t *testing.T) {
	// tool_calls delta with a null function must be skipped without error
	sseInput := strings.Join([]string{
		`data: {"id":"c3","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":null}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"c3","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, `"tool_use"`) {
		t.Errorf("tool_use block should not be emitted for nil function: %s", output)
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestOpenAIToAnthropicStream_ReasoningFallback(t *testing.T) {
	// SenseNova style: text lives in the reasoning field
	sseInput := strings.Join([]string{
		`data: {"id":"c4","object":"chat.completion.chunk","model":"sensenova","choices":[{"index":0,"delta":{"reasoning":"你好"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c4","object":"chat.completion.chunk","model":"sensenova","choices":[{"index":0,"delta":{"reasoning":"世界"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c4","object":"chat.completion.chunk","model":"sensenova","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "sensenova", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"你好"`) || !strings.Contains(output, `"世界"`) {
		t.Errorf("reasoning deltas should be streamed as content: %s", output)
	}
}

func TestOpenAIToAnthropicStream_ContentOverReasoning(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c5","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello","reasoning":"hidden"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c5","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"Hello"`) {
		t.Errorf("content should be streamed: %s", output)
	}
	if strings.Contains(output, `"hidden"`) {
		t.Errorf("reasoning leaked when content present: %s", output)
	}
}

func TestOpenAIToAnthropicStream_DONEHandling(t *testing.T) {
	// [DONE] without a prior finish_reason triggers end_turn
	sseInput := "data: [DONE]\n\n"
	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_stop") {
		t.Errorf("expected message_stop after [DONE]: %s", output)
	}
	if !strings.Contains(output, `"end_turn"`) {
		t.Errorf("expected end_turn stop reason: %s", output)
	}
}

func TestOpenAIToAnthropicStream_FinishReason(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c6","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_delta") {
		t.Errorf("expected message_delta after finish_reason: %s", output)
	}
	if !strings.Contains(output, `"end_turn"`) {
		t.Errorf("expected end_turn: %s", output)
	}
}

func TestOpenAIToAnthropicStream_MissingChoices(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c7","object":"chat.completion.chunk","model":"gpt-4o","choices":[]}`,
		``,
		`data: {"id":"c7","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"ok"`) {
		t.Error("expected later chunk with choices to be processed")
	}
}

func TestOpenAIToAnthropicStream_InvalidJSON(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {invalid json`,
		``,
		`data: {"id":"c8","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"ok"`) {
		t.Error("expected valid chunk to be processed after malformed one")
	}
}

func TestOpenAIToAnthropicStream_NonSSELines(t *testing.T) {
	sseInput := strings.Join([]string{
		``,
		`not a data: line`,
		`event: something`,
		`data: {"id":"c9","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"test"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"test"`) {
		t.Error("expected valid data line to be processed")
	}
}

func TestOpenAIToAnthropicStream_DataWithoutSpace(t *testing.T) {
	sseInput := strings.Join([]string{
		`data:{"id":"c10","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"nospace"},"finish_reason":"stop"}]}`,
		``,
		`data:[DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"nospace"`) {
		t.Errorf("expected data line without space to be parsed: %s", output)
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Errorf("expected message_stop from [DONE]: %s", output)
	}
}

func TestOpenAIToAnthropicStream_EmptyInput(t *testing.T) {
	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(""), &buf, &buf, true, "gpt-4o", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("missing message_start for empty input")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop for empty input")
	}
}

func TestOpenAIToAnthropicStream_StreamUsageOverrides(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"c11","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":42,"total_tokens":52}}`,
		``,
		`data: {"id":"c11","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, &buf, true, "gpt-4o", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"output_tokens":42`) {
		t.Errorf("expected stream usage completion_tokens to override delta count: %s", buf.String())
	}
}

func TestOpenAIToAnthropicStream_WriteError(t *testing.T) {
	// message_start write fails -> error returned
	err := OpenAIToAnthropicStream(strings.NewReader("data: [DONE]\n\n"), errorWriter{}, nil, false, "gpt-4o", 0)
	if err == nil {
		t.Fatal("expected error when writer fails")
	}
	if !strings.Contains(err.Error(), "write message_start") {
		t.Errorf("expected message_start write error, got: %v", err)
	}
}

func TestOpenAIToAnthropicStream_NoFlush(t *testing.T) {
	// canFlush=false must still work (no flusher calls)
	sseInput := strings.Join([]string{
		`data: {"id":"c12","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf safeBuffer
	err := OpenAIToAnthropicStream(strings.NewReader(sseInput), &buf, nil, false, "gpt-4o", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"hi"`) {
		t.Error("expected content to be written without flushing")
	}
}

// --- MapFinishReason tests ---

func TestMapFinishReason_Nil(t *testing.T) {
	got := MapFinishReason(nil)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapFinishReason_Stop(t *testing.T) {
	s := "stop"
	got := MapFinishReason(&s)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapFinishReason_Length(t *testing.T) {
	s := "length"
	got := MapFinishReason(&s)
	if got == nil || *got != "max_tokens" {
		t.Errorf("expected 'max_tokens', got %v", got)
	}
}

func TestMapFinishReason_ToolCalls(t *testing.T) {
	s := "tool_calls"
	got := MapFinishReason(&s)
	if got == nil || *got != "tool_use" {
		t.Errorf("expected 'tool_use', got %v", got)
	}
}

func TestMapFinishReason_ContentFilter(t *testing.T) {
	s := "content_filter"
	got := MapFinishReason(&s)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapFinishReason_Unknown(t *testing.T) {
	s := "weird"
	got := MapFinishReason(&s)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn' for unknown reason, got %v", got)
	}
}

// --- WriteAnthropicEvent tests ---

func TestWriteAnthropicEvent(t *testing.T) {
	var buf safeBuffer
	payload := map[string]string{"type": "test_event", "data": "hello"}

	err := WriteAnthropicEvent(&buf, &buf, true, "test", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "event: test") {
		t.Error("missing event line")
	}
	if !strings.Contains(output, "data: ") {
		t.Error("missing data line")
	}
	if !strings.Contains(output, `"type":"test_event"`) {
		t.Error("missing payload in data line")
	}
	if !strings.HasSuffix(output, "\n\n") {
		t.Error("expected blank line separator at end")
	}
}

func TestWriteAnthropicEvent_NoFlush(t *testing.T) {
	var buf safeBuffer
	err := WriteAnthropicEvent(&buf, nil, false, "test", map[string]string{"type": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "event: test") {
		t.Error("missing event line")
	}
}

func TestWriteAnthropicEvent_MarshalError(t *testing.T) {
	var buf safeBuffer
	// json.Marshal fails on channel values
	err := WriteAnthropicEvent(&buf, &buf, true, "test", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal event") {
		t.Errorf("unexpected error: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("expected nothing written on marshal error, got %q", buf.String())
	}
}

func TestWriteAnthropicEvent_WriteError(t *testing.T) {
	err := WriteAnthropicEvent(errorWriter{}, nil, false, "test", map[string]string{"type": "x"})
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write event") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- GenerateStreamMessageID tests ---

func TestGenerateStreamMessageID_Format(t *testing.T) {
	id := GenerateStreamMessageID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("expected prefix 'msg_', got %q", id)
	}
	num := strings.TrimPrefix(id, "msg_")
	if num == "" {
		t.Fatalf("expected a numeric suffix, got empty")
	}
	for _, c := range num {
		if c < '0' || c > '9' {
			t.Errorf("expected numeric suffix, got %q", id)
			break
		}
	}
}

func TestGenerateStreamMessageID_Unique(t *testing.T) {
	a := GenerateStreamMessageID()
	b := GenerateStreamMessageID()
	if a == b {
		t.Errorf("expected unique IDs, got %q twice", a)
	}
}

// --- Heartbeat tests ---

func TestOpenAIToAnthropicStreamWithHeartbeat_EmitsDuringIdle(t *testing.T) {
	reader := &blockReader{release: make(chan struct{})}
	var buf safeBuffer

	done := make(chan error, 1)
	go func() {
		done <- OpenAIToAnthropicStreamWithHeartbeat(reader, &buf, &buf, true, "gpt-4o", 10, 10*time.Millisecond)
	}()

	// While the stream is idle (blocked on the reader), heartbeats must be emitted.
	deadline := time.Now().Add(2 * time.Second)
	pingCount := 0
	for time.Now().Before(deadline) {
		pingCount = strings.Count(buf.String(), `"type":"ping"`)
		if pingCount >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if pingCount < 2 {
		t.Fatalf("expected heartbeat pings during idle, got %d", pingCount)
	}

	close(reader.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish after reader release")
	}

	// The initial ping plus heartbeats must all be present.
	if !strings.Contains(buf.String(), "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(buf.String(), "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestOpenAIToAnthropicStreamWithHeartbeat_DisabledWhenCanFlushFalse(t *testing.T) {
	reader := &blockReader{release: make(chan struct{})}
	var buf safeBuffer

	done := make(chan error, 1)
	go func() {
		done <- OpenAIToAnthropicStreamWithHeartbeat(reader, &buf, &buf, false, "gpt-4o", 10, 10*time.Millisecond)
	}()

	time.Sleep(30 * time.Millisecond)
	close(reader.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish after reader release")
	}

	// canFlush=false: no heartbeat goroutine, so only the initial ping exists.
	output := buf.String()
	pingCount := strings.Count(output, `"type":"ping"`)
	if pingCount != 1 {
		t.Errorf("expected exactly 1 ping with heartbeat disabled, got %d", pingCount)
	}
}
