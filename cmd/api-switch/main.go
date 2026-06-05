package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/user/api-switch/internal/config"
	"github.com/user/api-switch/internal/logutil"
	"github.com/user/api-switch/internal/provider"
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
	serveCmd.Flags().CountP("verbose", "v", "Verbose output (-v for more info, -vv for debug)")
	serveCmd.Flags().BoolP("quiet", "q", false, "Suppress all non-error output")

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
		&cobra.Command{
			Use:   "import <provider>",
			Short: "Auto-import all available models from a provider",
			Long: `Query an OpenAI-compatible provider's /v1/models endpoint to
discover available models and add them to the routing table.

Examples:
  api-switch model import deepseek
  api-switch model import openai`,
			Args: cobra.ExactArgs(1),
			RunE: runModelImport,
		},
	)

	// provider command
	providerCmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage providers",
		Long: `Manage provider configurations.

Supports built-in presets for popular providers:
  deepseek, moonshot, qwen, glm, kimi, yi, step, ernie, hunyuan

Examples:
  api-switch provider list                          List all configured providers
  api-switch provider add deepseek --key sk-xxx     Add DeepSeek with API key
  api-switch provider add qwen --key sk-xxx         Add Qwen (DashScope) with API key`,
	}
	providerCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	providerCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all configured providers",
			RunE:  runProviderList,
		},
	)

	// provider add subcommand
	providerAddCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a provider (supports built-in presets)",
		Long: `Add a provider by name. If it matches a known provider preset
(deepseek, moonshot, qwen, glm, kimi, yi, step, ernie, hunyuan),
the base URL and defaults are filled in automatically.

You only need to provide the API key via --key flag.

Known providers and their defaults:
  deepseek  -> https://api.deepseek.com               (models: deepseek-chat, deepseek-coder)
  moonshot  -> https://api.moonshot.cn/v1             (models: moonshot-v1-8k, moonshot-v1-32k)
  qwen      -> https://dashscope.aliyuncs.com/v1       (models: qwen-plus, qwen-max, qwen-turbo)
  glm       -> https://open.bigmodel.cn/api/paas/v4   (models: glm-4-flash, glm-4-plus)
  kimi      -> https://api.moonshot.cn/v1             (models: kimi-latest)
  yi        -> https://api.lingyiwanwu.com/v1         (models: yi-lightning, yi-medium)
  step      -> https://api.stepfun.com/v1             (models: step-1-8k, step-1-32k)
  ernie     -> https://aip.baidubce.com/...            (models: ernie-4.0, ernie-3.5)
  hunyuan   -> https://api.hunyuan.cloud.tencent.com/v1 (models: hunyuan-lite, hunyuan-standard)

If the name does not match a known provider, you must also provide --url and --type.

Examples:
  api-switch provider add deepseek --key sk-xxx          # Known preset
  api-switch provider add qwen --key sk-xxx               # Known preset
  api-switch provider add my-custom --url https://... --type openai --key sk-xxx`,
		Args: cobra.ExactArgs(1),
		RunE: runProviderAdd,
	}
	providerAddCmd.Flags().String("key", "", "API key for the provider (required)")
	providerAddCmd.Flags().String("url", "", "Base URL (optional for known providers)")
	providerAddCmd.Flags().String("type", "", "Provider type: anthropic or openai (optional for known providers)")
	providerAddCmd.Flags().StringSlice("models", nil, "Additional models to import (comma-separated)")
	providerCmd.AddCommand(providerAddCmd)

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

	// doctor command
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics on your API-Switch configuration",
		Long: `Run comprehensive diagnostics to identify configuration issues.

Checks performed:
  - Config file existence, permissions, and format
  - Provider configuration completeness and validity
  - Model routing table consistency
  - Network connectivity to each provider
  - Claude Code settings.json correctness
  - Port availability

Examples:
  api-switch doctor           Run all checks
  api-switch doctor -v        Verbose output with suggestions`,
		RunE: runDoctor,
	}
	doctorCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// monitor command
	monitorCmd := &cobra.Command{
		Use:   "monitor",
		Short: "View real-time request traffic",
		Long: `Connect to a running API-Switch proxy and display live request traffic.

By default, connects to localhost:8080 and shows a streaming log of requests.
Use --port to specify a different proxy port.

Examples:
  api-switch monitor          Watch live traffic on default port 8080
  api-switch monitor --port 9090  Watch traffic on port 9090
  api-switch monitor --web    Print the web dashboard URL`,
		RunE: runMonitor,
	}
	monitorCmd.Flags().IntVarP(&port, "port", "p", 8080, "proxy server port")
	monitorCmd.Flags().Bool("web", false, "show web dashboard URL instead of terminal view")

	rootCmd.AddCommand(useCmd, setupCmd, serveCmd, modelCmd, providerCmd, configCmd, genClaudeCmd, doctorCmd, monitorCmd)

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
	// Apply log level
	verbose, _ := cmd.Flags().GetCount("verbose")
	quiet, _ := cmd.Flags().GetBool("quiet")
	switch {
	case quiet:
		logutil.SetLevel(logutil.LevelError)
		log.SetOutput(io.Discard) // suppress standard log output
	case verbose >= 2:
		logutil.SetLevel(logutil.LevelDebug)
	case verbose >= 1:
		logutil.SetLevel(logutil.LevelInfo)
	default:
		logutil.SetLevel(logutil.LevelInfo)
	}

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
	logutil.Info("Starting API-Switch proxy on %s", addr)
	logutil.Info("Configured models: %d, providers: %d", len(cfg.Models), len(cfg.Providers))
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	return srv.StartWithConfigFile(addr, configPath)
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

