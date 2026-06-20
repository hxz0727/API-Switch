package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Models == nil {
		t.Error("expected non-nil Models map")
	}
	if cfg.Providers == nil {
		t.Error("expected non-nil Providers map")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Error("expected non-empty config path")
	}
	// Should contain .api-switch.yaml
	if filepath.Base(path) != ".api-switch.yaml" {
		t.Errorf("expected filename '.api-switch.yaml', got %q", filepath.Base(path))
	}
}

func TestLoad_NonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "test-config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Port = 9090
	cfg.Providers["test"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test123",
		BaseURL: "https://api.test.com/v1",
	}
	cfg.Models["test-model"] = ModelConfig{
		Provider:      "test",
		ModelOverride: "test-model-v2",
	}

	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", loaded.Server.Port)
	}
	if loaded.Providers["test"].APIKey != "sk-test123" {
		t.Errorf("expected API key 'sk-test123', got %q", loaded.Providers["test"].APIKey)
	}
	if loaded.Models["test-model"].ModelOverride != "test-model-v2" {
		t.Errorf("expected model_override 'test-model-v2', got %q", loaded.Models["test-model"].ModelOverride)
	}
}

func TestValidate_NoProviders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = nil
	cfg.Models = map[string]ModelConfig{"m": {Provider: "p"}}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for no providers")
	}
}

func TestValidate_NoModels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{"p": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.test.com/v1"}}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for no models")
	}
}

func TestValidate_MissingProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.test.com/v1"},
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p2"}, // references non-existent provider
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing provider reference")
	}
}

func TestValidate_EmptyAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "openai", BaseURL: "https://api.test.com/v1"}, // no API key
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p1"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestValidate_InvalidProviderType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "invalid", APIKey: "sk-xxx", BaseURL: "https://api.test.com/v1"},
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p1"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid provider type")
	}
}

func TestValidate_EmptyBaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "openai", APIKey: "sk-xxx"}, // no base URL
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p1"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty base URL")
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 0
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.test.com/v1"},
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p1"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestValidate_Success(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 8080
	cfg.Providers = map[string]ProviderConfig{
		"p1": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.test.com/v1"},
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p1"},
	}
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRouteModel_Success(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"deepseek": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.deepseek.com", DefaultMaxTokens: 8192},
	}
	cfg.Models = map[string]ModelConfig{
		"deepseek-chat": {Provider: "deepseek", ModelOverride: "deepseek-chat-v2"},
	}

	providerName, provCfg, actualModel, err := cfg.RouteModel("deepseek-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if providerName != "deepseek" {
		t.Errorf("expected provider 'deepseek', got %q", providerName)
	}
	if provCfg.Type != "openai" {
		t.Errorf("expected type 'openai', got %q", provCfg.Type)
	}
	if actualModel != "deepseek-chat-v2" {
		t.Errorf("expected actual model 'deepseek-chat-v2', got %q", actualModel)
	}
}

func TestRouteModel_NoOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"deepseek": {Type: "openai", APIKey: "sk-xxx", BaseURL: "https://api.deepseek.com"},
	}
	cfg.Models = map[string]ModelConfig{
		"deepseek-chat": {Provider: "deepseek"}, // no model_override
	}

	_, _, actualModel, err := cfg.RouteModel("deepseek-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actualModel != "deepseek-chat" {
		t.Errorf("expected actual model 'deepseek-chat', got %q", actualModel)
	}
}

func TestRouteModel_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	_, _, _, err := cfg.RouteModel("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent model")
	}
}

func TestRouteModel_ProviderNotFound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "nonexistent"},
	}
	_, _, _, err := cfg.RouteModel("m1")
	if err == nil {
		t.Error("expected error for non-existent provider")
	}
}

