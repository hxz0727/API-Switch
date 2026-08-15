package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// isolateDir points the daemon's data dir (which is derived from os.UserHomeDir)
// at a fresh temp directory so tests never touch a real ~/.api-switch and cannot
// interfere with — or be affected by — an actually running daemon.
func isolateDir(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0755); err != nil {
		t.Fatalf("failed to create daemon dir: %v", err)
	}
}

func writePIDFile(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(pidFile(), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}
}

// writeHelperScript creates an executable shell script that records its
// arguments to argsFile and then stays alive (via exec sleep) so it behaves
// like a long-running daemon. exec keeps the same PID for the sleep process,
// which lets tests kill it reliably.
func writeHelperScript(t *testing.T, argsFile string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "daemon-helper.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsFile + "\"\nexec sleep 300\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write helper script: %v", err)
	}
	return script
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func TestDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if want, got := home+"/.api-switch", Dir(); want != got {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestPidFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if want, got := home+"/.api-switch/"+pidFileName, pidFile(); want != got {
		t.Errorf("pidFile() = %q, want %q", got, want)
	}
}

func TestLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if want, got := home+"/.api-switch/"+logFileName, logFile(); want != got {
		t.Errorf("logFile() = %q, want %q", got, want)
	}
}

func TestLogPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := LogPath(); got != logFile() {
		t.Errorf("LogPath() = %q, want %q", got, logFile())
	}
}

func TestPathsUnderDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, p := range []string{pidFile(), logFile(), LogPath()} {
		if !strings.HasPrefix(p, Dir()) {
			t.Errorf("%q should be under Dir() %q", p, Dir())
		}
	}
}

func TestRunning_NoPIDFile(t *testing.T) {
	isolateDir(t)
	if Running() {
		t.Error("Running() should be false without a pid file")
	}
}

func TestRunning_InvalidContent(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "not-a-pid")
	if Running() {
		t.Error("Running() should be false for invalid pid file content")
	}
}

func TestRunning_NegativePID(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "-1")
	if Running() {
		t.Error("Running() should be false for a negative pid")
	}
}

func TestRunning_ZeroPID(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "0")
	if Running() {
		t.Error("Running() should be false for pid 0")
	}
}

func TestRunning_NonExistentProcess(t *testing.T) {
	isolateDir(t)
	// A pid above pid_max: kill(pid, 0) deterministically returns ESRCH.
	writePIDFile(t, "99999999")
	if Running() {
		t.Error("Running() should be false for a non-existent process")
	}
}

func TestRunning_LiveProcess(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, strconv.Itoa(os.Getpid()))
	if !Running() {
		t.Error("Running() should be true for the live test process pid")
	}
	// The pid file must not be removed for a live process.
	if _, err := os.Stat(pidFile()); err != nil {
		t.Errorf("pid file should still exist: %v", err)
	}
}

func TestPID_NoFile(t *testing.T) {
	isolateDir(t)
	if got := PID(); got != -1 {
		t.Errorf("PID() = %d, want -1", got)
	}
}

func TestPID_InvalidContent(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "not-a-pid")
	if got := PID(); got != -1 {
		t.Errorf("PID() = %d, want -1", got)
	}
}

func TestPID_TrailingNewline(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "12345\n")
	if got := PID(); got != -1 {
		t.Errorf("PID() = %d, want -1 (Atoi fails on trailing newline)", got)
	}
}

func TestPID_Valid(t *testing.T) {
	isolateDir(t)
	writePIDFile(t, "12345")
	if got := PID(); got != 12345 {
		t.Errorf("PID() = %d, want 12345", got)
	}
}

func TestStop_NotRunning(t *testing.T) {
	isolateDir(t)
	err := Stop()
	if err == nil {
		t.Fatal("expected error stopping a non-running daemon")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStop_Success(t *testing.T) {
	isolateDir(t)
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	writePIDFile(t, strconv.Itoa(cmd.Process.Pid))
	if !Running() {
		t.Fatal("Running() should be true before stop")
	}
	if err := Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	if _, err := os.Stat(pidFile()); !os.IsNotExist(err) {
		t.Error("pid file should be removed after stop")
	}
	if Running() {
		t.Error("Running() should be false after stop")
	}
	// Stop sent SIGTERM, so the child should exit promptly; reap it.
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("helper process did not terminate after SIGTERM")
	}
}
