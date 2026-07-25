package converter

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// ContentToString extracts a plain text string from an anthropic content field,
// which may be either a plain string or an array of content blocks.
func ContentToString(content json.RawMessage) string {
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

// HasContentBeyondText checks if content has blocks beyond text (images, tool_use, etc).
func HasContentBeyondText(content json.RawMessage) bool {
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

// ConvertContentBlocks converts Anthropic content blocks to OpenAI message format.
func ConvertContentBlocks(content json.RawMessage) (string, []openai.ToolCall) {
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ContentToString(content), nil
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

// ConvertAnthropicMessage converts a single Anthropic message into one or more
// OpenAI messages and appends them to the messages slice.
// Returns the number of OpenAI messages appended.
func ConvertAnthropicMessage(msg anthropic.Message, messages *[]openai.Message) int {
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
					contentParts = append(contentParts, ContentToString(b.Content))
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
			oaiMsg.Content = ContentToString(msg.Content)
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

	// Modern Anthropic user message with tool_result blocks
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

		for _, tr := range toolResults {
			oaiTool := openai.Message{
				Role:       "tool",
				ToolCallID: tr.ToolUseID,
				Content:    ToolResultContent(tr),
			}
			*messages = append(*messages, oaiTool)
			emitted++
		}

		if len(nonResultBlocks) > 0 {
			userMsg := openai.Message{Role: "user"}
			if HasOnlyText(nonResultBlocks) {
				var texts []string
				for _, b := range nonResultBlocks {
					if b.Type == "text" {
						texts = append(texts, b.Text)
					}
				}
				userMsg.Content = strings.Join(texts, "\n")
			} else {
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

		if emitted > 0 {
			return emitted
		}
	}

	// Standard path: single OpenAI message
	oaiMsg := openai.Message{Role: msg.Role}

	if msg.Role == "assistant" && contentIsArray {
		content, toolCalls := ConvertContentBlocks(msg.Content)
		oaiMsg.Content = content
		if len(toolCalls) > 0 {
			oaiMsg.ToolCalls = toolCalls
		}
	} else if msg.Role == "user" && contentIsArray {
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
			oaiMsg.Content = ContentToString(msg.Content)
		}
	} else {
		oaiMsg.Content = ContentToString(msg.Content)
	}

	*messages = append(*messages, oaiMsg)
	return 1
}

// ToolResultContent extracts the text content from an Anthropic tool_result block.
func ToolResultContent(tr anthropic.ContentBlock) string {
	isErr := tr.IsError != nil && *tr.IsError
	if tr.Content != nil {
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
		var s string
		if err := json.Unmarshal(tr.Content, &s); err == nil {
			if isErr {
				return "[Tool Error] " + s
			}
			return s
		}
	}
	if isErr {
		return "[Tool Error]"
	}
	return ""
}

// HasOnlyText reports whether the given blocks are all text blocks.
func HasOnlyText(blocks []anthropic.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type != "text" {
			return false
		}
	}
	return true
}

// AnthropicToOpenAI converts an Anthropic Messages request to an OpenAI ChatCompletion request.
func AnthropicToOpenAI(antReq *anthropic.MessagesRequest, model string, defaultMaxTokens int) *openai.ChatCompletionRequest {
	var messages []openai.Message

	var systemParts []string
	if systemContent := ContentToString(antReq.System); systemContent != "" {
		systemParts = append(systemParts, systemContent)
	}

	var conversationMsgs []anthropic.Message
	for _, msg := range antReq.Messages {
		if msg.Role == "system" {
			if sc := ContentToString(msg.Content); sc != "" {
				systemParts = append(systemParts, sc)
			}
		} else {
			conversationMsgs = append(conversationMsgs, msg)
		}
	}

	if len(systemParts) > 0 {
		messages = append(messages, openai.Message{
			Role:    "system",
			Content: strings.Join(systemParts, "\n"),
		})
	}

	for _, msg := range conversationMsgs {
		ConvertAnthropicMessage(msg, &messages)
	}

	// Strip orphaned tool_calls
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
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

	if len(antReq.StopSequences) > 0 {
		req.Stop = antReq.StopSequences
	}

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

	if antReq.ToolChoice != nil {
		var choice struct {
			Type string `json:"type"`
			Name string `json:"name,omitempty"`
		}
		if err := json.Unmarshal(antReq.ToolChoice, &choice); err == nil {
			if choice.Type == "" {
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
			req.ToolChoice = "auto"
			log.Printf("WARNING: failed to parse tool_choice: %v", err)
		}
	}

	return req
}
