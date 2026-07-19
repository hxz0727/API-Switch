package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	port    int
	cfgPath string
)

// Version injected at build time via -ldflags, or falls back to default.
var Version = "0.9.3"

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
	serveCmd.Flags().Bool("no-auto-update", false, "Disable automatic update check on startup")
	serveCmd.Flags().Bool("no-save-port", false, "Do not persist the port to config (use with -p)")

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
			Use:   "known",
			Short: "List all known provider presets",
			RunE:  runProviderKnown,
		},
		&cobra.Command{
			Use:   "test <name>",
			Short: "Test connectivity to a provider",
			Args:  cobra.ExactArgs(1),
			RunE:  runProviderTest,
		},
		&cobra.Command{
			Use:   "add <name>",
			Short: "Add a new provider",
			Long: `Add a new LLM provider.

For known providers (deepseek, qwen, moonshot, ...), defaults are auto-filled:
  api-switch provider add deepseek --key sk-xxx

For custom providers, specify all options:
  api-switch provider add my-api --type openai --url https://... --key sk-xxx`,
			Args: cobra.ExactArgs(1),
			RunE: runProviderAdd,
		},
	)
	providerAddCmd := providerCmd.Commands()[3]
	providerAddCmd.Flags().String("type", "", "Provider type: anthropic or openai")
	providerAddCmd.Flags().String("url", "", "Provider base URL")
	providerAddCmd.Flags().String("key", "", "API key for the provider")
	providerAddCmd.Flags().String("api-version", "", "API version (for Anthropic)")
	providerAddCmd.Flags().Int("max-tokens", 0, "Default max tokens for this provider")

	// config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	configCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show the current configuration",
			RunE:  runConfigShow,
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a configuration value",
			Long: `Set a configuration value.

Keys use dotted notation:
  server.port                    Server port
  providers.deepseek.api_key     Provider API key
  providers.deepseek.base_url    Provider base URL

Examples:
  api-switch config set server.port 9000
  api-switch config set providers.openai.api_key sk-xxx`,
			Args: cobra.ExactArgs(2),
			RunE: runConfigSet,
		},
		&cobra.Command{
			Use:   "init",
			Short: "Create a default config file",
			RunE:  runConfigInit,
		},
	)

	// test command
	testCmd := &cobra.Command{
		Use:   "test <model>",
		Short: "Test a model through the proxy",
		Args:  cobra.ExactArgs(1),
		RunE:  runTest,
	}
	testCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// doctor command
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics to check configuration",
		RunE:  runDoctor,
	}
	doctorCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// daemon commands
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the proxy server as a daemon",
		RunE:  runStart,
	}
	startCmd.Flags().IntVarP(&port, "port", "p", 0, "proxy server port (overrides config)")
	startCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the proxy server daemon",
		RunE:  runStop,
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the proxy server is running",
		RunE:  runStatus,
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the proxy server daemon",
		RunE:  runRestart,
	}
	restartCmd.Flags().IntVarP(&port, "port", "p", 0, "proxy server port (overrides config)")
	restartCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Show proxy server logs",
		RunE:  runLogs,
	}

	// update command
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update to the latest version",
		RunE:  runUpdate,
	}
	updateCmd.Flags().Bool("check", false, "Only check for updates, don't install")

	// monitor command
	monitorCmd := &cobra.Command{
		Use:   "monitor",
		Short: "Connect to a running API-Switch and show live request events",
		RunE:  runMonitor,
	}
	monitorCmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")

	// usage command
	usageCmd := &cobra.Command{
		Use:     "usage",
		Short:   "Show token usage statistics",
		Aliases: []string{"stats"},
		RunE:    runUsage,
	}
	usageCmd.Flags().Int("days", 7, "Number of days to show")

	// version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("api-switch version %s\n", Version)
		},
	}

	// Add all commands
	rootCmd.AddCommand(useCmd, setupCmd, serveCmd, startCmd, stopCmd, statusCmd, restartCmd, logsCmd, updateCmd, modelCmd, providerCmd, configCmd, testCmd, doctorCmd, monitorCmd, usageCmd, versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
	}
}

// Utility functions

func splitModels(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
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

func maskToken(token string) string {
	return maskKey(token)
}

func maskValue(key, value string) string {
	if len(key) >= 7 && key[len(key)-7:] == "api_key" {
		return maskKey(value)
	}
	return value
}
