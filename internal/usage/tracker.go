package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DailyUsage records token usage for a single day.
type DailyUsage struct {
	Date        string `json:"date"`
	InputTokens int64  `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Requests    int64  `json:"requests"`
	Errors      int64  `json:"errors"`
}

// Summary holds aggregated statistics.
type Summary struct {
	TotalInputTokens  int64            `json:"total_input_tokens"`
	TotalOutputTokens int64            `json:"total_output_tokens"`
	TotalRequests     int64            `json:"total_requests"`
	TotalErrors       int64            `json:"total_errors"`
	Daily             map[string]*DailyUsage `json:"daily"`
}

// Tracker tracks token usage with file-based persistence.
type Tracker struct {
	mu   sync.Mutex
	path string
	data Summary
}

// NewTracker creates or loads a usage tracker from the given file path.
func NewTracker(path string) (*Tracker, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create usage directory: %w", err)
	}

	t := &Tracker{
		path: path,
		data: Summary{
			Daily: make(map[string]*DailyUsage),
		},
	}

	// Try to load existing data
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &t.data); err == nil {
			// Ensure Daily map is initialized
			if t.data.Daily == nil {
				t.data.Daily = make(map[string]*DailyUsage)
			}
		}
	}

	return t, nil
}

// Record adds a request's token usage.
func (t *Tracker) Record(inputTokens, outputTokens int, isError bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	date := time.Now().Format("2006-01-02")
	daily, ok := t.data.Daily[date]
	if !ok {
		daily = &DailyUsage{Date: date}
		t.data.Daily[date] = daily
	}

	daily.Requests++
	daily.InputTokens += int64(inputTokens)
	daily.OutputTokens += int64(outputTokens)
	if isError {
		daily.Errors++
	}

	t.data.TotalRequests++
	t.data.TotalInputTokens += int64(inputTokens)
	t.data.TotalOutputTokens += int64(outputTokens)
	if isError {
		t.data.TotalErrors++
	}
}

// Snapshot returns a copy of the current usage summary.
func (t *Tracker) Snapshot() Summary {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.data
	s.Daily = make(map[string]*DailyUsage)
	for k, v := range t.data.Daily {
		cp := *v
		s.Daily[k] = &cp
	}
	return s
}

// Save persists the current state to disk.
func (t *Tracker) Save() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal usage data: %w", err)
	}

	if err := os.WriteFile(t.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write usage data: %w", err)
	}

	return nil
}

// Reset clears all usage data and saves.
func (t *Tracker) Reset() error {
	t.mu.Lock()
	t.data = Summary{Daily: make(map[string]*DailyUsage)}
	t.mu.Unlock()
	return t.Save()
}
