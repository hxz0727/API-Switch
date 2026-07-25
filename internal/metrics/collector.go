package metrics

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Collector collects and exports metrics in Prometheus text format.
type Collector struct {
	mu       sync.RWMutex
	counters map[string]*counter
	gauges   map[string]*gauge
}

type counter struct {
	name   string
	help   string
	labels map[string]float64 // label string -> value
}

type gauge struct {
	name   string
	help   string
	value  float64
	labels map[string]float64
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		counters: make(map[string]*counter),
		gauges:   make(map[string]*gauge),
	}
}

// IncrCounter increments a counter with labels.
func (c *Collector) IncrCounter(name string, labels map[string]string, delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := labelKey(labels)
	if _, exists := c.counters[name]; !exists {
		c.counters[name] = &counter{
			name:   name,
			labels: make(map[string]float64),
		}
	}
	c.counters[name].labels[key] += delta
}

// SetGauge sets a gauge value with labels.
func (c *Collector) SetGauge(name string, labels map[string]string, value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := labelKey(labels)
	if _, exists := c.gauges[name]; !exists {
		c.gauges[name] = &gauge{
			name:   name,
			labels: make(map[string]float64),
		}
	}
	c.gauges[name].labels[key] = value
}

// SetGaugeHelp sets the help text for a gauge.
func (c *Collector) SetGaugeHelp(name string, help string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if g, ok := c.gauges[name]; ok {
		g.help = help
	} else {
		c.gauges[name] = &gauge{name: name, help: help, labels: make(map[string]float64)}
	}
}

// SetCounterHelp sets the help text for a counter.
func (c *Collector) SetCounterHelp(name string, help string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ct, ok := c.counters[name]; ok {
		ct.help = help
	} else {
		c.counters[name] = &counter{name: name, help: help, labels: make(map[string]float64)}
	}
}

// Export exports all metrics in Prometheus text format.
func (c *Collector) Export() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder

	// Export counters
	for _, ct := range c.counters {
		if ct.help != "" {
			sb.WriteString(fmt.Sprintf("# HELP %s %s\n", ct.name, ct.help))
		}
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n", ct.name))
		for labels, value := range ct.labels {
			if labels == "" {
				sb.WriteString(fmt.Sprintf("%s %g\n", ct.name, value))
			} else {
				sb.WriteString(fmt.Sprintf("%s{%s} %g\n", ct.name, labels, value))
			}
		}
	}

	// Export gauges
	for _, g := range c.gauges {
		if g.help != "" {
			sb.WriteString(fmt.Sprintf("# HELP %s %s\n", g.name, g.help))
		}
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", g.name))
		if len(g.labels) == 0 {
			sb.WriteString(fmt.Sprintf("%s %g\n", g.name, g.value))
		} else {
			for labels, value := range g.labels {
				if labels == "" {
					sb.WriteString(fmt.Sprintf("%s %g\n", g.name, value))
				} else {
					sb.WriteString(fmt.Sprintf("%s{%s} %g\n", g.name, labels, value))
				}
			}
		}
	}

	return sb.String()
}

// labelKey creates a sorted label string for use as a map key.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	// Simple join - not sorted but consistent within a single call
	return strings.Join(parts, ",")
}

// RequestMetrics holds metrics for a single request.
type RequestMetrics struct {
	Provider     string
	Model        string
	Status       string
	Duration     time.Duration
	InputTokens  int
	OutputTokens int
}

// MetricsTracker integrates with the monitor tracker to collect Prometheus metrics.
type MetricsTracker struct {
	collector *Collector
	startTime time.Time
}

// NewMetricsTracker creates a new metrics tracker.
func NewMetricsTracker() *MetricsTracker {
	return &MetricsTracker{
		collector: NewCollector(),
		startTime: time.Now(),
	}
}

// GetCollector returns the underlying collector.
func (mt *MetricsTracker) GetCollector() *Collector {
	return mt.collector
}

// RecordRequest records metrics for a completed request.
func (mt *MetricsTracker) RecordRequest(m *RequestMetrics) {
	labels := map[string]string{
		"provider": m.Provider,
		"model":    m.Model,
		"status":   m.Status,
	}

	mt.collector.IncrCounter("api_switch_requests_total", labels, 1)
	mt.collector.IncrCounter("api_switch_request_duration_seconds_sum", labels, m.Duration.Seconds())

	tokenLabels := map[string]string{
		"provider": m.Provider,
		"direction": "input",
	}
	mt.collector.IncrCounter("api_switch_tokens_total", tokenLabels, float64(m.InputTokens))

	tokenLabels["direction"] = "output"
	mt.collector.IncrCounter("api_switch_tokens_total", tokenLabels, float64(m.OutputTokens))
}

// UpdateProviderHealth updates the provider health gauge.
func (mt *MetricsTracker) UpdateProviderHealth(provider string, healthy bool) {
	labels := map[string]string{"provider": provider}
	value := float64(0)
	if healthy {
		value = 1
	}
	mt.collector.SetGauge("api_switch_provider_healthy", labels, value)
}

// UpdateCircuitBreakerState updates the circuit breaker state gauge.
func (mt *MetricsTracker) UpdateCircuitBreakerState(provider string, state string) {
	labels := map[string]string{"provider": provider, "state": state}
	mt.collector.SetGauge("api_switch_circuit_breaker_state", labels, 1)

	// Reset other states
	for _, s := range []string{"closed", "open", "half_open"} {
		if s != state {
			otherLabels := map[string]string{"provider": provider, "state": s}
			mt.collector.SetGauge("api_switch_circuit_breaker_state", otherLabels, 0)
		}
	}
}

// UpdateActiveRequests updates the active requests gauge.
func (mt *MetricsTracker) UpdateActiveRequests(count int) {
	mt.collector.SetGauge("api_switch_active_requests", nil, float64(count))
}

// UpdateUptime updates the uptime gauge.
func (mt *MetricsTracker) UpdateUptime() {
	mt.collector.SetGauge("api_switch_uptime_seconds", nil, time.Since(mt.startTime).Seconds())
}
