// Package project reads the target repository's contract with intern:
// how to verify a change and which paths agents must not touch. The
// contract lives in the target repo so the architect persona can maintain
// it alongside the code.
package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigPath is the contract file's location relative to the repo root.
const ConfigPath = ".intern/project.yaml"

// DefaultCommandTimeout bounds each verify command.
const DefaultCommandTimeout = 10 * time.Minute

// maxOutputBytes bounds the captured output of each command.
const maxOutputBytes = 16 * 1024

// Command is a named shell command run from the repo root.
type Command struct {
	Name    string `yaml:"name"`
	Run     string `yaml:"run"`
	Timeout string `yaml:"timeout,omitempty"` // Go duration, e.g. "15m"
}

// Config is the parsed contract.
type Config struct {
	// Setup commands prepare the workspace (e.g. install dependencies)
	// before verify runs.
	Setup []Command `yaml:"setup,omitempty"`
	// Verify commands must all pass for a change to be pushed.
	Verify []Command `yaml:"verify"`
	// ProtectedPaths are path prefixes agents must not change.
	ProtectedPaths []string `yaml:"protected_paths,omitempty"`
}

// Load reads the contract from repoRoot. found is false when the file
// doesn't exist, which is not an error.
func Load(repoRoot string) (cfg Config, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, ConfigPath))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read %s: %w", ConfigPath, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("parse %s: %w", ConfigPath, err)
	}
	for i, c := range append(append([]Command{}, cfg.Setup...), cfg.Verify...) {
		if strings.TrimSpace(c.Run) == "" {
			return Config{}, true, fmt.Errorf("parse %s: command %d (%q) has no run", ConfigPath, i, c.Name)
		}
		if c.Timeout != "" {
			if _, err := time.ParseDuration(c.Timeout); err != nil {
				return Config{}, true, fmt.Errorf("parse %s: command %q timeout: %w", ConfigPath, c.Name, err)
			}
		}
	}
	return cfg, true, nil
}

// CheckResult is the outcome of one command.
type CheckResult struct {
	Name     string
	Command  string
	Passed   bool
	Output   string // combined stdout/stderr, truncated in the middle
	Duration time.Duration
}

// RunCommands runs cmds in order from repoRoot and stops at the first
// failure. ok is true only if every command passed.
func RunCommands(ctx context.Context, repoRoot string, cmds []Command) (results []CheckResult, ok bool) {
	for _, c := range cmds {
		r := runOne(ctx, repoRoot, c)
		results = append(results, r)
		if !r.Passed {
			return results, false
		}
	}
	return results, true
}

func runOne(ctx context.Context, repoRoot string, c Command) CheckResult {
	timeout := DefaultCommandTimeout
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err == nil {
			timeout = d
		}
	}
	name := c.Name
	if name == "" {
		name = c.Run
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(cctx, "sh", "-c", c.Run)
	cmd.Dir = repoRoot
	killProcessGroupOnCancel(cmd)
	out, err := cmd.CombinedOutput()
	res := CheckResult{
		Name:     name,
		Command:  c.Run,
		Passed:   err == nil,
		Output:   truncateMiddle(strings.TrimSpace(string(out)), maxOutputBytes),
		Duration: time.Since(start),
	}
	if cctx.Err() == context.DeadlineExceeded {
		res.Output = strings.TrimSpace(res.Output + "\n(timed out after " + timeout.String() + ")")
	}
	return res
}

// Summary renders results as PR-friendly lines, with output for failures.
func Summary(results []CheckResult) []string {
	var lines []string
	for _, r := range results {
		if r.Passed {
			lines = append(lines, fmt.Sprintf("%s: PASSED (%s)", r.Name, r.Duration.Round(time.Second)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: FAILED", r.Name))
		if r.Output != "" {
			lines = append(lines, "```\n"+r.Output+"\n```")
		}
	}
	return lines
}

// FailureReport renders failed results as feedback for a runner.
func FailureReport(results []CheckResult) string {
	var b strings.Builder
	for _, r := range results {
		if r.Passed {
			continue
		}
		fmt.Fprintf(&b, "Command `%s` (%s) failed:\n%s\n\n", r.Command, r.Name, r.Output)
	}
	return strings.TrimSpace(b.String())
}

func truncateMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max / 2
	return s[:head] + "\n...\n" + s[len(s)-(max-head):]
}
