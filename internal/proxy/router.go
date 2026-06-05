package proxy

import (
	"sync"

	"github.com/user/api-switch/internal/config"
	"github.com/user/api-switch/internal/provider"
)

// Router routes requests to the correct provider based on model name.
type Router struct {
	cfg     *config.Config
	mu      sync.RWMutex
	clients map[string]clientEntry
}

type clientEntry struct {
	anthropic *provider.AnthropicClient
	openai    *provider.OpenAIClient
}

// NewRouter creates a new router from the config.
func NewRouter(cfg *config.Config) *Router {
	r := &Router{
		cfg:     cfg,
		clients: make(map[string]clientEntry),
	}
	r.initClients()
	return r
}

// Reload reinitializes the router with a new config.
// This allows hot-reloading without restarting the server.
func (r *Router) Reload(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
	r.clients = make(map[string]clientEntry)
	r.initClients()
}

func (r *Router) initClients() {
	for name, provCfg := range r.cfg.Providers {
		entry := clientEntry{}
		switch provCfg.Type {
		case "anthropic":
			entry.anthropic = provider.NewAnthropicClient(&provCfg)
		case "openai":
			entry.openai = provider.NewOpenAIClient(&provCfg)
		}
		r.clients[name] = entry
	}
}

// RouteResult contains the routing decision for a model request.
type RouteResult struct {
	ProviderType     string // "anthropic" or "openai"
	ProviderName     string
	ActualModel      string
	DefaultMaxTokens int
	Anthropic        *provider.AnthropicClient
	OpenAI           *provider.OpenAIClient
}

// Route determines which provider handles the given model.
func (r *Router) Route(model string) (*RouteResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerName, provCfg, actualModel, err := r.cfg.RouteModel(model)
	if err != nil {
		return nil, err
	}

	entry, ok := r.clients[providerName]
	if !ok {
		return nil, err
	}

	result := &RouteResult{
		ProviderType:     provCfg.Type,
		ProviderName:     providerName,
		ActualModel:      actualModel,
		DefaultMaxTokens: provCfg.DefaultMaxTokens,
		Anthropic:        entry.anthropic,
		OpenAI:           entry.openai,
	}

	return result, nil
}
