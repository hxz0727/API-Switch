package proxy

import (
	"strings"
	"testing"

	"github.com/hxz0727/API-Switch/internal/config"
)

// routerTestConfig returns a config with one openai provider and one model.
func routerTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				Type:             "openai",
				APIKey:           "sk-test",
				BaseURL:          "https://api.example.com/v1",
				DefaultMaxTokens: 2048,
			},
		},
		Models: map[string]config.ModelConfig{
			"test-model": {Provider: "test-provider"},
		},
	}
}

// =====================================================================
// RouteTest
// =====================================================================

func TestRouteTest_Success(t *testing.T) {
	cfg := routerTestConfig()
	res, err := RouteTest(cfg, "test-model")
	if err != nil {
		t.Fatalf("RouteTest failed: %v", err)
	}
	if res.ProviderName != "test-provider" {
		t.Errorf("ProviderName = %q, want test-provider", res.ProviderName)
	}
	if res.ProviderType != "openai" {
		t.Errorf("ProviderType = %q, want openai", res.ProviderType)
	}
	if res.ActualModel != "test-model" {
		t.Errorf("ActualModel = %q, want test-model", res.ActualModel)
	}
	if res.DefaultMaxTokens != 2048 {
		t.Errorf("DefaultMaxTokens = %d, want 2048", res.DefaultMaxTokens)
	}
	if res.Provider == nil {
		t.Error("expected non-nil Provider")
	}
}

func TestRouteTest_ModelOverride(t *testing.T) {
	cfg := routerTestConfig()
	cfg.Models["test-model"] = config.ModelConfig{Provider: "test-provider", ModelOverride: "gpt-4o"}
	res, err := RouteTest(cfg, "test-model")
	if err != nil {
		t.Fatalf("RouteTest failed: %v", err)
	}
	if res.ActualModel != "gpt-4o" {
		t.Errorf("ActualModel = %q, want gpt-4o", res.ActualModel)
	}
}

func TestRouteTest_ModelNotFound(t *testing.T) {
	cfg := routerTestConfig()
	_, err := RouteTest(cfg, "no-such-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
	if !strings.Contains(err.Error(), "no-such-model") {
		t.Errorf("expected error to mention model name, got %q", err)
	}
}

func TestRouteTest_UnsupportedProviderType(t *testing.T) {
	cfg := routerTestConfig()
	cfg.Providers["test-provider"] = config.ProviderConfig{
		Type:    "not-a-real-provider",
		APIKey:  "sk-test",
		BaseURL: "https://api.example.com/v1",
	}
	_, err := RouteTest(cfg, "test-model")
	if err == nil {
		t.Fatal("expected error for unsupported provider type")
	}
	if !strings.Contains(err.Error(), "not-a-real-provider") {
		t.Errorf("expected error to mention provider type, got %q", err)
	}
}

// =====================================================================
// initProviders / NewRouter
// =====================================================================

func TestNewRouter_SkipsUnsupportedProvider(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Providers: map[string]config.ProviderConfig{
			"bad": {
				Type:    "unknown-type",
				APIKey:  "k",
				BaseURL: "https://api.example.com/v1",
			},
			"good": {
				Type:    "openai",
				APIKey:  "sk-test",
				BaseURL: "https://api.example.com/v1",
			},
		},
		Models: map[string]config.ModelConfig{
			"m": {Provider: "good"},
		},
	}

	r := NewRouter(cfg)
	if len(r.providers) != 1 {
		t.Fatalf("expected exactly 1 initialized provider (good), got %d: %v", len(r.providers), r.providers)
	}
	if _, ok := r.providers["good"]; !ok {
		t.Error("expected 'good' provider to be initialized")
	}
	if _, ok := r.providers["bad"]; ok {
		t.Error("expected unsupported 'bad' provider NOT to be initialized")
	}
}

func TestRouter_Reload(t *testing.T) {
	r := NewRouter(routerTestConfig())
	if len(r.providers) != 1 {
		t.Fatalf("expected 1 provider after init, got %d", len(r.providers))
	}

	newCfg := routerTestConfig()
	newCfg.Providers["second"] = config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-2",
		BaseURL: "https://api2.example.com/v1",
	}
	r.Reload(newCfg)
	if len(r.providers) != 2 {
		t.Errorf("expected 2 providers after reload, got %d", len(r.providers))
	}
	if r.cfg != newCfg {
		t.Error("expected router cfg to be updated after Reload")
	}
}

// =====================================================================
// Route
// =====================================================================

func TestRoute_Success(t *testing.T) {
	r := NewRouter(routerTestConfig())
	res, err := r.Route("test-model")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if res.ProviderName != "test-provider" {
		t.Errorf("ProviderName = %q, want test-provider", res.ProviderName)
	}
	if res.Provider == nil {
		t.Error("expected non-nil Provider")
	}
}

func TestRoute_ModelNotFound(t *testing.T) {
	r := NewRouter(routerTestConfig())
	_, err := r.Route("missing-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestRoute_ProviderClientNotInitialized(t *testing.T) {
	// A provider whose type is unsupported gets skipped by initProviders,
	// so RouteModel succeeds but the provider map has no client → error.
	cfg := routerTestConfig()
	cfg.Providers["bad"] = config.ProviderConfig{
		Type:    "unknown-type",
		APIKey:  "k",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models["bad-model"] = config.ModelConfig{Provider: "bad"}

	r := NewRouter(cfg)
	_, err := r.Route("bad-model")
	if err == nil {
		t.Fatal("expected error when provider client was not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected error about missing client, got %q", err)
	}
}
