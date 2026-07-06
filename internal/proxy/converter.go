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
			// For user messages with images, convert content blocks to OpenAI multimodal format.
			var blocks []anthropic.ContentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				var contentParts []openai.ContentPart
				for _, b := range blocks {
					switch b.Type {
					case "text":
						contentParts = append(contentParts, openai.ContentPart{
							Type: "text",
							Text: b.Text,
						})
					case "image":
						if b.Source != nil {
							// Build the image URL
							// Anthropic format: {"type": "base64", "media_type": "image/png", "data": "..."}
							// OpenAI format: {"url": "data:image/png;base64,..."} or direct URL
							var imageURL string
							if b.Source.Type == "base64" && b.Source.Data != "" {
								// Convert base64 data to data URL
								imageURL = fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
							} else if b.Source.Type == "url" && b.Source.URL != "" {
								// Direct URL
								imageURL = b.Source.URL
							}
							if imageURL != "" {
								contentParts = append(contentParts, openai.ContentPart{
									Type: "image_url",
									ImageURL: &openai.ImageURL{
										URL: imageURL,
									},
								})
							}
						}
					}
				}
				if len(contentParts) > 0 {
					oaiMsg.Content = contentParts
				} else {
					oaiMsg.Content = contentToString(msg.Content)
				}
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
				if oaiMsg.Content == "" {
					oaiMsg.Content = strings.Join(contentParts, "\n")
				}
			}
			if oaiMsg.Content == "" {
				oaiMsg.Content = contentToString(msg.Content)
			}
		}

		messages = append(messages, oaiMsg)
	}

	// Strip orphaned tool_calls from assistant messages that lack a following
	// tool-role message. Some providers (DeepSeek) reject requests where assistant
	// tool_calls are not immediately followed by paired tool results.
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Check if the very next message is a tool-role response
			if i+1 >= len(messages) || messages[i+1].Role != "tool" {
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
