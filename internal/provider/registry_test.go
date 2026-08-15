package provider

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

func testProviderConfig() *config.ProviderConfig {
	return &config.ProviderConfig{
		Type:             "openai",
		APIKey:           "test-key",
		BaseURL:          "http://127.0.0.1:9",
		DefaultMaxTokens: 2048,
	}
}

func TestRegistry_KnownTypesRegistered(t *testing.T) {
	// These are registered via init() in the provider implementation files.
	if !IsRegistered("anthropic") {
		t.Error("expected 'anthropic' type to be registered")
	}
	if !IsRegistered("openai") {
		t.Error("expected 'openai' type to be registered")
	}
}

func TestRegistry_RegisteredTypes(t *testing.T) {
	types := RegisteredTypes()
	if len(types) == 0 {
		t.Fatal("expected at least one registered type")
	}
	seen := map[string]bool{}
	for _, typ := range types {
		seen[typ] = true
	}
	for _, want := range []string{"anthropic", "openai"} {
		if !seen[want] {
			t.Errorf("expected %q in RegisteredTypes, got %v", want, types)
		}
	}
}

func TestRegistry_Create_KnownTypes(t *testing.T) {
	cfg := testProviderConfig()

	anthropicProv, err := Create("anthropic", "claude-1", cfg)
	if err != nil {
		t.Fatalf("Create(anthropic): %v", err)
	}
	if anthropicProv.Name() != "claude-1" {
		t.Errorf("expected Name()=claude-1, got %q", anthropicProv.Name())
	}
	if anthropicProv.Type() != "anthropic" {
		t.Errorf("expected Type()=anthropic, got %q", anthropicProv.Type())
	}

	openAIProv, err := Create("openai", "deepseek-1", cfg)
	if err != nil {
		t.Fatalf("Create(openai): %v", err)
	}
	if openAIProv.Name() != "deepseek-1" {
		t.Errorf("expected Name()=deepseek-1, got %q", openAIProv.Name())
	}
	if openAIProv.Type() != "openai" {
		t.Errorf("expected Type()=openai, got %q", openAIProv.Type())
	}
}

func TestRegistry_Create_UnknownType(t *testing.T) {
	_, err := Create("nonexistent-type", "x", testProviderConfig())
	if err == nil {
		t.Fatal("expected error for unknown provider type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown provider type") {
		t.Errorf("expected 'unknown provider type' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "nonexistent-type") {
		t.Errorf("expected type name in error, got %q", err.Error())
	}
}

func TestRegistry_IsRegistered(t *testing.T) {
	// Registered via init() in the provider implementation files.
	if !IsRegistered("openai") {
		t.Error("expected 'openai' to be registered")
	}
	if IsRegistered("definitely-not-a-type") {
		t.Error("expected false for unregistered type")
	}
}

func TestRegistry_DuplicateRegistrationOverrides(t *testing.T) {
	// Register a type, then register the same type again with a different
	// factory. The latest registration must win.
	const testType = "test-duplicate-type"

	Register(testType, func(name string, cfg *config.ProviderConfig) Provider {
		return &mockProviderImpl{name: name, typ: "v1"}
	})
	Register(testType, func(name string, cfg *config.ProviderConfig) Provider {
		return &mockProviderImpl{name: name, typ: "v2"}
	})

	if !IsRegistered(testType) {
		t.Fatal("expected test type to be registered after duplicate registration")
	}

	p, err := Create(testType, "instance", testProviderConfig())
	if err != nil {
		t.Fatalf("Create(test type): %v", err)
	}
	if p.Type() != "v2" {
		t.Errorf("expected latest factory to win (v2), got %q", p.Type())
	}
}

func TestRegistry_Create_NoPanicOnMissingCfg(t *testing.T) {
	// Factory receives the cfg as-is; a nil cfg must not panic at creation
	// time for the built-in types (they only store it).
	p, err := Create("anthropic", "x", nil)
	if err != nil {
		t.Fatalf("expected nil cfg to be tolerated, got %v", err)
	}
	if p.Name() != "x" {
		t.Errorf("expected Name()=x, got %q", p.Name())
	}
}

// mockProviderImpl is a lightweight Provider used to exercise the registry.
type mockProviderImpl struct {
	name string
	typ  string
}

func (m *mockProviderImpl) Name() string { return m.name }
func (m *mockProviderImpl) Type() string { return m.typ }
func (m *mockProviderImpl) SendMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (*anthropic.MessagesResponse, error) {
	return nil, nil
}
func (m *mockProviderImpl) StreamMessage(ctx context.Context, req *anthropic.MessagesRequest, model string, maxTokens int) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockProviderImpl) Ping() error                       { return nil }
func (m *mockProviderImpl) ListModels() ([]string, error)    { return nil, nil }
