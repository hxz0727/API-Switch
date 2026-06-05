package proxy

import (
	"fmt"
	"time"

	"github.com/user/api-switch/pkg/anthropic"
	"github.com/user/api-switch/pkg/openai"
)

// ConvertOpenAIToAnthropic converts an OpenAI ChatCompletion response to an Anthropic Messages response.
func ConvertOpenAIToAnthropic(oaiResp *openai.ChatCompletionResponse, requestedModel string) *anthropic.MessagesResponse {
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

	// Build content blocks
	content := []anthropic.ContentBlock{
		{
			Type: "text",
			Text: choice.Message.Content,
		},
	}

	// Map finish_reason -> stop_reason
	stopReason := mapFinishReason(choice.FinishReason)

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

// mapFinishReason maps an OpenAI finish_reason to an Anthropic stop_reason.
func mapFinishReason(reason *string) *string {
	if reason == nil {
		s := "end_turn"
		return &s
	}
	mapping := map[string]string{
		"stop":          "end_turn",
		"length":        "max_tokens",
		"tool_calls":    "tool_use",
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
