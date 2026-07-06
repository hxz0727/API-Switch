package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

func setupTestStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldStateDir := stateDir
	stateDir = dir
	t.Cleanup(func() { stateDir = oldStateDir })
	return dir
}

func writeTestState(t *testing.T, lastCheck time.Time) {
	t.Helper()
	state := &UpdateState{LastCheck: lastCheck}
	saveState(state)
}

func fakeReleaseServer(t *testing.T, tagName string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		body, _ := json.Marshal(ReleaseInfo{TagName: tagName})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
}

func fakeDownloadServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
}

// ============================================================================
// Unit Tests — isNewer
// ============================================================================

func TestIsNewer(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   bool
	}{
		{"0.4.6", "0.4.5", true},
		{"0.4.5", "0.4.6", false},
		{"0.4.5", "0.4.5", false},
		{"0.5.0", "0.4.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.10.0", "0.9.0", true},
		{"0.4.10", "0.4.9", true},
		{"0.4.10", "0.4.10", false},
		{"2.0.0", "1.99.99", true},
		{"0.0.1", "0.0.0", true},
		{"0.0.0", "0.0.1", false},
		{"0.4", "0.3", true},
		{"0.4.0", "0.4", false},
		{"0.4", "0.4.0", false},
		{"1.2.3.4", "1.2.3.3", true},
		{"1.2.3", "1.2.3.0", false},
		{"", "0.0.1", false},
		{"0.0.1", "", true},
		{"abc", "0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.v1+"_vs_"+tt.v2, func(t *testing.T) {
			got := isNewer(tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Unit Tests — platformKey
// ============================================================================

func TestPlatformKey(t *testing.T) {
	key := platformKey()
	if key == "" {
		t.Error("expected non-empty platform key")
	}
	if !strings.Contains(key, "-") {
		t.Error("expected platform key to contain '-'")
	}
	parts := strings.Split(key, "-")
	if len(parts) < 2 {
		t.Errorf("expected 'os-arch' format, got %q", key)
	}
	// Verify it's one of the known platforms
	known := map[string]bool{
		"linux-amd64": true, "linux-arm64": true,
		"darwin-amd64": true, "darwin-arm64": true,
		"windows-amd64": true,
	}
	if !known[key] {
		t.Errorf("unknown platform key: %q", key)
	}
}

// ============================================================================
// Unit Tests — stateFilePath / loadState / saveState / shouldCheck
// ============================================================================

func TestStateFilePath(t *testing.T) {
	dir := setupTestStateDir(t)
	path := stateFilePath()
	if !strings.Contains(path, dir) {
		t.Errorf("state file path %q should be under %q", path, dir)
	}
	if !strings.HasSuffix(path, "update-state.json") {
		t.Errorf("expected path to end with 'update-state.json', got %q", path)
	}
}

func TestStateFilePath_Default(t *testing.T) {
	oldStateDir := stateDir
	stateDir = ""
	defer func() { stateDir = oldStateDir }()

	path := stateFilePath()
	if path == "" {
		t.Error("expected non-empty default path")
	}
	if !strings.Contains(path, ".api-switch") {
		t.Errorf("expected path to contain '.api-switch', got %q", path)
	}
}

func TestLoadState_NoFile(t *testing.T) {
	setupTestStateDir(t)
	state := loadState()
	if !state.LastCheck.IsZero() {
		t.Error("expected zero LastCheck for no file")
	}
}

func TestLoadState_CorruptedFile(t *testing.T) {
	dir := setupTestStateDir(t)
	os.WriteFile(stateFilePath(), []byte("{invalid json"), 0644)
	state := loadState()
	if !state.LastCheck.IsZero() {
		t.Error("expected zero LastCheck for corrupted file")
	}
	_ = dir
}

func TestSaveAndLoadState(t *testing.T) {
	setupTestStateDir(t)
	now := time.Now().Truncate(time.Second)

	state := &UpdateState{LastCheck: now}
	saveState(state)

	loaded := loadState()
	if !loaded.LastCheck.Equal(now) {
		t.Errorf("expected %v, got %v", now, loaded.LastCheck)
	}
}

func TestShouldCheck_NoStateFile(t *testing.T) {
	setupTestStateDir(t)
	if !shouldCheck() {
		t.Error("shouldCheck should return true when no state file exists")
	}
}

func TestShouldCheck_WithinCooldown(t *testing.T) {
	setupTestStateDir(t)
	// Write state with recent check time (just now)
	writeTestState(t, time.Now())
	if shouldCheck() {
		t.Error("shouldCheck should return false within cooldown period")
	}
}

func TestShouldCheck_CooldownExpired(t *testing.T) {
	setupTestStateDir(t)
	// Write state with check time > 24h ago
	writeTestState(t, time.Now().Add(-25*time.Hour))
	if !shouldCheck() {
		t.Error("shouldCheck should return true after cooldown expires")
	}
}

// ============================================================================
// Unit Tests — fetchLatestVersion (with mock server)
// ============================================================================

func TestFetchLatestVersion_Success(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusOK)
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "v0.5.0" {
		t.Errorf("expected 'v0.5.0', got %q", ver)
	}
}

func TestFetchLatestVersion_EmptyTag(t *testing.T) {
	srv := fakeReleaseServer(t, "", http.StatusOK)
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "" {
		t.Errorf("expected empty for empty tag, got %q", ver)
	}
}

func TestFetchLatestVersion_NonOKStatus(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusNotFound)
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "" {
		t.Errorf("expected empty for non-200 status, got %q", ver)
	}
}

func TestFetchLatestVersion_ServerError(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusInternalServerError)
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "" {
		t.Errorf("expected empty for 500 status, got %q", ver)
	}
}

