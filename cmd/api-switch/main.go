package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/user/api-switch/internal/config"
	"github.com/user/api-switch/internal/proxy"
)

var (
	port    int
	cfgPath string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "api-switch",
		Short: "LLM API proxy with automatic protocol conversion for Claude Code",
	}

	// serve command
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the proxy server",
		RunE:  runServe,
	}
	serveCmd.Flags().IntVarP(&port, "port", "p", 0, "proxy server port (overrides config)")
	serveCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// use command - switch the active model in Claude Code
	useCmd := &cobra.Command{
		Use:   "use [model]",
		Short: "Switch the active model in Claude Code",
		Long: `Switch the active model and update Claude Code settings.

With a model name: switch to that model (updates ~/.claude/settings.json).
Without arguments: list all configured models.

Claude Code hot-reloads its settings, so the change takes effect
immediately in your running session.

Examples:
  api-switch use                  List all available models
  api-switch use gpt-4o           Switch to GPT-4o
  api-switch use deepseek-chat    Switch to DeepSeek Chat
  api-switch use claude-sonnet-4  Switch back to Claude`,
		Args: cobra.MaximumNArgs(1),
		RunE: runUse,
	}
	useCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// setup command
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure a provider and generate Claude Code config in one step",
		RunE:  runSetup,
	}
	setupCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	setupCmd.Flags().String("name", "", "Provider name (e.g. anthropic, openai, deepseek)")
	setupCmd.Flags().String("type", "", "Provider type: anthropic or openai")
	setupCmd.Flags().String("url", "", "Provider base URL (e.g. https://api.openai.com)")
	setupCmd.Flags().String("key", "", "API key for the provider")
	setupCmd.Flags().String("models", "", "Comma-separated model names (e.g. gpt-4o,gpt-4)")
	setupCmd.Flags().IntVarP(&port, "port", "p", 8080, "proxy server port")

	// model command
	modelCmd := &cobra.Command{
		Use:   "model",
		Short: "Manage model routing",
	}
	modelCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	modelCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all configured models and their providers",
			RunE:  runModelList,
		},
		&cobra.Command{
			Use:   "add <name> <provider> [model_override]",
			Short: "Add a model to the routing table",
			Args:  cobra.RangeArgs(2, 3),
			RunE:  runModelAdd,
		},
		&cobra.Command{
			Use:   "remove <name>",
			Short: "Remove a model from the routing table",
			Args:  cobra.ExactArgs(1),
			RunE:  runModelRemove,
		},
	)

	// provider command
	providerCmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage providers",
	}
	providerCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	providerCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all configured providers",
			RunE:  runProviderList,
		},
	)

	// config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	configCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show current config",
			RunE:  runConfigShow,
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a config value (e.g. providers.anthropic.api_key)",
			Args:  cobra.ExactArgs(2),
			RunE:  runConfigSet,
		},
		&cobra.Command{
			Use:   "init",
			Short: "Create default config file",
			RunE:  runConfigInit,
		},
	)

	// generate-claude-config command
	genClaudeCmd := &cobra.Command{
		Use:   "generate-claude-config",
		Short: "Generate or update Claude Code settings.json (proxy URL only)",
		RunE:  runGenerateClaudeConfig,
	}
	genClaudeCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	genClaudeCmd.Flags().IntVarP(&port, "port", "p", 8080, "proxy server port")

	rootCmd.AddCommand(useCmd, setupCmd, serveCmd, modelCmd, providerCmd, configCmd, genClaudeCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, string, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, "", err
	}
	path := cfgPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	return cfg, path, nil
}

func getProviderType(cfg *config.Config, model string) string {
	mcfg, ok := cfg.Models[model]
	if !ok {
		return ""
	}
	prov, ok := cfg.Providers[mcfg.Provider]
	if !ok {
		return ""
	}
	return prov.Type
}

