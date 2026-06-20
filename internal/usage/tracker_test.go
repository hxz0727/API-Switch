package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTracker(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tr.path != path {
		t.Errorf("expected path %q, got %q", path, tr.path)
	}

	// Verify directory was created
	_, err = os.Stat(tmpDir)
	if err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestRecord(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 50, false)
	tr.Record(200, 100, false)

	snap := tr.Snapshot()
	if snap.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 300 {
		t.Errorf("expected 300 input tokens, got %d", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 150 {
		t.Errorf("expected 150 output tokens, got %d", snap.TotalOutputTokens)
	}
}

func TestRecordWithError(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 0, true)

	snap := tr.Snapshot()
	if snap.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", snap.TotalErrors)
	}
}

func TestRecordWithCache(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.RecordWithCache(500, 200, 300, false)

	snap := tr.Snapshot()
	if snap.TotalCacheHits != 1 {
		t.Errorf("expected 1 cache hit, got %d", snap.TotalCacheHits)
	}
	if snap.TotalCacheReadTokens != 300 {
		t.Errorf("expected 300 cache read tokens, got %d", snap.TotalCacheReadTokens)
	}
	if snap.TotalInputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 200 {
		t.Errorf("expected 200 output tokens, got %d", snap.TotalOutputTokens)
	}
}

func TestDailyBreakdown(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 50, false)
	tr.Record(200, 100, false)

	snap := tr.Snapshot()
	if len(snap.Daily) != 1 {
		t.Fatalf("expected 1 daily entry, got %d", len(snap.Daily))
	}

	// Find today's entry
	today := ""
	for date := range snap.Daily {
		today = date
		break
	}

	daily := snap.Daily[today]
	if daily.Requests != 2 {
		t.Errorf("expected 2 daily requests, got %d", daily.Requests)
	}
	if daily.InputTokens != 300 {
		t.Errorf("expected 300 daily input tokens, got %d", daily.InputTokens)
	}
	if daily.OutputTokens != 150 {
		t.Errorf("expected 150 daily output tokens, got %d", daily.OutputTokens)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 50, false)
	tr.RecordWithCache(200, 100, 150, false)

	if err := tr.Save(); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Load a new tracker from the same file
	tr2, err := NewTracker(path)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	snap := tr2.Snapshot()
	if snap.TotalRequests != 2 {
		t.Errorf("expected 2 total requests after load, got %d", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 300 {
		t.Errorf("expected 300 input tokens after load, got %d", snap.TotalInputTokens)
	}
	if snap.TotalCacheHits != 1 {
		t.Errorf("expected 1 cache hit after load, got %d", snap.TotalCacheHits)
	}
}

func TestReset(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 50, false)
	tr.Record(200, 100, false)

	if err := tr.Reset(); err != nil {
		t.Fatalf("failed to reset: %v", err)
	}

	snap := tr.Snapshot()
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 requests after reset, got %d", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 0 {
		t.Errorf("expected 0 input tokens after reset, got %d", snap.TotalInputTokens)
	}
}

func TestSnapshot_IsCopy(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr.Record(100, 50, false)

	snap := tr.Snapshot()

	// Modify the snapshot — should not affect original
	for _, daily := range snap.Daily {
		daily.InputTokens = 999
	}

	// Get fresh snapshot
	snap2 := tr.Snapshot()
	for _, daily := range snap2.Daily {
		if daily.InputTokens == 999 {
			t.Error("snapshot modification leaked to original data")
		}
	}
}

func TestRecord_NoError(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "usage.json")

	tr, err := NewTracker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RecordWithCache with 0 cache tokens should not count as cache hit
	tr.RecordWithCache(100, 50, 0, false)

	snap := tr.Snapshot()
	if snap.TotalCacheHits != 0 {
		t.Errorf("expected 0 cache hits for 0 cache tokens, got %d", snap.TotalCacheHits)
	}
}
