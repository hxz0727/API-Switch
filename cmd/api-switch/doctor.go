package main

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/config"
)

// runDoctor handles the doctor command.
func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println("API-Switch Diagnostics")
	fmt.Println("======================")
	fmt.Println()

	issues := 0

	// Check 1: Config file
	fmt.Println("1. Checking config file...")
	cfg, configPath, err := loadConfig()
	if err != nil {
		fmt.Printf("   ❌ Cannot load config: %v\n", err)
		issues++
	} else {
		fmt.Printf("   ✓ Config file: %s\n", configPath)
		fmt.Printf("   ✓ Providers: %d, Models: %d\n", len(cfg.Providers), len(cfg.Models))

		// Validate config
		if err := cfg.Validate(); err != nil {
			fmt.Printf("   ❌ Config validation: %v\n", err)
			issues++
		} else {
			fmt.Println("   ✓ Config is valid")
		}
	}
	fmt.Println()

	// Check 2: Claude Code settings
	fmt.Println("2. Checking Claude Code settings...")
	claudeSettingsPath := config.ClaudeSettingsPath()
	claudeSettings, err := config.LoadClaudeSettings(claudeSettingsPath)
	if err != nil {
		fmt.Printf("   ⚠ Cannot load Claude settings: %v\n", err)
	} else {
		fmt.Printf("   ✓ Settings file: %s\n", claudeSettingsPath)
		if claudeSettings.Env == nil {
			fmt.Println("   ⚠ No environment variables set")
		} else {
			if baseURL, ok := claudeSettings.Env["ANTHROPIC_BASE_URL"]; ok {
				fmt.Printf("   ✓ ANTHROPIC_BASE_URL: %s\n", baseURL)
				// Check if it matches proxy port
				if cfg != nil {
					expected := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
					if baseURL != expected {
						fmt.Printf("   ⚠ Expected: %s\n", expected)
					}
				}
			}
			if model, ok := claudeSettings.Env["ANTHROPIC_MODEL"]; ok {
				fmt.Printf("   ✓ ANTHROPIC_MODEL: %s\n", model)
				// Check if model is configured
				if cfg != nil {
					if _, exists := cfg.Models[model]; !exists {
						fmt.Printf("   ⚠ Model not in config\n")
					}
				}
			}
		}
	}
	fmt.Println()

	// Check 3: Network connectivity
	fmt.Println("3. Checking network connectivity...")
	if cfg != nil {
		for provName, prov := range cfg.Providers {
			fmt.Printf("   Provider %q:\n", provName)
			if prov.BaseURL == "" {
				fmt.Println("      ❌ No base URL configured")
				issues++
				continue
			}

			// Try to connect
			client := &http.Client{Timeout: 10 * time.Second}
			testURL := prov.BaseURL
			if prov.Type == "openai" {
				testURL = prov.BaseURL + "/models"
			}

			req, err := http.NewRequest("GET", testURL, nil)
			if err != nil {
				fmt.Printf("      ❌ Invalid URL: %v\n", err)
				issues++
				continue
			}
			if prov.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+prov.APIKey)
			}

			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("      ❌ Connection failed: %v\n", err)
				issues++
			} else {
				resp.Body.Close()
				fmt.Printf("      ✓ Connected (HTTP %d)\n", resp.StatusCode)
			}
		}
	}
	fmt.Println()

	// Check 4: Port availability
	fmt.Println("4. Checking port availability...")
	if cfg != nil {
		port := cfg.Server.Port
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("   ⚠ Port %d is in use (proxy may be running)\n", port)
		} else {
			listener.Close()
			fmt.Printf("   ✓ Port %d is available\n", port)
		}
	}
	fmt.Println()

	// Check 5: API Keys format
	fmt.Println("5. Checking API keys...")
	if cfg != nil {
		for provName, prov := range cfg.Providers {
			if prov.APIKey == "" {
				fmt.Printf("   ❌ Provider %q: No API key\n", provName)
				issues++
			} else {
				fmt.Printf("   ✓ Provider %q: API key present (%s)\n", provName, maskKey(prov.APIKey))
			}
		}
	}
	fmt.Println()

	// Summary
	fmt.Println("======================")
	if issues == 0 {
		fmt.Println("✓ All checks passed!")
	} else {
		fmt.Printf("❌ Found %d issue(s)\n", issues)
	}

	return nil
}
