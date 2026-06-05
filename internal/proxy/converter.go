package proxy

import (
	"encoding/json"
	"strings"

	"github.com/user/api-switch/pkg/anthropic"
	"github.com/user/api-switch/pkg/openai"
)

// contentToString extracts a plain text string from an anthropic content field,
// which may be either a plain string or an array of content blocks.
func contentToString(content json.RawMessage) string {
	// Try as plain string first
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
		var texts []string
		for _, block := range blocks {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// ConvertAnthropicToOpenAI converts an Anthropic Messages request to an OpenAI ChatCompletion request.
func ConvertAnthropicToOpenAI(antReq *anthropic.MessagesRequest, model string, defaultMaxTokens int) *openai.ChatCompletionRequest {
	var messages []openai.Message

	// If Anthropic request has a system field, prepend it as a system message
	if systemContent := contentToString(antReq.System); systemContent != "" {
		messages = append(messages, openai.Message{
			Role:    "system",
			Content: systemContent,
		})
	}

	// Copy conversation messages, converting content blocks to plain text
	for _, msg := range antReq.Messages {
		messages = append(messages, openai.Message{
			Role:    msg.Role,
			Content: contentToString(msg.Content),
		})
	}

	// max_tokens
	maxTokens := antReq.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	req := &openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   &maxTokens,
		Temperature: antReq.Temperature,
		TopP:        antReq.TopP,
		Stream:      antReq.Stream,
	}

	// stop_sequences -> stop
	if len(antReq.StopSequences) > 0 {
		req.Stop = antReq.StopSequences
	}

	return req
}
