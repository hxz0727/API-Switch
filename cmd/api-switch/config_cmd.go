package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/hxz0727/API-Switch/internal/config"
)

// runConfigShow handles the config show command.
func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Printf("Config file: %s\n\n", configPath)

	// Show config as YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// runConfigSet handles the config set command.
func runConfigSet(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: api-switch config set <key> <value>")
	}
	key := args[0]
	value := args[1]

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	if err := config.SetValue(cfg, key, value); err != nil {
		return err
	}

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Set %s = %s\n", key, maskValue(key, value))
	return nil
}

// runConfigInit handles the config init command.
func runConfigInit(cmd *cobra.Command, args []string) error {
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config file already exists at %s", configPath)
	}

	// Create default config
	cfg := config.DefaultConfig()

	// Add a sample provider (commented out in YAML)
	sampleProvider := config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-your-api-key-here",
		BaseURL: "https://api.openai.com/v1",
	}
	cfg.Providers["openai"] = sampleProvider
	cfg.Models["gpt-4o"] = config.ModelConfig{Provider: "openai"}

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	fmt.Printf("Created config file: %s\n", configPath)
	fmt.Println()
	fmt.Println("Edit the file to add your API keys, then:")
	fmt.Println("  api-switch model import <provider>")
	fmt.Println("  api-switch use <model>")
	fmt.Println("  api-switch serve")

	return nil
}
