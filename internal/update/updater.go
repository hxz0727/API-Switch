package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hxz0727/API-Switch/internal/logutil"
)

const (
	checkInterval = 24 * time.Hour
)

// Package-level variables for test injection.
var (
	// GitHubReleaseAPI is the GitHub API endpoint for the latest release.
	GitHubReleaseAPI = "https://api.github.com/repos/hxz0727/API-Switch/releases/latest"
	// GiteeReleaseAPI is the Gitee API endpoint for the latest release (China mirror).
	GiteeReleaseAPI = "https://gitee.com/api/v5/repos/776311606/API-Switch/releases/latest"
	// GitHubReleasePage is the GitHub releases page.
	GitHubReleasePage = "https://github.com/hxz0727/API-Switch/releases"
	// GiteeReleasePage is the Gitee releases page.
	GiteeReleasePage = "https://gitee.com/776311606/API-Switch/releases"
	// GitHubDownloadBase is the base URL for downloading release binaries from GitHub.
	GitHubDownloadBase = "https://github.com/hxz0727/API-Switch/releases/download"
	// GiteeRawBase is the base URL for downloading release binaries from Gitee.
	// Binary is stored in versioned subdirectory: release/vX.Y.Z/api-switch-{plat}
	GiteeRawBase = "https://gitee.com/776311606/API-Switch/raw/release"

	// stateDir is the directory where update state is stored (injectable for tests).
	stateDir = ""
)

// ReleaseInfo represents a GitHub/Gitee release.
type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// UpdateState tracks when we last checked for updates.
type UpdateState struct {
	LastCheck time.Time `json:"last_check"`
}

// CheckResult is the result of an update check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateNeeded   bool
}

// stateFilePath returns the path to the update state file.
func stateFilePath() string {
	dir := stateDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".api-switch")
	}
	return filepath.Join(dir, "update-state.json")
}

func loadState() *UpdateState {
	data, err := os.ReadFile(stateFilePath())
	if err != nil {
		return &UpdateState{}
	}
	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return &UpdateState{}
	}
	return &state
}

func saveState(state *UpdateState) {
	dir := filepath.Dir(stateFilePath())
	os.MkdirAll(dir, 0755)
	data, _ := json.Marshal(state)
	os.WriteFile(stateFilePath(), data, 0644)
}

// shouldCheck returns true if enough time has passed since the last check.
func shouldCheck() bool {
	state := loadState()
	if state.LastCheck.IsZero() {
		return true
	}
	return time.Since(state.LastCheck) > checkInterval
}

// CheckLatestVersion queries GitHub/Gitee for the latest release tag.
// Returns the tag name (e.g. "v0.4.6") or empty string on failure.
func CheckLatestVersion() string {
	// Try Gitee first (China mirror)
	if ver := fetchLatestVersion(GiteeReleaseAPI); ver != "" {
		return ver
	}
	// Fall back to GitHub
	if ver := fetchLatestVersion(GitHubReleaseAPI); ver != "" {
		return ver
	}
	return ""
}

func fetchLatestVersion(apiURL string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}

	var release ReleaseInfo
	if err := json.Unmarshal(body, &release); err != nil {
		return ""
	}

	return strings.TrimSpace(release.TagName)
}

// CheckForUpdate compares the current version with the latest release.
// Returns nil if no update is needed, or a CheckResult if an update is available.
func CheckForUpdate(currentVersion string) *CheckResult {
	if !shouldCheck() {
		return nil
	}

	// Mark that we checked
	state := loadState()
	state.LastCheck = time.Now()
	saveState(state)

	latest := CheckLatestVersion()
	if latest == "" {
		return nil
	}

	// Normalize versions: strip "v" prefix
	current := strings.TrimPrefix(currentVersion, "v")
	latestVer := strings.TrimPrefix(latest, "v")

	if current == latestVer {
		return nil
	}

	// Simple version comparison (handles semver like 0.4.5 vs 0.4.6)
	if !isNewer(latestVer, current) {
		return nil
	}

	return &CheckResult{
		CurrentVersion: current,
		LatestVersion:  latestVer,
		UpdateNeeded:   true,
	}
}

