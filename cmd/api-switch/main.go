package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/logutil"
	"github.com/hxz0727/API-Switch/internal/provider"
	"github.com/hxz0727/API-Switch/internal/proxy"
	usageutil "github.com/hxz0727/API-Switch/internal/usage"
)

var (
	port    int
	cfgPath string
)

// Version injected at build time via -ldflags, or falls back to default.
var Version = "0.2.3-dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "api-switch",
		Short: "Claude Code 多模型代理 — 一键切换 DeepSeek、Qwen、GLM、Moonshot，协议自动转换，零配置即用",
		RunE:  runRoot,
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
		Use:   "setup [name]",
		Short: "Configure a provider and generate Claude Code config in one step",
		Long: `Configure a provider and update Claude Code settings.

If the name matches a known provider (deepseek, qwen, moonshot, ...),
the type, URL, and models are filled in automatically. You only
need to provide --key.

If the name is custom, provide --type, --url, --models.

Examples:
  api-switch setup deepseek --key sk-xxx    # Known provider
  api-switch setup --name custom --type openai --url https://... --key sk-xxx --models m1,m2`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSetup,
	}
	setupCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	setupCmd.Flags().String("name", "", "Provider name")
	setupCmd.Flags().String("type", "", "Provider type: anthropic or openai")
	setupCmd.Flags().String("url", "", "Provider base URL")
	setupCmd.Flags().String("key", "", "API key for the provider")
	setupCmd.Flags().String("models", "", "Comma-separated model names (e.g. gpt-4o,gpt-4)")
	setupCmd.Flags().IntVarP(&port, "port", "p", 8080, "proxy server port")

	// model command + aliases
	modelCmd := &cobra.Command{
		Use:   "model",
		Short: "Manage model routing",
	}
	modelCmd.Aliases = []string{"models"}
	modelCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	modelCmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List all configured models and their providers",
			RunE:    runModelList,
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

Use --filter to import only matching models.

Examples:
  api-switch model import deepseek
  api-switch model import openai --filter gpt-4`,
			Args: cobra.ExactArgs(1),
			RunE: runModelImport,
		},
	)
	modelImportCmd := modelCmd.Commands()[3]
	modelImportCmd.Flags().String("filter", "", "Only import models matching this substring")

	// provider command + aliases
	providerCmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage providers",
		Long: `Manage provider configurations.

Supports built-in presets for popular providers:
  deepseek, moonshot, qwen, glm, kimi, yi, step, ernie, hunyuan

Examples:
  api-switch provider list                          List all configured providers
  api-switch provider add deepseek --key sk-xxx     Add DeepSeek with API key
  api-switch provider test deepseek                 Test connectivity`,
	}
	providerCmd.Aliases = []string{"providers"}
	providerCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	providerCmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List all configured providers",
			RunE:    runProviderList,
		},
		&cobra.Command{
			Use:     "test <name>",
			Aliases: []string{"ping"},
			Short:   "Test connectivity to a provider",
			Args:    cobra.ExactArgs(1),
			Long: `Test whether a provider is reachable and responding.

Sends a lightweight request to the provider's API and reports
the latency and any issues found.

