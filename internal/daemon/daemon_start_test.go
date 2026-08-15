package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStart_InvalidBinary(t *testing.T) {
	isolateDir(t)
	pid, err := Start("/nonexistent/binary/that/does/not/exist", 8080, "")
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID on failure, got %d", pid)
	}
	if !strings.Contains(err.Error(), "failed to start daemon") {
		t.Errorf("unexpected error: %v", err)
	}
	// The log file is opened before exec, so it should exist even on failure.
	if _, err := os.Stat(logFile()); err != nil {
		t.Errorf("log file should have been created: %v", err)
	}
	if _, err := os.Stat(pidFile()); !os.IsNotExist(err) {
		t.Error("pid file should not exist after failed start")
	}
}

func TestStart_LogOpenFailure(t *testing.T) {
	// Point HOME at a directory that doesn't exist so opening the log fails.
	t.Setenv("HOME", filepath.Join(t.TempDir(), "missing"))
	pid, err := Start("/bin/true", 8080, "")
	if err == nil {
		t.Fatal("expected error opening log file")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID, got %d", pid)
	}
	if !strings.Contains(err.Error(), "cannot open log file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, strconv.Itoa(os.Getpid()))
	pid, err := Start("/bin/true", 8080, "")
	if err == nil {
		t.Fatal("expected error when daemon is already running")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID, got %d", pid)
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStart_PIDWriteFailure(t *testing.T) {
	isolateDir(t)
	script := writeHelperScript(t, filepath.Join(t.TempDir(), "args"))
	// Make pidFile().tmp a directory so os.WriteFile fails with EISDIR.
	if err := os.Mkdir(pidFile()+".tmp", 0755); err != nil {
		t.Fatal(err)
	}
	pid, err := Start(script, 8080, "")
	if err == nil {
		t.Fatal("expected error writing PID temp file")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID, got %d", pid)
	}
	if !strings.Contains(err.Error(), "failed to write PID temp file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStart_RenameFailure(t *testing.T) {
	isolateDir(t)
	script := writeHelperScript(t, filepath.Join(t.TempDir(), "args"))
	// Make pidFile() a directory so renaming the temp file onto it fails (EISDIR).
	if err := os.Mkdir(pidFile(), 0755); err != nil {
		t.Fatal(err)
	}
	pid, err := Start(script, 8080, "")
	if err == nil {
		t.Fatal("expected error renaming PID file")
	}
	if pid != 0 {
		t.Errorf("expected 0 PID, got %d", pid)
	}
	if !strings.Contains(err.Error(), "failed to rename PID file") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := os.Stat(pidFile() + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp pid file should have been cleaned up")
	}
}

func TestStart_Success(t *testing.T) {
	isolateDir(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	script := writeHelperScript(t, argsFile)
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	pid, err := Start(script, 8080, cfgPath)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}
	t.Cleanup(func() {
		p, _ := os.FindProcess(pid)
		_ = p.Signal(syscall.SIGKILL)
		_ = os.Remove(pidFile())
	})

	// The pid file should contain the started PID.
	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(pidFile())
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(data)) == strconv.Itoa(pid)
	}, "pid file to contain the daemon PID")

	if !Running() {
		t.Error("Running() should be true after successful start")
	}
	if got := PID(); got != pid {
		t.Errorf("PID() = %d, want %d", got, pid)
	}
	if _, err := os.Stat(logFile()); err != nil {
		t.Errorf("log file should exist: %v", err)
	}

	// The child should have received the constructed arguments.
	want := strings.Join([]string{"serve", "--port", "8080", "--config", cfgPath}, "\n")
	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(data)) == want
	}, "helper script to record the daemon arguments")

	if err := Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	if _, err := os.Stat(pidFile()); !os.IsNotExist(err) {
		t.Error("pid file should be removed after stop")
	}
	if Running() {
		t.Error("Running() should be false after stop")
	}
}

func TestStart_Success_NoPortNoConfig(t *testing.T) {
	isolateDir(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	script := writeHelperScript(t, argsFile)

	pid, err := Start(script, 0, "")
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}
	t.Cleanup(func() {
		p, _ := os.FindProcess(pid)
		_ = p.Signal(syscall.SIGKILL)
		_ = os.Remove(pidFile())
	})

	// With no port/config the child should only receive "serve".
	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(data)) == "serve"
	}, "helper script to record the daemon arguments")

	if err := Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}