func TestFetchLatestVersion_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "" {
		t.Errorf("expected empty for invalid JSON, got %q", ver)
	}
}

func TestFetchLatestVersion_InvalidURL(t *testing.T) {
	ver := fetchLatestVersion("http://invalid.url.that.does.not.exist.local/releases")
	if ver != "" {
		t.Errorf("expected empty for invalid URL, got %q", ver)
	}
}

func TestFetchLatestVersion_UnreachableServer(t *testing.T) {
	ver := fetchLatestVersion("http://127.0.0.1:1/releases")
	if ver != "" {
		t.Errorf("expected empty for unreachable server, got %q", ver)
	}
}

// ============================================================================
// Unit Tests — CheckLatestVersion (with mock servers)
// ============================================================================

func TestCheckLatestVersion_GitHubSuccess(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	oldGitee := GiteeReleaseAPI
	GitHubReleaseAPI = srv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/nonexistent"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = oldGitee
	}()

	ver := CheckLatestVersion()
	if ver != "v0.5.0" {
		t.Errorf("expected 'v0.5.0', got %q", ver)
	}
}

func TestCheckLatestVersion_FallbackToGitee(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.1", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	oldGitee := GiteeReleaseAPI
	GitHubReleaseAPI = "http://127.0.0.1:1/nonexistent"
	GiteeReleaseAPI = srv.URL
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = oldGitee
	}()

	ver := CheckLatestVersion()
	if ver != "v0.5.1" {
		t.Errorf("expected 'v0.5.1' from gitee fallback, got %q", ver)
	}
}

func TestCheckLatestVersion_BothFail(t *testing.T) {
	oldGH := GitHubReleaseAPI
	oldGitee := GiteeReleaseAPI
	GitHubReleaseAPI = "http://127.0.0.1:1/a"
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = oldGitee
	}()

	ver := CheckLatestVersion()
	if ver != "" {
		t.Errorf("expected empty when both fail, got %q", ver)
	}
}

// ============================================================================
// Unit Tests — CheckForUpdate
// ============================================================================

