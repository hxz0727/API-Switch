package streaming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
	"github.com/hxz0727/API-Switch/pkg/openai"
)

// OpenAIToAnthropicStream reads OpenAI SSE events and writes Anthropic SSE events.
func OpenAIToAnthropicStream(openaiBody io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool, requestedModel string, inputTokens int) error {
	scanner := bufio.NewScanner(openaiBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	msgID := generateMessageID()
	contentBlockStarted := false
	contentBlockIndex := 0
	totalOutputTokens := 0
	toolCallAccumulators := make(map[int]*toolCallAcc) // index -> accumulator

	// Emit message_start
	writeAnthropicEvent(writer, flusher, canFlush, "message_start", anthropic.MessageStartEvent{
		Type: "message_start",
		Message: anthropic.MessagesResponse{
			ID:      msgID,
			Type:    "message",
			Role:    "assistant",
			Content: []anthropic.ContentBlock{},
			Model:   requestedModel,
			Usage: anthropic.ResponseUsage{
				InputTokens:  inputTokens,
				OutputTokens: 0,
			},
		},
	})

	// Emit ping
	writeAnthropicEvent(writer, flusher, canFlush, "ping", map[string]string{"type": "ping"})

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for stream end
		if data == "[DONE]" {
			break
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Malformed SSE data — log and skip
			log.Printf("[DEBUG] SSE parse error (skipping chunk): %v, raw: %.200s", err, data)
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

	// If there's content in the delta
	if choice.Delta.Content != "" {
		// Start content block if not started
		if !contentBlockStarted {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
				Type:  "content_block_start",
				Index: contentBlockIndex,
				ContentBlock: anthropic.ContentBlock{
					Type: "text",
					Text: "",
				},
			})
			contentBlockStarted = true
		}

		// Emit text delta
		writeAnthropicEvent(writer, flusher, canFlush, "content_block_delta", anthropic.ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: contentBlockIndex,
			Delta: anthropic.DeltaBlock{
				Type: "text_delta",
				Text: choice.Delta.Content,
			},
		})
		totalOutputTokens++
	}

	// Handle tool call deltas
	for _, tcd := range choice.Delta.ToolCalls {
		if tcd.Function == nil {
			continue
		}
		acc, exists := toolCallAccumulators[tcd.Index]
		if !exists {
			acc = &toolCallAcc{id: tcd.ID, name: tcd.Function.Name}
			toolCallAccumulators[tcd.Index] = acc

			// Start a tool_use content block
			contentBlockIndex++
			acc.blockIndex = contentBlockIndex
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
				Type:  "content_block_start",
				Index: contentBlockIndex,
				ContentBlock: anthropic.ContentBlock{
					Type: "tool_use",
					ID:   tcd.ID,
					Name: tcd.Function.Name,
				},
			})
		}

		if tcd.Function.Arguments != "" {
			acc.arguments.WriteString(tcd.Function.Arguments)
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_delta", anthropic.ContentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: acc.blockIndex,
				Delta: anthropic.DeltaBlock{
					Type: "input_json_delta",
					Text: tcd.Function.Arguments,
				},
			})
			totalOutputTokens++
		}
	}

	// If there's a finish_reason, close up the stream
	if choice.FinishReason != nil {
		// Close text content block if started
		if contentBlockStarted {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: contentBlockIndex,
			})
		}

		// Close any accumulated tool call content blocks
		for _, acc := range toolCallAccumulators {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: acc.blockIndex,
			})
		}

		// Map finish_reason to stop_reason
		stopReason := mapFinishReason(choice.FinishReason)

		// Emit message_delta with stop_reason
		writeAnthropicEvent(writer, flusher, canFlush, "message_delta", anthropic.MessageDeltaEvent{
			Type: "message_delta",
			Delta: anthropic.MessageDelta{
				StopReason: stopReason,
			},
			Usage: anthropic.DeltaUsage{
				OutputTokens: totalOutputTokens,
			},
		})

		// Emit message_stop
		writeAnthropicEvent(writer, flusher, canFlush, "message_stop", anthropic.MessageStopEvent{
			Type: "message_stop",
		})
	}

		// Track usage if present in the final chunk
		if chunk.Usage != nil {
			totalOutputTokens = chunk.Usage.CompletionTokens
		}
	}

	return scanner.Err()
}

// AnthropicPassthroughStream reads Anthropic SSE events from the upstream and writes them to the client.
// This is used when the model is an Anthropic model and we're just proxying.
func AnthropicPassthroughStream(body io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(writer, "%s\n", line)
		if canFlush {
			flusher.Flush()
		}

		// SSE events are separated by blank lines
		if line == "" {
			continue
		}
	}

	return scanner.Err()
}

func writeAnthropicEvent(w io.Writer, flusher http.Flusher, canFlush bool, eventType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	if canFlush {
		flusher.Flush()
	}
}

func mapFinishReason(reason *string) *string {
	if reason == nil {
		s := "end_turn"
		return &s
	}
	mapping := map[string]string{
		"stop":           "end_turn",
		"length":         "max_tokens",
		"tool_calls":     "tool_use",
		"content_filter": "end_turn",
	}
	if mapped, ok := mapping[*reason]; ok {
		return &mapped
	}
	return reason
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// toolCallAcc accumulates streaming tool call data from multiple SSE chunks.
type toolCallAcc struct {
	id        string
	name      string
	blockIndex int
	arguments strings.Builder
}
