//go:build unix

package gitresolver

import (
	"os/exec"
	"syscall"
)

// killGroup runs cmd in a process group of its own, so a timeout kills git and every child it started, such as ssh.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
