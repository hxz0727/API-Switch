package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/provider"
)

// runProviderList handles the provider list command.
func runProviderList(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured")
		fmt.Println()
		fmt.Println("Add a provider:")
		fmt.Println("  api-switch provider add deepseek --key sk-xxx")
		return nil
	}

	fmt.Println("Configured providers:")
	fmt.Println()
	for name, prov := range cfg.Providers {
		fmt.Printf("  %s\n", name)
		fmt.Printf("    Type:     %s\n", prov.Type)
		fmt.Printf("    Base URL: %s\n", prov.BaseURL)
		fmt.Printf("    API Key:  %s\n", maskKey(prov.APIKey))
		if prov.DefaultMaxTokens > 0 {
			fmt.Printf("    Max Tokens: %d\n", prov.DefaultMaxTokens)
		}
		fmt.Println()
	}

	return nil
}

// runProviderKnown handles the provider known command.
func runProviderKnown(cmd *cobra.Command, args []string) error {
	known := config.KnownProviders()

	fmt.Println("Known provider presets:")
	fmt.Println()

	// Sort for consistent output
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tmpl := known[name]
		fmt.Printf("  %s\n", name)
		fmt.Printf("    Type:     %s\n", tmpl.Type)
		fmt.Printf("    Base URL: %s\n", tmpl.BaseURL)
		if tmpl.DefaultMaxTokens > 0 {
			fmt.Printf("    Max Tokens: %d\n", tmpl.DefaultMaxTokens)
		}
		if len(tmpl.Models) > 0 {
			fmt.Printf("    Models:   %s\n", strings.Join(tmpl.Models, ", "))
		}
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  api-switch provider add <name> --key <api-key>")
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  api-switch provider add deepseek --key sk-xxx")

	return nil
}

// runProviderTest handles the provider test command.
func runProviderTest(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("provider name required")
	}
	providerName := args[0]

	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	provCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}

	fmt.Printf("Testing provider %q...\n", providerName)
	fmt.Printf("  Type: %s\n", provCfg.Type)
	fmt.Printf("  URL:  %s\n", provCfg.BaseURL)

	// For OpenAI-compatible providers, test with a simple models list request
	if provCfg.Type == "openai" {
		client := provider.NewOpenAIClient(&provCfg)
		models, err := client.ListModels()
		if err != nil {
			fmt.Printf("  Status: FAILED\n")
			fmt.Printf("  Error: %v\n", err)
			return nil
		}
		fmt.Printf("  Status: OK\n")
		fmt.Printf("  Available models: %d\n", len(models))
		if len(models) > 0 && len(models) <= 10 {
			fmt.Printf("  Models: %s\n", strings.Join(models, ", "))
		} else if len(models) > 10 {
			fmt.Printf("  Models: %s ... (and %d more)\n", strings.Join(models[:5], ", "), len(models)-5)
		}
		return nil
	}

	// For Anthropic, just check connectivity
	if provCfg.Type == "anthropic" {
		client := provider.NewAnthropicClient(&provCfg)
		// Anthropic doesn't have a simple health check, so we just validate the config
		if client == nil {
			fmt.Printf("  Status: FAILED\n")
			fmt.Printf("  Error: invalid client configuration\n")
			return nil
		}
		fmt.Printf("  Status: OK (configuration valid)\n")
		return nil
	}

	return fmt.Errorf("unknown provider type: %s", provCfg.Type)
}

// runProviderAdd handles the provider add command.
func runProviderAdd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("provider name required")
	}
	providerName := args[0]

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	// Get flags
	provType, _ := cmd.Flags().GetString("type")
	baseURL, _ := cmd.Flags().GetString("url")
	apiKey, _ := cmd.Flags().GetString("key")
	apiVersion, _ := cmd.Flags().GetString("api-version")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")

	// Check if it's a known provider
	known := config.KnownProviders()
	if tmpl, ok := known[providerName]; ok {
		// Use template defaults
		if provType == "" {
			provType = tmpl.Type
		}
		if baseURL == "" {
			baseURL = tmpl.BaseURL
		}
		if maxTokens == 0 {
			maxTokens = tmpl.DefaultMaxTokens
		}
		fmt.Printf("Using preset for known provider %q\n", providerName)
	}

	// Validate required fields
	if provType == "" {
		return fmt.Errorf("--type is required (use 'anthropic' or 'openai')")
	}
	if provType != "anthropic" && provType != "openai" {
		return fmt.Errorf("invalid type %q; must be 'anthropic' or 'openai'", provType)
	}
	if baseURL == "" {
		return fmt.Errorf("--url is required")
	}
	if apiKey == "" {
		return fmt.Errorf("--key is required")
	}

	// Create or update provider
	prov := config.ProviderConfig{
		Type:             provType,
		APIKey:           apiKey,
		BaseURL:          baseURL,
		APIVersion:       apiVersion,
		DefaultMaxTokens: maxTokens,
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	cfg.Providers[providerName] = prov

	// Save config
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Provider %q added successfully\n", providerName)
	fmt.Printf("  Type:     %s\n", provType)
	fmt.Printf("  Base URL: %s\n", baseURL)
	fmt.Printf("  API Key:  %s\n", maskKey(apiKey))

	// Suggest importing models
	if tmpl, ok := known[providerName]; ok && len(tmpl.Models) > 0 {
		fmt.Println()
		fmt.Println("Available models:")
		for _, m := range tmpl.Models {
			fmt.Printf("  - %s\n", m)
		}
		fmt.Println()
		fmt.Printf("Import all models: api-switch model import %s\n", providerName)
	}

	return nil
}

