package monitor

import (
	"fmt"
	"sync"
	"time"
)

// RequestEvent represents a single proxied request.
type RequestEvent struct {
	ID              string        `json:"id"`
	Timestamp       time.Time     `json:"timestamp"`
	Model           string        `json:"model"`
	Provider        string        `json:"provider"`
	ProviderType    string        `json:"provider_type"`
	Stream          bool          `json:"stream"`
	Duration        time.Duration `json:"duration"`
	Status          string        `json:"status"` // "ok", "error", "cancelled"
	InputTokens     int           `json:"input_tokens,omitempty"`
	OutputTokens    int           `json:"output_tokens,omitempty"`
	CacheReadTokens int           `json:"cache_read_tokens,omitempty"` // tokens served from prompt cache
	Error           string        `json:"error,omitempty"`
}

// ProviderStats holds per-provider statistics.
type ProviderStats struct {
	RequestCount  int64         `json:"request_count"`
	ErrorCount    int64         `json:"error_count"`
	TotalDuration time.Duration `json:"total_duration"`
	InputTokens   int64         `json:"input_tokens"`
	OutputTokens  int64         `json:"output_tokens"`
	LatencyHist   *Histogram    `json:"-"`
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

	// Enhanced metrics
	providerStats map[string]*ProviderStats
	globalHist    *Histogram
	recentErrors  []string // last N error messages
	maxErrors     int
}

// NewTracker creates a tracker with the given capacity.
func NewTracker(capacity int) *Tracker {
	return &Tracker{
		capacity:      capacity,
		events:        make([]*RequestEvent, capacity),
		listeners:     make(map[chan *RequestEvent]struct{}),
		providerStats: make(map[string]*ProviderStats),
		globalHist:    NewHistogram(),
		maxErrors:     100,
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

	// Update per-provider stats
	if ev.Provider != "" {
		ps, exists := t.providerStats[ev.Provider]
		if !exists {
			ps = &ProviderStats{LatencyHist: NewHistogram()}
			t.providerStats[ev.Provider] = ps
		}
		ps.RequestCount++
		ps.TotalDuration += ev.Duration
		ps.InputTokens += int64(ev.InputTokens)
		ps.OutputTokens += int64(ev.OutputTokens)
		ps.LatencyHist.Record(ev.Duration.Seconds())
		if ev.Status == "error" {
			ps.ErrorCount++
		}
	}

	// Update global histogram
	t.globalHist.Record(ev.Duration.Seconds())

	// Track recent errors
	if ev.Status == "error" && ev.Error != "" {
		t.recentErrors = append(t.recentErrors, ev.Error)
		if len(t.recentErrors) > t.maxErrors {
			t.recentErrors = t.recentErrors[1:]
		}
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

	// Global latency percentiles
	globalSnap := t.globalHist.Snapshot()

	stats := map[string]interface{}{
		"total_requests": totalRequests,
		"lifetime":       t.counter,
		"capacity":       t.capacity,
		"models":         modelCounts,
		"status":         statusCounts,
		"latency": map[string]interface{}{
			"avg_ms": globalSnap.Mean * 1000,
			"p50_ms": globalSnap.P50 * 1000,
			"p95_ms": globalSnap.P95 * 1000,
			"p99_ms": globalSnap.P99 * 1000,
			"min_ms": globalSnap.Min * 1000,
			"max_ms": globalSnap.Max * 1000,
		},
	}

	// Keep backward-compatible avg_duration_ms at top level
	if totalRequests > 0 {
		stats["avg_duration_ms"] = globalSnap.Mean * 1000
	}

	// Per-provider stats
	providers := make(map[string]interface{}, len(t.providerStats))
	for name, ps := range t.providerStats {
		snap := ps.LatencyHist.Snapshot()
		errorRate := float64(0)
		if ps.RequestCount > 0 {
			errorRate = float64(ps.ErrorCount) / float64(ps.RequestCount) * 100
		}
		providers[name] = map[string]interface{}{
			"requests":    ps.RequestCount,
			"errors":      ps.ErrorCount,
			"error_rate":  errorRate,
			"input_tokens":  ps.InputTokens,
			"output_tokens": ps.OutputTokens,
			"latency": map[string]interface{}{
				"avg_ms": snap.Mean * 1000,
				"p50_ms": snap.P50 * 1000,
				"p95_ms": snap.P95 * 1000,
				"p99_ms": snap.P99 * 1000,
			},
		}
	}
	stats["providers"] = providers

	// Error rate
	errorRate := float64(0)
	if totalRequests > 0 {
		errorRate = float64(statusCounts["error"]) / float64(totalRequests) * 100
	}
	stats["error_rate"] = errorRate

	// Recent errors
	if len(t.recentErrors) > 0 {
		// Return last 10 errors
		start := 0
		if len(t.recentErrors) > 10 {
			start = len(t.recentErrors) - 10
		}
		stats["recent_errors"] = t.recentErrors[start:]
	}

	return stats
}

// GetProviderStats returns stats for a specific provider.
func (t *Tracker) GetProviderStats(providerName string) *ProviderStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if ps, ok := t.providerStats[providerName]; ok {
		return &ProviderStats{
			RequestCount:  ps.RequestCount,
			ErrorCount:    ps.ErrorCount,
			TotalDuration: ps.TotalDuration,
			InputTokens:   ps.InputTokens,
			OutputTokens:  ps.OutputTokens,
		}
	}
	return nil
}
