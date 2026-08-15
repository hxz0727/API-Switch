package proxy

import (
	"github.com/hxz0727/API-Switch/internal/converter"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// ConvertOpenAIToAnthropic converts an OpenAI ChatCompletion response to an Anthropic Messages response.
// Deprecated: Use converter.OpenAIToAnthropic instead.
func ConvertOpenAIToAnthropic(oaiResp *openai.ChatCompletionResponse, requestedModel string) *anthropic.MessagesResponse {
	return converter.OpenAIToAnthropic(oaiResp, requestedModel)
}

// mapOpenAIFinishReason maps an OpenAI finish_reason to an Anthropic stop_reason.
// Deprecated: Use converter.MapOpenAIFinishReason instead.
func mapOpenAIFinishReason(reason *string) *string {
	return converter.MapOpenAIFinishReason(reason)
}

// GenerateMessageID generates an Anthropic-style message ID.
// Deprecated: Use converter.GenerateMessageID instead.
func GenerateMessageID() string {
	return converter.GenerateMessageID()
}
