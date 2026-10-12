package provider

import (
	"fmt"
	"time"

	"intern/internal/config"
	"intern/internal/runner"
	"intern/internal/runner/claudecode"
	"intern/internal/sandbox"
)

// NewSandbox builds the sandbox selected by SANDBOX.
func NewSandbox(cfg *config.Config) (sandbox.Sandbox, error) {
	switch cfg.Sandbox {
	case "", "local":
		return &sandbox.Local{PassEnv: cfg.SandboxPassEnv}, nil
	case "docker":
		return &sandbox.Docker{Image: cfg.SandboxImage, Network: cfg.SandboxNetwork}, nil
	default:
		return nil, fmt.Errorf("unsupported sandbox %q (supported: local, docker)", cfg.Sandbox)
	}
}

// NewRunner returns the runner selected by AI_RUNNER, or nil for
// "legacy", in which case the coordinator keeps its built-in legacy
// runner.
func NewRunner(cfg *config.Config) (runner.Runner, error) {
	switch cfg.AIRunner {
	case "", "legacy":
		return nil, nil
	case "claude-code":
		sb, err := NewSandbox(cfg)
		if err != nil {
			return nil, err
		}
		var timeout time.Duration
		if cfg.RunnerTimeout != "" {
			if timeout, err = time.ParseDuration(cfg.RunnerTimeout); err != nil {
				return nil, fmt.Errorf("RUNNER_TIMEOUT: %w", err)
			}
		}
		return claudecode.New(claudecode.Config{
			Binary:           cfg.ClaudeBinary,
			Model:            cfg.ClaudeModel,
			Effort:           cfg.ClaudeEffort,
			APIKey:           cfg.AnthropicAPIKey,
			Sandbox:          sb,
			AllowedCommands:  cfg.RunnerAllowedCommands,
			DefaultBudgetUSD: cfg.TicketBudgetUSD,
			Timeout:          timeout,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported AI runner %q (supported: legacy, claude-code)", cfg.AIRunner)
	}
}
