package monitor

import (
	"fmt"
	"sync"
	"time"
)

// RequestEvent represents a single proxied request.
type RequestEvent struct {
	ID           string        `json:"id"`
	Timestamp    time.Time     `json:"timestamp"`
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	ProviderType string        `json:"provider_type"`
	Stream       bool          `json:"stream"`
	Duration     time.Duration `json:"duration"`
	Status       string        `json:"status"` // "ok", "error", "cancelled"
	InputTokens  int           `json:"input_tokens,omitempty"`
	OutputTokens int           `json:"output_tokens,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// Tracker is a thread-safe ring buffer of request events with SSE subscriber support.
type Tracker struct {
	mu        sync.RWMutex
	capacity  int
	events    []*RequestEvent
	head      int
	count     int
	listeners map[chan *RequestEvent]struct{}
	counter   int64
}

// NewTracker creates a tracker with the given capacity.
func NewTracker(capacity int) *Tracker {
	return &Tracker{
		capacity:  capacity,
		events:    make([]*RequestEvent, capacity),
		listeners: make(map[chan *RequestEvent]struct{}),
	}
}

// NextID generates a monotonically increasing request ID.
func (t *Tracker) NextID() string {
	t.mu.Lock()
	t.counter++
	id := fmt.Sprintf("req_%d", t.counter)
	t.mu.Unlock()
	return id
}

// Record stores an event and broadcasts it to all SSE subscribers.
func (t *Tracker) Record(ev *RequestEvent) {
	t.mu.Lock()
	t.events[t.head] = ev
	t.head = (t.head + 1) % t.capacity
	if t.count < t.capacity {
		t.count++
	}

	// Broadcast to all listeners (non-blocking)
	for ch := range t.listeners {
		select {
		case ch <- ev:
		default:
		}
	}
	t.mu.Unlock()
}

// Recent returns the most recent n events (newest first).
func (t *Tracker) Recent(n int) []*RequestEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if n <= 0 || n > t.count {
		n = t.count
	}
	result := make([]*RequestEvent, 0, n)
	for i := 0; i < n; i++ {
		idx := (t.head - 1 - i + t.capacity) % t.capacity
		if t.events[idx] != nil {
			result = append(result, t.events[idx])
		}
	}
	return result
}

// Subscribe returns a channel that receives new events and a cleanup function.
func (t *Tracker) Subscribe(buffer int) (<-chan *RequestEvent, func()) {
	ch := make(chan *RequestEvent, buffer)
	t.mu.Lock()
	t.listeners[ch] = struct{}{}
	t.mu.Unlock()
	return ch, func() {
		t.mu.Lock()
		delete(t.listeners, ch)
		close(ch)
		t.mu.Unlock()
	}
}

// Stats returns summary statistics.
func (t *Tracker) Stats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	totalRequests := t.count
	modelCounts := make(map[string]int)
	statusCounts := make(map[string]int)
	var totalDuration time.Duration

	for i := 0; i < t.count; i++ {
		idx := (t.head - 1 - i + t.capacity) % t.capacity
		ev := t.events[idx]
		if ev == nil {
			continue
		}
		modelCounts[ev.Model]++
		statusCounts[ev.Status]++
		totalDuration += ev.Duration
	}

	stats := map[string]interface{}{
		"total_requests": totalRequests,
		"capacity":       t.capacity,
		"models":         modelCounts,
		"status":         statusCounts,
	}
	if totalRequests > 0 {
		stats["avg_duration_ms"] = float64(totalDuration.Milliseconds()) / float64(totalRequests)
	}
	return stats
}
