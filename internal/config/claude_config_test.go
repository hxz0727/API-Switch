package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClaudeCodeConfigDir verifies the config directory helper honours the
// CLAUDE_CONFIG_DIR env override and falls back to ~/.claude otherwise.
func TestClaudeCodeConfigDir(t *testing.T) {
	// Default fallback (no env set)
	t.Run("default", func(t *testing.T) {
		os.Unsetenv("CLAUDE_CONFIG_DIR")
		dir := ClaudeCodeConfigDir()
		if dir == "" {
			t.Fatal("expected non-empty dir")
		}
		if filepath.Base(dir) != ".claude" {
			t.Errorf("expected trailing path component '.claude', got %q", dir)
		}
	})

	// Env override
	t.Run("override", func(t *testing.T) {
		os.Setenv("CLAUDE_CONFIG_DIR", "/tmp/custom-claude")
		defer os.Unsetenv("CLAUDE_CONFIG_DIR")
		dir := ClaudeCodeConfigDir()
		if dir != "/tmp/custom-claude" {
			t.Errorf("expected override path, got %q", dir)
		}
	})
}

// TestClaudeSettingsPath verifies it returns a path inside the config dir.
func TestClaudeSettingsPath(t *testing.T) {
	os.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude-test")
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")
	p := ClaudeSettingsPath()
	if p != "/tmp/claude-test/settings.json" {
		t.Errorf("expected /tmp/claude-test/settings.json, got %q", p)
	}
}

// TestLoadClaudeSettings_NotFound verifies a missing file returns empty
// settings and no error.
func TestLoadClaudeSettings_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	s, err := LoadClaudeSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil settings")
	}
	if s.Env != nil {
		t.Errorf("expected nil env on empty settings, got %v", s.Env)
	}
}

