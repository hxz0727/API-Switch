package provider

import (
	"fmt"
	"sync"

	"github.com/hxz0727/API-Switch/internal/config"
)

var (
	registryMu sync.RWMutex
	factories  = map[string]func(name string, cfg *config.ProviderConfig) Provider{}
)

// Register registers a provider factory for the given type.
// Typically called in init() functions of provider implementations.
func Register(providerType string, factory func(name string, cfg *config.ProviderConfig) Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	factories[providerType] = factory
}

// Create instantiates a provider of the given type with the provided config.
func Create(providerType string, name string, cfg *config.ProviderConfig) (Provider, error) {
	registryMu.RLock()
	factory, ok := factories[providerType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %q (registered types: %v)", providerType, RegisteredTypes())
	}
	return factory(name, cfg), nil
}

// IsRegistered checks if a provider type has been registered.
func IsRegistered(providerType string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := factories[providerType]
	return ok
}

// RegisteredTypes returns a list of all registered provider types.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(factories))
	for t := range factories {
		types = append(types, t)
	}
	return types
}
