package provider

import (
	"context"
	"io"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// AnthropicProvider wraps AnthropicClient to implement the Provider interface.
type AnthropicProvider struct {
	name   string
	client *AnthropicClient
}

// NewAnthropicProvider creates a new AnthropicProvider.
func NewAnthropicProvider(name string, cfg *config.ProviderConfig) *AnthropicProvider {
	return &AnthropicProvider{
		name:   name,
		client: NewAnthropicClient(cfg),
	}
}

func (p *AnthropicProvider) Name() string { return p.name }

func (p *AnthropicProvider) Type() string { return "anthropic" }

func (p *AnthropicProvider) SendMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (*anthropic.MessagesResponse, error) {
	reqCopy := *req
	reqCopy.Model = model
	return p.client.SendMessageWithContext(ctx, &reqCopy)
}

func (p *AnthropicProvider) StreamMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (io.ReadCloser, error) {
	reqCopy := *req
	reqCopy.Model = model
	return p.client.StreamMessageWithContext(ctx, &reqCopy)
}

func (p *AnthropicProvider) Ping() error {
	return p.client.Ping()
}

func (p *AnthropicProvider) ListModels() ([]string, error) {
	// Anthropic API does not have a public /v1/models endpoint
	return nil, nil
}

// GetClient returns the underlying AnthropicClient for backward compatibility.
func (p *AnthropicProvider) GetClient() *AnthropicClient {
	return p.client
}

func init() {
	Register("anthropic", func(name string, cfg *config.ProviderConfig) Provider {
		return NewAnthropicProvider(name, cfg)
	})
}
