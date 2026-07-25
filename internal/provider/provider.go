package provider

import (
	"context"
	"io"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// Provider defines the unified interface for all LLM providers.
// All providers accept and return Anthropic Messages API format.
// Protocol conversion (e.g., Anthropic -> OpenAI) is encapsulated within the implementation.
type Provider interface {
	// Name returns the provider instance name (e.g., "deepseek", "qwen").
	Name() string

	// Type returns the protocol type (e.g., "anthropic", "openai").
	Type() string

	// SendMessage sends a non-streaming request and returns an Anthropic-format response.
	// The model parameter is the actual model name to send to the provider.
	// The maxTokens parameter is the default max tokens if not specified in the request.
	SendMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (*anthropic.MessagesResponse, error)

	// StreamMessage sends a streaming request and returns an SSE event stream.
	// The returned ReadCloser emits Anthropic-format SSE events.
	StreamMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (io.ReadCloser, error)

	// Ping checks if the provider is reachable.
	Ping() error

	// ListModels returns the available model IDs from this provider.
	ListModels() ([]string, error)
}