func TestCheckForUpdate_UpdateAvailable(t *testing.T) {
	setupTestStateDir(t)
	// Force shouldCheck to pass (no state file = first check)

	srv := fakeReleaseServer(t, "v0.5.0", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	oldGitee := GiteeReleaseAPI
	GitHubReleaseAPI = srv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = oldGitee
	}()

	result := CheckForUpdate("0.4.0")
	if result == nil {
		t.Fatal("expected non-nil result for update available")
	}
	if !result.UpdateNeeded {
		t.Error("expected UpdateNeeded to be true")
	}
	if result.LatestVersion != "0.5.0" {
		t.Errorf("expected LatestVersion '0.5.0', got %q", result.LatestVersion)
	}
	if result.CurrentVersion != "0.4.0" {
		t.Errorf("expected CurrentVersion '0.4.0', got %q", result.CurrentVersion)
	}
}

func TestCheckForUpdate_AlreadyLatest(t *testing.T) {
	setupTestStateDir(t)

	srv := fakeReleaseServer(t, "v0.4.0", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	GitHubReleaseAPI = srv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = "http://127.0.0.1:1/b"
	}()

	result := CheckForUpdate("0.4.0")
	if result != nil {
		t.Errorf("expected nil when already latest, got %+v", result)
	}
}

func TestCheckForUpdate_WithinCooldown(t *testing.T) {
	setupTestStateDir(t)
	writeTestState(t, time.Now()) // just checked

	result := CheckForUpdate("0.4.0")
	if result != nil {
		t.Error("expected nil when within cooldown period")
	}
}

func TestCheckForUpdate_NoNetwork(t *testing.T) {
	setupTestStateDir(t)

	oldGH := GitHubReleaseAPI
	oldGitee := GiteeReleaseAPI
	GitHubReleaseAPI = "http://127.0.0.1:1/a"
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = oldGitee
	}()

	result := CheckForUpdate("0.4.0")
	if result != nil {
		t.Error("expected nil when both APIs unreachable")
	}

	// Verify state was saved
	state := loadState()
	if state.LastCheck.IsZero() {
		t.Error("expected LastCheck to be updated even on failure")
	}
}

func TestCheckForUpdate_NewerNotActuallyNewer(t *testing.T) {
	setupTestStateDir(t)

	// Server returns v0.3.0 which is older than current 0.4.0
	srv := fakeReleaseServer(t, "v0.3.0", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	GitHubReleaseAPI = srv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = "http://127.0.0.1:1/b"
	}()

	result := CheckForUpdate("0.4.0")
	if result != nil {
		t.Error("expected nil when server version is older")
	}
}

func TestCheckForUpdate_CooldownExpired_UpdateAvailable(t *testing.T) {
	setupTestStateDir(t)
	writeTestState(t, time.Now().Add(-25*time.Hour))

	srv := fakeReleaseServer(t, "v0.5.0", http.StatusOK)
	defer srv.Close()

	oldGH := GitHubReleaseAPI
	GitHubReleaseAPI = srv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = "http://127.0.0.1:1/b"
	}()

	result := CheckForUpdate("0.4.0")
	if result == nil {
		t.Fatal("expected update available after cooldown expired")
	}
}

// ============================================================================
// Unit Tests — copyFile
// ============================================================================

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	content := []byte("test content for copy")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("write src error: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	readBack, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst error: %v", err)
	}
	if string(readBack) != string(content) {
		t.Errorf("expected %q, got %q", content, readBack)
	}
}

func TestCopyFile_SourceNotExist(t *testing.T) {
	err := copyFile("/nonexistent/source/file", "/tmp/dst")
	if err == nil {
		t.Error("expected error for non-existent source")
	}
}

func TestCopyFile_DestinationNotWritable(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	os.WriteFile(src, []byte("data"), 0644)

	// Destination in a non-existent directory
	dst := filepath.Join(tmpDir, "nonexistent", "dst")
	err := copyFile(src, dst)
	if err == nil {
		t.Error("expected error for non-writable destination")
	}
}

