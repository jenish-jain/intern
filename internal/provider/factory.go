package provider

import (
	"fmt"

	"intern/internal/ai/agent"
	"intern/internal/ai/agent/anthropic"
	"intern/internal/config"
)

// NewAgent creates an AI agent based on the configured provider.
// It returns the appropriate implementation of the Agent interface.
//
// Supported providers:
// - "anthropic": Claude API (requires ANTHROPIC_API_KEY)
// - "ollama": Local LLM via Ollama (requires OLLAMA_MODEL)
//
// The factory validates provider-specific requirements and returns
// an error if the provider is unsupported or misconfigured.
func NewAgent(cfg *config.Config) (agent.Agent, error) {
	switch cfg.AIProvider {
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("Anthropic API key is required for provider 'anthropic'")
		}
		return anthropic.NewClient(cfg.AnthropicAPIKey), nil

	case "ollama":
		// Ollama implementation will be added in the next step
		return nil, fmt.Errorf("Ollama provider not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported AI provider: %s (supported: anthropic, ollama)", cfg.AIProvider)
	}
}
