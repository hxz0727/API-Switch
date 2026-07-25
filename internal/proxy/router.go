package proxy

import (
	"fmt"
	"sync"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/provider"
)

// Router routes requests to the correct provider based on model name.
type Router struct {
	cfg       *config.Config
	mu        sync.RWMutex
	providers map[string]provider.Provider // provider name -> Provider instance
}

// NewRouter creates a new router from the config.
func NewRouter(cfg *config.Config) *Router {
	r := &Router{
		cfg:       cfg,
		providers: make(map[string]provider.Provider),
	}
	r.initProviders()
	return r
}

// Reload reinitializes the router with a new config.
func (r *Router) Reload(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
	r.providers = make(map[string]provider.Provider)
	r.initProviders()
}

func (r *Router) initProviders() {
	for name, provCfg := range r.cfg.Providers {
		p, err := provider.Create(provCfg.Type, name, &provCfg)
		if err != nil {
			// Log error but continue — this provider will be unavailable
			fmt.Printf("WARNING: failed to create provider %q (type %q): %v\n", name, provCfg.Type, err)
			continue
		}
		r.providers[name] = p
	}
}

// RouteResult contains the routing decision for a model request.
type RouteResult struct {
	ProviderType     string // "anthropic" or "openai"
	ProviderName     string
	ActualModel      string
	DefaultMaxTokens int
	Provider         provider.Provider // unified provider interface
}

// Route determines which provider handles the given model.
func (r *Router) Route(model string) (*RouteResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerName, provCfg, actualModel, err := r.cfg.RouteModel(model)
	if err != nil {
		return nil, err
	}

	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q is configured but client was not initialized (unsupported type %q)", providerName, provCfg.Type)
	}

	return &RouteResult{
		ProviderType:     provCfg.Type,
		ProviderName:     providerName,
		ActualModel:      actualModel,
		DefaultMaxTokens: provCfg.DefaultMaxTokens,
		Provider:         p,
	}, nil
}

// RouteTest is a standalone route lookup used by the test command.
func RouteTest(cfg *config.Config, model string) (*RouteResult, error) {
	providerName, provCfg, actualModel, err := cfg.RouteModel(model)
	if err != nil {
		return nil, err
	}

	p, err := provider.Create(provCfg.Type, providerName, provCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider %q: %w", providerName, err)
	}

	return &RouteResult{
		ProviderType:     provCfg.Type,
		ProviderName:     providerName,
		ActualModel:      actualModel,
		DefaultMaxTokens: provCfg.DefaultMaxTokens,
		Provider:         p,
	}, nil
}
