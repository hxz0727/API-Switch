package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hxz0727/API-Switch/pkg/openai"
)

func TestConvertOpenAIToAnthropic_TextResponse(t *testing.T) {
	finishReason := "stop"
	oaiResp := &openai.ChatCompletionResponse{
		ID:     "chatcmpl-001",
		Model:  "gpt-4o",
		Object: "chat.completion",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:    "assistant",
					Content: "Hello, how can I help?",
				},
				FinishReason: &finishReason,
			},
		},
		Usage: openai.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	result := ConvertOpenAIToAnthropic(oaiResp, "gpt-4o")

	if result.ID != "chatcmpl-001" {
		t.Errorf("expected ID 'chatcmpl-001', got %q", result.ID)
	}
	if result.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", result.Model)
	}
	if result.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result.Role)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "Hello, how can I help?" {
		t.Errorf("unexpected text: %q", result.Content[0].Text)
	}
	if *result.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", *result.StopReason)
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected input_tokens 10, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 5 {
		t.Errorf("expected output_tokens 5, got %d", result.Usage.OutputTokens)
	}
}

func TestConvertOpenAIToAnthropic_EmptyChoices(t *testing.T) {
	oaiResp := &openai.ChatCompletionResponse{
		ID:     "chatcmpl-002",
		Model:  "gpt-4o",
		Object: "chat.completion",
		Usage:  openai.Usage{},
	}

	result := ConvertOpenAIToAnthropic(oaiResp, "gpt-4o")

	if result.ID != "chatcmpl-002" {
		t.Errorf("expected ID 'chatcmpl-002', got %q", result.ID)
	}
	if len(result.Content) != 0 {
		t.Errorf("expected 0 content blocks, got %d", len(result.Content))
	}
}

func TestConvertOpenAIToAnthropic_ToolCalls(t *testing.T) {
	finishReason := "tool_calls"
	oaiResp := &openai.ChatCompletionResponse{
		ID:     "chatcmpl-003",
		Model:  "gpt-4o",
		Object: "chat.completion",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role: "assistant",
					ToolCalls: []openai.ToolCall{
						{
							ID:   "call_001",
							Type: "function",
							Function: openai.ToolCallFunc{
								Name:      "get_weather",
								Arguments: `{"city":"Beijing"}`,
							},
						},
					},
				},
				FinishReason: &finishReason,
			},
		},
		Usage: openai.Usage{
			PromptTokens:     20,
			CompletionTokens: 15,
			TotalTokens:      35,
		},
	}

	result := ConvertOpenAIToAnthropic(oaiResp, "gpt-4o")

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	block := result.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("expected content type 'tool_use', got %q", block.Type)
	}
	if block.ID != "call_001" {
		t.Errorf("expected ID 'call_001', got %q", block.ID)
	}
	if block.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", block.Name)
	}

	// Verify input is valid JSON
	var input map[string]string
	if err := json.Unmarshal(block.Input, &input); err != nil {
		t.Errorf("failed to parse tool input: %v", err)
	}
	if input["city"] != "Beijing" {
		t.Errorf("expected city 'Beijing', got %q", input["city"])
	}

	if *result.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %q", *result.StopReason)
	}
}

func TestConvertOpenAIToAnthropic_TextAndToolCalls(t *testing.T) {
	finishReason := "stop"
	oaiResp := &openai.ChatCompletionResponse{
		ID:     "chatcmpl-004",
		Model:  "gpt-4o",
		Object: "chat.completion",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:    "assistant",
					Content: "Let me check the weather.",
					ToolCalls: []openai.ToolCall{
						{
							ID:   "call_002",
							Type: "function",
							Function: openai.ToolCallFunc{
								Name:      "get_weather",
								Arguments: `{"city":"Shanghai"}`,
							},
						},
					},
				},
				FinishReason: &finishReason,
			},
		},
		Usage: openai.Usage{
			PromptTokens:     10,
			CompletionTokens: 10,
			TotalTokens:      20,
		},
	}

	result := ConvertOpenAIToAnthropic(oaiResp, "gpt-4o")

	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected first block type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[1].Type != "tool_use" {
		t.Errorf("expected second block type 'tool_use', got %q", result.Content[1].Type)
	}
	// Tool calls present should override stop_reason to "tool_use"
	if *result.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %q", *result.StopReason)
	}
}

// --- mapOpenAIFinishReason tests ---

func TestMapOpenAIFinishReason_Nil(t *testing.T) {
	result := mapOpenAIFinishReason(nil)
	if result == nil || *result != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", result)
	}
}

func TestMapOpenAIFinishReason_Stop(t *testing.T) {
	s := "stop"
	result := mapOpenAIFinishReason(&s)
	if result == nil || *result != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", result)
	}
}

func TestMapOpenAIFinishReason_Length(t *testing.T) {
	s := "length"
	result := mapOpenAIFinishReason(&s)
	if result == nil || *result != "max_tokens" {
		t.Errorf("expected 'max_tokens', got %v", result)
	}
}

func TestMapOpenAIFinishReason_ToolCalls(t *testing.T) {
	s := "tool_calls"
	result := mapOpenAIFinishReason(&s)
	if result == nil || *result != "tool_use" {
		t.Errorf("expected 'tool_use', got %v", result)
	}
}

func TestMapOpenAIFinishReason_ContentFilter(t *testing.T) {
	s := "content_filter"
	result := mapOpenAIFinishReason(&s)
	if result == nil || *result != "end_turn" {
		t.Errorf("expected 'end_turn', got %v", result)
	}
}

func TestMapOpenAIFinishReason_Unknown(t *testing.T) {
	s := "unknown_reason"
	result := mapOpenAIFinishReason(&s)
	if result == nil || *result != "unknown_reason" {
		t.Errorf("expected 'unknown_reason', got %v", result)
	}
}

// --- GenerateMessageID tests ---

func TestGenerateMessageID(t *testing.T) {
	id := GenerateMessageID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("expected ID to start with 'msg_', got %q", id)
	}
}

func TestGenerateMessageID_Unique(t *testing.T) {
	id1 := GenerateMessageID()
	id2 := GenerateMessageID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}
