package converter

import (
	"encoding/json"
	"testing"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// --- ContentToString tests ---

func TestContentToString_PlainString(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	if got := ContentToString(raw); got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestContentToString_TextBlocks(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`)
	if got := ContentToString(raw); got != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", got)
	}
}

func TestContentToString_ArrayWithToolUse(t *testing.T) {
	// tool_use blocks must be ignored; only text blocks are joined
	raw := json.RawMessage(`[
		{"type":"text","text":"let me check"},
		{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Beijing"}}
	]`)
	if got := ContentToString(raw); got != "let me check" {
		t.Errorf("expected 'let me check', got %q", got)
	}
}

func TestContentToString_MalformedJSON(t *testing.T) {
	raw := json.RawMessage(`{invalid`)
	if got := ContentToString(raw); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestContentToString_Empty(t *testing.T) {
	raw := json.RawMessage(`""`)
	if got := ContentToString(raw); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- HasContentBeyondText tests ---

func TestHasContentBeyondText_TextOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"}]`)
	if HasContentBeyondText(raw) {
		t.Error("expected false for text-only content")
	}
}

func TestHasContentBeyondText_TextAndImage(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"see this"},{"type":"image","source":{"type":"url","url":"http://example.com/i.png"}}]`)
	if !HasContentBeyondText(raw) {
		t.Error("expected true for text+image content")
	}
}

func TestHasContentBeyondText_ToolUse(t *testing.T) {
	raw := json.RawMessage(`[{"type":"tool_use","id":"t","name":"f","input":{}}]`)
	if !HasContentBeyondText(raw) {
		t.Error("expected true for tool_use content")
	}
}

func TestHasContentBeyondText_ImageOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]`)
	if !HasContentBeyondText(raw) {
		t.Error("expected true for image-only content")
	}
}

func TestHasContentBeyondText_Invalid(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if HasContentBeyondText(raw) {
		t.Error("expected false for invalid content")
	}
}

// --- ConvertContentBlocks tests ---

func TestConvertContentBlocks_Text(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)
	text, toolCalls := ConvertContentBlocks(raw)
	if text != "a\nb" {
		t.Errorf("expected 'a\\nb', got %q", text)
	}
	if len(toolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(toolCalls))
	}
}

func TestConvertContentBlocks_ToolUse(t *testing.T) {
	raw := json.RawMessage(`[{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Beijing"}}]`)
	text, toolCalls := ConvertContentBlocks(raw)
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "toolu_1" || tc.Type != "function" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"city":"Beijing"}` {
		t.Errorf("unexpected arguments: %q", tc.Function.Arguments)
	}
}

func TestConvertContentBlocks_Mixed(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"calling"},
		{"type":"tool_use","id":"toolu_2","name":"get_time","input":{"zone":"EST"}}
	]`)
	text, toolCalls := ConvertContentBlocks(raw)
	if text != "calling" {
		t.Errorf("expected 'calling', got %q", text)
	}
	if len(toolCalls) != 1 || toolCalls[0].Function.Name != "get_time" {
		t.Errorf("unexpected tool calls: %+v", toolCalls)
	}
}

func TestConvertContentBlocks_Empty(t *testing.T) {
	raw := json.RawMessage(`[]`)
	text, toolCalls := ConvertContentBlocks(raw)
	if text != "" || len(toolCalls) != 0 {
		t.Errorf("expected empty output, got text=%q toolCalls=%d", text, len(toolCalls))
	}
}

func TestConvertContentBlocks_FallbackToString(t *testing.T) {
	raw := json.RawMessage(`"plain string"`)
	text, toolCalls := ConvertContentBlocks(raw)
	if text != "plain string" || len(toolCalls) != 0 {
		t.Errorf("expected fallback to string, got text=%q toolCalls=%d", text, len(toolCalls))
	}
}

// --- ToolResultContent tests ---

func TestToolResultContent_StringContent(t *testing.T) {
	tr := anthropic.ContentBlock{
		Type:      "tool_result",
		ToolUseID: "toolu_1",
		Content:   json.RawMessage(`"25°C"`),
	}
	if got := ToolResultContent(tr); got != "25°C" {
		t.Errorf("expected '25°C', got %q", got)
	}
}

