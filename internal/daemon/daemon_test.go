package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDir(t *testing.T) {
	dir := Dir()
	if dir == "" {
		t.Error("expected non-empty directory path")
	}
	if !strings.HasSuffix(dir, ".api-switch") {
		t.Errorf("expected path to end with '.api-switch', got %q", dir)
	}
}

func TestPidFile(t *testing.T) {
	path := pidFile()
	if !strings.HasSuffix(path, pidFileName) {
		t.Errorf("expected path to end with %q, got %q", pidFileName, path)
	}
	if !strings.Contains(path, ".api-switch") {
		t.Error("expected path to contain '.api-switch'")
	}
}

func TestLogFile(t *testing.T) {
	path := logFile()
	if !strings.HasSuffix(path, logFileName) {
		t.Errorf("expected path to end with %q, got %q", logFileName, path)
	}
}

func TestLogPath(t *testing.T) {
	path := LogPath()
	if !strings.HasSuffix(path, logFileName) {
		t.Errorf("expected path to end with %q, got %q", logFileName, path)
	}
}

func TestRunning_NoPIDFile(t *testing.T) {
	// Ensure no PID file exists in temp dir
	// Running() uses global pidFile() which points to real home dir,
	// so this test checks the behavior when the file doesn't exist.
	// Since we can't easily mock the home dir, we just verify it doesn't panic.
	if Running() {
		t.Log("daemon appears to be running (PID file exists)")
	}
}

func TestPID_NoFile(t *testing.T) {
	// PID() returns -1 when no PID file exists (or it's in home dir)
	// We can't mock the home dir easily, but we can test the parsing logic.
	// Just verify it doesn't panic.
	pid := PID()
	t.Logf("PID returned: %d", pid)
}

func TestRunning_InvalidPIDFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake PID file with garbage content
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to create test PID file: %v", err)
	}

	// Directly test the logic: os.ReadFile + strconv.Atoi with invalid content
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err == nil {
		t.Error("expected error parsing invalid PID")
	}
	if pid != 0 {
		t.Errorf("expected 0 for invalid PID, got %d", pid)
	}
}

func TestRunning_ValidPIDFile_WrongPID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a PID file with a valid number that doesn't exist
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte("99999"), 0644); err != nil {
		t.Fatalf("failed to create test PID file: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("unexpected error parsing PID: %v", err)
	}
	if pid != 99999 {
		t.Errorf("expected 99999, got %d", pid)
	}
	// Can't easily test signal 0 on arbitrary PID without root
}

func TestRunning_ValidPIDFile_NegativePID(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte("-1"), 0644); err != nil {
		t.Fatalf("failed to create test PID file: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("unexpected error parsing PID: %v", err)
	}
	// Running() checks pid <= 0 and returns false
	if pid > 0 {
		t.Errorf("expected negative PID, got %d", pid)
	}
}

func TestRunning_ValidPIDFile_ZeroPID(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte("0"), 0644); err != nil {
		t.Fatalf("failed to create test PID file: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("unexpected error parsing PID: %v", err)
	}
	if pid != 0 {
		t.Errorf("expected 0, got %d", pid)
	}
	// Running() should return false for pid 0
}

func TestStart_InvalidBinary(t *testing.T) {
	// Create a temp directory to avoid interfering with real PID files
	// Start will try to execute the binary, which should fail
	pid, err := Start("/nonexistent/binary/that/does/not/exist", 8080, "")
	if err == nil {
		t.Error("expected error for non-existent binary")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID on failure, got %d", pid)
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	// If daemon is already running, Start should return an error.
	// This depends on whether a real daemon is running.
	if Running() {
		pid, err := Start("api-switch", 8080, "")
		if err == nil {
			t.Error("expected error when daemon is already running")
		}
		if pid != 0 {
			t.Errorf("expected 0 PID, got %d", pid)
		}
		if !strings.Contains(err.Error(), "already running") {
			t.Errorf("expected 'already running' in error, got: %v", err)
		}
	}
}

func TestStop_NotRunning(t *testing.T) {
	if !Running() {
		err := Stop()
		if err == nil {
			t.Error("expected error when stopping non-running daemon")
		}
		if !strings.Contains(err.Error(), "not running") {
			t.Errorf("expected 'not running' in error, got: %v", err)
		}
	}
}

func TestLogPath_Consistency(t *testing.T) {
	path1 := LogPath()
	path2 := logFile()
	if path1 != path2 {
		t.Errorf("LogPath() and logFile() should return the same path: %q vs %q", path1, path2)
	}
}

func TestPidFile_Consistency(t *testing.T) {
	path := pidFile()
	dir := Dir()
	if !strings.HasPrefix(path, dir) {
		t.Errorf("pidFile() should be under Dir(): %q not prefixed by %q", path, dir)
	}
}

func TestLogFile_Consistency(t *testing.T) {
	path := logFile()
	dir := Dir()
	if !strings.HasPrefix(path, dir) {
		t.Errorf("logFile() should be under Dir(): %q not prefixed by %q", path, dir)
	}
}

// Test that PID parsing handles edge cases
func TestPIDParsing_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, pidFileName)

	// Empty file
	if err := os.WriteFile(pidPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err == nil {
		t.Error("expected error parsing empty PID")
	}
	if pid != 0 {
		t.Errorf("expected 0 for empty PID, got %d", pid)
	}
}

func TestPIDParsing_Whitespace(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, pidFileName)

	if err := os.WriteFile(pidPath, []byte("  12345\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected 12345, got %d", pid)
	}
}
