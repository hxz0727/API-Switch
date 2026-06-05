package proxy

import (
	"encoding/json"
	"fmt"
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
	var blocks []anthropic.ContentBlock
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

// hasContentBeyondText checks if content has blocks beyond text (images, tool_use, etc).
func hasContentBeyondText(content json.RawMessage) bool {
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type != "text" {
			return true
		}
	}
	return false
}

// convertContentBlocks converts Anthropic content blocks to OpenAI message format.
// Returns the string content for simple text-only messages, or if multi-modal,
// it's folded into the system/assistant flow.
func convertContentBlocks(content json.RawMessage) (string, []openai.ToolCall) {
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return contentToString(content), nil
	}

	var texts []string
	var toolCalls []openai.ToolCall
	for _, block := range blocks {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "tool_use":
			inputStr := ""
			if block.Input != nil {
				inputBytes, _ := json.Marshal(block.Input)
				inputStr = string(inputBytes)
			}
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openai.ToolCallFunc{
					Name:      block.Name,
					Arguments: inputStr,
				},
			})
		}
	}

	return strings.Join(texts, "\n"), toolCalls
}

// ConvertAnthropicToOpenAI converts an Anthropic Messages request to an OpenAI ChatCompletion request.
func ConvertAnthropicToOpenAI(antReq *anthropic.MessagesRequest, model string, defaultMaxTokens int) *openai.ChatCompletionRequest {
	var messages []openai.Message

	// Collect all system content: from top-level "system" field + any "system"-role messages
	var systemParts []string
	if systemContent := contentToString(antReq.System); systemContent != "" {
		systemParts = append(systemParts, systemContent)
	}

	// Separate system messages from conversation messages
	var conversationMsgs []anthropic.Message
	for _, msg := range antReq.Messages {
		if msg.Role == "system" {
			if sc := contentToString(msg.Content); sc != "" {
				systemParts = append(systemParts, sc)
			}
		} else {
			conversationMsgs = append(conversationMsgs, msg)
		}
	}

	// Place combined system content at the beginning (required by most OpenAI-compatible APIs)
	if len(systemParts) > 0 {
		messages = append(messages, openai.Message{
			Role:    "system",
			Content: strings.Join(systemParts, "\n"),
		})
	}

	// Copy conversation messages (system messages already merged above)
	for _, msg := range conversationMsgs {
		oaiMsg := openai.Message{
			Role: msg.Role,
		}

		if msg.Role == "assistant" && hasContentBeyondText(msg.Content) {
			content, toolCalls := convertContentBlocks(msg.Content)
			oaiMsg.Content = content
			if len(toolCalls) > 0 {
				oaiMsg.ToolCalls = toolCalls
			}
		} else if msg.Role == "user" && hasContentBeyondText(msg.Content) {
			// For user messages with images, convert content blocks
			var blocks []anthropic.ContentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				var parts []string
				for _, b := range blocks {
					if b.Type == "text" {
						parts = append(parts, b.Text)
					} else if b.Type == "image" && b.Source != nil {
						parts = append(parts, fmt.Sprintf("[Image: %s (base64 data)]", b.Source.MediaType))
					}
				}
				oaiMsg.Content = strings.Join(parts, "\n")
			} else {
				oaiMsg.Content = contentToString(msg.Content)
			}
		} else {
			oaiMsg.Content = contentToString(msg.Content)
		}

		// Handle tool_result role mapping
		if msg.Role == "tool_result" {
			oaiMsg.Role = "tool"
			// Extract tool_use_id from content blocks
			var blocks []anthropic.ContentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.ToolUseID != "" {
						oaiMsg.ToolCallID = b.ToolUseID
					}
					if b.Type == "text" || b.Content != nil {
						if oaiMsg.Content == "" {
							if b.Text != "" {
								oaiMsg.Content = b.Text
							} else if b.Content != nil {
								oaiMsg.Content = contentToString(b.Content)
							}
						}
					}
				}
			}
			if oaiMsg.Content == "" {
				oaiMsg.Content = contentToString(msg.Content)
			}
		}

		messages = append(messages, oaiMsg)
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

	// Convert Anthropic tools to OpenAI tools
	if len(antReq.Tools) > 0 {
		req.Tools = make([]openai.Tool, 0, len(antReq.Tools))
		for _, t := range antReq.Tools {
			req.Tools = append(req.Tools, openai.Tool{
				Type: "function",
				Function: openai.FunctionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	// tool_choice — convert Anthropic format to OpenAI format
	// Anthropic: {"type": "auto"} | {"type": "any"} | {"type": "tool", "name": "xxx"}
	// OpenAI:   "auto" | "required" | "none" | {"type": "function", "function": {"name": "xxx"}}
	if antReq.ToolChoice != nil {
		var choice struct {
			Type string `json:"type"`
			Name string `json:"name,omitempty"`
		}
		if err := json.Unmarshal(antReq.ToolChoice, &choice); err == nil && choice.Type != "" {
			switch choice.Type {
			case "auto":
				req.ToolChoice = "auto"
			case "any":
				req.ToolChoice = "required"
			case "tool":
				if choice.Name != "" {
					req.ToolChoice = map[string]interface{}{
						"type": "function",
						"function": map[string]string{
							"name": choice.Name,
						},
					}
				} else {
					req.ToolChoice = "required"
				}
			default:
				req.ToolChoice = "auto"
			}
		}
	}

	return req
}
