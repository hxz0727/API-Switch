package streaming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
	textBlockStarted := false
	textBlockIndex := 0           // always index 0 for the text content block
	nextBlockIndex := 0           // next available block index, incremented on each content_block_start
	totalOutputTokens := 0
	toolCallAccumulators := make(map[int]*toolCallAcc)
	streamSawFinish := false
	var streamUsage *openai.Usage

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

	// done emits content_block_stop + message_delta + message_stop and returns nil.
	done := func(reason *string) {
		if textBlockStarted {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: textBlockIndex,
			})
		}
		for _, acc := range toolCallAccumulators {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: acc.blockIndex,
			})
		}
		outputTokens := totalOutputTokens
		if streamUsage != nil && streamUsage.CompletionTokens > 0 {
			outputTokens = streamUsage.CompletionTokens
		}
		writeAnthropicEvent(writer, flusher, canFlush, "message_delta", anthropic.MessageDeltaEvent{
			Type: "message_delta",
			Delta: anthropic.MessageDelta{
				StopReason: reason,
			},
			Usage: anthropic.DeltaUsage{
				OutputTokens: outputTokens,
			},
		})
		writeAnthropicEvent(writer, flusher, canFlush, "message_stop", anthropic.MessageStopEvent{
			Type: "message_stop",
		})
		if canFlush {
			flusher.Flush()
		}
	}

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
			if !streamSawFinish {
				// Provider ended stream without finish_reason — close gracefully
				s := "end_turn"
				done(&s)
			}
			return nil
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		// Capture usage from any chunk that has it (before finish_reason processing)
		if chunk.Usage != nil {
			streamUsage = chunk.Usage
		}

		choice := chunk.Choices[0]

	// Forward content deltas (skip reasoning/thinking field)
	if choice.Delta.Content != "" {
		// Start text content block if not started (index 0)
		if !textBlockStarted {
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
				Type:  "content_block_start",
				Index: textBlockIndex,
				ContentBlock: anthropic.ContentBlock{
					Type: "text",
					Text: "",
				},
			})
			textBlockStarted = true
			nextBlockIndex = textBlockIndex + 1
		}

		// Emit text delta
		writeAnthropicEvent(writer, flusher, canFlush, "content_block_delta", anthropic.ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: textBlockIndex,
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

			// Start a tool_use content block at next available index
			acc.blockIndex = nextBlockIndex
			nextBlockIndex++
			writeAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
				Type:  "content_block_start",
				Index: acc.blockIndex,
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
					Type:        "input_json_delta",
					PartialJSON: tcd.Function.Arguments,
				},
			})
			totalOutputTokens++
		}
	}

	// If there's a finish_reason, close up the stream
	// Note: some providers send "finish_reason":"" (empty string) in non-final chunks;
	// json.Unmarshal sets FinishReason to a non-nil pointer pointing to "",
	// so we must check the dereferenced value is non-empty.
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		done(mapFinishReason(choice.FinishReason))
		streamSawFinish = true
		return nil
	}
	}

	// scanner ended without [DONE] or finish_reason — close gracefully
	if !streamSawFinish {
		s := "end_turn"
		done(&s)
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
	// Fallback for unknown values — do not leak provider-specific reasons
	s := "end_turn"
	return &s
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