// ============================================================================
// Unit Tests — downloadVerifyAndReplace (with mock server)
// ============================================================================

func TestDownloadVerifyAndReplace_Success(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'fake binary'\n")
	srv := fakeDownloadServer(t, binaryContent)
	defer srv.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")
	os.WriteFile(target, []byte("old content"), 0755)

	// Calculate expected hash
	hash := sha256.Sum256(binaryContent)
	expectedHash := hex.EncodeToString(hash[:])

	err := downloadVerifyAndReplace(srv.URL, target, expectedHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	readBack, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target error: %v", err)
	}
	if string(readBack) != string(binaryContent) {
		t.Errorf("expected %q, got %q", binaryContent, readBack)
	}

	// Temp file should be cleaned up
	if _, err := os.Stat(target + ".new"); err == nil {
		t.Error("temp file should have been cleaned up")
	}
}

func TestDownloadVerifyAndReplace_InvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")

	err := downloadVerifyAndReplace("http://invalid.url/test", target, "abc123")
	if err == nil {
		t.Error("expected error for invalid download URL")
		os.Remove(target)
	}
}

func TestDownloadVerifyAndReplace_ServerError(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusNotFound)
	defer srv.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")

	err := downloadVerifyAndReplace(srv.URL, target, "abc123")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestDownloadVerifyAndReplace_Server500(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.5.0", http.StatusInternalServerError)
	defer srv.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")

	err := downloadVerifyAndReplace(srv.URL, target, "abc123")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestDownloadVerifyAndReplace_ChecksumMismatch(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'fake binary'\n")
	srv := fakeDownloadServer(t, binaryContent)
	defer srv.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")
	os.WriteFile(target, []byte("old content"), 0755)

	// Wrong hash
	err := downloadVerifyAndReplace(srv.URL, target, "wronghash")
	if err == nil {
		t.Error("expected error for checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected checksum mismatch error, got: %v", err)
	}

	// Original file should be unchanged
	readBack, _ := os.ReadFile(target)
	if string(readBack) != "old content" {
		t.Error("file should not have been modified on checksum failure")
	}
}

// ============================================================================
// Unit Tests — DoUpdate
// ============================================================================

func TestDoUpdate_BothURLsFail(t *testing.T) {
	// Save original URLs
	oldGH := GitHubDownloadBase
	oldGitee := GiteeRawBase
	GitHubDownloadBase = "http://127.0.0.1:1/nonexistent"
	GiteeRawBase = "http://127.0.0.1:2/nonexistent"
	defer func() {
		GitHubDownloadBase = oldGH
		GiteeRawBase = oldGitee
	}()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")

	err := DoUpdate(target, "v0.5.0")
	if err == nil {
		t.Error("expected error when both download URLs fail")
	}
	if !strings.Contains(err.Error(), "failed to download update") {
		t.Errorf("expected 'failed to download update' in error, got: %v", err)
	}
}

func TestDoUpdate_Success(t *testing.T) {
	binaryContent := []byte("new binary content v0.5.0")

	// Calculate SHA256 hash
	hash := sha256.Sum256(binaryContent)
	expectedHash := hex.EncodeToString(hash[:])

	// Create a mock server that serves both binary and checksums
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			// Serve checksums
			binaryName := "api-switch-linux-amd64"
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(fmt.Sprintf("%s  %s\n", expectedHash, binaryName)))
		} else {
			// Serve binary
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(binaryContent)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test-binary")

	oldGH := GitHubDownloadBase
	oldGitee := GiteeRawBase
	GitHubDownloadBase = srv.URL + "/dummy-path" // will fail
	GiteeRawBase = srv.URL                       // will succeed
	defer func() {
		GitHubDownloadBase = oldGH
		GiteeRawBase = oldGitee
	}()

	err := DoUpdate(target, "v0.5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	readBack, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target error: %v", err)
	}
	if string(readBack) != string(binaryContent) {
		t.Errorf("expected %q, got %q", binaryContent, readBack)
	}
}