// isNewer returns true if v1 is newer than v2 (simple semver comparison).
func isNewer(v1, v2 string) bool {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &n1)
		}
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &n2)
		}
		if n1 > n2 {
			return true
		}
		if n1 < n2 {
			return false
		}
	}
	return false
}

// DoUpdate performs the actual self-update.
// It downloads the latest binary and replaces the current one.
func DoUpdate(currentBinary string, latestVersion string) error {
	// Determine platform
	plat := platformKey()
	if plat == "" {
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Try Gitee first, then GitHub
	urls := []string{
		fmt.Sprintf("%s/%s/api-switch-%s", GiteeRawBase, latestVersion, plat),
		fmt.Sprintf("%s/%s/api-switch-%s", GitHubDownloadBase, latestVersion, plat),
	}

	if runtime.GOOS == "windows" {
		urls[0] = fmt.Sprintf("%s/%s/api-switch-windows-amd64.exe", GiteeRawBase, latestVersion)
		urls[1] = fmt.Sprintf("%s/%s/api-switch-windows-amd64.exe", GitHubDownloadBase, latestVersion)
	}

	var downloadErr error
	for _, url := range urls {
		if err := downloadAndReplace(url, currentBinary); err == nil {
			logutil.Info("Auto-update successful: %s", latestVersion)
			return nil
		} else {
			downloadErr = err
		}
	}

	return fmt.Errorf("failed to download update: %w", downloadErr)
}

func platformKey() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "darwin-arm64"
		}
		return "darwin-amd64"
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
		return "linux-amd64"
	case "windows":
		return "windows-amd64"
	default:
		return ""
	}
}

func downloadAndReplace(url, targetPath string) error {
	logutil.Debug("Auto-update: downloading %s", url)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("cannot create parent directory: %w", err)
	}

	// Download to temp file
	tmpFile := targetPath + ".new"
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("download incomplete: %w", err)
	}
	f.Close()

	// Replace current binary atomically
	if err := os.Rename(tmpFile, targetPath); err != nil {
		// On some systems, rename across devices fails; try copy+delete
		if err := copyFile(tmpFile, targetPath); err != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("cannot replace binary: %w", err)
		}
		os.Remove(tmpFile)
	}

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// AutoUpdate performs a non-blocking update check and auto-update.
// It should be called from a goroutine during server startup.
func AutoUpdate(currentVersion string) {
	// Get current binary path
	currentBinary, err := os.Executable()
	if err != nil {
		logutil.Debug("Auto-update: cannot find current binary: %v", err)
		return
	}

	result := CheckForUpdate(currentVersion)
	if result == nil || !result.UpdateNeeded {
		return
	}

	logutil.Info("Auto-update: new version available: %s (current: %s)", result.LatestVersion, result.CurrentVersion)
	logutil.Info("Auto-update: downloading and installing...")

	if err := DoUpdate(currentBinary, "v"+result.LatestVersion); err != nil {
		logutil.Warn("Auto-update failed: %v", err)
		logutil.Info("Manual update: api-switch update")
		return
	}

	// Restart: exec the new binary
	logutil.Info("Auto-update: restarting with new version...")
	if err := ExecSelf(currentBinary); err != nil {
		logutil.Warn("Auto-update: restart failed: %v — please restart manually", err)
	}
}

// ExecSelf replaces the current process with the given binary.
func ExecSelf(binary string) error {
	args := os.Args[1:]
	// If running as daemon, the parent will handle restart
	if len(args) > 0 && args[0] == "serve" {
		args = append([]string{"serve"}, args[1:]...)
	}
	env := os.Environ()
	return syscallExec(binary, args, env)
}

// restart replaces the current process with the new binary.
func restart(binary string) error {
	return ExecSelf(binary)
}
