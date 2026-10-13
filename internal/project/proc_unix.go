//go:build unix

package project

import (
	"os/exec"
	"syscall"
	"time"
)

// killProcessGroupOnCancel runs cmd in its own process group and kills the
// whole group on timeout, so children of "sh -c" (test runners, servers)
// don't outlive the command or hold its output pipe open.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
}