func TestToolResultContent_ArrayContent(t *testing.T) {
	tr := anthropic.ContentBlock{
		Type:    "tool_result",
		Content: json.RawMessage(`[{"type":"text","text":"sunny"},{"type":"text","text":"25°C"}]`),
	}
	if got := ToolResultContent(tr); got != "sunny\n25°C" {
		t.Errorf("expected 'sunny\\n25°C', got %q", got)
	}
}

func TestToolResultContent_IsError(t *testing.T) {
	isErr := true
	tr := anthropic.ContentBlock{
		Type:    "tool_result",
		IsError: &isErr,
		Content: json.RawMessage(`"failed to fetch"`),
	}
	if got := ToolResultContent(tr); got != "[Tool Error] failed to fetch" {
		t.Errorf("expected '[Tool Error] failed to fetch', got %q", got)
	}
}

func TestToolResultContent_IsErrorArray(t *testing.T) {
	isErr := true
	tr := anthropic.ContentBlock{
		Type:    "tool_result",
		IsError: &isErr,
		Content: json.RawMessage(`[{"type":"text","text":"boom"}]`),
	}
	if got := ToolResultContent(tr); got != "[Tool Error] boom" {
		t.Errorf("expected '[Tool Error] boom', got %q", got)
	}
}

func TestToolResultContent_IsErrorNoContent(t *testing.T) {
	isErr := true
	tr := anthropic.ContentBlock{Type: "tool_result", IsError: &isErr}
	if got := ToolResultContent(tr); got != "[Tool Error]" {
		t.Errorf("expected '[Tool Error]', got %q", got)
	}
}

