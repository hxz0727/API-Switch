package resilience

import (
	"context"
	"sync"
	"time"

	"github.com/hxz0727/API-Switch/internal/logutil"
	"github.com/hxz0727/API-Switch/internal/provider"
)

// ProviderHealth holds health information for a provider.
type ProviderHealth struct {
	Healthy        bool          `json:"healthy"`
	LastCheck      time.Time     `json:"last_check"`
	LastError      string        `json:"last_error,omitempty"`
	ConsecutiveOK  int           `json:"consecutive_ok"`
	Latency        time.Duration `json:"latency"`
	CircuitState   string        `json:"circuit_state"`
}

// HealthChecker periodically checks provider health.
type HealthChecker struct {
	mu          sync.RWMutex
	statuses    map[string]*ProviderHealth
	breakers    map[string]*CircuitBreaker
	interval    time.Duration
	stopCh      chan struct{}
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(interval time.Duration) *HealthChecker {
	return &HealthChecker{
		statuses: make(map[string]*ProviderHealth),
		breakers: make(map[string]*CircuitBreaker),
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// RegisterProvider registers a provider for health monitoring.
func (hc *HealthChecker) RegisterProvider(name string, breaker *CircuitBreaker) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.statuses[name] = &ProviderHealth{Healthy: true}
	hc.breakers[name] = breaker
}

// Start begins periodic health checking.
func (hc *HealthChecker) Start(ctx context.Context, providers map[string]provider.Provider) {
	go func() {
		// Initial check after a short delay
		time.Sleep(5 * time.Second)
		hc.checkAll(providers)

		ticker := time.NewTicker(hc.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hc.stopCh:
				return
			case <-ticker.C:
				hc.checkAll(providers)
			}
		}
	}()
}

// Stop stops the health checker.
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

func (hc *HealthChecker) checkAll(providers map[string]provider.Provider) {
	for name, p := range providers {
		hc.checkProvider(name, p)
	}
}

func (hc *HealthChecker) checkProvider(name string, p provider.Provider) {
	start := time.Now()
	err := p.Ping()
	latency := time.Since(start)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	health, exists := hc.statuses[name]
	if !exists {
		health = &ProviderHealth{}
		hc.statuses[name] = health
	}

	health.LastCheck = time.Now()
	health.Latency = latency

	if err != nil {
		health.Healthy = false
		health.LastError = err.Error()
		health.ConsecutiveOK = 0
		logutil.Debug("Health check failed for %s: %v", name, err)
	} else {
		health.Healthy = true
		health.LastError = ""
		health.ConsecutiveOK++
		logutil.Debug("Health check OK for %s (latency: %v)", name, latency)
	}

	// Update circuit state from breaker
	if breaker, ok := hc.breakers[name]; ok {
		health.CircuitState = breaker.State().String()
	}
}

// GetHealth returns the health status of a provider.
func (hc *HealthChecker) GetHealth(name string) *ProviderHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	if h, ok := hc.statuses[name]; ok {
		// Return a copy
		copy := *h
		return &copy
	}
	return nil
}

// GetAllHealth returns health status for all providers.
func (hc *HealthChecker) GetAllHealth() map[string]*ProviderHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	result := make(map[string]*ProviderHealth, len(hc.statuses))
	for name, h := range hc.statuses {
		copy := *h
		result[name] = &copy
	}
	return result
}

// IsHealthy checks if a provider is healthy.
func (hc *HealthChecker) IsHealthy(name string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	if h, ok := hc.statuses[name]; ok {
		return h.Healthy
	}
	return true // Assume healthy if not registered
}