// runModelList handles the model list command.
func runModelList(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Models) == 0 {
		fmt.Println("No models configured")
		fmt.Println()
		fmt.Println("Add a model:")
		fmt.Println("  api-switch model add <name> <provider>")
		fmt.Println()
		fmt.Println("Or import from a provider:")
		fmt.Println("  api-switch model import <provider>")
		return nil
	}

	fmt.Println("Configured models:")
	fmt.Println()

	// Group by provider
	byProvider := make(map[string][]string)
	for modelName, modelCfg := range cfg.Models {
		byProvider[modelCfg.Provider] = append(byProvider[modelCfg.Provider], modelName)
	}

	for provName, models := range byProvider {
		fmt.Printf("  %s:\n", provName)
		for _, m := range models {
			override := cfg.Models[m].ModelOverride
			if override != "" {
				fmt.Printf("    - %s → %s\n", m, override)
			} else {
				fmt.Printf("    - %s\n", m)
			}
		}
		fmt.Println()
	}

	return nil
}

// runModelAdd handles the model add command.
func runModelAdd(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: api-switch model add <name> <provider> [model_override]")
	}
	modelName := args[0]
	providerName := args[1]
	var modelOverride string
	if len(args) >= 3 {
		modelOverride = args[2]
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	// Validate provider exists
	if _, ok := cfg.Providers[providerName]; !ok {
		return fmt.Errorf("provider %q not found; add it first: api-switch provider add %s --key <key>", providerName, providerName)
	}

	if cfg.Models == nil {
		cfg.Models = make(map[string]config.ModelConfig)
	}
	cfg.Models[modelName] = config.ModelConfig{
		Provider:     providerName,
		ModelOverride: modelOverride,
	}

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Model %q added → provider %q\n", modelName, providerName)
	if modelOverride != "" {
		fmt.Printf("  Actual model name sent to API: %s\n", modelOverride)
	}

	return nil
}

// runModelRemove handles the model remove command.
func runModelRemove(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("model name required")
	}
	modelName := args[0]

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	if _, ok := cfg.Models[modelName]; !ok {
		return fmt.Errorf("model %q not found", modelName)
	}

	delete(cfg.Models, modelName)

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Model %q removed\n", modelName)
	return nil
}

// runModelImport handles the model import command.
func runModelImport(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("provider name required")
	}
	providerName := args[0]
	filter, _ := cmd.Flags().GetString("filter")

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	provCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}

	if provCfg.Type != "openai" {
		return fmt.Errorf("model import only supports OpenAI-compatible providers")
	}

	// Query the provider for available models
	client := provider.NewOpenAIClient(&provCfg)
	models, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	// Filter if requested
	if filter != "" {
		filtered := []string{}
		for _, m := range models {
			if strings.Contains(strings.ToLower(m), strings.ToLower(filter)) {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}

	if len(models) == 0 {
		fmt.Println("No models found")
		return nil
	}

	fmt.Printf("Found %d models:\n", len(models))
	for _, m := range models {
		fmt.Printf("  - %s\n", m)
	}

	// Add all models
	added := 0
	for _, m := range models {
		if _, exists := cfg.Models[m]; !exists {
			cfg.Models[m] = config.ModelConfig{Provider: providerName}
			added++
		}
	}

	if added > 0 {
		if err := config.Save(configPath, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Printf("\nAdded %d new models\n", added)
	} else {
		fmt.Println("\nAll models already configured")
	}

	return nil
}
