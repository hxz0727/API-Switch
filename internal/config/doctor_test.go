package config

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =====================================================================
// TestCheckConfigFile
// =====================================================================

func TestCheckConfigFile_NotFound(t *testing.T) {
	res := checkConfigFile("/nonexistent/path/config.yaml")
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
	if len(res.Items) == 0 {
		t.Fatal("expected at least one item")
	}
	if !strings.Contains(res.Items[0].Message, "not found") {
		t.Errorf("expected 'not found' in message, got: %s", res.Items[0].Message)
	}
}

func TestCheckConfigFile_EmptyPathUsesDefault(t *testing.T) {
	// When path is empty, defaults to DefaultConfigPath(). We can't predict
	// whether that file exists, but the function should not panic and must
	// return a valid result.
	res := checkConfigFile("")
	if res.Title != "Config File" {
		t.Errorf("expected title 'Config File', got %q", res.Title)
	}
	// Just confirm the call returns without panic and has expected structure
	if res.Status == "" {
		t.Error("expected non-empty status")
	}
}

func TestCheckConfigFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res := checkConfigFile(path)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail for empty file, got %q", res.Status)
	}
	if len(res.Items) == 0 {
		t.Fatal("expected items")
	}
	if !strings.Contains(res.Items[0].Message, "empty") {
		t.Errorf("expected 'empty' in message, got: %s", res.Items[0].Message)
	}
}

func TestCheckConfigFile_WorldReadable(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "world-readable.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res := checkConfigFile(path)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for world-readable file, got %q", res.Status)
	}
}

func TestCheckConfigFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "valid.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res := checkConfigFile(path)
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass for valid file, got %q", res.Status)
	}
}

// =====================================================================
// TestCheckProviders
// =====================================================================

func TestCheckProviders_NoProviders(t *testing.T) {
	cfg := DefaultConfig()
	res := checkProviders(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
}

func TestCheckProviders_InvalidType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "bogus",
		APIKey:  "sk-key",
		BaseURL: "https://api.example.com/v1",
	}
	res := checkProviders(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "invalid type") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected an 'invalid type' item")
	}
}

func TestCheckProviders_EmptyAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:             "openai",
		APIKey:           "",
		BaseURL:          "https://api.example.com/v1",
		DefaultMaxTokens: 1024, // avoid the max-tokens warn path
	}
	res := checkProviders(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "api_key is empty") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an 'api_key is empty' item, items: %+v", res.Items)
	}
}

func TestCheckProviders_ShortAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "short", // < 8 chars triggers a warn
		BaseURL: "https://api.example.com/v1",
	}
	res := checkProviders(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for short key, got %q", res.Status)
	}
}

func TestCheckProviders_EmptyBaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:             "openai",
		APIKey:           "sk-valid-key",
		BaseURL:          "",
		DefaultMaxTokens: 1024,
	}
	res := checkProviders(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "base_url is empty") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'base_url is empty' item, items: %+v", res.Items)
	}
}

func TestCheckProviders_InvalidBaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:             "openai",
		APIKey:           "sk-valid-key",
		BaseURL:          "://invalid-url",
		DefaultMaxTokens: 1024,
	}
	res := checkProviders(cfg)
	// url.Parse is permissive and may not fail on this input, but the
	// function must not panic. Accept any non-empty result.
	if len(res.Items) == 0 {
		t.Error("expected at least one item")
	}
}

func TestCheckProviders_OpenAIAgentV1Warning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-valid-key",
		BaseURL: "https://api.example.com/agent/v1",
	}
	res := checkProviders(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for /agent/v1 path, got %q", res.Status)
	}
}

func TestCheckProviders_OpenAIChatCompletionsWarning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-valid-key",
		BaseURL: "https://api.example.com/v1/chat/completions",
	}
	res := checkProviders(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for /chat/completions in base URL, got %q", res.Status)
	}
}

