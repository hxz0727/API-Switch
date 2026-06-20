//go:build !windows

package update

import "syscall"

func syscallExec(binary string, args []string, env []string) error {
	return syscall.Exec(binary, append([]string{binary}, args...), env)
}
