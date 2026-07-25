package streaming

import (
	"bufio"
	"fmt"
	"io"
	"net/http"

	"github.com/hxz0727/API-Switch/internal/converter"
)

// OpenAIToAnthropicStream reads OpenAI SSE events and writes Anthropic SSE events.
func OpenAIToAnthropicStream(openaiBody io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool, requestedModel string, inputTokens int) error {
	return converter.OpenAIToAnthropicStream(openaiBody, writer, flusher, canFlush, requestedModel, inputTokens)
}

// AnthropicPassthroughStream reads Anthropic SSE events from the upstream and writes them to the client.
// This is used when the model is an Anthropic model and we're just proxying.
func AnthropicPassthroughStream(body io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4MB max line

	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintf(writer, "%s\n", line); err != nil {
			return fmt.Errorf("write passthrough: %w", err)
		}
		if canFlush {
			flusher.Flush()
		}

		if line == "" {
			continue
		}
	}

	return scanner.Err()
}