func TestSetValue_ServerPort(t *testing.T) {
	cfg := DefaultConfig()
	err := SetValue(cfg, "server.port", "9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
}

func TestSetValue_InvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	err := SetValue(cfg, "server.port", "notanumber")
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestSetValue_UnknownKey(t *testing.T) {
	cfg := DefaultConfig()
	err := SetValue(cfg, "server.unknown", "value")
	if err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestSetProviderValue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"test": {},
	}
	err := SetProviderValue(cfg, "providers.test.api_key", "sk-newkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["test"].APIKey != "sk-newkey" {
		t.Errorf("expected API key 'sk-newkey', got %q", cfg.Providers["test"].APIKey)
	}
}

func TestSetProviderValue_BaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"test": {},
	}
	err := SetProviderValue(cfg, "providers.test.base_url", "https://new.api.com/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["test"].BaseURL != "https://new.api.com/v1" {
		t.Errorf("unexpected base URL: %q", cfg.Providers["test"].BaseURL)
	}
}

func TestSetProviderValue_DefaultMaxTokens(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"test": {},
	}
	err := SetProviderValue(cfg, "providers.test.default_max_tokens", "4096")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["test"].DefaultMaxTokens != 4096 {
		t.Errorf("expected 4096, got %d", cfg.Providers["test"].DefaultMaxTokens)
	}
}

func TestSetProviderValue_InvalidFormat(t *testing.T) {
	cfg := DefaultConfig()
	err := SetProviderValue(cfg, "invalid.format", "value")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestSetProviderValue_UnknownField(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"test": {},
	}
	err := SetProviderValue(cfg, "providers.test.unknown_field", "value")
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestSetProviderValue_NewProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = make(map[string]ProviderConfig)
	err := SetProviderValue(cfg, "providers.newprov.api_key", "sk-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["newprov"].APIKey != "sk-new" {
		t.Errorf("expected API key 'sk-new', got %q", cfg.Providers["newprov"].APIKey)
	}
}

func TestKnownProviders(t *testing.T) {
	known := KnownProviders()
	if len(known) == 0 {
		t.Error("expected non-empty known providers")
	}

	// Verify some well-known providers exist
	expectedProviders := []string{"deepseek", "qwen", "moonshot", "glm"}
	for _, name := range expectedProviders {
		if _, ok := known[name]; !ok {
			t.Errorf("expected known provider %q", name)
		}
	}

	// Verify deepseek has correct defaults
	deepseek, ok := known["deepseek"]
	if !ok {
		t.Fatal("deepseek not found")
	}
	if deepseek.Type != "openai" {
		t.Errorf("expected deepseek type 'openai', got %q", deepseek.Type)
	}
	if deepseek.BaseURL != "https://api.deepseek.com" {
		t.Errorf("unexpected deepseek URL: %q", deepseek.BaseURL)
	}
}

func TestListModels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"anthropic": {Type: "anthropic", APIKey: "sk-ant", BaseURL: "https://api.anthropic.com"},
		"openai":    {Type: "openai", APIKey: "sk-oai", BaseURL: "https://api.openai.com/v1"},
	}
	cfg.Models = map[string]ModelConfig{
		"claude-sonnet": {Provider: "anthropic"},
		"gpt-4o":        {Provider: "openai"},
	}

	entries := cfg.ListModels()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Anthropic models should come first
	if entries[0].Name != "claude-sonnet" {
		t.Errorf("expected claude-sonnet first, got %q", entries[0].Name)
	}
	if entries[0].Marker != "✓" {
		t.Errorf("expected marker '✓' for anthropic, got %q", entries[0].Marker)
	}
	if entries[1].Marker != " " {
		t.Errorf("expected marker ' ' for openai, got %q", entries[1].Marker)
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	// Should fall back to default path
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Error("expected non-nil config")
	}
}

func TestSave_DefaultPath(t *testing.T) {
	tmpDir := t.TempDir()

	// We can't easily override DefaultConfigPath, so test Save with explicit path
	cfgPath := filepath.Join(tmpDir, "save-test.yaml")
	cfg := DefaultConfig()

	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Verify file exists and has correct permissions
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("saved file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestYamlMarshal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 3000
	data, err := YamlMarshal(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty YAML output")
	}
}
