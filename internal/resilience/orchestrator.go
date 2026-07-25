package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hxz0727/API-Switch/internal/logutil"
	"github.com/hxz0727/API-Switch/internal/provider"
)

// FallbackRoute represents a fallback provider route.
type FallbackRoute struct {
	Provider     provider.Provider
	ProviderName string
	ActualModel  string
	MaxTokens    int
}

// Orchestrator combines retry, circuit breaker, and failover.
type Orchestrator struct {
	mu           sync.RWMutex
	breakers     map[string]*CircuitBreaker
	healthChecker *HealthChecker
	retryConfig  RetryConfig
	breakerConfig CircuitBreakerConfig
}

// NewOrchestrator creates a new resilience orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		breakers:      make(map[string]*CircuitBreaker),
		healthChecker: NewHealthChecker(60 * time.Second),
		retryConfig:   DefaultRetryConfig(),
		breakerConfig: DefaultCircuitBreakerConfig(),
	}
}

// GetOrCreateBreaker gets or creates a circuit breaker for a provider.
func (o *Orchestrator) GetOrCreateBreaker(providerName string) *CircuitBreaker {
	o.mu.Lock()
	defer o.mu.Unlock()

	if cb, ok := o.breakers[providerName]; ok {
		return cb
	}
	cb := NewCircuitBreaker(o.breakerConfig)
	o.breakers[providerName] = cb
	o.healthChecker.RegisterProvider(providerName, cb)
	return cb
}

// StartHealthChecker starts the background health checking.
func (o *Orchestrator) StartHealthChecker(ctx context.Context, providers map[string]provider.Provider) {
	o.healthChecker.Start(ctx, providers)
}

// StopHealthChecker stops the background health checking.
func (o *Orchestrator) StopHealthChecker() {
	o.healthChecker.Stop()
}

// Execute runs fn with retry and circuit breaker protection.
// If the primary provider fails, it tries fallbacks in order.
func (o *Orchestrator) Execute(ctx context.Context, primaryProvider provider.Provider, primaryModel string, primaryMaxTokens int, fallbacks []FallbackRoute, fn func(provider.Provider, string, int) error) error {
	// Build the list of providers to try
	type attempt struct {
		provider  provider.Provider
		model     string
		maxTokens int
		name      string
	}

	attempts := []attempt{
		{primaryProvider, primaryModel, primaryMaxTokens, primaryProvider.Name()},
	}
	for _, fb := range fallbacks {
		attempts = append(attempts, attempt{fb.Provider, fb.ActualModel, fb.MaxTokens, fb.ProviderName})
	}

	var lastErr error

	for i, a := range attempts {
		breaker := o.GetOrCreateBreaker(a.name)

		// Check circuit breaker
		if !breaker.Allow() {
			logutil.Warn("Circuit breaker OPEN for provider %s, skipping", a.name)
			lastErr = fmt.Errorf("provider %s circuit breaker is open", a.name)
			continue
		}

		// Check health
		if !o.healthChecker.IsHealthy(a.name) {
			logutil.Warn("Provider %s is unhealthy, trying next", a.name)
			lastErr = fmt.Errorf("provider %s is unhealthy", a.name)
			continue
		}

		// Execute with retry
		err := WithRetry(ctx, o.retryConfig, func() error {
			return fn(a.provider, a.model, a.maxTokens)
		})

		if err == nil {
			breaker.RecordSuccess()
			return nil
		}

		breaker.RecordFailure()
		lastErr = err

		// If this was the primary and there are fallbacks, log the failover
		if i == 0 && len(fallbacks) > 0 {
			logutil.Warn("Primary provider %s failed: %v. Trying fallbacks...", a.name, err)
		}
	}

	return lastErr
}

// GetBreakerStats returns circuit breaker statistics for all providers.
func (o *Orchestrator) GetBreakerStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	stats := make(map[string]interface{}, len(o.breakers))
	for name, cb := range o.breakers {
		stats[name] = cb.Stats()
	}
	return stats
}

// GetHealthStatus returns health status for all providers.
func (o *Orchestrator) GetHealthStatus() map[string]*ProviderHealth {
	return o.healthChecker.GetAllHealth()
}

// IsProviderHealthy checks if a provider is healthy.
func (o *Orchestrator) IsProviderHealthy(name string) bool {
	return o.healthChecker.IsHealthy(name)
}
