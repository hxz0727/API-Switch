package provider

import (
	"context"
	"io"
	"net/http"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/converter"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// OpenAIProvider wraps OpenAIClient to implement the Provider interface.
// It handles protocol conversion (Anthropic <-> OpenAI) internally.
type OpenAIProvider struct {
	name             string
	client           *OpenAIClient
	defaultMaxTokens int
}

// NewOpenAIProvider creates a new OpenAIProvider.
func NewOpenAIProvider(name string, cfg *config.ProviderConfig) *OpenAIProvider {
	return &OpenAIProvider{
		name:             name,
		client:           NewOpenAIClient(cfg),
		defaultMaxTokens: cfg.DefaultMaxTokens,
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) Type() string { return "openai" }

func (p *OpenAIProvider) SendMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (*anthropic.MessagesResponse, error) {
	// Convert Anthropic request -> OpenAI request
	oaiReq := converter.AnthropicToOpenAI(req, model, maxTokens)

	// Send to OpenAI-compatible provider
	oaiResp, err := p.client.SendMessageWithContext(ctx, oaiReq)
	if err != nil {
		return nil, err
	}

	// Convert OpenAI response -> Anthropic response
	return converter.OpenAIToAnthropic(oaiResp, req.Model), nil
}

func (p *OpenAIProvider) StreamMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (io.ReadCloser, error) {
	// Convert Anthropic request -> OpenAI request
	oaiReq := converter.AnthropicToOpenAI(req, model, maxTokens)

	// Send streaming request to OpenAI-compatible provider
	respBody, err := p.client.StreamMessageWithContext(ctx, oaiReq)
	if err != nil {
		return nil, err
	}

	// Create a pipe: the converter writes Anthropic SSE events to the pipe writer,
	// and the caller reads from the pipe reader.
	pr, pw := io.Pipe()

	// Run the streaming conversion in a goroutine
	go func() {
		defer respBody.Close()
		inputTokens := estimateInputTokens(req)
		convErr := converter.OpenAIToAnthropicStream(respBody, pw, nil, false, req.Model, inputTokens)
		pw.CloseWithError(convErr)
	}()

	return pr, nil
}

// estimateInputTokens provides a rough estimate of input tokens.
func estimateInputTokens(req *anthropic.MessagesRequest) int {
	// Simple heuristic: ~4 chars per token
	total := 0
	if req.System != nil {
		total += len(converter.ContentToString(req.System)) / 4
	}
	for _, msg := range req.Messages {
		total += len(converter.ContentToString(msg.Content)) / 4
	}
	return total
}

func (p *OpenAIProvider) Ping() error {
	return p.client.Ping()
}

func (p *OpenAIProvider) ListModels() ([]string, error) {
	return p.client.ListModels()
}

// GetClient returns the underlying OpenAIClient for backward compatibility.
func (p *OpenAIProvider) GetClient() *OpenAIClient {
	return p.client
}

// StreamToWriter converts an OpenAI stream to Anthropic SSE events and writes to the given writer.
// This is used by the handler when it needs to write directly to the HTTP response.
func (p *OpenAIProvider) StreamToWriter(ctx context.Context, w io.Writer, flusher http.Flusher, canFlush bool, req *anthropic.MessagesRequest, model string, maxTokens int) error {
	oaiReq := converter.AnthropicToOpenAI(req, model, maxTokens)

	respBody, err := p.client.StreamMessageWithContext(ctx, oaiReq)
	if err != nil {
		return err
	}
	defer respBody.Close()

	inputTokens := estimateInputTokens(req)
	return converter.OpenAIToAnthropicStream(respBody, w, flusher, canFlush, req.Model, inputTokens)
}

func init() {
	Register("openai", func(name string, cfg *config.ProviderConfig) Provider {
		return NewOpenAIProvider(name, cfg)
	})
}
