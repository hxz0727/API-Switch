package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// AnthropicClient is an HTTP client for the Anthropic Messages API.
type AnthropicClient struct {
	cfg    *config.ProviderConfig
	client *http.Client
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(cfg *config.ProviderConfig) *AnthropicClient {
	return &AnthropicClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *AnthropicClient) newRequest(body io.Reader) (*http.Request, error) {
	return c.newRequestWithContext(context.Background(), body)
}

func (c *AnthropicClient) newRequestWithContext(ctx context.Context, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/v1/messages", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", c.cfg.APIVersion)
	return req, nil
}

// SendMessage sends a non-streaming request to the Anthropic API.
func (c *AnthropicClient) SendMessage(req *anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	return c.SendMessageWithContext(context.Background(), req)
}

// SendMessageWithContext sends a non-streaming request with context for cancellation.
func (c *AnthropicClient) SendMessageWithContext(ctx context.Context, req *anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := c.newRequestWithContext(ctx, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the full body before decoding to avoid "body already closed" issues
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(rawBody))
	}

	var antResp anthropic.MessagesResponse
	if err := json.Unmarshal(rawBody, &antResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &antResp, nil
}

// SendMessageRaw sends a raw request and returns decoded JSON.
// Used by the test command for lightweight end-to-end checks.
func (c *AnthropicClient) SendMessageRaw(req interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := c.newRequest(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(rawBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// Ping checks if the API is reachable with a short timeout.
func (c *AnthropicClient) Ping() error {
	hc := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", c.cfg.BaseURL+"/v1/messages", nil)
	req.Header.Set("x-api-key", c.cfg.APIKey)
	_, err := hc.Do(req)
	return err
}

// StreamMessage sends a streaming request to the Anthropic API and returns the response body for reading SSE events.
func (c *AnthropicClient) StreamMessage(req *anthropic.MessagesRequest) (io.ReadCloser, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := c.newRequest(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("anthropic streaming error (status %d): %s", resp.StatusCode, string(rawBody))
	}

	return resp.Body, nil
}
