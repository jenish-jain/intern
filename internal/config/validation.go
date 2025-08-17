package config

import (
	"fmt"
)

func ValidateConfig(cfg *Config) error {
	if cfg.JiraURL == "" || cfg.JiraEmail == "" || cfg.JiraAPIToken == "" || cfg.JiraProject == "" {
		return fmt.Errorf("missing JIRA configuration")
	}
	if cfg.GitHubToken == "" || cfg.GitHubOwner == "" || cfg.GitHubRepo == "" {
		return fmt.Errorf("missing GitHub configuration")
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("missing Anthropic API key")
	}
	if cfg.AgentUsername == "" {
		return fmt.Errorf("missing agent username")
	}
	if cfg.PollingInterval == "" {
		return fmt.Errorf("missing polling interval")
	}
	if cfg.MaxConcurrentTickets <= 0 {
		return fmt.Errorf("max concurrent tickets must be > 0")
	}
	return nil
}
