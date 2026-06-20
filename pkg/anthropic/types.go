package anthropic

import "encoding/json"

// MessagesRequest represents an Anthropic Messages API request.
type MessagesRequest struct {
	Model         string           `json:"model"`
	Messages      []Message        `json:"messages"`
	System        json.RawMessage  `json:"system,omitempty"`
	MaxTokens     int              `json:"max_tokens"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	TopK          *int             `json:"top_k,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	Stream        bool             `json:"stream,omitempty"`
	Tools         []ToolDefinition `json:"tools,omitempty"`
	ToolChoice    json.RawMessage  `json:"tool_choice,omitempty"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Message represents a single message in the conversation.
// Content can be either a plain string or an array of content blocks.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// MessagesResponse represents an Anthropic Messages API response.
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        ResponseUsage  `json:"usage"`
}

// ContentBlock represents a content block in a request or response.
type ContentBlock struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	ID     string          `json:"id,omitempty"`     // tool_use id
	Name   string          `json:"name,omitempty"`   // tool_use name
	Input  json.RawMessage `json:"input,omitempty"`  // tool_use input
	Source *ImageSource    `json:"source,omitempty"` // image source
	// For tool_result in requests
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
}

// ImageSource represents an image source in a content block.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// ResponseUsage represents token usage in the response.
type ResponseUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// --- Streaming Event Types ---

// StreamEvent is a minimal struct to detect the event type.
type StreamEvent struct {
	Type string `json:"type"`
}

// MessageStartEvent represents a message_start streaming event.
type MessageStartEvent struct {
	Type    string           `json:"type"`
	Message MessagesResponse `json:"message"`
}

// ContentBlockStartEvent represents a content_block_start streaming event.
type ContentBlockStartEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDeltaEvent represents a content_block_delta streaming event.
type ContentBlockDeltaEvent struct {
	Type  string     `json:"type"`
	Index int        `json:"index"`
	Delta DeltaBlock `json:"delta"`
}

// DeltaBlock represents the delta in a content_block_delta event.
type DeltaBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ContentBlockStopEvent represents a content_block_stop streaming event.
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// MessageDeltaEvent represents a message_delta streaming event.
type MessageDeltaEvent struct {
	Type  string       `json:"type"`
	Delta MessageDelta `json:"delta"`
	Usage DeltaUsage   `json:"usage"`
}

// MessageDelta represents the delta in a message_delta event.
type MessageDelta struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// DeltaUsage represents token usage in a message_delta event.
type DeltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// MessageStopEvent represents a message_stop streaming event.
type MessageStopEvent struct {
	Type string `json:"type"`
}

// ErrorEvent represents an error streaming event.
type ErrorEvent struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// RawEvent holds the raw JSON for any unhandled event type.
type RawEvent struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}
