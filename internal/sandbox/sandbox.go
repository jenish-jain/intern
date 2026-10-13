// Package sandbox runs a runner's process (for example the Claude Code
// CLI) somewhere it can do no harm outside the ticket's workspace.
//
// Two implementations exist. Local runs the process on the same host with
// a scrubbed environment. It is good for development and for single-tenant
// deployments, but it is not a security boundary. Docker runs each job in a
// fresh container with only the workspace mounted, which is.
package sandbox

import (
	"context"
	"io"
)

// Cmd is a process to run in the sandbox.
type Cmd struct {
	// Dir is the host path of the workspace. It is the working directory
	// of the process (mounted into the container for Docker).
	Dir  string
	Args []string
	// Env holds extra KEY=VALUE pairs. Nothing else from the host
	// environment reaches the process unless the sandbox allows it.
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Sandbox runs commands in isolation.
type Sandbox interface {
	// Run runs cmd to completion. A non-zero exit is returned as an error.
	Run(ctx context.Context, cmd Cmd) error
	// Isolated reports whether the sandbox is a real boundary, which is
	// what makes it safe to let an agent skip permission prompts.
	Isolated() bool
}