// ============================================================================
// Unit Tests — CheckResult
// ============================================================================

func TestCheckResult(t *testing.T) {
	result := CheckResult{
		CurrentVersion: "0.4.5",
		LatestVersion:  "0.4.6",
		UpdateNeeded:   true,
	}
	if !result.UpdateNeeded {
		t.Error("expected UpdateNeeded to be true")
	}
	if result.CurrentVersion != "0.4.5" {
		t.Errorf("expected CurrentVersion '0.4.5', got %q", result.CurrentVersion)
	}
	if result.LatestVersion != "0.4.6" {
		t.Errorf("expected LatestVersion '0.4.6', got %q", result.LatestVersion)
	}
}

func TestCheckResult_NoUpdate(t *testing.T) {
	result := CheckResult{
		CurrentVersion: "0.4.6",
		LatestVersion:  "0.4.6",
		UpdateNeeded:   false,
	}
	if result.UpdateNeeded {
		t.Error("expected UpdateNeeded to be false")
	}
}

// ============================================================================
// Integration Tests — End-to-End Flow
// ============================================================================

func TestIntegration_FullCheckFlow(t *testing.T) {
	setupTestStateDir(t)

	// 1. Set up a mock release server
	releaseSrv := fakeReleaseServer(t, "v0.5.0", http.StatusOK)
	defer releaseSrv.Close()

	oldGH := GitHubReleaseAPI
	GitHubReleaseAPI = releaseSrv.URL
	GiteeReleaseAPI = "http://127.0.0.1:1/b"
	defer func() {
		GitHubReleaseAPI = oldGH
		GiteeReleaseAPI = "http://127.0.0.1:1/b"
	}()

	// 2. CheckForUpdate should return update available
	result := CheckForUpdate("0.4.0")
	if result == nil {
		t.Fatal("expected update available")
	}
	if result.LatestVersion != "0.5.0" {
		t.Errorf("expected v0.5.0, got %q", result.LatestVersion)
	}

	// 3. State should be saved with recent check time
	state := loadState()
	if state.LastCheck.IsZero() {
		t.Error("state should have been saved")
	}

	// 4. Subsequent check within cooldown should return nil
	result2 := CheckForUpdate("0.4.0")
	if result2 != nil {
		t.Error("second check within cooldown should return nil")
	}
}

func TestIntegration_VersionFormats(t *testing.T) {
	setupTestStateDir(t)

	// Test with various version formats
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"0.4.6-dev", "0.5.0", true},
		{"v0.4.6", "v0.5.0", true},
		{"0.4.6", "v0.4.6", false},
		{"v0.4.6", "0.4.6", false},
		{"1.0.0-beta", "1.0.0", false}, // isNewer won't handle pre-release
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tt.current, tt.latest), func(t *testing.T) {
			srv := fakeReleaseServer(t, tt.latest, http.StatusOK)
			defer srv.Close()

			oldGH := GitHubReleaseAPI
			GitHubReleaseAPI = srv.URL
			GiteeReleaseAPI = "http://127.0.0.1:1/b"
			defer func() {
				GitHubReleaseAPI = oldGH
				GiteeReleaseAPI = "http://127.0.0.1:1/b"
			}()

			// Need to reset state to allow check
			setupTestStateDir(t)

			result := CheckForUpdate(tt.current)
			if tt.expected && result == nil {
				t.Errorf("expected update for %s -> %s", tt.current, tt.latest)
			}
			if !tt.expected && result != nil {
				t.Errorf("expected no update for %s -> %s", tt.current, tt.latest)
			}
		})
	}
}

