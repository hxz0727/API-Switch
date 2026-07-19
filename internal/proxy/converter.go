package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
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

// convertAnthropicMessage converts a single Anthropic message into one or more
// OpenAI messages and appends them to the messages slice.
// Returns the number of OpenAI messages appended.
//
// Handles modern Anthropic format where tool_result blocks appear INSIDE a
// "user" role message (Claude Code standard). Each tool_result block becomes
// its own "tool" role OpenAI message.
func convertAnthropicMessage(msg anthropic.Message, messages *[]openai.Message) int {
	emitted := 0

	// Handle legacy top-level "tool_result" role
	if msg.Role == "tool_result" {
		oaiMsg := openai.Message{Role: "tool"}
		var blocks []anthropic.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			var contentParts []string
			for _, b := range blocks {
				if b.ToolUseID != "" && oaiMsg.ToolCallID == "" {
					oaiMsg.ToolCallID = b.ToolUseID
				}
				if b.Type == "text" && b.Text != "" {
					contentParts = append(contentParts, b.Text)
				} else if b.Content != nil {
					contentParts = append(contentParts, contentToString(b.Content))
				}
			}
			if len(contentParts) > 0 {
				oaiMsg.Content = strings.Join(contentParts, "\n")
			}
		} else {
			var rawMap map[string]interface{}
			if json.Unmarshal(msg.Content, &rawMap) == nil {
				if id, ok := rawMap["tool_use_id"].(string); ok && id != "" {
					oaiMsg.ToolCallID = id
				}
			}
		}
		if oaiMsg.Content == "" {
			oaiMsg.Content = contentToString(msg.Content)
		}
		*messages = append(*messages, oaiMsg)
		return 1
	}

	// Parse content blocks (modern Anthropic format)
	var blocks []anthropic.ContentBlock
	contentIsArray := false
	if msg.Content != nil {
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			contentIsArray = true
		}
	}

	// Modern Anthropic user message with tool_result blocks: emit one tool msg per result
	if msg.Role == "user" && contentIsArray {
		var toolResults []anthropic.ContentBlock
		var nonResultBlocks []anthropic.ContentBlock
		for _, b := range blocks {
			if b.Type == "tool_result" {
				toolResults = append(toolResults, b)
			} else {
				nonResultBlocks = append(nonResultBlocks, b)
			}
		}

		// Emit one tool message per tool_result block
		for _, tr := range toolResults {
			oaiTool := openai.Message{
				Role:       "tool",
				ToolCallID: tr.ToolUseID,
				Content:    toolResultContent(tr),
			}
			*messages = append(*messages, oaiTool)
			emitted++
		}

		// If there are non-tool_result blocks, emit a user message with those
		if len(nonResultBlocks) > 0 {
			userMsg := openai.Message{Role: "user"}
			if hasOnlyText(nonResultBlocks) {
				// All text — send as plain string
				var texts []string
				for _, b := range nonResultBlocks {
					if b.Type == "text" {
						texts = append(texts, b.Text)
					}
				}
				userMsg.Content = strings.Join(texts, "\n")
			} else {
				// Has images or other multimodal content
				var parts []openai.ContentPart
				for _, b := range nonResultBlocks {
					switch b.Type {
					case "text":
						parts = append(parts, openai.ContentPart{Type: "text", Text: b.Text})
					case "image":
						if b.Source != nil {
							var imageURL string
							if b.Source.Type == "base64" && b.Source.Data != "" {
								imageURL = fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
							} else if b.Source.Type == "url" && b.Source.URL != "" {
								imageURL = b.Source.URL
							}
							if imageURL != "" {
								parts = append(parts, openai.ContentPart{
									Type:     "image_url",
									ImageURL: &openai.ImageURL{URL: imageURL},
								})
							}
						}
					}
				}
				if len(parts) > 0 {
					userMsg.Content = parts
				}
			}
			*messages = append(*messages, userMsg)
			emitted++
		}

		// If we emitted at least one tool message, return
		if emitted > 0 {
			return emitted
		}
	}

	// Standard path: single OpenAI message
	oaiMsg := openai.Message{Role: msg.Role}

	if msg.Role == "assistant" && contentIsArray {
		content, toolCalls := convertContentBlocks(msg.Content)
		oaiMsg.Content = content
		if len(toolCalls) > 0 {
			oaiMsg.ToolCalls = toolCalls
		}
	} else if msg.Role == "user" && contentIsArray {
		// User message with images (no tool_results — handled above)
		var parts []openai.ContentPart
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, openai.ContentPart{Type: "text", Text: b.Text})
			case "image":
				if b.Source != nil {
					var imageURL string
					if b.Source.Type == "base64" && b.Source.Data != "" {
						imageURL = fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
					} else if b.Source.Type == "url" && b.Source.URL != "" {
						imageURL = b.Source.URL
					}
					if imageURL != "" {
						parts = append(parts, openai.ContentPart{
							Type:     "image_url",
							ImageURL: &openai.ImageURL{URL: imageURL},
						})
					}
				}
			}
		}
		if len(parts) > 0 {
			oaiMsg.Content = parts
		} else {
			oaiMsg.Content = contentToString(msg.Content)
		}
	} else {
		oaiMsg.Content = contentToString(msg.Content)
	}

	*messages = append(*messages, oaiMsg)
	return 1
}