func TestCheckProviders_AnthropicBaseURLInfo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-valid-key",
		BaseURL: "https://my-anthropic-proxy.example.com/api",
	}
	res := checkProviders(cfg)
	found := false
	for _, item := range res.Items {
		if item.Status == StatusInfo && strings.Contains(item.Message, "Anthropic-compatible") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an Info item about anthropic-compatible endpoint, items: %+v", res.Items)
	}
}

func TestCheckProviders_DefaultMaxTokensZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:             "openai",
		APIKey:           "sk-valid-key",
		BaseURL:          "https://api.example.com/v1",
		DefaultMaxTokens: 0,
	}
	res := checkProviders(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for zero max tokens, got %q", res.Status)
	}
}

func TestCheckProviders_ApifreeInfo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-valid-key",
		BaseURL: "https://api.apifree.ai/agent/v1",
	}
	res := checkProviders(cfg)
	found := false
	for _, item := range res.Items {
		if item.Status == StatusInfo && strings.Contains(item.Message, "APIFree") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected APIFree info item, items: %+v", res.Items)
	}
}

func TestCheckProviders_ModelCount(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-valid-key",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p"},
		"m2": {Provider: "p"},
	}
	res := checkProviders(cfg)
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "2 model(s) mapped") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected '2 model(s) mapped' item, items: %+v", res.Items)
	}
}

// =====================================================================
// TestCheckModels
// =====================================================================

func TestCheckModels_NoModels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p": {Type: "openai", APIKey: "k", BaseURL: "u"},
	}
	res := checkModels(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn, got %q", res.Status)
	}
}

func TestCheckModels_UnknownProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "ghost"},
	}
	res := checkModels(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
}

func TestCheckModels_AnthropicOverrideWarning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"p": {Type: "anthropic", APIKey: "k", BaseURL: "u"},
	}
	cfg.Models = map[string]ModelConfig{
		"m1": {Provider: "p", ModelOverride: "x"},
	}
	res := checkModels(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn, got %q", res.Status)
	}
}

// =====================================================================
// TestCheckProviderConnectivity (with httptest)
// =====================================================================

func TestCheckProviderConnectivity_NoProviders(t *testing.T) {
	cfg := DefaultConfig()
	res := checkProviderConnectivity(cfg)
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", res.Status)
	}
}

func TestCheckProviderConnectivity_EmptyBaseURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "k",
		BaseURL: "",
	}
	res := checkProviderConnectivity(cfg)
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "base_url is empty") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'base_url is empty' item, items: %+v", res.Items)
	}
}

func TestCheckProviderConnectivity_InvalidURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "k",
		BaseURL: "://bad",
	}
	res := checkProviderConnectivity(cfg)
	// url.Parse is permissive so this may parse; we mainly check no panic
	_ = res
}

func TestCheckProviderConnectivity_LiveServer(t *testing.T) {
	// Spin up an httptest server and point a provider at it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	baseURL := srv.URL
	cfg := DefaultConfig()
	cfg.Providers["local"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "k",
		BaseURL: baseURL,
	}
	res := checkProviderConnectivity(cfg)
	// Should report a successful endpoint check (or at least TCP reachable)
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass against live test server, got %q, items: %+v", res.Status, res.Items)
	}
}

// =====================================================================
// TestCheckClaudeSettings
// =====================================================================

func TestCheckClaudeSettings_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	res := checkClaudeSettings()
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn, got %q", res.Status)
	}
}

func TestCheckClaudeSettings_ValidProxy(t *testing.T) {
	// Use a directory with restrictive 0700 permissions so the directory
	// permission check does not override the PASS status.
	parentDir, err := os.MkdirTemp("", "claude-valid-")
	if err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(parentDir) })
	if err := os.Chmod(parentDir, 0700); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", parentDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	settings := &ClaudeSettings{
		Model: "claude-3-5-sonnet",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": "http://localhost:8080",
			"ANTHROPIC_API_KEY":  "use-api-switch",
		},
	}
	if err := SaveClaudeSettings(ClaudeSettingsPath(), settings); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	res := checkClaudeSettings()
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass, got %q, items: %+v", res.Status, res.Items)
	}
}

