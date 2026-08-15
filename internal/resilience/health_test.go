package resilience

import (
	"context"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/provider"
)

func TestNewHealthChecker_Defaults(t *testing.T) {
	hc := NewHealthChecker(30 * time.Second)
	if hc.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %s", hc.interval)
	}
	if len(hc.statuses) != 0 || len(hc.breakers) != 0 {
		t.Errorf("expected empty statuses/breakers maps")
	}
	if hc.stopCh == nil {
		t.Error("expected non-nil stopCh")
	}
}

func TestHealthChecker_RegisterProvider(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	hc.RegisterProvider("deepseek", cb)

	if !hc.IsHealthy("deepseek") {
		t.Error("expected registered provider to start healthy")
	}
	h := hc.GetHealth("deepseek")
	if h == nil {
		t.Fatal("expected health status after registration")
	}
	if !h.Healthy {
		t.Error("expected Healthy=true after registration")
	}
}

func TestHealthChecker_IsHealthy_UnknownProvider(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	// Unregistered providers are assumed healthy.
	if !hc.IsHealthy("unknown") {
		t.Error("expected IsHealthy(unknown) to return true")
	}
}

func TestHealthChecker_GetHealth_UnknownProvider(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	if h := hc.GetHealth("unknown"); h != nil {
		t.Errorf("expected nil for unknown provider, got %+v", h)
	}
}

func TestHealthChecker_GetHealth_ReturnsCopy(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	hc.RegisterProvider("deepseek", NewCircuitBreaker(DefaultCircuitBreakerConfig()))

	h := hc.GetHealth("deepseek")
	h.Healthy = false
	h.ConsecutiveOK = 99

	// Mutating the returned copy must not affect the stored status.
	h2 := hc.GetHealth("deepseek")
	if !h2.Healthy {
		t.Error("stored status should still be Healthy=true (copy must not alias)")
	}
	if h2.ConsecutiveOK != 0 {
		t.Errorf("stored ConsecutiveOK should be 0, got %d", h2.ConsecutiveOK)
	}
}

func TestHealthChecker_GetAllHealth_ReturnsCopies(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	hc.RegisterProvider("a", nil)
	hc.RegisterProvider("b", nil)

	all := hc.GetAllHealth()
	if len(all) != 2 {
		t.Fatalf("expected 2 health statuses, got %d", len(all))
	}

	all["a"].Healthy = false
	all["b"].ConsecutiveOK = 7

	h2 := hc.GetHealth("a")
	if !h2.Healthy {
		t.Error("GetAllHealth must return copies; stored Healthy should be true")
	}
	if h2.ConsecutiveOK != 0 {
		t.Errorf("expected ConsecutiveOK 0, got %d", h2.ConsecutiveOK)
	}
}

func TestHealthChecker_CheckProvider_Success(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	p := newFakeProvider("deepseek")
	hc.RegisterProvider("deepseek", NewCircuitBreaker(DefaultCircuitBreakerConfig()))

	hc.checkAll(map[string]provider.Provider{p.Name(): p})

	h := hc.GetHealth("deepseek")
	if h == nil {
		t.Fatal("expected health status")
	}
	if !h.Healthy {
		t.Error("expected Healthy=true on successful ping")
	}
	if h.ConsecutiveOK != 1 {
		t.Errorf("expected ConsecutiveOK=1, got %d", h.ConsecutiveOK)
	}
	if h.LastError != "" {
		t.Errorf("expected empty LastError, got %q", h.LastError)
	}
	if h.LastCheck.IsZero() {
		t.Error("expected LastCheck to be set")
	}
	if h.Latency < 0 {
		t.Errorf("expected non-negative latency, got %v", h.Latency)
	}
}

