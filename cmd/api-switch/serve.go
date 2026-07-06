package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/config"
	"github.com/hxz0727/API-Switch/internal/daemon"
	"github.com/hxz0727/API-Switch/internal/logutil"
	"github.com/hxz0727/API-Switch/internal/proxy"
	"github.com/hxz0727/API-Switch/internal/update"
	usageutil "github.com/hxz0727/API-Switch/internal/usage"
)

// runRoot handles the root command with no arguments.
func runRoot(cmd *cobra.Command, args []string) error {
	fmt.Println("API-Switch — LLM API proxy for Claude Code")
	fmt.Println("================================================")
	fmt.Println()
	fmt.Println("快速开始：")
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
	fmt.Println("查看已知厂商：api-switch provider known")
	fmt.Println("查看所有命令：api-switch --help")
	fmt.Println()
	fmt.Printf("当前版本：%s\n", Version)
	return nil
}

// loadConfig loads the config file and returns it along with the path.
func loadConfig() (*config.Config, string, error) {
	path := cfgPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, err
	}
	return cfg, path, nil
}

// getProviderType returns the provider type for a given model.
func getProviderType(cfg *config.Config, model string) string {
	modelCfg, ok := cfg.Models[model]
	if !ok {
		return ""
	}
	prov, ok := cfg.Providers[modelCfg.Provider]
	if !ok {
		return ""
	}
	return prov.Type
}

// runServe handles the serve command.
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
	portExplicitlySet := cmd.Flags().Changed("port")
	if !portExplicitlySet {
		p = 8080
	}
	noSavePort, _ := cmd.Flags().GetBool("no-save-port")

	// Resolve config file path
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	// Persist port to config (unless --no-save-port)
	if !noSavePort && p != cfg.Server.Port {
		cfg.Server.Port = p
		if err := config.Save(configPath, cfg); err != nil {
			logutil.Warn("Failed to save port to config: %v", err)
		}
	}

	// Sync Claude Code settings.json ANTHROPIC_BASE_URL to the current port
	proxyURL := fmt.Sprintf("http://localhost:%d", p)
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		logutil.Warn("Failed to load Claude settings: %v", err)
	} else {
		if claudeSettings.Env == nil {
			claudeSettings.Env = make(map[string]string)
		}
		if claudeSettings.Env["ANTHROPIC_BASE_URL"] != proxyURL {
			claudeSettings.Env["ANTHROPIC_BASE_URL"] = proxyURL
			if err := config.SaveClaudeSettings(claudeSettingsPath, claudeSettings); err != nil {
				logutil.Warn("Failed to sync Claude settings: %v", err)
			}
		}
	}

	srv := proxy.NewServer(cfg)
	addr := fmt.Sprintf(":%d", p)
	logutil.Info("Starting API-Switch proxy on %s", addr)
	logutil.Info("Configured models: %d, providers: %d", len(cfg.Models), len(cfg.Providers))
	if cfg.Server.AuthToken != "" {
		logutil.Info("API authentication: enabled (token: %s...)", maskToken(cfg.Server.AuthToken))
	} else {
		logutil.Info("API authentication: disabled (set server.auth_token in config to enable)")
	}

	// Auto-update check (runs in background, does not block startup)
	noAutoUpdate, _ := cmd.Flags().GetBool("no-auto-update")
	if !noAutoUpdate {
		go update.AutoUpdate(Version)
	}

	// Handle graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.StartWithConfigFile(addr, configPath)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case sig := <-sigCh:
		logutil.Info("Received signal %v, shutting down gracefully...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logutil.Error("Shutdown error: %v", err)
		}
		logutil.Info("Server stopped")
	}
	return nil
}

// runStart handles the start command (daemon mode).
func runStart(cmd *cobra.Command, args []string) error {
	if daemon.Running() {
		return fmt.Errorf("api-switch is already running (PID %d)", daemon.PID())
	}
	pid, err := daemon.Start(getBinaryPath(), port, cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("api-switch started (PID %d)\n", pid)
	return nil
}

// runStop handles the stop command.
func runStop(cmd *cobra.Command, args []string) error {
	if !daemon.Running() {
		fmt.Println("api-switch is not running")
		return nil
	}
	fmt.Printf("Stopping api-switch (PID %d)...\n", daemon.PID())
	daemon.Stop()
	fmt.Println("Stopped")
	return nil
}

// runStatus handles the status command.
func runStatus(cmd *cobra.Command, args []string) error {
	if daemon.Running() {
		fmt.Printf("api-switch is running (PID %d)\n", daemon.PID())
		fmt.Printf("Log: %s\n", daemon.LogPath())
	} else {
		fmt.Println("api-switch is not running")
	}

	// Show active model from Claude settings
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err == nil && claudeSettings != nil {
		if claudeSettings.Env != nil {
			if model, ok := claudeSettings.Env["ANTHROPIC_MODEL"]; ok && model != "" {
				fmt.Printf("Active Model: %s\n", model)
			}
		}
	}

	// Show config summary
	cfg, _, err := loadConfig()
	if err == nil {
		fmt.Printf("Configured Models: %d, Providers: %d\n", len(cfg.Models), len(cfg.Providers))
	}

	return nil
}

// runRestart handles the restart command.
func runRestart(cmd *cobra.Command, args []string) error {
	if daemon.Running() {
		fmt.Println("Stopping...")
		daemon.Stop()
	}
	fmt.Println("Starting...")
	return runStart(cmd, args)
}

// runLogs handles the logs command.
func runLogs(cmd *cobra.Command, args []string) error {
	logPath := daemon.LogPath()
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("cannot open log file: %w", err)
	}
	defer f.Close()

	// Seek to end - 4KB for tail-like behavior
	stat, _ := f.Stat()
	if stat.Size() > 4096 {
		f.Seek(-4096, io.SeekEnd)
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	return scanner.Err()
}

// getBinaryPath returns the path to the current binary.
func getBinaryPath() string {
	bin, err := os.Executable()
	if err != nil {
		return "api-switch"
	}
	return bin
}

// runUsage handles the usage command.
func runUsage(cmd *cobra.Command, args []string) error {
	days, _ := cmd.Flags().GetInt("days")
	if days <= 0 {
		days = 7
	}

	ut, err := usageutil.NewTracker(proxy.DefaultUsagePath())
	if err != nil {
		return fmt.Errorf("cannot load usage data: %w", err)
	}

	// Get daily usage from tracker
	snapshot := ut.Snapshot()
	daily := snapshot.Daily

	if len(daily) == 0 {
		fmt.Println("No usage data available")
		return nil
	}

	// Get the last N days
	now := time.Now()
	fmt.Printf("Token usage (last %d days):\n\n", days)
	fmt.Printf("%-12s %12s %12s %12s %10s\n", "Date", "Input", "Output", "Cache Read", "Errors")
	fmt.Println(strings.Repeat("-", 60))

	var totalInput, totalOutput, totalCache int64
	var totalErrors int64
	count := 0

	// Iterate in reverse chronological order
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		if d, ok := daily[date]; ok {
			fmt.Printf("%-12s %12d %12d %12d %10d\n", d.Date, d.InputTokens, d.OutputTokens, d.CacheReadTokens, d.Errors)
			totalInput += d.InputTokens
			totalOutput += d.OutputTokens
			totalCache += d.CacheReadTokens
			totalErrors += d.Errors
			count++
		}
	}

	if count > 0 {
		fmt.Println(strings.Repeat("-", 60))
		fmt.Printf("%-12s %12d %12d %12d %10d\n", "Total", totalInput, totalOutput, totalCache, totalErrors)
	}

	return nil
}
