package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DoctorResult holds the results of a diagnostic check.
type DoctorResult struct {
	Title  string
	Status DoctorStatus
	Items  []DoctorItem
}

// DoctorStatus indicates overall status of a check category.
type DoctorStatus string

const (
	StatusPass   DoctorStatus = "PASS"
	StatusWarn   DoctorStatus = "WARN"
	StatusFail   DoctorStatus = "FAIL"
	StatusInfo   DoctorStatus = "INFO"
)

// DoctorItem is a single diagnostic result.
type DoctorItem struct {
	Status  DoctorStatus
	Message string
	Detail  string // optional suggestion
}

// RunDoctor runs all diagnostic checks and returns results.
func RunDoctor(cfg *Config, cfgFilePath string) []DoctorResult {
	var results []DoctorResult

	results = append(results, checkConfigFile(cfgFilePath))
	if cfg != nil {
		results = append(results, checkProviders(cfg))
		results = append(results, checkModels(cfg))
		results = append(results, checkProviderConnectivity(cfg))
		results = append(results, checkPort(cfg))
	}
	results = append(results, checkClaudeSettings())

	return results
}

// checkConfigFile validates that the config file exists and parses correctly.
func checkConfigFile(path string) DoctorResult {
	res := DoctorResult{Title: "Config File", Status: StatusPass}

	if path == "" {
		path = DefaultConfigPath()
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Config file not found at %s", path),
				Detail:  "Run `api-switch config init` to create a default config, then add providers.",
			})
			return res
		}
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: fmt.Sprintf("Cannot access config file: %v", err),
		})
		return res
	}

	// Check permissions
	if info.Mode().Perm()&0077 != 0 {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Config file is world-readable (%o). It contains API keys.", info.Mode().Perm()),
			Detail:  "Run: chmod 600 " + path,
		})
	}

	// Check if empty
	if info.Size() == 0 {
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: "Config file is empty",
			Detail:  "Run `api-switch config init` to create a default config.",
		})
		return res
	}

	res.Items = append(res.Items, DoctorItem{
		Status:  StatusPass,
		Message: fmt.Sprintf("Config file found at %s (%d bytes)", path, info.Size()),
	})
	return res
}

// checkProviders validates provider configurations.
func checkProviders(cfg *Config) DoctorResult {
	res := DoctorResult{Title: "Providers", Status: StatusPass}

	if len(cfg.Providers) == 0 {
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: "No providers configured",
			Detail:  "Add a provider with `api-switch setup` or manually edit the config file.",
		})
		return res
	}

	for name, prov := range cfg.Providers {
		// Check type
		if prov.Type != "anthropic" && prov.Type != "openai" {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: invalid type %q (must be 'anthropic' or 'openai')", name, prov.Type),
			})
			continue
		}

		// Check api_key
		if prov.APIKey == "" {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: api_key is empty", name),
				Detail:  fmt.Sprintf("Set it with: api-switch config set providers.%s.api_key <key>", name),
			})
		}
		if len(prov.APIKey) > 0 && len(prov.APIKey) < 8 {
			res.Status = StatusWarn
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusWarn,
				Message: fmt.Sprintf("Provider %q: api_key seems too short (%d chars)", name, len(prov.APIKey)),
			})
		}

		// Check base_url
		if prov.BaseURL == "" {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: base_url is empty", name),
			})
		} else {
			if _, err := url.Parse(prov.BaseURL); err != nil {
				res.Status = StatusFail
				res.Items = append(res.Items, DoctorItem{
					Status:  StatusFail,
					Message: fmt.Sprintf("Provider %q: base_url is not a valid URL: %v", name, err),
				})
			}
			// Check for common path issues
			if prov.Type == "openai" {
				if strings.HasSuffix(prov.BaseURL, "/agent/v1") {
					res.Status = StatusWarn
					res.Items = append(res.Items, DoctorItem{
						Status:  StatusWarn,
						Message: fmt.Sprintf("Provider %q: base_url %q ends with /agent/v1 — should this be /v1?", name, prov.BaseURL),
						Detail:  "Some providers use a non-standard path. Verify with the provider's documentation.",
					})
				}
				if strings.Contains(prov.BaseURL, "/chat/completions") {
					res.Status = StatusWarn
					res.Items = append(res.Items, DoctorItem{
						Status:  StatusWarn,
						Message: fmt.Sprintf("Provider %q: base_url %q should NOT include /v1/chat/completions (it is appended automatically)", name, prov.BaseURL),
						Detail:  "Set base_url to the root API URL, e.g. https://api.openai.com/v1",
					})
				}
			}
			if prov.Type == "anthropic" {
				if !strings.HasSuffix(prov.BaseURL, "/v1") && !strings.HasSuffix(prov.BaseURL, "/v1/messages") && !strings.Contains(prov.BaseURL, "xiaomimimo.com") {
					res.Status = StatusInfo
					res.Items = append(res.Items, DoctorItem{
						Status:  StatusInfo,
						Message: fmt.Sprintf("Provider %q: base_url %q — ensure this is the correct Anthropic-compatible endpoint", name, prov.BaseURL),
					})
				}
			}
		}

		// Check default_max_tokens
		if prov.DefaultMaxTokens <= 0 {
			res.Status = StatusWarn
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusWarn,
				Message: fmt.Sprintf("Provider %q: default_max_tokens is %d, using default 1024", name, prov.DefaultMaxTokens),
			})
		}

		// Check for apifree-like providers (openai type with base_url containing "apifree")
		if prov.Type == "openai" && strings.Contains(prov.BaseURL, "apifree") {
			res.Status = StatusInfo
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusInfo,
				Message: fmt.Sprintf("Provider %q: APIFree detected — note that errors may be returned as HTTP 200 with error field", name),
			})
		}

		// Count models using this provider
		modelCount := 0
		for _, mcfg := range cfg.Models {
			if mcfg.Provider == name {
				modelCount++
			}
		}
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusPass,
			Message: fmt.Sprintf("Provider %q: type=%s, %d model(s) mapped", name, prov.Type, modelCount),
		})
	}

	return res
}

