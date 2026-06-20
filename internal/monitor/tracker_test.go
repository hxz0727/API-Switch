package monitor

import (
	"sync"
	"testing"
	"time"
)

func TestNewTracker(t *testing.T) {
	tr := NewTracker(100)
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tr.capacity != 100 {
		t.Errorf("expected capacity 100, got %d", tr.capacity)
	}
	if len(tr.listeners) != 0 {
		t.Errorf("expected 0 listeners, got %d", len(tr.listeners))
	}
}

func TestNextID(t *testing.T) {
	tr := NewTracker(10)

	id1 := tr.NextID()
	id2 := tr.NextID()
	id3 := tr.NextID()

	if id1 != "req_1" {
		t.Errorf("expected 'req_1', got %q", id1)
	}
	if id2 != "req_2" {
		t.Errorf("expected 'req_2', got %q", id2)
	}
	if id3 != "req_3" {
		t.Errorf("expected 'req_3', got %q", id3)
	}
}

func TestRecord(t *testing.T) {
	tr := NewTracker(10)

	ev := &RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "gpt-4o",
		Provider:  "openai",
		Status:    "ok",
	}

	tr.Record(ev)

	recent := tr.Recent(1)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent event, got %d", len(recent))
	}
	if recent[0].Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", recent[0].Model)
	}
}

func TestRecent(t *testing.T) {
	tr := NewTracker(10)

	for i := 0; i < 5; i++ {
		tr.Record(&RequestEvent{
			ID:        tr.NextID(),
			Timestamp: time.Now(),
			Model:     "gpt-4o",
			Status:    "ok",
		})
	}

	recent := tr.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
}

func TestRecent_MoreThanRecorded(t *testing.T) {
	tr := NewTracker(100)

	tr.Record(&RequestEvent{ID: tr.NextID(), Timestamp: time.Now(), Status: "ok"})

	recent := tr.Recent(10)
	if len(recent) != 1 {
		t.Errorf("expected 1 event, got %d", len(recent))
	}
}

func TestRingBufferWrap(t *testing.T) {
	tr := NewTracker(3)

	// Record 5 events, only last 3 should be retained
	for i := 0; i < 5; i++ {
		tr.Record(&RequestEvent{
			ID:        tr.NextID(),
			Timestamp: time.Now(),
			Model:     "model",
			Status:    "ok",
		})
	}

	recent := tr.Recent(5)
	if len(recent) != 3 {
		t.Errorf("expected 3 events (capacity), got %d", len(recent))
	}
}

func TestSubscribe(t *testing.T) {
	tr := NewTracker(10)

	ch, cleanup := tr.Subscribe(5)
	defer cleanup()

	ev := &RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "gpt-4o",
		Status:    "ok",
	}

	tr.Record(ev)

	select {
	case received := <-ch:
		if received.Model != "gpt-4o" {
			t.Errorf("expected model 'gpt-4o', got %q", received.Model)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event on channel")
	}
}

func TestSubscribe_MultipleListeners(t *testing.T) {
	tr := NewTracker(10)

	ch1, cleanup1 := tr.Subscribe(5)
	defer cleanup1()
	ch2, cleanup2 := tr.Subscribe(5)
	defer cleanup2()

	ev := &RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "gpt-4o",
		Status:    "ok",
	}

	tr.Record(ev)

	// Both listeners should receive the event
	for i, ch := range []<-chan *RequestEvent{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Model != "gpt-4o" {
				t.Errorf("listener %d: expected model 'gpt-4o', got %q", i, received.Model)
			}
		case <-time.After(time.Second):
			t.Errorf("listener %d: timeout waiting for event", i)
		}
	}
}

func TestSubscribe_NonBlocking(t *testing.T) {
	tr := NewTracker(10)

	// Channel with 0 buffer — if subscriber doesn't read, should not block Record
	ch, cleanup := tr.Subscribe(0)
	defer cleanup()

	// Fill the channel
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			tr.Record(&RequestEvent{
				ID:        tr.NextID(),
				Timestamp: time.Now(),
				Status:    "ok",
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Record did not block — good
	case <-time.After(2 * time.Second):
		t.Error("Record blocked despite non-blocking send")
	}

	// Drain channel
	for len(ch) > 0 {
		<-ch
	}
}

func TestSubscribe_Cleanup(t *testing.T) {
	tr := NewTracker(10)

	ch, cleanup := tr.Subscribe(5)
	cleanup()

	// After cleanup, channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after cleanup")
	}
}

func TestStats(t *testing.T) {
	tr := NewTracker(100)

	tr.Record(&RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "gpt-4o",
		Provider:  "openai",
		Status:    "ok",
		Duration:  100 * time.Millisecond,
	})
	tr.Record(&RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "gpt-4o",
		Provider:  "openai",
		Status:    "ok",
		Duration:  200 * time.Millisecond,
	})
	tr.Record(&RequestEvent{
		ID:        tr.NextID(),
		Timestamp: time.Now(),
		Model:     "claude-sonnet",
		Provider:  "anthropic",
		Status:    "error",
		Error:     "timeout",
		Duration:  50 * time.Millisecond,
	})

	stats := tr.Stats()

	if total, ok := stats["total_requests"].(int); !ok || total != 3 {
		t.Errorf("expected total_requests 3, got %v", stats["total_requests"])
	}

	models, ok := stats["models"].(map[string]int)
	if !ok {
		t.Fatal("expected models map")
	}
	if models["gpt-4o"] != 2 {
		t.Errorf("expected 2 gpt-4o requests, got %d", models["gpt-4o"])
	}
	if models["claude-sonnet"] != 1 {
		t.Errorf("expected 1 claude-sonnet request, got %d", models["claude-sonnet"])
	}

	status, ok := stats["status"].(map[string]int)
	if !ok {
		t.Fatal("expected status map")
	}
	if status["ok"] != 2 {
		t.Errorf("expected 2 ok, got %d", status["ok"])
	}
	if status["error"] != 1 {
		t.Errorf("expected 1 error, got %d", status["error"])
	}

	// Check avg duration
	if avg, ok := stats["avg_duration_ms"].(float64); !ok || avg <= 0 {
		t.Errorf("expected positive avg_duration_ms, got %v", avg)
	}
}

func TestStats_Empty(t *testing.T) {
	tr := NewTracker(100)
	stats := tr.Stats()

	if total, ok := stats["total_requests"].(int); !ok || total != 0 {
		t.Errorf("expected total_requests 0, got %v", stats["total_requests"])
	}
	if _, ok := stats["avg_duration_ms"]; ok {
		t.Error("expected no avg_duration_ms for empty tracker")
	}
}

func TestConcurrency(t *testing.T) {
	tr := NewTracker(1000)
	var wg sync.WaitGroup

	// Concurrent records
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				tr.Record(&RequestEvent{
					ID:        tr.NextID(),
					Timestamp: time.Now(),
					Status:    "ok",
				})
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				tr.Recent(10)
				tr.Stats()
			}
		}()
	}

	wg.Wait()

	// After all goroutines, should have exactly 1000 events
	stats := tr.Stats()
	total := stats["total_requests"].(int)
	if total != 1000 {
		t.Errorf("expected 1000 total requests, got %d", total)
	}
}