Examples:
  api-switch provider test deepseek`,
			RunE: runProviderTest,
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

If --key is not provided, you will be prompted interactively.

Examples:
  api-switch provider add deepseek --key sk-xxx`,
		Args: cobra.ExactArgs(1),
		RunE: runProviderAdd,
	}
	providerAddCmd.Flags().String("key", "", "API key for the provider")
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
			Use:     "show",
			Aliases: []string{"cat"},
			Short:   "Show current config",
			RunE:    runConfigShow,
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

	// test command
	testCmd := &cobra.Command{
		Use:   "test [model]",
		Short: "Send a test message to verify end-to-end functionality",
		Long: `Send a brief test message to the specified model to verify
that the proxy is working correctly end-to-end.

If no model is specified, uses the currently active model
from Claude Code settings.

Examples:
  api-switch test                    Test the currently active model
  api-switch test deepseek-chat      Test a specific model`,
		Args: cobra.MaximumNArgs(1),
		RunE: runTest,
	}

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

	// usage command
	usageCmd := &cobra.Command{
		Use:     "usage",
		Aliases: []string{"stats"},
		Short:   "Show token usage statistics (daily and total)",
		Long: `Display token usage statistics for the API-Switch proxy.

Shows daily breakdown and lifetime totals of input/output tokens.

Examples:
  api-switch usage              Show usage summary
  api-switch usage --reset      Reset all usage statistics`,
		RunE: runUsage,
	}
	usageCmd.Flags().Bool("reset", false, "Reset all usage statistics")

	// version command
	versionCmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"-v", "--version"},
		Short:   "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("api-switch-cc v%s\n", Version)
			return nil
		},
	}

	rootCmd.AddCommand(useCmd, setupCmd, serveCmd, modelCmd, providerCmd, configCmd, testCmd, doctorCmd, monitorCmd, usageCmd, versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runRoot(cmd *cobra.Command, args []string) error {
	fmt.Println("API-Switch — LLM API proxy for Claude Code")
	fmt.Println(strings.Repeat("=", 48))
	fmt.Println()
	fmt.Println("快速开始：")
	fmt.Println()
	fmt.Println("  # 1. 添加一个供应商（支持已知厂商预设）")
	fmt.Println("  api-switch provider add deepseek --key sk-xxx")
	fmt.Println()
	fmt.Println("  # 2. 导入模型")
	fmt.Println("  api-switch model import deepseek")
	fmt.Println()
	fmt.Println("  # 3. 切换到该模型")
	fmt.Println("  api-switch use deepseek-chat")
	fmt.Println()
	fmt.Println("  # 4. 启动代理")
	fmt.Println("  api-switch serve")
	fmt.Println()
	fmt.Println("其它命令：")
	fmt.Println("  api-switch doctor          一键诊断")
	fmt.Println("  api-switch test [model]    端到端测试")
	fmt.Println("  api-switch monitor         实时流量监控")
	fmt.Println("  api-switch --help          查看所有命令")
	fmt.Println()
	return nil
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
	fmt.Println("      Start chatting or use /model in Claude Code.")
	return nil
}

func runSetup(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	providerType, _ := cmd.Flags().GetString("type")
	url, _ := cmd.Flags().GetString("url")
	key, _ := cmd.Flags().GetString("key")
	modelsStr, _ := cmd.Flags().GetString("models")
	proxyPort := port

	// First positional arg is the provider name (if provided)
	if len(args) > 0 && name == "" {
		name = args[0]
	}

	if name == "" {
		return fmt.Errorf("provider name is required; use either `api-switch setup deepseek --key sk-xxx` or `api-switch setup --name custom ...`")
	}

	// Auto-fill from known provider templates
	known := config.KnownProviders()
	tmpl, isKnown := known[name]
	if isKnown {
		if providerType == "" {
			providerType = tmpl.Type
		}
		if url == "" {
			url = tmpl.BaseURL
		}
	}

	if key == "" {
		return fmt.Errorf("--key is required; provide your API key for %q", name)
	}
	if providerType == "" {
		return fmt.Errorf("--type is required for custom providers (must be 'anthropic' or 'openai')")
	}
	if url == "" {
		return fmt.Errorf("--url is required; set the API base URL for %q", name)
	}
	if providerType != "anthropic" && providerType != "openai" {
		return fmt.Errorf("invalid type %q; must be 'anthropic' or 'openai'", providerType)
	}

	models := splitModels(modelsStr)
	if isKnown && len(models) == 0 {
		models = tmpl.Models
	}

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

func runTest(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	model := ""
	if len(args) > 0 {
		model = args[0]
	} else {
		// Read from Claude settings
		claudeSettings, err := config.LoadClaudeSettings(config.ClaudeSettingsPath())
		if err == nil && claudeSettings.Model != "" {
			model = claudeSettings.Model
		}
	}

	if model == "" {
		return fmt.Errorf("no model specified and no active model found; use `api-switch test <model>` or activate one with `api-switch use <model>`")
	}

	// Find the provider
	route, err := proxy.RouteTest(cfg, model)
	if err != nil {
		return err
	}

	fmt.Printf("Testing model %q via provider %q ...\n", model, route.ProviderName)
	fmt.Println()

	// Execute based on provider type
	var result string
	var inputTok, outputTok int

	switch route.ProviderType {
	case "anthropic":
		antReq := struct {
			Model     string              `json:"model"`
			MaxTokens int                 `json:"max_tokens"`
			Messages  []map[string]string `json:"messages"`
		}{
			Model:     route.ActualModel,
			MaxTokens: 10,
			Messages:  []map[string]string{{"role": "user", "content": "Hello"}},
		}
		resp, err := route.Anthropic.SendMessageRaw(antReq)
		if err != nil {
			return fmt.Errorf("API call failed: %w", err)
		}
		result = fmt.Sprintf("stop_reason=%s", resp["stop_reason"])
		if u, ok := resp["usage"].(map[string]interface{}); ok {
			inputTok, _ = u["input_tokens"].(int)
			outputTok, _ = u["output_tokens"].(int)
		}
	case "openai":
		fmt.Println("  Testing OpenAI models requires the proxy server.")
		fmt.Printf("  Start it with: api-switch serve -p %d\n", cfg.Server.Port)
		fmt.Println("  Then send a request via curl.")
		return nil
	default:
		return fmt.Errorf("provider type %q not supported for testing", route.ProviderType)
	}

	fmt.Printf("  ✓ Response received\n")
	fmt.Printf("    Result:  %s\n", result)
	fmt.Printf("    Input:   %d tokens\n", inputTok)
	fmt.Printf("    Output:  %d tokens\n", outputTok)
	fmt.Println()
	fmt.Println("  End-to-end test passed! Your API-Switch proxy is working.")
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	// Apply log level
	verbose, _ := cmd.Flags().GetCount("verbose")
	quiet, _ := cmd.Flags().GetBool("quiet")
	switch {
	case quiet:
		logutil.SetLevel(logutil.LevelError)
		log.SetOutput(io.Discard)
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
	filter, _ := cmd.Flags().GetString("filter")

	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	provCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not found in config; add it with `api-switch provider add %s --key <key>`", providerName, providerName)
	}
	if provCfg.Type != "openai" {
		return fmt.Errorf("model import is only supported for OpenAI-compatible providers (got type %q). For Anthropic models, add them manually with `api-switch model add <name> %s`", provCfg.Type, providerName)
	}

	client := provider.NewOpenAIClient(&provCfg)
	models, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models from %q: %w\n  Check that the API key and base URL are correct.", providerName, err)
	}

	if len(models) == 0 {
		fmt.Printf("No models returned by %q.\n", providerName)
		return nil
	}

	// Apply filter
	if filter != "" {
		var filtered []string
		for _, m := range models {
			if strings.Contains(strings.ToLower(m), strings.ToLower(filter)) {
				filtered = append(filtered, m)
			}
		}
		models = filtered
		if len(models) == 0 {
			fmt.Printf("No models matching %q found.", filter)
			return nil
		}
	}

	// Confirm for large imports
	if len(models) > 20 {
		fmt.Printf("Found %d models from %q.\n", len(models), providerName)
		fmt.Print("Import all? (Y/n): ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "n" || answer == "no" {
			fmt.Println("Import cancelled.")
			return nil
		}
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
	if imported > 30 {
		// Show only first 30 to avoid scroll spam
		for _, m := range models[:30] {
			if cfg.Models[m].Provider == providerName {
				fmt.Printf("  - %s\n", m)
			}
		}
		fmt.Printf("  ... and %d more\n", imported-30)
	} else {
		for _, m := range models {
			if cfg.Models[m].Provider == providerName {
				fmt.Printf("  - %s\n", m)
			}
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

func runProviderTest(cmd *cobra.Command, args []string) error {
	name := args[0]
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	provCfg, ok := cfg.Providers[name]
	if !ok {
		return fmt.Errorf("provider %q not found in config", name)
	}

	fmt.Printf("Testing provider %q (%s) ...\n", name, provCfg.Type)
	fmt.Printf("  URL: %s\n", provCfg.BaseURL)
	fmt.Println()

	switch provCfg.Type {
	case "anthropic":
		client := provider.NewAnthropicClient(&provCfg)
		err = client.Ping()
	case "openai":
		client := provider.NewOpenAIClient(&provCfg)
		err = client.Ping()
	default:
		return fmt.Errorf("unknown provider type: %s", provCfg.Type)
	}

	if err != nil {
		return fmt.Errorf("connection failed: %w\n  Check the base URL and API key.", err)
	}

	fmt.Println("  ✓ Provider is reachable and responding!")
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
		return fmt.Errorf("provider %q already exists; use `api-switch config set providers.%s.api_key <key>` to update it", name, name)
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

	// Interactive key prompt if not provided
	if key == "" {
		fmt.Printf("Enter API key for %q: ", name)
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		key = strings.TrimSpace(input)
		if key == "" {
			return fmt.Errorf("API key is required")
		}
	}

	if url == "" {
		return fmt.Errorf("--url is required (no known preset for provider %q)\n  Known presets: deepseek, qwen, moonshot, glm, kimi, yi, step, ernie, hunyuan", name)
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
	fmt.Println("  api-switch setup deepseek --key sk-xxx")
	fmt.Println()
	fmt.Println("Then start:")
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
		if len(r.Items) == 0 {
			continue
		}

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

func runUsage(cmd *cobra.Command, args []string) error {
	reset, _ := cmd.Flags().GetBool("reset")

	ut, err := usageutil.NewTracker(proxy.DefaultUsagePath())
	if err != nil {
		return fmt.Errorf("failed to load usage data: %w", err)
	}

	if reset {
		if err := ut.Reset(); err != nil {
			return fmt.Errorf("failed to reset usage: %w", err)
		}
		fmt.Println("Usage statistics reset.")
		return nil
	}

	snap := ut.Snapshot()

	if snap.TotalRequests == 0 {
		fmt.Println("No usage data yet. Start the proxy with `api-switch serve` to begin tracking.")
		return nil
	}

	fmt.Println()
	fmt.Println("  API-Switch 用量统计")
	fmt.Println("  " + strings.Repeat("=", 55))
	fmt.Println()

	// Show daily breakdown (sorted by date descending)
	if len(snap.Daily) > 0 {
		fmt.Printf("  %-12s %8s %10s %6s %8s\n", "日期", "请求数", "Token数", "缓存命中", "出错")
		fmt.Println("  " + strings.Repeat("-", 60))
		var dates []string
		for d := range snap.Daily {
			dates = append(dates, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dates)))
		for _, d := range dates {
			daily := snap.Daily[d]
			total := daily.InputTokens + daily.OutputTokens
			cacheHit := "-"
			if daily.CacheHit > 0 {
				cacheHit = fmt.Sprintf("%d 次/%dK", daily.CacheHit, daily.CacheReadTokens/1000)
			}
			errs := ""
			if daily.Errors > 0 {
				errs = fmt.Sprintf("%d", daily.Errors)
			} else {
				errs = "-"
			}
			fmt.Printf("  %-12s %8d %10d %6s %8s\n", d, daily.Requests, total, cacheHit, errs)
		}
		fmt.Println()
	}

	// Lifetime totals
	fmt.Printf("  总用量：\n")
	fmt.Printf("    请求数:        %d\n", snap.TotalRequests)
	fmt.Printf("    Token数:       %d (输入 %d + 输出 %d)\n",
		snap.TotalInputTokens+snap.TotalOutputTokens,
		snap.TotalInputTokens, snap.TotalOutputTokens)
	if snap.TotalCacheHits > 0 {
		fmt.Printf("    缓存命中:      %d 次 (%dK tokens)\n",
			snap.TotalCacheHits, snap.TotalCacheReadTokens/1000)
		rate := float64(snap.TotalCacheHits) / float64(snap.TotalRequests) * 100
		fmt.Printf("    缓存命中率:    %.1f%%\n", rate)
	}
	if snap.TotalErrors > 0 {
		fmt.Printf("    出错:          %d\n", snap.TotalErrors)
	}

	fmt.Println()
	fmt.Println("  数据文件: " + proxy.DefaultUsagePath())
	fmt.Println("  重置:     api-switch usage --reset")
	fmt.Println()

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
