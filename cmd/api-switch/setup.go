package main

import (
	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/provider"
)

func init() {
	// Wire up the provider registry with config validation.
	// This avoids circular imports between config and provider packages.
	config.IsProviderTypeRegistered = provider.IsRegistered
}
