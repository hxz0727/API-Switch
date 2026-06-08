package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	pidFileName = "api-switch.pid"
	logFileName = "api-switch.log"
)

// Dir returns the daemon data directory.
func Dir() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("%s/.api-switch", home)
}

func pidFile() string { return Dir() + "/" + pidFileName }
func logFile() string { return Dir() + "/" + logFileName }

// Running checks whether the daemon is currently running.
func Running() bool {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile())
		return false
	}
	// On Unix, FindProcess always succeeds; check with signal 0
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

// PID returns the daemon PID, or -1 if not running.
func PID() int {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return -1
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return -1
	}
	return pid
}

// Start launches the api-switch serve process in the background.
// Returns the PID on success, or an error.
func Start(binary string, port int, cfgPath string) (int, error) {
	if Running() {
		return 0, fmt.Errorf("api-switch is already running (PID %d)", PID())
	}

	args := []string{"serve"}
	if port != 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}

	// Open log file for stdout/stderr
	out, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, fmt.Errorf("cannot open log file: %w", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Stdout = out
	cmd.Stderr = out

	// Detach from parent: set new process group so it survives terminal hangup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		out.Close()
		return 0, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Write PID file
	if err := os.WriteFile(pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		cmd.Process.Kill()
		out.Close()
		return 0, fmt.Errorf("failed to write PID file: %w", err)
	}

	// Don't wait for the child — release the file descriptor and let it run
	go func() {
		cmd.Wait()
		out.Close()
		os.Remove(pidFile())
	}()

	return cmd.Process.Pid, nil
}

// Stop sends SIGTERM to the daemon process and removes the PID file.
func Stop() error {
	if !Running() {
		return fmt.Errorf("api-switch is not running")
	}
	pid := PID()
	p, _ := os.FindProcess(pid)
	if err := p.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidFile())
		return fmt.Errorf("failed to stop daemon: %w", err)
	}
	os.Remove(pidFile())
	return nil
}

// LogPath returns the path to the daemon log file.
func LogPath() string { return logFile() }