func TestHealthChecker_CheckProvider_Failure(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	p := newFakeProvider("deepseek")
	p.setPingErr(errTest("connection refused"))
	hc.RegisterProvider("deepseek", NewCircuitBreaker(DefaultCircuitBreakerConfig()))

	hc.checkAll(map[string]provider.Provider{p.Name(): p})

	h := hc.GetHealth("deepseek")
	if h == nil {
		t.Fatal("expected health status")
	}
	if h.Healthy {
		t.Error("expected Healthy=false on failed ping")
	}
	if h.ConsecutiveOK != 0 {
		t.Errorf("expected ConsecutiveOK=0 after failure, got %d", h.ConsecutiveOK)
	}
	if h.LastError != "connection refused" {
		t.Errorf("expected LastError='connection refused', got %q", h.LastError)
	}
}

func TestHealthChecker_ConsecutiveOK_Tracking(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	p := newFakeProvider("deepseek")
	hc.RegisterProvider("deepseek", NewCircuitBreaker(DefaultCircuitBreakerConfig()))

	hc.checkAll(map[string]provider.Provider{p.Name(): p})
	hc.checkAll(map[string]provider.Provider{p.Name(): p})
	if h := hc.GetHealth("deepseek"); h.ConsecutiveOK != 2 {
		t.Fatalf("expected ConsecutiveOK=2 after two successes, got %d", h.ConsecutiveOK)
	}

	// A failure resets the consecutive counter.
	p.setPingErr(errTest("boom"))
	hc.checkAll(map[string]provider.Provider{p.Name(): p})
	if h := hc.GetHealth("deepseek"); h.ConsecutiveOK != 0 {
		t.Fatalf("expected ConsecutiveOK=0 after failure, got %d", h.ConsecutiveOK)
	}
	if h := hc.GetHealth("deepseek"); h.Healthy {
		t.Fatal("expected provider unhealthy after failed ping")
	}

	// Recovery.
	p.setPingErr(nil)
	hc.checkAll(map[string]provider.Provider{p.Name(): p})
	if h := hc.GetHealth("deepseek"); h.ConsecutiveOK != 1 || !h.Healthy {
		t.Fatalf("expected recovery to ConsecutiveOK=1 and Healthy, got %+v", h)
	}
}

func TestHealthChecker_CheckProvider_CircuitState(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	cfg := CircuitBreakerConfig{FailureThreshold: 2, SuccessThreshold: 1, Timeout: time.Minute}
	cb := NewCircuitBreaker(cfg)
	hc.RegisterProvider("deepseek", cb)

	p := newFakeProvider("deepseek")
	hc.checkAll(map[string]provider.Provider{p.Name(): p})

	if h := hc.GetHealth("deepseek"); h.CircuitState != "closed" {
		t.Fatalf("expected circuit state 'closed', got %q", h.CircuitState)
	}

	// Trip the breaker so the next health check reflects it.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("expected breaker to be open")
	}

	hc.checkAll(map[string]provider.Provider{p.Name(): p})
	if h := hc.GetHealth("deepseek"); h.CircuitState != "open" {
		t.Fatalf("expected circuit state 'open', got %q", h.CircuitState)
	}
}

func TestHealthChecker_CheckProvider_Unregistered(t *testing.T) {
	// checkProvider creates a status entry on the fly for unregistered providers.
	hc := NewHealthChecker(time.Second)
	p := newFakeProvider("deepseek")

	hc.checkAll(map[string]provider.Provider{p.Name(): p})

	h := hc.GetHealth("deepseek")
	if h == nil {
		t.Fatal("expected health status created for unregistered provider")
	}
	if !h.Healthy {
		t.Error("expected Healthy=true for successful ping on unregistered provider")
	}
}

func TestHealthChecker_StartStop(t *testing.T) {
	hc := NewHealthChecker(time.Second)
	p := newFakeProvider("deepseek")
	hc.RegisterProvider("deepseek", NewCircuitBreaker(DefaultCircuitBreakerConfig()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.Start(ctx, map[string]provider.Provider{p.Name(): p})
	// Stop must not panic and must terminate the background goroutine.
	hc.Stop()
}