func runUse(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		// List all models with the active one marked
		claudeSettings, err := config.LoadClaudeSettings(config.ClaudeSettingsPath())
		if err != nil {
			claudeSettings = &config.ClaudeSettings{}
		}
		activeModel := claudeSettings.Model

		entries := cfg.ListModels()

		fmt.Printf("  %-36s %-12s %s\n", "AVAILABLE MODELS", "TYPE", "PROVIDER")
		fmt.Println("  " + strings.Repeat("-", 72))
		for _, e := range entries {
			marker := " "
			if e.Name == activeModel {
				marker = "→"
			}
			fmt.Printf(" %s %-35s %-12s %s\n", marker, e.Name, e.ProviderType, e.Provider)
		}
		fmt.Println()
		fmt.Println("Switch model:  api-switch use <model-name>")
		fmt.Println("Start proxy:   api-switch serve")
		return nil
	}

	model := args[0]

	// Verify model exists in routing table
	providerType := getProviderType(cfg, model)
	if providerType == "" {
		return fmt.Errorf("model %q not found; add it first:\n  api-switch model add %s <provider>", model, model)
	}

	// Update Claude Code settings
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		return err
	}

	// Ensure base settings exist
	proxyPort := port
	if proxyPort == 0 {
		proxyPort = cfg.Server.Port
	}
	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	config.InitClaudeSettings(claudeSettings, proxyURL)

	// Activate the model
	config.ActivateModel(claudeSettings, model, providerType)

	if err := config.SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
		return err
	}

	fmt.Printf("Switched to model: %s\n", model)
	fmt.Printf("Updated: %s\n", claudeSettingsPath)
	fmt.Println()
	fmt.Println("Next: Claude Code will pick up the change automatically.")
	fmt.Println("      Use /model in Claude Code or just start chatting.")
	return nil
}

func runSetup(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	providerType, _ := cmd.Flags().GetString("type")
	url, _ := cmd.Flags().GetString("url")
	key, _ := cmd.Flags().GetString("key")
	modelsStr, _ := cmd.Flags().GetString("models")
	proxyPort := port

	if name == "" || providerType == "" || url == "" || key == "" || modelsStr == "" {
		return fmt.Errorf("all flags are required: --name, --type, --url, --key, --models\n\nExample:\n  api-switch setup --name openai --type openai --url https://api.openai.com --key sk-xxx --models gpt-4o,gpt-4")
	}

	if providerType != "anthropic" && providerType != "openai" {
		return fmt.Errorf("invalid type %q; must be 'anthropic' or 'openai'", providerType)
	}

	models := splitModels(modelsStr)

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	cfg.Server.Port = proxyPort

	result, err := config.SetupProvider(cfg, name, providerType, url, key, models, proxyPort)
	if err != nil {
		return err
	}

	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Printf("  Provider:  %s (%s)\n", result.ProviderAdded, providerType)
	fmt.Printf("  Base URL:  %s\n", url)
	fmt.Printf("  API Key:   %s\n", maskKey(key))
	fmt.Printf("  Models:    %s\n", strings.Join(result.ModelsAdded, ", "))
	fmt.Println()
	fmt.Println("Generated files:")
	fmt.Printf("  API-Switch:    %s\n", result.APISwitchConfig)
	fmt.Printf("  Claude Code:   %s\n", result.ClaudeSettings)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Switch to a model:  api-switch use <model>")
	fmt.Println("  2. Start the proxy:    api-switch serve")
	return nil
}

func runGenerateClaudeConfig(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	proxyPort := port
	if proxyPort == 0 {
		proxyPort = cfg.Server.Port
	}

	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	claudeSettingsPath := config.ClaudeSettingsPath()

	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		return err
	}

	config.InitClaudeSettings(claudeSettings, proxyURL)

	if err := config.SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
		return err
	}

	fmt.Printf("Updated Claude Code base settings at %s\n", claudeSettingsPath)
	fmt.Printf("  ANTHROPIC_BASE_URL = %s\n", proxyURL)
	fmt.Println()
	fmt.Println("Switch to a model:")
	fmt.Println("  api-switch use <model>")
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	p := port
	if p == 0 {
		p = cfg.Server.Port
	}

	srv := proxy.NewServer(cfg)
	addr := fmt.Sprintf(":%d", p)
	log.Printf("Starting API-Switch proxy on %s", addr)
	log.Printf("Configured models: %d, providers: %d", len(cfg.Models), len(cfg.Providers))
	return srv.Start(addr)
}