func runModelImport(cmd *cobra.Command, args []string) error {
	providerName := args[0]
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	provCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not found in config", providerName)
	}
	if provCfg.Type != "openai" {
		return fmt.Errorf("model import is only supported for OpenAI-compatible providers (got type %q)", provCfg.Type)
	}

	client := provider.NewOpenAIClient(&provCfg)
	models, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models from %q: %w", providerName, err)
	}

	if len(models) == 0 {
		fmt.Printf("No models returned by %q.\n", providerName)
		return nil
	}

	imported := 0
	for _, m := range models {
		if _, exists := cfg.Models[m]; !exists {
			cfg.Models[m] = config.ModelConfig{
				Provider:      providerName,
				ModelOverride: m,
			}
			imported++
		}
	}

	if err := config.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Imported %d new models from provider %q:\n", imported, providerName)
	for _, m := range models {
		if cfg.Models[m].Provider == providerName {
			fmt.Printf("  - %s\n", m)
		}
	}
	return nil
}

func runProviderList(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured.")
		fmt.Println()
		fmt.Println("Add a provider with:")
		fmt.Println("  api-switch provider add deepseek --key sk-xxx")
		fmt.Println("  api-switch provider add qwen --key sk-xxx")
		fmt.Println("  api-switch provider add openai --url https://api.openai.com --type openai --key sk-xxx")
		return nil
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

func runProviderAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	key, _ := cmd.Flags().GetString("key")
	url, _ := cmd.Flags().GetString("url")
	provType, _ := cmd.Flags().GetString("type")
	addModels, _ := cmd.Flags().GetStringSlice("models")

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	// Check if provider already exists
	if _, ok := cfg.Providers[name]; ok {
		return fmt.Errorf("provider %q already exists; use `api-switch config set providers.%s.<field> <value>` to update it", name, name)
	}

	// Check for known provider preset
	known := config.KnownProviders()
	tmpl, isKnown := known[name]

	if isKnown {
		if url == "" {
			url = tmpl.BaseURL
		}
		if provType == "" {
			provType = tmpl.Type
		}
	}

	if key == "" {
		return fmt.Errorf("--key is required for provider %q", name)
	}
	if url == "" {
		return fmt.Errorf("--url is required (no known preset for provider %q)", name)
	}
	if provType == "" {
		return fmt.Errorf("--type is required (must be 'anthropic' or 'openai')")
	}
	if provType != "anthropic" && provType != "openai" {
		return fmt.Errorf("invalid type %q; must be 'anthropic' or 'openai'", provType)
	}

	defaultMaxTokens := 1024
	if isKnown && tmpl.DefaultMaxTokens > 0 {
		defaultMaxTokens = tmpl.DefaultMaxTokens
	}

	cfg.Providers[name] = config.ProviderConfig{
		Type:             provType,
		APIKey:           key,
		BaseURL:          url,
		DefaultMaxTokens: defaultMaxTokens,
	}

	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Added provider %q (%s)\n", name, provType)
	fmt.Printf("  API Key:   %s\n", maskKey(key))
	fmt.Printf("  Base URL:  %s\n", url)

	// Suggest adding models
	if isKnown && len(tmpl.Models) > 0 {
		allModels := append([]string{}, tmpl.Models...)
		allModels = append(allModels, addModels...)
		fmt.Println()
		fmt.Println("Suggested models:")
		for _, m := range allModels {
			fmt.Printf("  - %s\n", m)
		}
		fmt.Println()
		fmt.Println("Add models with:")
		fmt.Printf("  api-switch model add <name> %s\n", name)
		fmt.Printf("  api-switch model import %s    (auto-import from API)\n", name)
	}
	if len(addModels) > 0 {
		for _, m := range addModels {
			cfg.Models[m] = config.ModelConfig{
				Provider:      name,
				ModelOverride: m,
			}
		}
		if err := config.Save(configPath, cfg); err != nil {
			return err
		}
		fmt.Printf("Added %d model(s) to routing table.\n", len(addModels))
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Switch to a model:  api-switch use <model>\n")
	fmt.Printf("  2. Start the proxy:    api-switch serve\n")
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

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, configPath, err := loadConfig()

	fmt.Println()
	fmt.Println("  API-Switch Diagnostics")
	fmt.Println("  " + strings.Repeat("═", 50))
	fmt.Println()

	var results []config.DoctorResult
	if err != nil {
		// Config file doesn't exist or is invalid — still run what checks we can
		results = config.RunDoctor(nil, configPath)
	} else {
		results = config.RunDoctor(cfg, configPath)
	}

	allPassed := true
	for _, r := range results {
		// Skip empty results
		if len(r.Items) == 0 {
			continue
		}

		// Print category header
		statusSymbol := "✓"
		switch r.Status {
		case config.StatusPass:
			statusSymbol = "✓"
		case config.StatusWarn:
			statusSymbol = "!"
			allPassed = false
		case config.StatusFail:
			statusSymbol = "✗"
			allPassed = false
		case config.StatusInfo:
			statusSymbol = "i"
		}

		fmt.Printf(" [%s] %s\n", statusSymbol, r.Title)
		fmt.Println("  " + strings.Repeat("─", 48))

		for _, item := range r.Items {
			itemSymbol := "  ✓"
			switch item.Status {
			case config.StatusPass:
				itemSymbol = "  ✓"
			case config.StatusWarn:
				itemSymbol = "  ⚠"
			case config.StatusFail:
				itemSymbol = "  ✗"
			case config.StatusInfo:
				itemSymbol = "  ·"
			}
			fmt.Printf("%s %s\n", itemSymbol, item.Message)
			if item.Detail != "" {
				fmt.Printf("    → %s\n", item.Detail)
			}
		}
		fmt.Println()
	}

	if allPassed {
		fmt.Println(" ✓ All checks passed! Your API-Switch is ready to use.")
	} else {
		fmt.Println(" ⚠ Some checks require attention. Review the warnings above.")
		fmt.Println()
		fmt.Println("Quick fixes:")
		fmt.Println("  - Missing config:     api-switch config init")
		fmt.Println("  - Add provider:       api-switch setup ...")
		fmt.Println("  - Add model:          api-switch model add <name> <provider>")
		fmt.Println("  - Switch model:       api-switch use <model>")
		fmt.Println("  - Set Claude config:  api-switch generate-claude-config")
		fmt.Println("  - Start proxy:        api-switch serve")
	}
	fmt.Println()

	return nil
}

func runMonitor(cmd *cobra.Command, args []string) error {
	webMode, _ := cmd.Flags().GetBool("web")

	if webMode {
		addr := fmt.Sprintf("localhost:%d", port)
		fmt.Printf("Web dashboard: http://%s/admin/\n", addr)
		fmt.Println()
		fmt.Println("Open this URL in your browser while the API-Switch proxy is running.")
		return nil
	}

	addr := fmt.Sprintf(":%d", port)
	if err := proxy.MonitorConnect(addr); err != nil {
		return fmt.Errorf("monitor error: %w", err)
	}
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
