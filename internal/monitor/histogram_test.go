package monitor

import (
	"testing"
)

func TestHistogram_BasicOperations(t *testing.T) {
	h := NewHistogram()

	// Empty histogram
	if h.Count() != 0 {
		t.Fatalf("expected count 0, got %d", h.Count())
	}
	if h.Mean() != 0 {
		t.Fatalf("expected mean 0, got %f", h.Mean())
	}

	// Record some values
	h.Record(0.001) // 1ms
	h.Record(0.005) // 5ms
	h.Record(0.010) // 10ms
	h.Record(0.050) // 50ms
	h.Record(0.100) // 100ms

	if h.Count() != 5 {
		t.Fatalf("expected count 5, got %d", h.Count())
	}

	snap := h.Snapshot()
	if snap.Count != 5 {
		t.Fatalf("expected snapshot count 5, got %d", snap.Count)
	}
	if snap.P50 <= 0 {
		t.Fatalf("expected P50 > 0, got %f", snap.P50)
	}
	if snap.P99 <= 0 {
		t.Fatalf("expected P99 > 0, got %f", snap.P99)
	}
}

func TestHistogram_Percentiles(t *testing.T) {
	h := NewHistogram()

	// Record 100 values: 0.001, 0.002, ..., 0.100
	for i := 1; i <= 100; i++ {
		h.Record(float64(i) * 0.001)
	}

	snap := h.Snapshot()
	if snap.P50 <= 0 {
		t.Errorf("expected P50 > 0, got %f", snap.P50)
	}
	if snap.P95 < snap.P50 {
		t.Errorf("expected P95 >= P50, got P95=%f, P50=%f", snap.P95, snap.P50)
	}
	if snap.P99 < snap.P95 {
		t.Errorf("expected P99 >= P95, got P99=%f, P95=%f", snap.P99, snap.P95)
	}
}

func TestHistogram_Reset(t *testing.T) {
	h := NewHistogram()
	h.Record(0.001)
	h.Record(0.002)

	if h.Count() != 2 {
		t.Fatalf("expected count 2, got %d", h.Count())
	}

	h.Reset()

	if h.Count() != 0 {
		t.Fatalf("expected count 0 after reset, got %d", h.Count())
	}
}

func TestHistogram_Snapshot(t *testing.T) {
	h := NewHistogram()
	h.Record(0.001)
	h.Record(0.002)
	h.Record(0.003)

	snap := h.Snapshot()
	if snap.Count != 3 {
		t.Errorf("expected count 3, got %d", snap.Count)
	}
	if snap.Mean <= 0 {
		t.Errorf("expected mean > 0, got %f", snap.Mean)
	}
}