// toolResultContent extracts the text content from an Anthropic tool_result block.
func toolResultContent(tr anthropic.ContentBlock) string {
	isErr := tr.IsError != nil && *tr.IsError
	// tool_result content can be a string, array of blocks, or array with text + is_error
	if tr.Content != nil {
		// Try to parse as array of content blocks
		var inner []anthropic.ContentBlock
		if err := json.Unmarshal(tr.Content, &inner); err == nil {
			var texts []string
			for _, b := range inner {
				if b.Type == "text" && b.Text != "" {
					texts = append(texts, b.Text)
				}
			}
			if len(texts) > 0 {
				result := strings.Join(texts, "\n")
				if isErr {
					return "[Tool Error] " + result
				}
				return result
			}
		}
		// Fall back to plain string
		var s string
		if err := json.Unmarshal(tr.Content, &s); err == nil {
			if isErr {
				return "[Tool Error] " + s
			}
			return s
		}
	}
	// Empty content
	if isErr {
		return "[Tool Error]"
	}
	return ""
}

// hasOnlyText reports whether the given blocks are all text blocks.
func hasOnlyText(blocks []anthropic.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type != "text" {
			return false
		}
	}
	return true
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

	// Copy conversation messages, expanding tool_result blocks into separate tool messages
	for _, msg := range conversationMsgs {
		emitted := convertAnthropicMessage(msg, &messages)
		_ = emitted // count not needed; messages appended in place
	}

	// Strip orphaned tool_calls from assistant messages that lack a following
	// tool-role message. Some providers (DeepSeek) reject requests where assistant
	// tool_calls are not immediately followed by paired tool results.
	// Only strip when the assistant's tool_calls are NOT followed by any tool message.
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Check if there is any "tool" role message anywhere after this index
			// (not just immediately after, because we may have tool messages at
			// later positions in the converted stream).
			hasToolResponse := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "tool" {
					hasToolResponse = true
					break
				}
			}
			if !hasToolResponse {
				messages[i].ToolCalls = nil
			}
		}
	}

	// max_tokens — ensure at least 1 to avoid "unlimited" generation
	maxTokens := antReq.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	req := &openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   &maxTokens,
		Temperature: antReq.Temperature,
		TopP:        antReq.TopP,
		TopK:        antReq.TopK,
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
		if err := json.Unmarshal(antReq.ToolChoice, &choice); err == nil {
			if choice.Type == "" {
				// Malformed tool_choice (valid JSON but no type) — default to "auto"
				req.ToolChoice = "auto"
				log.Printf("WARNING: tool_choice has empty type field, defaulting to auto")
			} else {
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
						req.ToolChoice = "auto"
					}
				default:
					req.ToolChoice = "auto"
				}
			}
		} else {
			// Malformed tool_choice JSON — default to "auto"
			req.ToolChoice = "auto"
			log.Printf("WARNING: failed to parse tool_choice: %v", err)
		}
	}

	return req
}
