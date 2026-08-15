package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCollector_IncrCounter_NoLabels(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", nil, 1)
	c.IncrCounter("requests_total", nil, 2)

	out := c.Export()
	if !strings.Contains(out, "requests_total 3\n") {
		t.Fatalf("expected counter value 3, got:\n%s", out)
	}
}

func TestCollector_IncrCounter_WithLabels(t *testing.T) {
	c := NewCollector()
	labels := map[string]string{"provider": "anthropic", "model": "claude-3"}
	c.IncrCounter("requests_total", labels, 1)

	out := c.Export()
	// labelKey joins with "," without sorting; order is non-deterministic for
	// map iteration, so verify each label pair and the value independently.
	line := findMetricLine(t, out, "requests_total")
	if line == "" {
		t.Fatalf("expected labeled counter in export, got:\n%s", out)
	}
	assertLabelPairs(t, line, map[string]string{"provider": "anthropic", "model": "claude-3"})
	if got := lineValue(line); got != "1" {
		t.Fatalf("expected counter value 1, got %q (line %q)", got, line)
	}
}

func TestCollector_IncrCounter_SameLabelSetAccumulates(t *testing.T) {
	// Single-key labels keep labelKey deterministic across calls.
	c := NewCollector()
	c.IncrCounter("requests_total", map[string]string{"provider": "anthropic"}, 1)
	c.IncrCounter("requests_total", map[string]string{"provider": "anthropic"}, 2)

	out := c.Export()
	assertMetricLine(t, out, "requests_total", map[string]string{"provider": "anthropic"}, "3")
}

func TestCollector_IncrCounter_SeparateLabelSets(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", map[string]string{"provider": "a"}, 1)
	c.IncrCounter("requests_total", map[string]string{"provider": "b"}, 2)

	out := c.Export()
	if !strings.Contains(out, `requests_total{provider="a"} 1`) {
		t.Fatalf("expected provider=a counter, got:\n%s", out)
	}
	if !strings.Contains(out, `requests_total{provider="b"} 2`) {
		t.Fatalf("expected provider=b counter, got:\n%s", out)
	}
}

func TestCollector_SetGauge(t *testing.T) {
	c := NewCollector()
	c.SetGauge("temp", nil, 21.5)
	c.SetGauge("temp", nil, 22.5) // overwrite

	out := c.Export()
	if !strings.Contains(out, "temp 22.5\n") {
		t.Fatalf("expected gauge value 22.5 (overwritten), got:\n%s", out)
	}
}

func TestCollector_SetGauge_Decrement(t *testing.T) {
	// Gauges can be set to a lower value (the decrement equivalent).
	c := NewCollector()
	c.SetGauge("active", nil, 10)
	c.SetGauge("active", nil, 4)

	out := c.Export()
	if !strings.Contains(out, "active 4\n") {
		t.Fatalf("expected gauge lowered to 4, got:\n%s", out)
	}
}

func TestCollector_SetGauge_WithLabels(t *testing.T) {
	c := NewCollector()
	c.SetGauge("provider_healthy", map[string]string{"provider": "deepseek"}, 1)

	out := c.Export()
	if !strings.Contains(out, `provider_healthy{provider="deepseek"} 1`) {
		t.Fatalf("expected labeled gauge, got:\n%s", out)
	}
}

func TestCollector_SetGaugeHelp(t *testing.T) {
	c := NewCollector()
	c.SetGauge("uptime", nil, 5)
	c.SetGaugeHelp("uptime", "seconds since start")

	out := c.Export()
	if !strings.Contains(out, "# HELP uptime seconds since start\n") {
		t.Fatalf("expected HELP line for gauge, got:\n%s", out)
	}
}

