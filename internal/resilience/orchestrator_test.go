package resilience

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/provider"
)

func TestOrchestrator_GetOrCreateBreaker(t *testing.T) {
	o := NewOrchestrator()
	cb1 := o.GetOrCreateBreaker("deepseek")
	cb2 := o.GetOrCreateBreaker("deepseek")

	if cb1 != cb2 {
		t.Fatal("expected the same breaker instance for the same provider")
	}

	// Different providers get different breakers.
	other := o.GetOrCreateBreaker("anthropic")
	if cb1 == other {
		t.Fatal("expected different breaker instances for different providers")
	}

	// Registration with the health checker happens implicitly.
	if !o.IsProviderHealthy("deepseek") {
		t.Error("expected registered provider to be healthy")
	}
}

func TestOrchestrator_Execute_PrimarySuccess(t *testing.T) {
	o := NewOrchestrator()
	primary := newFakeProvider("primary")

	calls := 0
	err := o.Execute(context.Background(), primary, "model-x", 128, nil, func(p provider.Provider, model string, maxTokens int) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	stats := o.GetBreakerStats()["primary"].(map[string]interface{})
	if stats["total_successes"].(int64) != 1 {
		t.Errorf("expected total_successes=1, got %v", stats["total_successes"])
	}
}

func TestOrchestrator_Execute_FallbackSuccess(t *testing.T) {
	o := NewOrchestrator()
	primary := newFakeProvider("primary")
	fallback := newFakeProvider("fallback")

	route := FallbackRoute{Provider: fallback, ProviderName: "fallback", ActualModel: "model-y", MaxTokens: 64}

	err := o.Execute(context.Background(), primary, "model-x", 128, []FallbackRoute{route}, func(p provider.Provider, model string, maxTokens int) error {
		if p.Name() == "primary" {
			return errors.New("primary down")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected failover to succeed, got %v", err)
	}

	stats := o.GetBreakerStats()["primary"].(map[string]interface{})
	if stats["total_failures"].(int64) != 1 {
		t.Errorf("expected primary total_failures=1, got %v", stats["total_failures"])
	}
	fallbackStats := o.GetBreakerStats()["fallback"].(map[string]interface{})
	if fallbackStats["total_successes"].(int64) != 1 {
		t.Errorf("expected fallback total_successes=1, got %v", fallbackStats["total_successes"])
	}
}

func TestOrchestrator_Execute_AllFail(t *testing.T) {
	o := NewOrchestrator()
	primary := newFakeProvider("primary")
	fallback := newFakeProvider("fallback")

	route := FallbackRoute{Provider: fallback, ProviderName: "fallback", ActualModel: "model-y", MaxTokens: 64}

	err := o.Execute(context.Background(), primary, "model-x", 128, []FallbackRoute{route}, func(p provider.Provider, model string, maxTokens int) error {
		return errors.New("boom")
	})

	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected last error to be returned, got %v", err)
	}

	// The orchestrator records a failure for each attempted provider.
	stats := o.GetBreakerStats()["primary"].(map[string]interface{})
	if stats["total_failures"].(int64) != 1 {
		t.Errorf("expected primary total_failures=1, got %v", stats["total_failures"])
	}
}

func TestOrchestrator_Execute_SkipsOpenBreaker(t *testing.T) {
	o := NewOrchestrator()
	// Use a low failure threshold so two failures trip the breaker.
	o.breakerConfig = CircuitBreakerConfig{FailureThreshold: 2, SuccessThreshold: 1, Timeout: time.Hour}
	primary := newFakeProvider("primary")
	fallback := newFakeProvider("fallback")

	// Trip the primary breaker so Execute skips it entirely.
	cb := o.GetOrCreateBreaker("primary")
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("expected primary breaker to be open")
	}

	primaryCalls := 0
	route := FallbackRoute{Provider: fallback, ProviderName: "fallback", ActualModel: "model-y", MaxTokens: 64}

	err := o.Execute(context.Background(), primary, "model-x", 128, []FallbackRoute{route}, func(p provider.Provider, model string, maxTokens int) error {
		if p.Name() == "primary" {
			primaryCalls++
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected fallback to handle request, got %v", err)
	}
	if primaryCalls != 0 {
		t.Errorf("expected primary not to be called when breaker is open, got %d calls", primaryCalls)
	}
}

func TestOrchestrator_Execute_AllBreakersOpen(t *testing.T) {
	o := NewOrchestrator()
	// Use a low failure threshold so two failures trip each breaker.
	o.breakerConfig = CircuitBreakerConfig{FailureThreshold: 2, SuccessThreshold: 1, Timeout: time.Hour}
	primary := newFakeProvider("primary")
	fallback := newFakeProvider("fallback")

	o.GetOrCreateBreaker("primary").RecordFailure()
	o.GetOrCreateBreaker("primary").RecordFailure()
	o.GetOrCreateBreaker("fallback").RecordFailure()
	o.GetOrCreateBreaker("fallback").RecordFailure()

	calls := 0
	route := FallbackRoute{Provider: fallback, ProviderName: "fallback", ActualModel: "model-y", MaxTokens: 64}

	err := o.Execute(context.Background(), primary, "model-x", 128, []FallbackRoute{route}, func(p provider.Provider, model string, maxTokens int) error {
		calls++
		return nil
	})

	if err == nil {
		t.Fatal("expected error when all breakers are open")
	}
	if !strings.Contains(err.Error(), "circuit breaker is open") {
		t.Errorf("expected circuit breaker error, got %v", err)
	}
	if calls != 0 {
		t.Errorf("expected no calls when all breakers open, got %d", calls)
	}
}

func TestOrchestrator_Execute_SkipsUnhealthyProvider(t *testing.T) {
	o := NewOrchestrator()
	primary := newFakeProvider("primary")
	fallback := newFakeProvider("fallback")

	// Mark the primary provider unhealthy via the health checker.
	o.GetOrCreateBreaker("primary")
	primary.setPingErr(errors.New("unhealthy"))
	o.healthChecker.checkAll(map[string]provider.Provider{primary.Name(): primary})

	if o.IsProviderHealthy("primary") {
		t.Fatal("expected primary provider to be unhealthy")
	}

	primaryCalls := 0
	route := FallbackRoute{Provider: fallback, ProviderName: "fallback", ActualModel: "model-y", MaxTokens: 64}

	err := o.Execute(context.Background(), primary, "model-x", 128, []FallbackRoute{route}, func(p provider.Provider, model string, maxTokens int) error {
		if p.Name() == "primary" {
			primaryCalls++
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected fallback to handle request, got %v", err)
	}
	if primaryCalls != 0 {
		t.Errorf("expected primary not to be called when unhealthy, got %d calls", primaryCalls)
	}
}

func TestOrchestrator_Execute_PassesArgs(t *testing.T) {
	o := NewOrchestrator()
	primary := newFakeProvider("primary")

	var gotModel string
	var gotMaxTokens int

	err := o.Execute(context.Background(), primary, "model-x", 512, nil, func(p provider.Provider, model string, maxTokens int) error {
		gotModel = model
		gotMaxTokens = maxTokens
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotModel != "model-x" {
		t.Errorf("expected model 'model-x', got %q", gotModel)
	}
	if gotMaxTokens != 512 {
		t.Errorf("expected maxTokens 512, got %d", gotMaxTokens)
	}
}

func TestOrchestrator_GetBreakerStats(t *testing.T) {
	o := NewOrchestrator()
	o.GetOrCreateBreaker("deepseek")
	o.GetOrCreateBreaker("anthropic")

	stats := o.GetBreakerStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 breaker stats, got %d", len(stats))
	}
	for name, s := range stats {
		st, ok := s.(map[string]interface{})
		if !ok {
			t.Fatalf("expected stats for %s to be map, got %T", name, s)
		}
		if st["state"] != "closed" {
			t.Errorf("expected state 'closed' for %s, got %v", name, st["state"])
		}
	}
}

func TestOrchestrator_GetHealthStatus(t *testing.T) {
	o := NewOrchestrator()
	o.GetOrCreateBreaker("deepseek")

	status := o.GetHealthStatus()
	if len(status) != 1 {
		t.Fatalf("expected 1 health status, got %d", len(status))
	}
	h, ok := status["deepseek"]
	if !ok {
		t.Fatal("expected health status for deepseek")
	}
	if !h.Healthy {
		t.Error("expected deepseek healthy")
	}
}

func TestOrchestrator_IsProviderHealthy_Unknown(t *testing.T) {
	o := NewOrchestrator()
	// Unregistered providers are assumed healthy.
	if !o.IsProviderHealthy("unknown") {
		t.Error("expected IsProviderHealthy(unknown) to be true")
	}
}

func TestOrchestrator_StartStopHealthChecker(t *testing.T) {
	o := NewOrchestrator()
	p := newFakeProvider("primary")
	o.GetOrCreateBreaker("primary")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	o.StartHealthChecker(ctx, map[string]provider.Provider{p.Name(): p})
	o.StopHealthChecker()
}
