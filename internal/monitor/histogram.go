package monitor

import (
	"math"
	"sort"
	"sync"
)

// Histogram is a lightweight HDR-style histogram for latency tracking.
// It uses logarithmic buckets to cover a wide range of values efficiently.
type Histogram struct {
	mu      sync.Mutex
	counts  []int64   // bucket counts
	bounds  []float64 // upper bound of each bucket
	total   int64     // total number of observations
	sum     float64   // sum of all observations
	min     float64
	max     float64
}

// NewHistogram creates a histogram with logarithmic buckets from minVal to maxVal.
// Default: 0.1ms to 60s with ~50 buckets.
func NewHistogram() *Histogram {
	// Create logarithmic buckets from 0.1ms to 60s
	// Bucket boundaries: 0.1ms, 0.2ms, 0.5ms, 1ms, 2ms, 5ms, 10ms, 20ms, 50ms,
	// 100ms, 200ms, 500ms, 1s, 2s, 5s, 10s, 20s, 30s, 60s
	bounds := []float64{
		0.0001, 0.0002, 0.0005, // 0.1ms, 0.2ms, 0.5ms
		0.001, 0.002, 0.005, // 1ms, 2ms, 5ms
		0.01, 0.02, 0.05, // 10ms, 20ms, 50ms
		0.1, 0.2, 0.5, // 100ms, 200ms, 500ms
		1.0, 2.0, 5.0, // 1s, 2s, 5s
		10.0, 15.0, 20.0, 30.0, 45.0, 60.0, // 10s, 15s, 20s, 30s, 45s, 60s
	}

	return &Histogram{
		counts: make([]int64, len(bounds)+1), // +1 for overflow bucket
		bounds: bounds,
		min:    math.MaxFloat64,
		max:    -math.MaxFloat64,
	}
}

// Record adds an observation value (in seconds) to the histogram.
func (h *Histogram) Record(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.total++
	h.sum += value
	if value < h.min {
		h.min = value
	}
	if value > h.max {
		h.max = value
	}

	// Find the appropriate bucket
	idx := sort.SearchFloat64s(h.bounds, value)
	if idx < len(h.bounds) {
		h.counts[idx]++
	} else {
		// Overflow bucket
		h.counts[len(h.bounds)]++
	}
}

// Percentile returns the value at the given percentile (0-100).
func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.total == 0 {
		return 0
	}

	target := int64(math.Ceil(float64(h.total) * p / 100.0))
	var cumulative int64

	for i, count := range h.counts {
		cumulative += count
		if cumulative >= target {
			if i < len(h.bounds) {
				return h.bounds[i]
			}
			return h.max // overflow bucket
		}
	}

	return h.max
}

// Count returns the total number of observations.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// Mean returns the average value.
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total == 0 {
		return 0
	}
	return h.sum / float64(h.total)
}

// Reset clears all observations.
func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counts = make([]int64, len(h.bounds)+1)
	h.total = 0
	h.sum = 0
	h.min = math.MaxFloat64
	h.max = -math.MaxFloat64
}

// Snapshot returns a copy of the histogram stats.
func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HistogramSnapshot{
		Count:  h.total,
		Mean:   h.mean(),
		P50:    h.percentileLocked(50),
		P95:    h.percentileLocked(95),
		P99:    h.percentileLocked(99),
		Min:    h.min,
		Max:    h.max,
	}
}

func (h *Histogram) mean() float64 {
	if h.total == 0 {
		return 0
	}
	return h.sum / float64(h.total)
}

func (h *Histogram) percentileLocked(p float64) float64 {
	if h.total == 0 {
		return 0
	}
	target := int64(math.Ceil(float64(h.total) * p / 100.0))
	var cumulative int64
	for i, count := range h.counts {
		cumulative += count
		if cumulative >= target {
			if i < len(h.bounds) {
				return h.bounds[i]
			}
			return h.max
		}
	}
	return h.max
}

// HistogramSnapshot is a point-in-time snapshot of histogram stats.
type HistogramSnapshot struct {
	Count int64   `json:"count"`
	Mean  float64 `json:"mean_ms"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	Min   float64 `json:"min_ms"`
	Max   float64 `json:"max_ms"`
}
