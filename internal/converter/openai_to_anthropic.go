package converter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// OpenAIToAnthropic converts an OpenAI ChatCompletion response to an Anthropic Messages response.
func OpenAIToAnthropic(oaiResp *openai.ChatCompletionResponse, requestedModel string) *anthropic.MessagesResponse {
	if len(oaiResp.Choices) == 0 {
		return &anthropic.MessagesResponse{
			ID:      oaiResp.ID,
			Type:    "message",
			Role:    "assistant",
			Content: []anthropic.ContentBlock{},
			Model:   requestedModel,
			Usage:   anthropic.ResponseUsage{},
		}
	}

	choice := oaiResp.Choices[0]

	var content []anthropic.ContentBlock

	contentStr := ""
	switch v := choice.Message.Content.(type) {
	case string:
		contentStr = v
	case []openai.ContentPart:
		for _, part := range v {
			if part.Type == "text" {
				contentStr += part.Text
			}
		}
	}
	if contentStr == "" && choice.Message.Reasoning != "" {
		contentStr = choice.Message.Reasoning
	}
	if contentStr != "" {
		content = append(content, anthropic.ContentBlock{
			Type: "text",
			Text: contentStr,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		var input json.RawMessage
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = json.RawMessage(fmt.Sprintf("%q", tc.Function.Arguments))
		}
		content = append(content, anthropic.ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	stopReason := MapOpenAIFinishReason(choice.FinishReason)
	if len(choice.Message.ToolCalls) > 0 {
		sr := "tool_use"
		stopReason = &sr
	}

	return &anthropic.MessagesResponse{
		ID:         oaiResp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      requestedModel,
		StopReason: stopReason,
		Usage: anthropic.ResponseUsage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		},
	}
}

// MapOpenAIFinishReason maps an OpenAI finish_reason to an Anthropic stop_reason.
func MapOpenAIFinishReason(reason *string) *string {
	if reason == nil {
		s := "end_turn"
		return &s
	}
	mapping := map[string]string{
		"stop":           "end_turn",
		"length":         "max_tokens",
		"tool_calls":     "tool_use",
		"content_filter": "end_turn",
	}
	if mapped, ok := mapping[*reason]; ok {
		return &mapped
	}
	return reason
}

// GenerateMessageID generates an Anthropic-style message ID.
func GenerateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
