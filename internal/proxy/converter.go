package proxy

import (
	"encoding/json"

	"github.com/hxz0727/API-Switch/internal/converter"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// contentToString extracts a plain text string from an anthropic content field.
// Deprecated: Use converter.ContentToString instead.
func contentToString(content json.RawMessage) string {
	return converter.ContentToString(content)
}

// hasContentBeyondText checks if content has blocks beyond text.
// Deprecated: Use converter.HasContentBeyondText instead.
func hasContentBeyondText(content json.RawMessage) bool {
	return converter.HasContentBeyondText(content)
}

// convertContentBlocks converts Anthropic content blocks to OpenAI message format.
// Deprecated: Use converter.ConvertContentBlocks instead.
func convertContentBlocks(content json.RawMessage) (string, []openai.ToolCall) {
	return converter.ConvertContentBlocks(content)
}

// convertAnthropicMessage converts a single Anthropic message into OpenAI messages.
// Deprecated: Use converter.ConvertAnthropicMessage instead.
func convertAnthropicMessage(msg anthropic.Message, messages *[]openai.Message) int {
	return converter.ConvertAnthropicMessage(msg, messages)
}

// toolResultContent extracts the text content from an Anthropic tool_result block.
// Deprecated: Use converter.ToolResultContent instead.
func toolResultContent(tr anthropic.ContentBlock) string {
	return converter.ToolResultContent(tr)
}

// hasOnlyText reports whether the given blocks are all text blocks.
// Deprecated: Use converter.HasOnlyText instead.
func hasOnlyText(blocks []anthropic.ContentBlock) bool {
	return converter.HasOnlyText(blocks)
}

// ConvertAnthropicToOpenAI converts an Anthropic Messages request to an OpenAI ChatCompletion request.
// Deprecated: Use converter.AnthropicToOpenAI instead.
func ConvertAnthropicToOpenAI(antReq *anthropic.MessagesRequest, model string, defaultMaxTokens int) *openai.ChatCompletionRequest {
	return converter.AnthropicToOpenAI(antReq, model, defaultMaxTokens)
}