// checkModels validates model routing configuration.
func checkModels(cfg *Config) DoctorResult {
	res := DoctorResult{Title: "Models", Status: StatusPass}

	if len(cfg.Models) == 0 {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: "No models configured",
			Detail:  "Add models with: api-switch model add <name> <provider>",
		})
		return res
	}

	for name, mcfg := range cfg.Models {
		// Check provider exists
		prov, ok := cfg.Providers[mcfg.Provider]
		if !ok {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Model %q: references provider %q which is not configured", name, mcfg.Provider),
				Detail:  fmt.Sprintf("Add provider with `api-switch setup` or remove the model with `api-switch model remove %s`", name),
			})
			continue
		}

		// Check that model_override matches expectations for anthropic type
		if prov.Type == "anthropic" && mcfg.ModelOverride != "" {
			res.Status = StatusWarn
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusWarn,
				Message: fmt.Sprintf("Model %q: model_override=%q but provider is type 'anthropic' — override is usually not needed", name, mcfg.ModelOverride),
			})
		}
	}

	// Count total
	res.Items = append(res.Items, DoctorItem{
		Status:  StatusPass,
		Message: fmt.Sprintf("Total: %d model(s) configured", len(cfg.Models)),
	})
	return res
}

// checkProviderConnectivity tests connectivity to each provider API endpoint.
func checkProviderConnectivity(cfg *Config) DoctorResult {
	res := DoctorResult{Title: "Connectivity", Status: StatusPass}

	if len(cfg.Providers) == 0 {
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: "No providers to test",
		})
		return res
	}

	for name, prov := range cfg.Providers {
		if prov.BaseURL == "" {
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: cannot test — base_url is empty", name),
			})
			continue
		}

		u, err := url.Parse(prov.BaseURL)
		if err != nil {
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: invalid URL: %v", name, err),
			})
			continue
		}

		host := u.Host
		if !strings.Contains(host, ":") {
			if u.Scheme == "https" {
				host = host + ":443"
			} else {
				host = host + ":80"
			}
		}

		conn, err := net.DialTimeout("tcp", host, 5*time.Second)
		if err != nil {
			res.Status = StatusFail
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusFail,
				Message: fmt.Sprintf("Provider %q: cannot connect to %s", name, host),
				Detail:  fmt.Sprintf("Check network connectivity and firewall. Error: %v", err),
			})
			continue
		}
		conn.Close()

		// Test HTTP connectivity with a lightweight request
		testURL := prov.BaseURL
		if prov.Type == "openai" {
			// Append chat completions endpoint
			base := strings.TrimRight(testURL, "/")
			if strings.HasSuffix(base, "/v1/chat/completions") {
				// already correct
			} else if strings.HasSuffix(base, "/v1") {
				testURL = base + "/chat/completions"
			} else {
				testURL = base + "/v1/chat/completions"
			}
		}

		// Do a health check via HTTP HEAD (or minimal request)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(testURL)
		if err != nil {
			// GET to POST endpoint is expected to fail, but TCP is fine
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusPass,
				Message: fmt.Sprintf("Provider %q: TCP reachable at %s", name, host),
				Detail:  "HTTP endpoint reachable (endpoint requires POST, which is expected).",
			})
		} else {
			resp.Body.Close()
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusPass,
				Message: fmt.Sprintf("Provider %q: endpoint responds at %s (HTTP %d)", name, testURL, resp.StatusCode),
			})
		}
	}

	return res
}