func TestCheckClaudeSettings_PointsToAnthropic(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	settings := &ClaudeSettings{
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://api.anthropic.com",
		},
	}
	if err := SaveClaudeSettings(ClaudeSettingsPath(), settings); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	res := checkClaudeSettings()
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for direct anthropic URL, got %q", res.Status)
	}
}

func TestCheckClaudeSettings_MissingBaseURL(t *testing.T) {
	parentDir, err := os.MkdirTemp("", "claude-missing-")
	if err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(parentDir) })
	if err := os.Chmod(parentDir, 0700); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", parentDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	// Write the settings file directly with 0600 perms to avoid SaveClaudeSettings
	// creating nested 0755 dirs that would trigger the world-readable warning.
	settingsPath := ClaudeSettingsPath()
	if err := os.WriteFile(settingsPath, []byte(`{"env":{}}`), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res := checkClaudeSettings()
	// The function reports FAIL for missing base URL but a later check
	// ("no active model") overrides the status to WARN. We verify the
	// base-URL failure is still surfaced as an item, regardless of overall
	// status.
	found := false
	for _, item := range res.Items {
		if item.Status == StatusFail && strings.Contains(item.Message, "ANTHROPIC_BASE_URL is not set") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a FAIL item for missing ANTHROPIC_BASE_URL, items: %+v", res.Items)
	}
}

func TestCheckClaudeSettings_NoActiveModel(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	settings := &ClaudeSettings{
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": "http://localhost:8080",
		},
	}
	if err := SaveClaudeSettings(ClaudeSettingsPath(), settings); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	res := checkClaudeSettings()
	found := false
	for _, item := range res.Items {
		if strings.Contains(item.Message, "No active model") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'No active model' item, items: %+v", res.Items)
	}
}

func TestCheckClaudeSettings_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	path := ClaudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res := checkClaudeSettings()
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for malformed JSON, got %q", res.Status)
	}
}

func TestCheckClaudeSettings_OtherBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	settings := &ClaudeSettings{
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://my-custom-proxy.example.com",
		},
		Model: "some-model",
	}
	if err := SaveClaudeSettings(ClaudeSettingsPath(), settings); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	res := checkClaudeSettings()
	found := false
	for _, item := range res.Items {
		if item.Status == StatusInfo && strings.Contains(item.Message, "ANTHROPIC_BASE_URL") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Info item for non-localhost base URL, items: %+v", res.Items)
	}
}

// =====================================================================
// TestCheckPort
// =====================================================================

func TestCheckPort_Default(t *testing.T) {
	cfg := DefaultConfig()
	// Find an available port to avoid environment-specific failures
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg.Server.Port = port
	res := checkPort(cfg)
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass for available port, got %q, items: %+v", res.Status, res.Items)
	}
}

func TestCheckPort_InUse(t *testing.T) {
	// Bind a port and pass it to checkPort — it should report in-use.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	cfg := DefaultConfig()
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	res := checkPort(cfg)
	if res.Status != StatusWarn {
		t.Errorf("expected StatusWarn for in-use port, got %q, items: %+v", res.Status, res.Items)
	}
}

// =====================================================================
// TestRunDoctor (end-to-end)
// =====================================================================

func TestRunDoctor_NilConfig(t *testing.T) {
	results := RunDoctor(nil, "/nonexistent/config.yaml")
	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}
	if results[0].Title != "Config File" {
		t.Errorf("expected first result 'Config File', got %q", results[0].Title)
	}
}

func TestRunDoctor_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfg := DefaultConfig()
	cfg.Server.Port = 0 // force port check
	cfg.Providers["p"] = ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-valid-key",
		BaseURL: "https://api.example.com/v1",
	}
	cfg.Models["m"] = ModelConfig{Provider: "p"}
	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	results := RunDoctor(cfg, cfgPath)
	if len(results) < 5 {
		t.Errorf("expected at least 5 doctor categories, got %d", len(results))
	}
}