func TestCollector_SetGaugeHelp_BeforeValue(t *testing.T) {
	c := NewCollector()
	c.SetGaugeHelp("uptime", "seconds since start")
	c.SetGauge("uptime", nil, 5)

	out := c.Export()
	if !strings.Contains(out, "# HELP uptime seconds since start\n") {
		t.Fatalf("expected HELP line for gauge created via SetGaugeHelp, got:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE uptime gauge\n") {
		t.Fatalf("expected TYPE line for gauge, got:\n%s", out)
	}
}

func TestCollector_SetCounterHelp(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", nil, 1)
	c.SetCounterHelp("requests_total", "total number of requests")

	out := c.Export()
	if !strings.Contains(out, "# HELP requests_total total number of requests\n") {
		t.Fatalf("expected HELP line for counter, got:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE requests_total counter\n") {
		t.Fatalf("expected TYPE line for counter, got:\n%s", out)
	}
}

func TestCollector_SetCounterHelp_BeforeValue(t *testing.T) {
	c := NewCollector()
	c.SetCounterHelp("requests_total", "total number of requests")
	c.IncrCounter("requests_total", nil, 1)

	out := c.Export()
	if !strings.Contains(out, "# HELP requests_total total number of requests\n") {
		t.Fatalf("expected HELP line for counter created via SetCounterHelp, got:\n%s", out)
	}
}

func TestCollector_Export_PrometheusFormat(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", nil, 3)
	c.SetCounterHelp("requests_total", "total requests")
	c.SetGauge("uptime_seconds", nil, 42)
	c.SetGaugeHelp("uptime_seconds", "uptime in seconds")

	out := c.Export()

	// Counter: help, type, value
	if !strings.Contains(out, "# HELP requests_total total requests\n") {
		t.Errorf("missing counter HELP line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE requests_total counter\n") {
		t.Errorf("missing counter TYPE line:\n%s", out)
	}
	if !strings.Contains(out, "requests_total 3\n") {
		t.Errorf("missing counter value line:\n%s", out)
	}

	// Gauge: help, type, value
	if !strings.Contains(out, "# HELP uptime_seconds uptime in seconds\n") {
		t.Errorf("missing gauge HELP line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE uptime_seconds gauge\n") {
		t.Errorf("missing gauge TYPE line:\n%s", out)
	}
	if !strings.Contains(out, "uptime_seconds 42\n") {
		t.Errorf("missing gauge value line:\n%s", out)
	}
}

func TestCollector_Export_Empty(t *testing.T) {
	c := NewCollector()
	if out := c.Export(); out != "" {
		t.Fatalf("expected empty export, got:\n%q", out)
	}
}

func TestCollector_Export_PersistsAcrossExports(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", nil, 2)
	if out := c.Export(); !strings.Contains(out, "requests_total 2\n") {
		t.Fatalf("first export should contain 2, got:\n%s", out)
	}

	// Export again without modification - value must persist.
	if out := c.Export(); !strings.Contains(out, "requests_total 2\n") {
		t.Fatalf("second export should still contain 2, got:\n%s", out)
	}

	// Further increments accumulate on top of the persisted value.
	c.IncrCounter("requests_total", nil, 5)
	if out := c.Export(); !strings.Contains(out, "requests_total 7\n") {
		t.Fatalf("expected accumulated value 7, got:\n%s", out)
	}
}

func TestCollector_ConcurrentIncrements(t *testing.T) {
	c := NewCollector()
	const goroutines = 32
	const perGoroutine = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				c.IncrCounter("requests_total", nil, 1)
				c.IncrCounter("requests_total", map[string]string{"provider": "deepseek"}, 1)
				c.SetGauge("active_requests", nil, float64(i))
			}
		}()
	}
	wg.Wait()

	out := c.Export()
	expected := goroutines * perGoroutine
	want := "requests_total " + itoa(expected) + "\n"
	if !strings.Contains(out, want) {
		t.Fatalf("expected unlabeled counter to be %d, got:\n%s", expected, out)
	}
	wantLabeled := `requests_total{provider="deepseek"} ` + itoa(expected) + "\n"
	if !strings.Contains(out, wantLabeled) {
		t.Fatalf("expected labeled counter to be %d, got:\n%s", expected, out)
	}
}

func TestCollector_ConcurrentReadsAndWrites(t *testing.T) {
	c := NewCollector()
	c.IncrCounter("requests_total", nil, 1)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				c.IncrCounter("requests_total", nil, 1)
				_ = c.Export()
			}
		}()
	}
	wg.Wait()

	if out := c.Export(); !strings.Contains(out, "requests_total 801\n") {
		t.Fatalf("expected 801 total increments, got:\n%s", out)
	}
}

func TestMetricsTracker_RecordRequest(t *testing.T) {
	mt := NewMetricsTracker()
	mt.RecordRequest(&RequestMetrics{
		Provider:     "deepseek",
		Model:        "deepseek-chat",
		Status:       "success",
		Duration:     time.Second,
		InputTokens:  100,
		OutputTokens: 50,
	})

	out := mt.GetCollector().Export()
	assertMetricLine(t, out, "api_switch_requests_total", map[string]string{
		"provider": "deepseek", "model": "deepseek-chat", "status": "success",
	}, "1")
	assertMetricLine(t, out, "api_switch_request_duration_seconds_sum", map[string]string{
		"provider": "deepseek", "model": "deepseek-chat", "status": "success",
	}, "1")
	assertMetricLine(t, out, "api_switch_tokens_total", map[string]string{
		"provider": "deepseek", "direction": "input",
	}, "100")
	assertMetricLine(t, out, "api_switch_tokens_total", map[string]string{
		"provider": "deepseek", "direction": "output",
	}, "50")
}

