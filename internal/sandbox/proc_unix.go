//go:build unix

package sandbox

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroupOnCancel kills the process and its children when the context
// ends, so a timed-out agent can't leave tool processes running.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 10 * time.Second
}
