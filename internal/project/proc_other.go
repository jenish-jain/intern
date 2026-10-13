//go:build !unix

package project

import "os/exec"

func killProcessGroupOnCancel(cmd *exec.Cmd) {}