func TestToolResultContent_EmptyContent(t *testing.T) {
	tr := anthropic.ContentBlock{Type: "tool_result", Content: nil}
	if got := ToolResultContent(tr); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestToolResultContent_UnparseableContent(t *testing.T) {
	tr := anthropic.ContentBlock{Type: "tool_result", Content: json.RawMessage(`{not json`)}
	if got := ToolResultContent(tr); got != "" {
		t.Errorf("expected empty string for unparseable content, got %q", got)
	}
}

// --- HasOnlyText tests ---

func TestHasOnlyText_AllText(t *testing.T) {
	blocks := []anthropic.ContentBlock{{Type: "text"}, {Type: "text"}}
	if !HasOnlyText(blocks) {
		t.Error("expected true for all-text blocks")
	}
}

func TestHasOnlyText_Mixed(t *testing.T) {
	blocks := []anthropic.ContentBlock{{Type: "text"}, {Type: "image"}}
	if HasOnlyText(blocks) {
		t.Error("expected false for mixed blocks")
	}
}

func TestHasOnlyText_Empty(t *testing.T) {
	if !HasOnlyText(nil) {
		t.Error("expected true for empty blocks")
	}
}

// --- AnthropicToOpenAI tests ---

func TestAnthropicToOpenAI_FullConversion(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 5
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)

	antReq := &anthropic.MessagesRequest{
		Model:         "claude-3-opus",
		System:        json.RawMessage(`"You are helpful."`),
		MaxTokens:     200,
		Temperature:   &temp,
		TopP:          &topP,
		TopK:          &topK,
		Stream:        true,
		StopSequences: []string{"END"},
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		Tools: []anthropic.ToolDefinition{
			{Name: "get_weather", Description: "Get weather", InputSchema: schema},
		},
		ToolChoice: json.RawMessage(`{"type":"auto"}`),
	}

	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)

	if req.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", req.Model)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages (system+user), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are helpful." {
		t.Errorf("unexpected system message: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "hi" {
		t.Errorf("unexpected user message: %+v", req.Messages[1])
	}
	if req.MaxTokens == nil || *req.MaxTokens != 200 {
		t.Errorf("expected max_tokens 200, got %v", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", req.TopP)
	}
	if req.TopK == nil || *req.TopK != 5 {
		t.Errorf("expected top_k 5, got %v", req.TopK)
	}
	if !req.Stream {
		t.Error("expected stream true")
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" {
		t.Errorf("unexpected stop sequences: %v", req.Stop)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "get_weather" ||
		req.Tools[0].Function.Description != "Get weather" {
		t.Errorf("unexpected tool: %+v", req.Tools[0])
	}
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto', got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_DefaultMaxTokens(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 0,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 8192)
	if req.MaxTokens == nil || *req.MaxTokens != 8192 {
		t.Errorf("expected default 8192, got %v", req.MaxTokens)
	}
}

func TestAnthropicToOpenAI_FallbackMaxTokens(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 0,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 0)
	if req.MaxTokens == nil || *req.MaxTokens != 1024 {
		t.Errorf("expected fallback 1024, got %v", req.MaxTokens)
	}
}

func TestAnthropicToOpenAI_SystemInMessages(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages: []anthropic.Message{
			{Role: "system", Content: json.RawMessage(`"Be concise."`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 100,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "Be concise." {
		t.Errorf("unexpected system message: %+v", req.Messages[0])
	}
}

func TestAnthropicToOpenAI_ToolChoiceAny(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{"type":"any"}`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != "required" {
		t.Errorf("expected tool_choice 'required', got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_ToolChoiceSpecific(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{"type":"tool","name":"get_weather"}`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	m, ok := req.ToolChoice.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map tool_choice, got %T", req.ToolChoice)
	}
	if m["type"] != "function" {
		t.Errorf("expected type 'function', got %v", m["type"])
	}
	fn, ok := m["function"].(map[string]string)
	if !ok || fn["name"] != "get_weather" {
		t.Errorf("unexpected function: %v", m["function"])
	}
}

func TestAnthropicToOpenAI_ToolChoiceToolNoName(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{"type":"tool"}`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto' when name missing, got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_ToolChoiceEmptyType(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{"type":""}`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto' for empty type, got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_ToolChoiceInvalidJSON(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{invalid`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto' for invalid JSON, got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_UnknownToolChoiceType(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
		ToolChoice: json.RawMessage(`{"type":"whatever"}`),
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice 'auto' for unknown type, got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_NoToolChoice(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages:  []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		MaxTokens: 100,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if req.ToolChoice != nil {
		t.Errorf("expected nil tool_choice, got %v", req.ToolChoice)
	}
}

func TestAnthropicToOpenAI_OrphanedToolCallsStripped(t *testing.T) {
	// assistant tool_use without a following tool message must be stripped
	antReq := &anthropic.MessagesRequest{
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"t1","name":"f","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`"thanks"`)},
		},
		MaxTokens: 100,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	if len(req.Messages[1].ToolCalls) != 0 {
		t.Errorf("expected orphaned tool_calls to be stripped, got %d", len(req.Messages[1].ToolCalls))
	}
}

func TestAnthropicToOpenAI_ToolCallsKeptWhenFollowedByTool(t *testing.T) {
	antReq := &anthropic.MessagesRequest{
		Messages: []anthropic.Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"t1","name":"f","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]`)},
		},
		MaxTokens: 100,
	}
	req := AnthropicToOpenAI(antReq, "gpt-4o", 4096)
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	if len(req.Messages[1].ToolCalls) != 1 {
		t.Errorf("expected tool_calls kept, got %d", len(req.Messages[1].ToolCalls))
	}
}

// --- ConvertAnthropicMessage tests ---

func TestConvertAnthropicMessage_ModernToolResult(t *testing.T) {
	msg := anthropic.Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_1","content":"25°C"}]`),
	}
	var messages []openai.Message
	n := ConvertAnthropicMessage(msg, &messages)

	if n != 1 {
		t.Fatalf("expected 1 emitted message, got %d", n)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	tool := messages[0]
	if tool.Role != "tool" || tool.ToolCallID != "toolu_1" || tool.Content != "25°C" {
		t.Errorf("unexpected tool message: %+v", tool)
	}
}

func TestConvertAnthropicMessage_ModernToolResultWithText(t *testing.T) {
	msg := anthropic.Message{
		Role:    "user",
		Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"toolu_1","content":"25°C"},
			{"type":"text","text":"thanks"}
		]`),
	}
	var messages []openai.Message
	n := ConvertAnthropicMessage(msg, &messages)

	if n != 2 {
		t.Fatalf("expected 2 emitted messages, got %d", n)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "tool" || messages[0].ToolCallID != "toolu_1" {
		t.Errorf("unexpected first message: %+v", messages[0])
	}
	if messages[1].Role != "user" || messages[1].Content != "thanks" {
		t.Errorf("unexpected second message: %+v", messages[1])
	}
}

func TestConvertAnthropicMessage_LegacyToolResult(t *testing.T) {
	msg := anthropic.Message{
		Role:    "tool_result",
		Content: json.RawMessage(`[{"tool_use_id":"toolu_1","type":"text","text":"done"}]`),
	}
	var messages []openai.Message
	n := ConvertAnthropicMessage(msg, &messages)

	if n != 1 || len(messages) != 1 {
		t.Fatalf("expected 1 message, got n=%d len=%d", n, len(messages))
	}
	m := messages[0]
	if m.Role != "tool" || m.ToolCallID != "toolu_1" || m.Content != "done" {
		t.Errorf("unexpected legacy tool message: %+v", m)
	}
}

func TestConvertAnthropicMessage_LegacyToolResultMapFormat(t *testing.T) {
	msg := anthropic.Message{
		Role:    "tool_result",
		Content: json.RawMessage(`{"tool_use_id":"toolu_2","content":"42"}`),
	}
	var messages []openai.Message
	ConvertAnthropicMessage(msg, &messages)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	m := messages[0]
	if m.Role != "tool" || m.ToolCallID != "toolu_2" {
		t.Errorf("unexpected tool message: %+v", m)
	}
	// The map-format content isn't text, and nil != "" in Go, so the
	// ContentToString fallback doesn't fire and Content stays nil.
	if m.Content != nil {
		t.Errorf("expected nil content, got %v", m.Content)
	}
}

func TestConvertAnthropicMessage_UserWithImage(t *testing.T) {
	msg := anthropic.Message{
		Role:    "user",
		Content: json.RawMessage(`[
			{"type":"text","text":"describe this"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
		]`),
	}
	var messages []openai.Message
	ConvertAnthropicMessage(msg, &messages)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	parts, ok := messages[0].Content.([]openai.ContentPart)
	if !ok {
		t.Fatalf("expected []openai.ContentPart, got %T", messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "describe this" {
		t.Errorf("unexpected first part: %+v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil ||
		parts[1].ImageURL.URL != "data:image/png;base64,abc" {
		t.Errorf("unexpected image part: %+v", parts[1])
	}
}

func TestConvertAnthropicMessage_UserWithImageURL(t *testing.T) {
	msg := anthropic.Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"image","source":{"type":"url","url":"http://example.com/i.png"}}]`),
	}
	var messages []openai.Message
	ConvertAnthropicMessage(msg, &messages)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	parts, ok := messages[0].Content.([]openai.ContentPart)
	if !ok {
		t.Fatalf("expected []openai.ContentPart, got %T", messages[0].Content)
	}
	if len(parts) != 1 || parts[0].ImageURL == nil || parts[0].ImageURL.URL != "http://example.com/i.png" {
		t.Errorf("unexpected parts: %+v", parts)
	}
}

func TestConvertAnthropicMessage_AssistantWithToolUse(t *testing.T) {
	msg := anthropic.Message{
		Role:    "assistant",
		Content: json.RawMessage(`[
			{"type":"text","text":"checking"},
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Beijing"}}
		]`),
	}
	var messages []openai.Message
	ConvertAnthropicMessage(msg, &messages)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	m := messages[0]
	if m.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", m.Role)
	}
	if m.Content != "checking" {
		t.Errorf("expected content 'checking', got %v", m.Content)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].ID != "toolu_1" || m.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("unexpected tool calls: %+v", m.ToolCalls)
	}
}

func TestConvertAnthropicMessage_PlainString(t *testing.T) {
	msg := anthropic.Message{Role: "user", Content: json.RawMessage(`"hello"`)}
	var messages []openai.Message
	n := ConvertAnthropicMessage(msg, &messages)

	if n != 1 || len(messages) != 1 {
		t.Fatalf("expected 1 message, got n=%d len=%d", n, len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Errorf("unexpected message: %+v", messages[0])
	}
}
