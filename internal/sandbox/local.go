package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// defaultPassEnv are host variables a local process needs to find tools
// and its own config. Credentials (GITHUB_TOKEN, JIRA_API_TOKEN, Slack
// tokens) are deliberately absent.
var defaultPassEnv = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TMPDIR", "TERM",
	"GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS", "GOTOOLCHAIN",
	"NVM_DIR", "NODE_PATH", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
}

// Local runs commands directly on the host with a scrubbed environment.
type Local struct {
	// PassEnv lists extra host variables to pass through.
	PassEnv []string
}

func (l *Local) Isolated() bool { return false }

func (l *Local) Run(ctx context.Context, c Cmd) error {
	if len(c.Args) == 0 {
		return fmt.Errorf("sandbox: empty command")
	}
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = append(scrubbedEnv(append(append([]string{}, defaultPassEnv...), l.PassEnv...)), c.Env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	killGroupOnCancel(cmd)
	return cmd.Run()
}

// scrubbedEnv returns KEY=VALUE pairs for the named host variables.
func scrubbedEnv(names []string) []string {
	var env []string
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		if v, ok := os.LookupEnv(n); ok {
			env = append(env, n+"="+v)
		}
	}
	return env
}
