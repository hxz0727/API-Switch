package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Models    map[string]ModelConfig    `yaml:"models"`
	Providers map[string]ProviderConfig `yaml:"providers"`
}

// ServerConfig represents server configuration.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// ModelConfig defines which provider a model uses and optional model name override.
type ModelConfig struct {
	Provider string `yaml:"provider"`
	// ModelOverride is the actual model name sent to the provider.
	// If empty, the key (model name in the request) is used as-is.
	ModelOverride string `yaml:"model_override,omitempty"`
}

// ProviderConfig represents a provider's configuration.
type ProviderConfig struct {
	Type             string `yaml:"type"` // "anthropic" or "openai"
	APIKey           string `yaml:"api_key"`
	BaseURL          string `yaml:"base_url"`
	APIVersion       string `yaml:"api_version,omitempty"`
	DefaultMaxTokens int    `yaml:"default_max_tokens"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Models:    map[string]ModelConfig{},
		Providers: map[string]ProviderConfig{},
	}
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".api-switch.yaml"
	}
	return filepath.Join(home, ".api-switch.yaml")
}

// Load reads the config file from the given path.
// If the file does not exist, returns default config.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Save writes the config to the given path.
func Save(path string, cfg *Config) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate checks that the config is usable.
func (c *Config) Validate() error {
	for modelName, modelCfg := range c.Models {
		prov, ok := c.Providers[modelCfg.Provider]
		if !ok {
			return fmt.Errorf("model %q references unknown provider %q", modelName, modelCfg.Provider)
		}
		if prov.APIKey == "" {
			return fmt.Errorf("API key for provider %q (used by model %q) is not set; use `api-switch config set %s.api_key <key>`", modelCfg.Provider, modelName, modelCfg.Provider)
		}
	}
	return nil
}

// RouteModel returns the provider name, provider config, and actual model name for a given model.
// If the model is not in the routing table, it returns an error.
func (c *Config) RouteModel(model string) (providerName string, provCfg *ProviderConfig, actualModel string, err error) {
	modelCfg, ok := c.Models[model]
	if !ok {
		return "", nil, "", fmt.Errorf("model %q not found in routing table; add it to config or use a configured model name", model)
	}

	prov, ok := c.Providers[modelCfg.Provider]
	if !ok {
		return "", nil, "", fmt.Errorf("provider %q not found for model %q", modelCfg.Provider, model)
	}

	actualModel = model
	if modelCfg.ModelOverride != "" {
		actualModel = modelCfg.ModelOverride
	}

	return modelCfg.Provider, &prov, actualModel, nil
}

// YamlMarshal marshals the config to YAML bytes.
func YamlMarshal(cfg *Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// SetValue sets a dotted key in the config.
func SetValue(cfg *Config, key, value string) error {
	parts := strings.SplitN(key, ".", 3)
	switch len(parts) {
	case 2:
		// e.g. providers.anthropic or models.gpt-4o
		switch parts[0] {
		case "server":
			switch parts[1] {
			case "port":
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid integer for %s: %s", key, value)
				}
				cfg.Server.Port = n
			default:
				return fmt.Errorf("unknown config key: %s", key)
			}
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
	case 3:
		switch parts[0] {
		case "providers":
			provName := parts[1]
			prov, ok := cfg.Providers[provName]
			if !ok {
				prov = ProviderConfig{}
			}
			switch parts[2] {
			case "api_key":
				prov.APIKey = value
			case "base_url":
				prov.BaseURL = value
			case "api_version":
				prov.APIVersion = value
			case "type":
				prov.Type = value
			case "default_max_tokens":
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid integer for %s: %s", key, value)
				}
				prov.DefaultMaxTokens = n
			default:
				return fmt.Errorf("unknown config key: %s", key)
			}
			cfg.Providers[provName] = prov
		case "models":
			return fmt.Errorf("use `api-switch model add <name> <provider>` to manage models")
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
	default:
		return fmt.Errorf("invalid config key format: %s; use providers.<name>.<field>", key)
	}
	return nil
}

// SetProviderValue sets a provider config value by dotted path.
func SetProviderValue(cfg *Config, key, value string) error {
	// Format: providers.<name>.<field>
	parts := strings.SplitN(key, ".", 3)
	if len(parts) != 3 || parts[0] != "providers" {
		return fmt.Errorf("expected format providers.<name>.<field>, got: %s", key)
	}

	provName := parts[1]
	prov, ok := cfg.Providers[provName]
	if !ok {
		prov = ProviderConfig{}
	}

	switch parts[2] {
	case "api_key":
		prov.APIKey = value
	case "base_url":
		prov.BaseURL = value
	case "api_version":
		prov.APIVersion = value
	case "type":
		prov.Type = value
	case "default_max_tokens":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer: %s", value)
		}
		prov.DefaultMaxTokens = n
	default:
		return fmt.Errorf("unknown provider field: %s", parts[2])
	}

	cfg.Providers[provName] = prov
	return nil
}

// ProviderTemplate defines the preset for a known provider.
type ProviderTemplate struct {
	Type             string
	BaseURL          string
	DefaultMaxTokens int
	Models           []string
}

// KnownProviders returns a map of known provider presets.
func KnownProviders() map[string]ProviderTemplate {
	return map[string]ProviderTemplate{
		"deepseek": {
			Type:    "openai",
			BaseURL: "https://api.deepseek.com",
			DefaultMaxTokens: 8192,
			Models: []string{"deepseek-chat", "deepseek-coder"},
		},
		"moonshot": {
			Type:    "openai",
			BaseURL: "https://api.moonshot.cn/v1",
			DefaultMaxTokens: 4096,
			Models: []string{"moonshot-v1-8k", "moonshot-v1-32k"},
		},
		"qwen": {
			Type:    "openai",
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			DefaultMaxTokens: 8192,
			Models: []string{"qwen-plus", "qwen-max", "qwen-turbo"},
		},
		"glm": {
			Type:    "openai",
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			DefaultMaxTokens: 4096,
			Models: []string{"glm-4-flash", "glm-4-plus"},
		},
		"kimi": {
			Type:    "openai",
			BaseURL: "https://api.moonshot.cn/v1",
			DefaultMaxTokens: 4096,
			Models: []string{"kimi-latest"},
		},
		"yi": {
			Type:    "openai",
			BaseURL: "https://api.lingyiwanwu.com/v1",
			DefaultMaxTokens: 4096,
			Models: []string{"yi-lightning", "yi-medium"},
		},
		"step": {
			Type:    "openai",
			BaseURL: "https://api.stepfun.com/v1",
			DefaultMaxTokens: 8192,
			Models: []string{"step-1-8k", "step-1-32k"},
		},
		"ernie": {
			Type:    "openai",
			BaseURL: "https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat",
			DefaultMaxTokens: 2048,
			Models: []string{"ernie-4.0", "ernie-3.5"},
		},
		"hunyuan": {
			Type:    "openai",
			BaseURL: "https://api.hunyuan.cloud.tencent.com/v1",
			DefaultMaxTokens: 4096,
			Models: []string{"hunyuan-lite", "hunyuan-standard"},
		},
	}
}
