//go:build windows

package daemon

import "os/exec"

func setSysProcAttr(cmd *exec.Cmd) {
	// No special process attributes needed on Windows
}
