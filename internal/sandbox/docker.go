package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ContainerWorkdir is where the workspace is mounted in the container.
const ContainerWorkdir = "/workspace"

// Docker runs each command in a fresh container with only the workspace
// mounted. The container gets no host environment and no credentials
// beyond Cmd.Env.
type Docker struct {
	Image string
	// Network is passed to --network ("bridge" when empty). Use "none" for
	// jobs that need no network, or a network with an egress proxy to
	// restrict hosts.
	Network string
	// Binary is the docker CLI ("docker" when empty).
	Binary string
	// ExtraArgs are appended to "docker run" before the image, e.g.
	// resource limits ("--memory=4g", "--cpus=2").
	ExtraArgs []string
}

func (d *Docker) Isolated() bool { return true }

func (d *Docker) Run(ctx context.Context, c Cmd) error {
	args, err := d.runArgs(c)
	if err != nil {
		return err
	}
	bin := d.Binary
	if bin == "" {
		bin = "docker"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	killGroupOnCancel(cmd)
	return cmd.Run()
}

func (d *Docker) runArgs(c Cmd) ([]string, error) {
	if d.Image == "" {
		return nil, fmt.Errorf("sandbox: docker image not configured")
	}
	if len(c.Args) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, err
	}
	network := d.Network
	if network == "" {
		network = "bridge"
	}
	args := []string{
		"run", "--rm", "-i",
		"--network", network,
		"--security-opt", "no-new-privileges",
		"-v", dir + ":" + ContainerWorkdir,
		"-w", ContainerWorkdir,
		// A writable HOME inside the container for tool caches and config.
		"-e", "HOME=/tmp/home",
	}
	// Run as the host user so files written to the mounted workspace stay
	// owned by the agent, not root.
	if uid := os.Getuid(); uid >= 0 {
		args = append(args, "--user", strconv.Itoa(uid)+":"+strconv.Itoa(os.Getgid()))
	}
	for _, kv := range c.Env {
		if !strings.Contains(kv, "=") {
			return nil, fmt.Errorf("sandbox: env entry %q is not KEY=VALUE", kv)
		}
		args = append(args, "-e", kv)
	}
	args = append(args, d.ExtraArgs...)
	args = append(args, d.Image)
	return append(args, c.Args...), nil
}
