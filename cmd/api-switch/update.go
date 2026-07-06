package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hxz0727/API-Switch/internal/update"
)

// runUpdate handles the update command.
func runUpdate(cmd *cobra.Command, args []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	// Try GitHub/Gitee release API for latest version
	latest := update.CheckLatestVersion()
	if latest != "" {
		currentVer := strings.TrimPrefix(Version, "v")
		latestVer := strings.TrimPrefix(latest, "v")

		if currentVer == latestVer {
			fmt.Printf("Already up to date (v%s)\n", currentVer)
			return nil
		}

		fmt.Printf("Current: v%s  →  Latest: %s\n", currentVer, latest)
		fmt.Println()

		if checkOnly {
			fmt.Printf("To update, run: api-switch update\n")
			return nil
		}

		fmt.Println("Downloading and installing update...")
		if err := update.DoUpdate(currentBinary, latest); err != nil {
			fmt.Printf("Direct download failed: %v\n", err)
			fmt.Println("Trying npm update instead...")
			return runUpdateLegacy(checkOnly)
		}

		fmt.Printf("Updated to %s. Restarting...\n", latest)
		if err := update.ExecSelf(currentBinary); err != nil {
			return fmt.Errorf("restart failed: %w — please run 'api-switch serve' manually", err)
		}
		return nil
	}

	// GitHub/Gitee API unavailable — try npm
	if isNPMInstalled() {
		binaryVer := strings.TrimPrefix(Version, "v")
		latestNPM, err := getLatestNPMVersion()
		if err == nil && latestNPM != "" {
			currentNPM := getInstalledNPMVersion()

			if currentNPM != "" && binaryVer != latestNPM {
				fmt.Printf("npm package: %s  →  binary: v%s\n", latestNPM, binaryVer)
				fmt.Println()
				if checkOnly {
					return nil
				}
				// Force npm update to refresh the cached binary
				fmt.Println("Updating via npm to refresh cached binary...")
				if err := runNPMUpdate(); err != nil {
					return err
				}
				// Also try direct download
				return update.DoUpdate(currentBinary, "v"+latestNPM)
			}
		}
	}

	fmt.Println("Could not check for updates (GitHub/Gitee API unavailable)")
	return nil
}

// runUpdateLegacy handles npm-based update.
func runUpdateLegacy(checkOnly bool) error {
	if !isNPMInstalled() {
		return fmt.Errorf("npm not found — download latest release from https://github.com/hxz0727/API-Switch/releases")
	}

	latest, err := getLatestNPMVersion()
	if err != nil || latest == "" {
		return fmt.Errorf("cannot determine latest npm version: %w", err)
	}

	if checkOnly {
		fmt.Printf("Latest npm version: %s\n", latest)
		return nil
	}

	return runNPMUpdate()
}

func isNPMInstalled() bool {
	_, err := exec.LookPath("npm")
	return err == nil
}

func getLatestNPMVersion() (string, error) {
	out, err := execCommand("npm", "view", "@anthropic-ai-proxy/api-switch", "version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getInstalledNPMVersion() string {
	out, err := execCommand("npm", "list", "-g", "--json", "@anthropic-ai-proxy/api-switch")
	if err != nil {
		return ""
	}

	var data struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return ""
	}

	if dep, ok := data.Dependencies["@anthropic-ai-proxy/api-switch"]; ok {
		return dep.Version
	}
	return ""
}

func runNPMUpdate() error {
	fmt.Println("Running npm update...")
	cmd := exec.Command("npm", "update", "-g", "@anthropic-ai-proxy/api-switch")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func execCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}