func runModelList(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%-35s %-15s %s\n", "MODEL", "PROVIDER", "ACTUAL MODEL")
	fmt.Println("---------------------------------------------------------------------------")
	for name, mcfg := range cfg.Models {
		actual := mcfg.ModelOverride
		if actual == "" {
			actual = name
		}
		fmt.Printf("%-35s %-15s %s\n", name, mcfg.Provider, actual)
	}
	return nil
}

func runModelAdd(cmd *cobra.Command, args []string) error {
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	name := args[0]
	providerName := args[1]
	modelOverride := ""
	if len(args) == 3 {
		modelOverride = args[2]
	}

	if _, ok := cfg.Providers[providerName]; !ok {
		return fmt.Errorf("provider %q not found; add it to config first", providerName)
	}

	cfg.Models[name] = config.ModelConfig{
		Provider:      providerName,
		ModelOverride: modelOverride,
	}

	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Added model %q -> provider %q\n", name, providerName)
	return nil
}

func runModelRemove(cmd *cobra.Command, args []string) error {
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	name := args[0]
	if _, ok := cfg.Models[name]; !ok {
		return fmt.Errorf("model %q not found in routing table", name)
	}

	delete(cfg.Models, name)
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Removed model %q\n", name)
	return nil
}

func runProviderList(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%-20s %-12s %s\n", "PROVIDER", "TYPE", "BASE URL")
	fmt.Println("------------------------------------------------------------")
	for name, pcfg := range cfg.Providers {
		keyStatus := "no key"
		if pcfg.APIKey != "" {
			keyStatus = maskKey(pcfg.APIKey)
		}
		fmt.Printf("%-20s %-12s %s (%s)\n", name, pcfg.Type, pcfg.BaseURL, keyStatus)
	}
	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	displayCfg := *cfg
	displayCfg.Providers = make(map[string]config.ProviderConfig)
	for name, prov := range cfg.Providers {
		p := prov
		if p.APIKey != "" {
			p.APIKey = maskKey(p.APIKey)
		}
		displayCfg.Providers[name] = p
	}

	data, err := config.YamlMarshal(&displayCfg)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	if err := config.SetProviderValue(cfg, key, value); err == nil {
		if err := config.Save(path, cfg); err != nil {
			return err
		}
		fmt.Printf("Set %s = %s\n", key, maskValue(key, value))
		return nil
	}

	if err := config.SetValue(cfg, key, value); err != nil {
		return err
	}

	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Set %s = %s\n", key, maskValue(key, value))
	return nil
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	path := cfgPath
	if path == "" {
		path = config.DefaultConfigPath()
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists at %s", path)
	}

	cfg := config.DefaultConfig()
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Created default config at %s\n", path)
	fmt.Println()
	fmt.Println("Quick setup with:")
	fmt.Println("  api-switch setup --name anthropic --type anthropic --url https://api.anthropic.com --key <key> --models claude-sonnet-4-20250514")
	fmt.Println("  api-switch setup --name openai --type openai --url https://api.openai.com --key <key> --models gpt-4o,gpt-4")
	fmt.Println()
	fmt.Println("Then switch to a model:")
	fmt.Println("  api-switch use gpt-4o")
	fmt.Println("  api-switch serve")
	return nil
}

func splitModels(s string) []string {
	var result []string
	for _, m := range strings.Split(s, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			result = append(result, m)
		}
	}
	return result
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func maskValue(key, value string) string {
	if len(key) >= 7 && key[len(key)-7:] == "api_key" {
		return maskKey(value)
	}
	return value
}