func TestMetricsTracker_UpdateProviderHealth(t *testing.T) {
	mt := NewMetricsTracker()
	mt.UpdateProviderHealth("deepseek", true)
	mt.UpdateProviderHealth("anthropic", false)

	out := mt.GetCollector().Export()
	if !strings.Contains(out, `api_switch_provider_healthy{provider="deepseek"} 1`) {
		t.Errorf("expected healthy provider gauge = 1, got:\n%s", out)
	}
	if !strings.Contains(out, `api_switch_provider_healthy{provider="anthropic"} 0`) {
		t.Errorf("expected unhealthy provider gauge = 0, got:\n%s", out)
	}
}

func TestMetricsTracker_UpdateCircuitBreakerState(t *testing.T) {
	mt := NewMetricsTracker()
	mt.UpdateCircuitBreakerState("deepseek", "open")

	out := mt.GetCollector().Export()
	// Within a single call each state gets exactly one gauge line: the current
	// state is set to 1, all others to 0.
	assertMetricLine(t, out, "api_switch_circuit_breaker_state", map[string]string{
		"provider": "deepseek", "state": "open",
	}, "1")
	assertMetricLine(t, out, "api_switch_circuit_breaker_state", map[string]string{
		"provider": "deepseek", "state": "closed",
	}, "0")
	assertMetricLine(t, out, "api_switch_circuit_breaker_state", map[string]string{
		"provider": "deepseek", "state": "half_open",
	}, "0")

	// Transitioning sets the new state to 1.
	mt.UpdateCircuitBreakerState("deepseek", "closed")
	out = mt.GetCollector().Export()
	assertMetricLine(t, out, "api_switch_circuit_breaker_state", map[string]string{
		"provider": "deepseek", "state": "closed",
	}, "1")
}

func TestMetricsTracker_UpdateActiveRequests(t *testing.T) {
	mt := NewMetricsTracker()
	mt.UpdateActiveRequests(7)

	out := mt.GetCollector().Export()
	if !strings.Contains(out, "api_switch_active_requests 7\n") {
		t.Fatalf("expected active requests gauge = 7, got:\n%s", out)
	}
}

func TestMetricsTracker_UpdateUptime(t *testing.T) {
	mt := NewMetricsTracker()
	mt.UpdateUptime()

	out := mt.GetCollector().Export()
	if !strings.Contains(out, "api_switch_uptime_seconds ") {
		t.Fatalf("expected uptime gauge in export, got:\n%s", out)
	}
}

func TestMetricsTracker_GetCollector(t *testing.T) {
	mt := NewMetricsTracker()
	if mt.GetCollector() == nil {
		t.Fatal("expected non-nil collector from GetCollector")
	}
}

// itoa converts a small positive integer to a decimal string for expected
// Prometheus output (which formats numbers with %g).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// findMetricLine returns the first exported value line for the given metric
// name (e.g. "requests_total{...} 3\n" → "requests_total{...} 3").
func findMetricLine(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if line == name || strings.HasPrefix(line, name+"{") || strings.HasPrefix(line, name+" ") {
			return line
		}
	}
	return ""
}

// lineValue returns the trailing numeric value of an exported metric line.
func lineValue(line string) string {
	idx := strings.LastIndex(line, " ")
	if idx < 0 {
		return ""
	}
	return line[idx+1:]
}

// assertLabelPairs verifies that a metric line contains every expected
// key="value" pair (order-independent).
func assertLabelPairs(t *testing.T, line string, labels map[string]string) {
	t.Helper()
	for k, v := range labels {
		want := k + `="` + v + `"`
		if !strings.Contains(line, want) {
			t.Errorf("expected metric line to contain label %q, got: %q", want, line)
		}
	}
}

// assertMetricLine finds the metric line for name in the export that carries
// all the expected label pairs and verifies its value. When a metric name has
// multiple label sets, the line containing every expected pair is selected.
func assertMetricLine(t *testing.T, out, name string, labels map[string]string, value string) {
	t.Helper()
	var line string
	for _, l := range strings.Split(out, "\n") {
		if l != name && !strings.HasPrefix(l, name+"{") && !strings.HasPrefix(l, name+" ") {
			continue
		}
		allMatch := true
		for k, v := range labels {
			if !strings.Contains(l, k+`="`+v+`"`) {
				allMatch = false
				break
			}
		}
		if allMatch {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("expected metric %q with labels %v in export, got:\n%s", name, labels, out)
	}
	if got := lineValue(line); got != value {
		t.Errorf("expected metric %q value %s, got %q (line %q)", name, value, got, line)
	}
}