// TestLoadClaudeSettings_InvalidJSON verifies malformed JSON produces an
// error.
func TestLoadClaudeSettings_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, err := LoadClaudeSettings(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestLoadClaudeSettings_Success verifies a well-formed settings file is
// parsed correctly.
func TestLoadClaudeSettings_Success(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.json")
	data := `{"model": "claude-3", "env": {"FOO": "bar"}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	s, err := LoadClaudeSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Model != "claude-3" {
		t.Errorf("expected model 'claude-3', got %q", s.Model)
	}
	if s.Env["FOO"] != "bar" {
		t.Errorf("expected env FOO=bar, got %q", s.Env["FOO"])
	}
}

// TestSaveClaudeSettings verifies a settings file is written and readable.
func TestSaveClaudeSettings(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "settings.json")
	settings := &ClaudeSettings{
		Model: "test-model",
		Env:   map[string]string{"KEY": "VALUE"},
	}
	if err := SaveClaudeSettings(path, settings); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadClaudeSettings(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Model != "test-model" {
		t.Errorf("expected 'test-model', got %q", loaded.Model)
	}
	if loaded.Env["KEY"] != "VALUE" {
		t.Errorf("expected KEY=VALUE, got %q", loaded.Env["KEY"])
	}
}

// TestInitClaudeSettings_Defaults verifies defaults are set when the
// settings struct is empty.
func TestInitClaudeSettings_Defaults(t *testing.T) {
	s := &ClaudeSettings{}
	InitClaudeSettings(s, "http://localhost:8080")
	if s.Schema == nil {
		t.Error("expected schema to be set")
	}
	if s.Env == nil {
		t.Fatal("expected env map to be initialised")
	}
	if s.Env["ANTHROPIC_BASE_URL"] != "http://localhost:8080" {
		t.Errorf("expected ANTHROPIC_BASE_URL to be set, got %q", s.Env["ANTHROPIC_BASE_URL"])
	}
	if s.Env["ANTHROPIC_API_KEY"] != "use-api-switch" {
		t.Errorf("expected default ANTHROPIC_API_KEY, got %q", s.Env["ANTHROPIC_API_KEY"])
	}
}

// TestInitClaudeSettings_PreservesAPIKey verifies that an existing
// ANTHROPIC_API_KEY env value is not overwritten.
func TestInitClaudeSettings_PreservesAPIKey(t *testing.T) {
	s := &ClaudeSettings{
		Env: map[string]string{"ANTHROPIC_API_KEY": "my-existing-key"},
	}
	InitClaudeSettings(s, "http://localhost:9090")
	if s.Env["ANTHROPIC_API_KEY"] != "my-existing-key" {
		t.Errorf("expected existing API key preserved, got %q", s.Env["ANTHROPIC_API_KEY"])
	}
}

// TestActivateModel_Anthropic verifies that activating an Anthropic model
// removes the CUSTOM_MODEL_OPTION env var.
func TestActivateModel_Anthropic(t *testing.T) {
	s := &ClaudeSettings{
		Env: map[string]string{"ANTHROPIC_CUSTOM_MODEL_OPTION": "old-model"},
	}
	ActivateModel(s, "claude-3-sonnet", "anthropic")
	if s.Model != "claude-3-sonnet" {
		t.Errorf("expected model 'claude-3-sonnet', got %q", s.Model)
	}
	if _, ok := s.Env["ANTHROPIC_CUSTOM_MODEL_OPTION"]; ok {
		t.Error("expected CUSTOM_MODEL_OPTION to be removed for anthropic model")
	}
}

// TestActivateModel_NonAnthropic verifies that non-Anthropic models set
// the CUSTOM_MODEL_OPTION env var so they appear in Claude's /model picker.
func TestActivateModel_NonAnthropic(t *testing.T) {
	s := &ClaudeSettings{}
	ActivateModel(s, "gpt-4o", "openai")
	if s.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", s.Model)
	}
	if s.Env["ANTHROPIC_CUSTOM_MODEL_OPTION"] != "gpt-4o" {
		t.Errorf("expected CUSTOM_MODEL_OPTION=gpt-4o, got %q", s.Env["ANTHROPIC_CUSTOM_MODEL_OPTION"])
	}
}

// TestListModels_SkipsMissingProvider verifies that models pointing to
// non-existent providers are filtered out.
func TestListModels_SkipsMissingProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p1"] = ProviderConfig{Type: "openai", APIKey: "k", BaseURL: "u"}
	cfg.Models = map[string]ModelConfig{
		"good":    {Provider: "p1"},
		"orphaned": {Provider: "does-not-exist"},
	}

	entries := cfg.ListModels()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (orphaned skipped), got %d", len(entries))
	}
	if entries[0].Name != "good" {
		t.Errorf("expected 'good', got %q", entries[0].Name)
	}
}

// TestListModels_SortOrder verifies anthropic models come first.
func TestListModels_SortOrder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"openai":     {Type: "openai", APIKey: "k", BaseURL: "u"},
		"anthropic":  {Type: "anthropic", APIKey: "k", BaseURL: "u"},
	}
	cfg.Models = map[string]ModelConfig{
		"gpt-4o":    {Provider: "openai"},
		"claude-3":  {Provider: "anthropic"},
		"o1":        {Provider: "openai"},
	}

	entries := cfg.ListModels()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// First should be the anthropic one
	if entries[0].ProviderType != "anthropic" {
		t.Errorf("expected first entry to be anthropic, got %q", entries[0].ProviderType)
	}
}

// TestSetupProvider_AddsNewProvider verifies that calling SetupProvider
// for a brand-new provider adds it to the config and its models to the
// routing table.
func TestSetupProvider_AddsNewProvider(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	cfg := DefaultConfig()
	result, err := SetupProvider(cfg, "testprov", "openai", "https://api.test.com/v1", "sk-key", []string{"test-model"}, 8080)
	if err != nil {
		t.Fatalf("SetupProvider failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ProviderAdded != "testprov" {
		t.Errorf("expected ProviderAdded 'testprov', got %q", result.ProviderAdded)
	}
	if len(result.ModelsAdded) != 1 || result.ModelsAdded[0] != "test-model" {
		t.Errorf("expected ModelsAdded [test-model], got %v", result.ModelsAdded)
	}
	prov, ok := cfg.Providers["testprov"]
	if !ok {
		t.Fatal("expected provider in config")
	}
	if prov.Type != "openai" {
		t.Errorf("expected type 'openai', got %q", prov.Type)
	}
	mc, ok := cfg.Models["test-model"]
	if !ok {
		t.Fatal("expected model in routing table")
	}
	if mc.Provider != "testprov" {
		t.Errorf("expected model provider 'testprov', got %q", mc.Provider)
	}
	if mc.ModelOverride != "test-model" {
		t.Errorf("expected model override 'test-model' (openai), got %q", mc.ModelOverride)
	}

	// Verify Claude settings were written
	settings, err := LoadClaudeSettings(result.ClaudeSettings)
	if err != nil {
		t.Fatalf("failed to read Claude settings: %v", err)
	}
	if settings.Env["ANTHROPIC_BASE_URL"] != "http://localhost:8080" {
		t.Errorf("expected ANTHROPIC_BASE_URL in Claude settings, got %q", settings.Env["ANTHROPIC_BASE_URL"])
	}
}

// TestSetupProvider_UpdatesExistingProvider verifies that calling
// SetupProvider for an existing provider updates the API key (and leaves
// other fields alone when baseURL is empty).
func TestSetupProvider_UpdatesExistingProvider(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "old-key",
		BaseURL: "https://original.example.com/v1",
	}

	_, err := SetupProvider(cfg, "p", "openai", "", "new-key", nil, 9000)
	if err != nil {
		t.Fatalf("SetupProvider failed: %v", err)
	}
	if cfg.Providers["p"].APIKey != "new-key" {
		t.Errorf("expected API key 'new-key', got %q", cfg.Providers["p"].APIKey)
	}
	if cfg.Providers["p"].BaseURL != "https://original.example.com/v1" {
		t.Errorf("expected original base URL preserved, got %q", cfg.Providers["p"].BaseURL)
	}
}
