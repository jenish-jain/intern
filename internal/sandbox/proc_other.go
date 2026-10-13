//go:build !unix

package sandbox

import "os/exec"

func killGroupOnCancel(cmd *exec.Cmd) {}
