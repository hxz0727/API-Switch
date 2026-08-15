package converter

import (
	"strings"
	"testing"

	"github.com/hxz0727/API-Switch/pkg/openai"
)

// --- OpenAIToAnthropic tests ---

func TestOpenAIToAnthropic_TextContent(t *testing.T) {
	stop := "stop"
	resp := &openai.ChatCompletionResponse{
		ID:    "chatcmpl_1",
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{
				Message:      openai.Message{Role: "assistant", Content: "Hello world"},
				FinishReason: &stop,
			},
		},
		Usage: openai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if out.ID != "chatcmpl_1" || out.Type != "message" || out.Role != "assistant" {
		t.Errorf("unexpected envelope: %+v", out)
	}
	if out.Model != "claude-3-opus" {
		t.Errorf("expected requested model 'claude-3-opus', got %q", out.Model)
	}
	if len(out.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(out.Content))
	}
	if out.Content[0].Type != "text" || out.Content[0].Text != "Hello world" {
		t.Errorf("unexpected content: %+v", out.Content[0])
	}
	if out.StopReason == nil || *out.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %v", out.StopReason)
	}
	if out.Usage.InputTokens != 10 || out.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage mapping: %+v", out.Usage)
	}
}

func TestOpenAIToAnthropic_EmptyContent(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		ID:    "chatcmpl_2",
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{Message: openai.Message{Role: "assistant", Content: ""}},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 0 {
		t.Errorf("expected no content blocks, got %d", len(out.Content))
	}
	if out.StopReason == nil || *out.StopReason != "end_turn" {
		t.Errorf("expected default stop_reason 'end_turn', got %v", out.StopReason)
	}
}

func TestOpenAIToAnthropic_EmptyChoices(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		ID:    "chatcmpl_3",
		Model: "gpt-4o",
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if out.ID != "chatcmpl_3" || out.Model != "claude-3-opus" {
		t.Errorf("unexpected response envelope: %+v", out)
	}
	if len(out.Content) != 0 {
		t.Errorf("expected no content blocks, got %d", len(out.Content))
	}
	if out.StopReason != nil {
		t.Errorf("expected nil stop_reason for empty choices, got %v", out.StopReason)
	}
}

func TestOpenAIToAnthropic_ContentParts(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{
				Message: openai.Message{
					Role: "assistant",
					Content: []openai.ContentPart{
						{Type: "text", Text: "part1"},
						{Type: "text", Text: "part2"},
					},
				},
			},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 1 || out.Content[0].Text != "part1part2" {
		t.Errorf("expected joined text parts, got %+v", out.Content)
	}
}

func TestOpenAIToAnthropic_ToolCalls(t *testing.T) {
	toolCalls := []openai.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: openai.ToolCallFunc{
				Name:      "get_weather",
				Arguments: `{"city":"Beijing"}`,
			},
		},
	}
	resp := &openai.ChatCompletionResponse{
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{
				Message: openai.Message{Role: "assistant", Content: "", ToolCalls: toolCalls},
			},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", len(out.Content))
	}
	block := out.Content[0]
	if block.Type != "tool_use" || block.ID != "call_1" || block.Name != "get_weather" {
		t.Errorf("unexpected tool_use block: %+v", block)
	}
	if string(block.Input) != `{"city":"Beijing"}` {
		t.Errorf("unexpected input: %s", block.Input)
	}
	if out.StopReason == nil || *out.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %v", out.StopReason)
	}
}

func TestOpenAIToAnthropic_ToolCallInvalidJSONArguments(t *testing.T) {
	toolCalls := []openai.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: openai.ToolCallFunc{
				Name:      "f",
				Arguments: "not valid json",
			},
		},
	}
	resp := &openai.ChatCompletionResponse{
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{Message: openai.Message{Role: "assistant", ToolCalls: toolCalls}},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", len(out.Content))
	}
	// invalid JSON must be quoted as a JSON string
	if !strings.HasPrefix(string(out.Content[0].Input), `"`) {
		t.Errorf("expected quoted fallback input, got %s", out.Content[0].Input)
	}
}

func TestOpenAIToAnthropic_ReasoningFallback(t *testing.T) {
	// SenseNova style: content empty but reasoning set
	resp := &openai.ChatCompletionResponse{
		Model: "sensenova",
		Choices: []openai.Choice{
			{
				Message: openai.Message{Role: "assistant", Content: "", Reasoning: "thinking out loud"},
			},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 1 || out.Content[0].Text != "thinking out loud" {
		t.Errorf("expected reasoning to be used as content, got %+v", out.Content)
	}
}

func TestOpenAIToAnthropic_ReasoningIgnoredWhenContentPresent(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		Model: "sensenova",
		Choices: []openai.Choice{
			{
				Message: openai.Message{Role: "assistant", Content: "real", Reasoning: "hidden"},
			},
		},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if len(out.Content) != 1 || out.Content[0].Text != "real" {
		t.Errorf("expected content to win, got %+v", out.Content)
	}
}

func TestOpenAIToAnthropic_UsageMapping(t *testing.T) {
	resp := &openai.ChatCompletionResponse{
		Model: "gpt-4o",
		Choices: []openai.Choice{
			{Message: openai.Message{Role: "assistant", Content: "hi"}},
		},
		Usage: openai.Usage{PromptTokens: 123, CompletionTokens: 45, TotalTokens: 168},
	}

	out := OpenAIToAnthropic(resp, "claude-3-opus")

	if out.Usage.InputTokens != 123 || out.Usage.OutputTokens != 45 {
		t.Errorf("unexpected usage mapping: %+v", out.Usage)
	}
}

// --- MapOpenAIFinishReason tests ---

func TestMapOpenAIFinishReason_Nil(t *testing.T) {
	got := MapOpenAIFinishReason(nil)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapOpenAIFinishReason_Stop(t *testing.T) {
	s := "stop"
	got := MapOpenAIFinishReason(&s)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapOpenAIFinishReason_Length(t *testing.T) {
	s := "length"
	got := MapOpenAIFinishReason(&s)
	if got == nil || *got != "max_tokens" {
		t.Errorf("expected 'max_tokens', got %v", got)
	}
}

func TestMapOpenAIFinishReason_ToolCalls(t *testing.T) {
	s := "tool_calls"
	got := MapOpenAIFinishReason(&s)
	if got == nil || *got != "tool_use" {
		t.Errorf("expected 'tool_use', got %v", got)
	}
}

func TestMapOpenAIFinishReason_ContentFilter(t *testing.T) {
	s := "content_filter"
	got := MapOpenAIFinishReason(&s)
	if got == nil || *got != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", got)
	}
}

func TestMapOpenAIFinishReason_Unknown(t *testing.T) {
	s := "weird_reason"
	got := MapOpenAIFinishReason(&s)
	if got == nil || *got != "weird_reason" {
		t.Errorf("expected passthrough 'weird_reason', got %v", got)
	}
}

// --- GenerateMessageID tests ---

func TestGenerateMessageID_Format(t *testing.T) {
	id := GenerateMessageID()
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

func TestGenerateMessageID_Unique(t *testing.T) {
	a := GenerateMessageID()
	b := GenerateMessageID()
	if a == b {
		t.Errorf("expected unique IDs, got %q twice", a)
	}
}