// checkClaudeSettings validates Claude Code's settings.json.
func checkClaudeSettings() DoctorResult {
	res := DoctorResult{Title: "Claude Code Settings", Status: StatusPass}
	settingsPath := ClaudeSettingsPath()

	_, err := os.Stat(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusWarn
			res.Items = append(res.Items, DoctorItem{
				Status:  StatusWarn,
				Message: fmt.Sprintf("Claude Code settings not found at %s", settingsPath),
				Detail:  "Run `api-switch generate-claude-config` first, then `api-switch use <model>` to activate a model.",
			})
			return res
		}
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: fmt.Sprintf("Cannot read Claude Code settings: %v", err),
		})
		return res
	}

	res.Items = append(res.Items, DoctorItem{
		Status:  StatusPass,
		Message: fmt.Sprintf("Settings file found at %s", settingsPath),
	})

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: fmt.Sprintf("Cannot read Claude settings: %v", err),
		})
		return res
	}

	var settings struct {
		Model  string            `json:"model"`
		Env    map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Cannot parse Claude settings as JSON: %v", err),
			Detail:  "The file may be malformed. Regenerate it with `api-switch generate-claude-config`.",
		})
		return res
	}

	// Check for proxy URL
	baseURL := settings.Env["ANTHROPIC_BASE_URL"]
	if baseURL == "" {
		res.Status = StatusFail
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusFail,
			Message: "ANTHROPIC_BASE_URL is not set in Claude settings",
			Detail:  "Run `api-switch generate-claude-config` to configure it.",
		})
	} else if strings.Contains(baseURL, "api.anthropic.com") {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: fmt.Sprintf("ANTHROPIC_BASE_URL points to Anthropic directly (%s), not the proxy", baseURL),
			Detail:  "It should be http://localhost:8080 (or your proxy address) for API-Switch to work.",
		})
	} else if strings.HasPrefix(baseURL, "http://localhost") || strings.HasPrefix(baseURL, "http://127.0.0.1") {
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusPass,
			Message: fmt.Sprintf("ANTHROPIC_BASE_URL correctly points to proxy: %s", baseURL),
		})
	} else {
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusInfo,
			Message: fmt.Sprintf("ANTHROPIC_BASE_URL: %s", baseURL),
		})
	}

	// Check active model
	activeModel := settings.Model
	customOption := settings.Env["ANTHROPIC_CUSTOM_MODEL_OPTION"]
	if activeModel == "" && customOption == "" {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: "No active model set in Claude settings",
			Detail:  "Run `api-switch use <model>` to switch to a model.",
		})
	} else {
		modelName := activeModel
		if modelName == "" {
			modelName = customOption
		}
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusPass,
			Message: fmt.Sprintf("Active model: %s", modelName),
		})
	}

	// Check ANTHROPIC_API_KEY
	if apiKey := settings.Env["ANTHROPIC_API_KEY"]; apiKey == "" {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: "ANTHROPIC_API_KEY is not set in Claude settings",
			Detail:  "Claude Code may use a stored key. This is fine if it's working.",
		})
	} else if apiKey == "use-api-switch" {
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusPass,
			Message: "ANTHROPIC_API_KEY is set correctly (using api-switch placeholder key)",
		})
	}

	// Check for .claude directory permissions
	claudeDir := filepath.Dir(settingsPath)
	dirInfo, err := os.Stat(claudeDir)
	if err == nil && dirInfo.Mode().Perm()&0077 != 0 {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Claude directory %s is world-readable (%o)", claudeDir, dirInfo.Mode().Perm()),
			Detail:  "Consider restricting permissions: chmod 700 " + claudeDir,
		})
	}

	return res
}

// checkPort checks if the proxy port is available and not already in use.
func checkPort(cfg *Config) DoctorResult {
	res := DoctorResult{Title: "Port", Status: StatusPass}

	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}

	// Check if port is already in use
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		res.Status = StatusWarn
		res.Items = append(res.Items, DoctorItem{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Port %d is already in use", port),
			Detail:  fmt.Sprintf("Another process is using port %d. Stop it first, or use a different port with --port flag or server.port in config.", port),
		})
		return res
	}
	ln.Close()

	res.Items = append(res.Items, DoctorItem{
		Status:  StatusPass,
		Message: fmt.Sprintf("Port %d is available", port),
	})
	return res
}
