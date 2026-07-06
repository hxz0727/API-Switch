package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/config"
)

// runUse handles the use command.
func runUse(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	// No arguments: list all models
	if len(args) == 0 {
		if len(cfg.Models) == 0 {
			fmt.Println("No models configured")
			fmt.Println()
			fmt.Println("Add a model:")
			fmt.Println("  api-switch model add <name> <provider>")
			return nil
		}

		fmt.Println("Configured models:")
		for name, modelCfg := range cfg.Models {
			override := ""
			if modelCfg.ModelOverride != "" {
				override = fmt.Sprintf(" → %s", modelCfg.ModelOverride)
			}
			fmt.Printf("  %s (%s)%s\n", name, modelCfg.Provider, override)
		}

		// Show current active model
		claudeSettingsPath := config.ClaudeSettingsPath()
		claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
		if err == nil && claudeSettings != nil && claudeSettings.Env != nil {
			if model, ok := claudeSettings.Env["ANTHROPIC_MODEL"]; ok && model != "" {
				fmt.Println()
				fmt.Printf("Active: %s\n", model)
			}
		}

		return nil
	}

	modelName := args[0]

	// Validate model exists
	if _, ok := cfg.Models[modelName]; !ok {
		// Check if it's a known model from a template
		known := config.KnownProviders()
		found := false
		for provName, tmpl := range known {
			for _, m := range tmpl.Models {
				if m == modelName {
					// Check if provider is configured
					if _, exists := cfg.Providers[provName]; !exists {
						return fmt.Errorf("model %q requires provider %q which is not configured.\nRun: api-switch provider add %s --key <key>", modelName, provName, provName)
					}
					// Add model
					cfg.Models[modelName] = config.ModelConfig{Provider: provName}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return fmt.Errorf("model %q not found in config or known presets.\nAdd it with: api-switch model add %s <provider>", modelName, modelName)
		}
	}

	// Update Claude Code settings
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		return fmt.Errorf("cannot load Claude settings: %w", err)
	}

	if claudeSettings.Env == nil {
		claudeSettings.Env = make(map[string]string)
	}

	// Set the model
	claudeSettings.Env["ANTHROPIC_MODEL"] = modelName

	// Ensure proxy URL is set
	proxyURL := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	if claudeSettings.Env["ANTHROPIC_BASE_URL"] != proxyURL {
		claudeSettings.Env["ANTHROPIC_BASE_URL"] = proxyURL
	}

	if err := config.SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
		return fmt.Errorf("cannot save Claude settings: %w", err)
	}

	fmt.Printf("Switched to model: %s\n", modelName)
	fmt.Println("Claude Code will hot-reload the settings automatically.")

	return nil
}

// runSetup handles the setup command.
func runSetup(cmd *cobra.Command, args []string) error {
	// Get flags
	name, _ := cmd.Flags().GetString("name")
	provType, _ := cmd.Flags().GetString("type")
	url, _ := cmd.Flags().GetString("url")
	key, _ := cmd.Flags().GetString("key")
	models, _ := cmd.Flags().GetString("models")

	// If positional arg provided, use it as name
	if len(args) > 0 {
		name = args[0]
	}

	if name == "" {
		return fmt.Errorf("provider name required")
	}
	if key == "" {
		return fmt.Errorf("--key is required")
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	// Check if it's a known provider
	known := config.KnownProviders()
	var provCfg config.ProviderConfig

	if tmpl, ok := known[name]; ok {
		// Use template
		provCfg = config.ProviderConfig{
			Type:             tmpl.Type,
			APIKey:           key,
			BaseURL:          tmpl.BaseURL,
			DefaultMaxTokens: tmpl.DefaultMaxTokens,
		}
		fmt.Printf("Using preset for %s\n", name)
	} else {
		// Custom provider
		if provType == "" {
			return fmt.Errorf("--type is required for custom providers (anthropic or openai)")
		}
		if url == "" {
			return fmt.Errorf("--url is required for custom providers")
		}
		provCfg = config.ProviderConfig{
			Type:    provType,
			APIKey:  key,
			BaseURL: url,
		}
	}

	// Override port if specified
	p := port
	if p == 0 {
		p = 8080
	}

	// Save provider
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	cfg.Providers[name] = provCfg

	// Add models
	var modelList []string
	if models != "" {
		modelList = splitModels(models)
	} else if tmpl, ok := known[name]; ok {
		modelList = tmpl.Models
	}

	if len(modelList) > 0 {
		if cfg.Models == nil {
			cfg.Models = make(map[string]config.ModelConfig)
		}
		for _, m := range modelList {
			cfg.Models[m] = config.ModelConfig{Provider: name}
		}
	}

	// Set port
	cfg.Server.Port = p

	// Save config
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Provider %q configured successfully\n", name)
	fmt.Printf("  Type:     %s\n", provCfg.Type)
	fmt.Printf("  Base URL: %s\n", provCfg.BaseURL)
	fmt.Printf("  API Key:  %s\n", maskKey(key))
	if len(modelList) > 0 {
		fmt.Printf("  Models:   %s\n", strings.Join(modelList, ", "))
	}
	fmt.Printf("  Port:     %d\n", p)

	// Update Claude Code settings
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		// Create new settings
		claudeSettings = &config.ClaudeSettings{}
	}
	if claudeSettings.Env == nil {
		claudeSettings.Env = make(map[string]string)
	}

	proxyURL := fmt.Sprintf("http://localhost:%d", p)
	claudeSettings.Env["ANTHROPIC_BASE_URL"] = proxyURL
	if len(modelList) > 0 {
		claudeSettings.Env["ANTHROPIC_MODEL"] = modelList[0]
	}

	if err := config.SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
		return fmt.Errorf("failed to save Claude settings: %w", err)
	}

	fmt.Println()
	fmt.Println("Claude Code settings updated:")
	fmt.Printf("  ANTHROPIC_BASE_URL: %s\n", proxyURL)
	if len(modelList) > 0 {
		fmt.Printf("  ANTHROPIC_MODEL:    %s\n", modelList[0])
	}
	fmt.Println()
	fmt.Println("Ready! Start the proxy with: api-switch serve")

	return nil
}

// runTest handles the test command.
func runTest(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("model name required")
	}
	modelName := args[0]

	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	modelCfg, ok := cfg.Models[modelName]
	if !ok {
		return fmt.Errorf("model %q not configured", modelName)
	}

	provCfg, ok := cfg.Providers[modelCfg.Provider]
	if !ok {
		return fmt.Errorf("provider %q not configured", modelCfg.Provider)
	}

	fmt.Printf("Testing model %q via provider %q...\n", modelName, modelCfg.Provider)

	// Create a minimal test request
	testReq := map[string]interface{}{
		"model":      modelName,
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say 'ok'"},
		},
	}

	reqBody, _ := json.Marshal(testReq)

	// Send to local proxy
	proxyURL := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	req, err := http.NewRequest("POST", proxyURL+"/v1/messages", strings.NewReader(string(reqBody)))
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if provCfg.APIKey != "" {
		req.Header.Set("x-api-key", provCfg.APIKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w\nMake sure the proxy is running: api-switch serve", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response status: %d\n", resp.StatusCode)

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Test passed!")
	} else {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		fmt.Printf("Error: %v\n", errResp)
	}

	return nil
}
