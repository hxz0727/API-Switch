package converter

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
// It includes a heartbeat mechanism to prevent connection timeouts during long generations.
func OpenAIToAnthropicStream(openaiBody io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool, requestedModel string, inputTokens int) error {
	return OpenAIToAnthropicStreamWithHeartbeat(openaiBody, writer, flusher, canFlush, requestedModel, inputTokens, 15*time.Second)
}

// OpenAIToAnthropicStreamWithHeartbeat reads OpenAI SSE events and writes Anthropic SSE events
// with periodic heartbeat pings to prevent connection timeouts.
func OpenAIToAnthropicStreamWithHeartbeat(openaiBody io.Reader, writer io.Writer, flusher http.Flusher, canFlush bool, requestedModel string, inputTokens int, heartbeatInterval time.Duration) error {
	scanner := bufio.NewScanner(openaiBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4MB max line

	msgID := GenerateMessageID()
	textBlockStarted := false
	textBlockIndex := 0
	nextBlockIndex := 0
	totalOutputTokens := 0
	toolCallAccumulators := make(map[int]*ToolCallAcc)
	streamSawFinish := false
	var streamUsage *openai.Usage

	// Start heartbeat goroutine
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	if canFlush && heartbeatInterval > 0 {
		go func() {
			ticker := time.NewTicker(heartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatDone:
					return
				case <-ticker.C:
					WriteAnthropicEvent(writer, flusher, canFlush, "ping", map[string]string{"type": "ping"})
				}
			}
		}()
	}

	// Emit message_start
	if err := WriteAnthropicEvent(writer, flusher, canFlush, "message_start", anthropic.MessageStartEvent{
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
	}); err != nil {
		return fmt.Errorf("write message_start: %w", err)
	}

	// Emit ping
	if err := WriteAnthropicEvent(writer, flusher, canFlush, "ping", map[string]string{"type": "ping"}); err != nil {
		return fmt.Errorf("write ping: %w", err)
	}

	done := func(reason *string) {
		if textBlockStarted {
			_ = WriteAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: textBlockIndex,
			})
		}
		for _, acc := range toolCallAccumulators {
			_ = WriteAnthropicEvent(writer, flusher, canFlush, "content_block_stop", anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: acc.BlockIndex,
			})
		}
		outputTokens := totalOutputTokens
		if streamUsage != nil && streamUsage.CompletionTokens > 0 {
			outputTokens = streamUsage.CompletionTokens
		}
		_ = WriteAnthropicEvent(writer, flusher, canFlush, "message_delta", anthropic.MessageDeltaEvent{
			Type: "message_delta",
			Delta: anthropic.MessageDelta{
				StopReason: reason,
			},
			Usage: anthropic.DeltaUsage{
				OutputTokens: outputTokens,
			},
		})
		_ = WriteAnthropicEvent(writer, flusher, canFlush, "message_stop", anthropic.MessageStopEvent{
			Type: "message_stop",
		})
		if canFlush {
			flusher.Flush()
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		if data == "[DONE]" {
			if !streamSawFinish {
				s := "end_turn"
				done(&s)
			}
			return nil
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if len(data) > 200 {
				log.Printf("WARNING: SSE JSON parse error (first 200 chars): %s... %v", data[:200], err)
			} else {
				log.Printf("WARNING: SSE JSON parse error: %s %v", data, err)
			}
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		if chunk.Usage != nil {
			streamUsage = chunk.Usage
		}

		choice := chunk.Choices[0]

		textDelta := choice.Delta.Content
		if textDelta == "" {
			textDelta = choice.Delta.Reasoning
		}

		if textDelta != "" {
			if !textBlockStarted {
				if err := WriteAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
					Type:  "content_block_start",
					Index: textBlockIndex,
					ContentBlock: anthropic.ContentBlock{
						Type: "text",
						Text: "",
					},
				}); err != nil {
					return fmt.Errorf("write text block start: %w", err)
				}
				textBlockStarted = true
				nextBlockIndex = textBlockIndex + 1
			}

			if err := WriteAnthropicEvent(writer, flusher, canFlush, "content_block_delta", anthropic.ContentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: textBlockIndex,
				Delta: anthropic.DeltaBlock{
					Type: "text_delta",
					Text: textDelta,
				},
			}); err != nil {
				return fmt.Errorf("write text delta: %w", err)
			}
			totalOutputTokens++
		}

		for _, tcd := range choice.Delta.ToolCalls {
			if tcd.Function == nil {
				continue
			}
			acc, exists := toolCallAccumulators[tcd.Index]
			if !exists {
				acc = &ToolCallAcc{ID: tcd.ID, Name: tcd.Function.Name}
				toolCallAccumulators[tcd.Index] = acc

				acc.BlockIndex = nextBlockIndex
				nextBlockIndex++
				if err := WriteAnthropicEvent(writer, flusher, canFlush, "content_block_start", anthropic.ContentBlockStartEvent{
					Type:  "content_block_start",
					Index: acc.BlockIndex,
					ContentBlock: anthropic.ContentBlock{
						Type: "tool_use",
						ID:   tcd.ID,
						Name: tcd.Function.Name,
					},
				}); err != nil {
					return fmt.Errorf("write tool block start: %w", err)
				}
			}

			if tcd.Function.Arguments != "" {
				acc.Arguments.WriteString(tcd.Function.Arguments)
				if err := WriteAnthropicEvent(writer, flusher, canFlush, "content_block_delta", anthropic.ContentBlockDeltaEvent{
					Type:  "content_block_delta",
					Index: acc.BlockIndex,
					Delta: anthropic.DeltaBlock{
						Type:        "input_json_delta",
						PartialJSON: tcd.Function.Arguments,
					},
				}); err != nil {
					return fmt.Errorf("write tool delta: %w", err)
				}
				totalOutputTokens++
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			done(MapFinishReason(choice.FinishReason))
			streamSawFinish = true
			return nil
		}
	}

	if !streamSawFinish {
		s := "end_turn"
		done(&s)
	}
	return scanner.Err()
}

// MapFinishReason maps OpenAI finish reasons to Anthropic equivalents.
func MapFinishReason(reason *string) *string {
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
	s := "end_turn"
	return &s
}

// WriteAnthropicEvent writes an Anthropic SSE event.
func WriteAnthropicEvent(w io.Writer, flusher http.Flusher, canFlush bool, eventType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if canFlush {
		flusher.Flush()
	}
	return nil
}

// GenerateStreamMessageID generates a message ID for streaming responses.
func GenerateStreamMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// ToolCallAcc accumulates streaming tool call data from multiple SSE chunks.
type ToolCallAcc struct {
	ID         string
	Name       string
	BlockIndex int
	Arguments  strings.Builder
}