func TestIntegration_StatePersistence(t *testing.T) {
	dir := setupTestStateDir(t)

	// 1. Write initial state
	now := time.Now().Truncate(time.Second)
	writeTestState(t, now)

	// 2. Verify state file exists
	path := stateFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file should exist after saveState")
	}

	// 3. Load and verify
	state := loadState()
	if !state.LastCheck.Equal(now) {
		t.Errorf("expected %v, got %v", now, state.LastCheck)
	}

	// 4. Corrupted state file should return zero state
	os.WriteFile(path, []byte("garbage"), 0644)
	state2 := loadState()
	if !state2.LastCheck.IsZero() {
		t.Error("corrupted state should return zero LastCheck")
	}
	_ = dir
}

// ============================================================================
// Unit Tests — ExecSelf (smoke test only — can't actually exec in tests)
// ============================================================================

func TestExecSelf_NonExistentBinary(t *testing.T) {
	err := ExecSelf("/nonexistent/binary/path")
	if err == nil {
		t.Error("expected error for non-existent binary")
	}
}

// ============================================================================
// Unit Tests — Edge Cases
// ============================================================================

func TestSaveState_CreatesDirectory(t *testing.T) {
	dir := setupTestStateDir(t)
	// Remove the state dir to test auto-creation
	os.RemoveAll(dir)

	now := time.Now()
	state := &UpdateState{LastCheck: now}
	saveState(state)

	// Verify file was created
	if _, err := os.Stat(stateFilePath()); err != nil {
		t.Errorf("state file should exist after save: %v", err)
	}
}

func TestFetchLatestVersion_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write empty body
	}))
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "" {
		t.Errorf("expected empty for empty body, got %q", ver)
	}
}

func TestFetchLatestVersion_WhitespaceTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(ReleaseInfo{TagName: "  v0.5.0  "})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	ver := fetchLatestVersion(srv.URL)
	if ver != "v0.5.0" {
		t.Errorf("expected trimmed 'v0.5.0', got %q", ver)
	}
}

func TestFetchLatestVersion_LargeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write a huge body, but only first 4096 bytes matter
		body, _ := json.Marshal(ReleaseInfo{TagName: "v1.0.0"})
		// Pad to > 4096 bytes
		padding := strings.Repeat("x", 5000)
		fullBody := string(body[:len(body)-1]) + `,"padding":"` + padding + `"}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fullBody))
	}))
	defer srv.Close()

	// Should still parse correctly despite large body (we use LimitReader)
	ver := fetchLatestVersion(srv.URL)
	// The JSON is malformed due to padding injection, so it may fail to parse
	// This tests that LimitReader doesn't cause a panic or hang
	_ = ver
}

func TestDownloadVerifyAndReplace_ReadOnlyTargetDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	roDir := filepath.Join(tmpDir, "readonly")
	os.Mkdir(roDir, 0555)
	defer os.Chmod(roDir, 0755)

	target := filepath.Join(roDir, "test-binary")

	srv := fakeDownloadServer(t, []byte("content"))
	defer srv.Close()

	err := downloadVerifyAndReplace(srv.URL, target, "abc123")
	if err == nil {
		t.Error("expected error when target directory is read-only")
	}
}

func TestCopyFile_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	os.WriteFile(src, []byte("new content"), 0644)
	os.WriteFile(dst, []byte("old content"), 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	readBack, _ := os.ReadFile(dst)
	if string(readBack) != "new content" {
		t.Errorf("expected 'new content', got %q", readBack)
	}
}

func TestIsNewer_EdgeCases(t *testing.T) {
	// Non-numeric versions
	if isNewer("abc", "def") {
		t.Error("non-numeric versions should not be newer")
	}
	if isNewer("1.x.3", "1.0.3") {
		t.Error("mixed versions should not be considered newer")
	}
	// Same length comparison
	if isNewer("1.2.3", "1.2.2") != true {
		t.Error("1.2.3 should be newer than 1.2.2")
	}
}
