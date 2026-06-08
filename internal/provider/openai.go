package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// OpenAIClient is an HTTP client for OpenAI-compatible APIs.
type OpenAIClient struct {
	cfg    *config.ProviderConfig
	client *http.Client
}

// NewOpenAIClient creates a new OpenAI-compatible API client.
func NewOpenAIClient(cfg *config.ProviderConfig) *OpenAIClient {
	return &OpenAIClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func openAIEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func (c *OpenAIClient) newRequest(body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest("POST", openAIEndpoint(c.cfg.BaseURL), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	return req, nil
}

// SendMessage sends a non-streaming request to the OpenAI-compatible API.
func (c *OpenAIClient) SendMessage(req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
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

	// Read the full body so we can inspect it for error responses
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(rawBody))
	}

	// Some providers (e.g. APIFree) return errors with HTTP 200.
	// Check if the body contains an "error" field before decoding as success.
	var errCheck struct {
		Code  int `json:"code"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rawBody, &errCheck); err == nil {
		if errCheck.Error != nil && errCheck.Error.Message != "" {
			return nil, fmt.Errorf("upstream API error (HTTP 200): %s", errCheck.Error.Message)
		}
	}

	var oaiResp openai.ChatCompletionResponse
	if err := json.Unmarshal(rawBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w\nraw body: %s", err, string(rawBody))
	}
	if len(oaiResp.Choices) == 0 {
		log.Printf("WARNING: OpenAI response has 0 choices. Raw body: %s", string(rawBody))
	}
	return &oaiResp, nil
}

// StreamMessage sends a streaming request and returns the response body for reading SSE events.
func (c *OpenAIClient) StreamMessage(req *openai.ChatCompletionRequest) (io.ReadCloser, error) {
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
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		bodyStr := string(raw)
		if bodyStr == "" {
			bodyStr = "(empty body)"
		}
		return nil, fmt.Errorf("openai streaming error (status %d): %s", resp.StatusCode, bodyStr)
	}

	// Some providers (e.g. APIFree) return error JSON (not SSE) with HTTP 200.
	// Peek at first bytes to detect non-SSE (JSON error) responses.
	reader := bufio.NewReaderSize(resp.Body, 1024)
	peek, err := reader.Peek(1)
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to peek streaming response: %w", err)
	}
	if peek[0] == '{' {
		// Looks like JSON, not SSE — likely an error wrapped in HTTP 200
		defer resp.Body.Close()
		errBytes, _ := io.ReadAll(reader)
		var errBody struct {
			Code  int `json:"code"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(errBytes, &errBody) == nil && errBody.Error != nil {
			return nil, fmt.Errorf("upstream API error (HTTP 200): %s", errBody.Error.Message)
		}
		return nil, fmt.Errorf("upstream API returned non-SSE response: %s", string(errBytes))
	}

	return &streamReadCloser{Reader: reader, closer: resp.Body}, nil
}

// streamReadCloser wraps a bufio.Reader and closes the original body on Close().
type streamReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

func (s *streamReadCloser) Close() error {
	return s.closer.Close()
}

func openAIModelsEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1/chat/completions") {
		baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	}
	if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v1/") {
		return strings.TrimRight(baseURL, "/") + "/models"
	}
	return baseURL + "/models"
}

// Ping checks if the provider is reachable via a lightweight /v1/models call.
func (c *OpenAIClient) Ping() error {
	endpoint := openAIModelsEndpoint(c.cfg.BaseURL)
	hc := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListModels fetches available models from an OpenAI-compatible provider.
func (c *OpenAIClient) ListModels() ([]string, error) {
	endpoint := openAIModelsEndpoint(c.cfg.BaseURL)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		bodyStr := string(raw)
		if bodyStr == "" {
			bodyStr = "(empty body)"
		}
		return nil, fmt.Errorf("list models API error (status %d): %s", resp.StatusCode, bodyStr)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode models list: %w", err)
	}

	var models []string
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}
