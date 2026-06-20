package proxy

import (
	"encoding/json"
	"testing"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// --- contentToString tests ---

func TestContentToString_PlainString(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	result := contentToString(raw)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestContentToString_ContentBlocks(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`)
	result := contentToString(raw)
	if result != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", result)
	}
}

func TestContentToString_Invalid(t *testing.T) {
	raw := json.RawMessage(`{invalid`)
	result := contentToString(raw)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// --- hasContentBeyondText tests ---

func TestHasContentBeyondText_TextOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"}]`)
	if hasContentBeyondText(raw) {
		t.Error("expected false for text-only content")
	}
}

func TestHasContentBeyondText_WithImage(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]`)
	if !hasContentBeyondText(raw) {
		t.Error("expected true for content with image")
	}
}

func TestHasContentBeyondText_Invalid(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if hasContentBeyondText(raw) {
		t.Error("expected false for invalid content")
	}
}

// --- convertContentBlocks tests ---

func TestConvertContentBlocks_TextOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`)
	text, toolCalls := convertContentBlocks(raw)
	if text != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", text)
	}
	if len(toolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(toolCalls))
	}
}

func TestConvertContentBlocks_ToolUse(t *testing.T) {
	raw := json.RawMessage(`[{"type":"tool_use","id":"toolu_001","name":"get_weather","input":{"city":"Beijing"}}]`)
	text, toolCalls := convertContentBlocks(raw)
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].ID != "toolu_001" {
		t.Errorf("expected ID 'toolu_001', got %q", toolCalls[0].ID)
	}
	if toolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", toolCalls[0].Function.Name)
	}
}

func TestConvertContentBlocks_Invalid(t *testing.T) {
	raw := json.RawMessage(`"plain string"`)
	text, toolCalls := convertContentBlocks(raw)
	if text != "plain string" {
		t.Errorf("expected 'plain string', got %q", text)
	}
	if len(toolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(toolCalls))
	}
}

// --- ConvertAnthropicToOpenAI tests ---

func TestConvertAnthropicToOpenAI_SimpleText(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
		MaxTokens: 1024,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if result.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", result.Model)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", result.Messages[0].Content)
	}
	if *result.MaxTokens != 1024 {
		t.Errorf("expected max_tokens 1024, got %d", *result.MaxTokens)
	}
}

func TestConvertAnthropicToOpenAI_SystemMessages(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model:  "claude-3-opus",
		System: json.RawMessage(`"You are a helpful assistant."`),
		Messages: []anthropic.Message{
			{Role: "system", Content: json.RawMessage(`"Be concise."`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "You are a helpful assistant.\nBe concise." {
		t.Errorf("unexpected system content: %q", result.Messages[0].Content)
	}
	if result.Messages[1].Role != "user" {
		t.Errorf("expected second message role 'user', got %q", result.Messages[1].Role)
	}
}

func TestConvertAnthropicToOpenAI_DefaultMaxTokens(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 0, // should use default
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 8192)

	if *result.MaxTokens != 8192 {
		t.Errorf("expected default max_tokens 8192, got %d", *result.MaxTokens)
	}
}

func TestConvertAnthropicToOpenAI_StopSequences(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens:     100,
		StopSequences: []string{"END", "STOP"},
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Stop) != 2 {
		t.Fatalf("expected 2 stop sequences, got %d", len(result.Stop))
	}
	if result.Stop[0] != "END" || result.Stop[1] != "STOP" {
		t.Errorf("unexpected stop sequences: %v", result.Stop)
	}
}

func TestConvertAnthropicToOpenAI_Tools(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"what is the weather?"`)},
		},
		MaxTokens: 100,
		Tools: []anthropic.ToolDefinition{
			{Name: "get_weather", Description: "Get weather", InputSchema: schema},
		},
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Type != "function" {
		t.Errorf("expected tool type 'function', got %q", result.Tools[0].Type)
	}
	if result.Tools[0].Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %q", result.Tools[0].Function.Name)
	}
}

func TestConvertAnthropicToOpenAI_ToolChoiceAuto(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens:  100,
		ToolChoice: json.RawMessage(`{"type":"auto"}`),
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if result.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto', got %v", result.ToolChoice)
	}
}

func TestConvertAnthropicToOpenAI_ToolChoiceAny(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens:  100,
		ToolChoice: json.RawMessage(`{"type":"any"}`),
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if result.ToolChoice != "required" {
		t.Errorf("expected tool_choice 'required', got %v", result.ToolChoice)
	}
}

func TestConvertAnthropicToOpenAI_ToolChoiceSpecific(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens:  100,
		ToolChoice: json.RawMessage(`{"type":"tool","name":"get_weather"}`),
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	m, ok := result.ToolChoice.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for tool_choice, got %T", result.ToolChoice)
	}
	if m["type"] != "function" {
		t.Errorf("expected type 'function', got %v", m["type"])
	}
}

func TestConvertAnthropicToOpenAI_AssistantWithToolUse(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"what is the weather in Beijing?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_001","name":"get_weather","input":{"city":"Beijing"}}]`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	assistantMsg := result.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected 'get_weather', got %q", assistantMsg.ToolCalls[0].Function.Name)
	}
}

func TestConvertAnthropicToOpenAI_ToolResult(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{Role: "tool_result", Content: json.RawMessage(`[{"tool_use_id":"toolu_001","type":"text","text":"25°C"}]`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	toolMsg := result.Messages[1]
	if toolMsg.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "toolu_001" {
		t.Errorf("expected tool_call_id 'toolu_001', got %q", toolMsg.ToolCallID)
	}
	if toolMsg.Content != "25°C" {
		t.Errorf("expected content '25°C', got %q", toolMsg.Content)
	}
}

func TestConvertAnthropicToOpenAI_ImagePlaceholder(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"describe this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc123"}}]`)},
		},
		MaxTokens: 100,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	content := result.Messages[0].Content
	if content != "describe this\n[Image: image/png (base64 data)]" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestConvertAnthropicToOpenAI_TemperatureAndTopP(t *testing.T) {
	temp := 0.7
	topP := 0.9
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens:   100,
		Temperature: &temp,
		TopP:        &topP,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if result.Temperature == nil || *result.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", result.Temperature)
	}
	if result.TopP == nil || *result.TopP != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", result.TopP)
	}
}

func TestConvertAnthropicToOpenAI_Stream(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Model: "claude-3-opus",
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 100,
		Stream:    true,
	}

	result := ConvertAnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if !result.Stream {
		t.Error("expected stream to be true")
	}
}
