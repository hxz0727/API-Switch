package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Claude Code settings types

// ClaudeSettings represents the Claude Code settings.json structure.
type ClaudeSettings struct {
	Schema          *string            `json:"$schema,omitempty"`
	Model           string             `json:"model,omitempty"`
	AvailableModels []string           `json:"availableModels,omitempty"`
	ModelOverrides  map[string]string  `json:"modelOverrides,omitempty"`
	Env             map[string]string  `json:"env,omitempty"`
	Permissions     *ClaudePermissions `json:"permissions,omitempty"`
}

// ClaudePermissions represents permission settings.
type ClaudePermissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// ClaudeCodeConfigDir returns the Claude Code config directory.
func ClaudeCodeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// ClaudeSettingsPath returns the path to Claude Code's user settings.json.
func ClaudeSettingsPath() string {
	return filepath.Join(ClaudeCodeConfigDir(), "settings.json")
}

// LoadClaudeSettings loads Claude Code settings from disk.
// Returns empty settings if file doesn't exist.
func LoadClaudeSettings(path string) (*ClaudeSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClaudeSettings{}, nil
		}
		return nil, fmt.Errorf("failed to read Claude settings: %w", err)
	}

	var settings ClaudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse Claude settings: %w", err)
	}
	return &settings, nil
}

// SaveClaudeSettings writes Claude Code settings to disk.
func SaveClaudeSettings(path string, settings *ClaudeSettings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create Claude config directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Claude settings: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write Claude settings: %w", err)
	}
	return nil
}

// SetupResult contains the results of a setup operation.
type SetupResult struct {
	ProviderAdded   string
	ModelsAdded     []string
	APISwitchConfig string
	ClaudeSettings  string
	ProxyURL        string
}

// SetupProvider adds a provider and its models to both API-Switch config and Claude Code settings.
func SetupProvider(cfg *Config, providerName, providerType, baseURL, apiKey string, models []string, proxyPort int) (*SetupResult, error) {
	// 1. Add provider to API-Switch config
	if _, exists := cfg.Providers[providerName]; !exists {
		cfg.Providers[providerName] = ProviderConfig{
			Type:             providerType,
			APIKey:           apiKey,
			BaseURL:          baseURL,
			DefaultMaxTokens: 1024,
		}
	} else {
		prov := cfg.Providers[providerName]
		prov.APIKey = apiKey
		if baseURL != "" {
			prov.BaseURL = baseURL
		}
		cfg.Providers[providerName] = prov
	}

	// 2. Add models to routing table
	for _, model := range models {
		modelOverride := ""
		if providerType == "openai" {
			modelOverride = model
		}
		cfg.Models[model] = ModelConfig{
			Provider:      providerName,
			ModelOverride: modelOverride,
		}
	}

	// 3. Generate Claude Code settings (only base URL + api key)
	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	claudeSettingsPath := ClaudeSettingsPath()
	claudeSettings, err := LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Claude settings: %w", err)
	}

	InitClaudeSettings(claudeSettings, proxyURL)

	if err := SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
		return nil, fmt.Errorf("failed to save Claude settings: %w", err)
	}

	return &SetupResult{
		ProviderAdded:   providerName,
		ModelsAdded:     models,
		APISwitchConfig: DefaultConfigPath(),
		ClaudeSettings:  claudeSettingsPath,
		ProxyURL:        proxyURL,
	}, nil
}

// InitClaudeSettings writes the base Claude Code settings needed for api-switch.
// Only sets ANTHROPIC_BASE_URL and ANTHROPIC_API_KEY — no model configuration.
// Model switching is done via `api-switch use <model>`.
func InitClaudeSettings(settings *ClaudeSettings, proxyURL string) {
	if settings.Schema == nil {
		schema := "https://json.schemastore.org/claude-code-settings.json"
		settings.Schema = &schema
	}

	if settings.Env == nil {
		settings.Env = make(map[string]string)
	}
	settings.Env["ANTHROPIC_BASE_URL"] = proxyURL
	if _, exists := settings.Env["ANTHROPIC_API_KEY"]; !exists {
		settings.Env["ANTHROPIC_API_KEY"] = "use-api-switch"
	}
}

// ActivateModel sets a specific model as the active model in Claude Code settings.
func ActivateModel(settings *ClaudeSettings, modelName string, providerType string) {
	settings.Model = modelName

	if settings.Env == nil {
		settings.Env = make(map[string]string)
	}

	// For non-Anthropic models, set CUSTOM_MODEL_OPTION so it appears in /model picker
	if providerType != "anthropic" {
		settings.Env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = modelName
	} else {
		delete(settings.Env, "ANTHROPIC_CUSTOM_MODEL_OPTION")
	}
}

// ListModels returns all configured models sorted by provider type then name.
func (c *Config) ListModels() []ModelEntry {
	type modelInfo struct {
		name         string
		providerName string
		providerType string
		providerURL  string
	}

	var infos []modelInfo
	for name, mcfg := range c.Models {
		prov, ok := c.Providers[mcfg.Provider]
		if !ok {
			continue
		}
		infos = append(infos, modelInfo{
			name:         name,
			providerName: mcfg.Provider,
			providerType: prov.Type,
			providerURL:  prov.BaseURL,
		})
	}

	// Sort: Claude models first, then non-Claude (alphabetically within each group)
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].providerType != infos[j].providerType {
			if infos[i].providerType == "anthropic" {
				return true
			}
			if infos[j].providerType == "anthropic" {
				return false
			}
		}
		return infos[i].name < infos[j].name
	})

	var entries []ModelEntry
	for _, info := range infos {
		marker := " "
		if info.providerType == "anthropic" {
			marker = "✓"
		}
		entries = append(entries, ModelEntry{
			Name:         info.name,
			Provider:     info.providerName,
			ProviderType: info.providerType,
			ProviderURL:  info.providerURL,
			Marker:       marker,
		})
	}
	return entries
}

// ModelEntry represents a model in the listing.
type ModelEntry struct {
	Name         string
	Provider     string
	ProviderType string
	ProviderURL  string
	Marker       string
}
