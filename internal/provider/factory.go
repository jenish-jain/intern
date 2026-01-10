package provider

import (
	"fmt"
	"time"

	"intern/internal/ai/agent"
	"intern/internal/ai/agent/anthropic"
	"intern/internal/ai/agent/ollama"
	"intern/internal/circuitbreaker"
	"intern/internal/config"
)

// NewAgent creates an AI agent based on the configured provider.
// It returns the appropriate implementation of the Agent interface,
// wrapped with circuit breaker protection for reliability.
//
// Supported providers:
// - "anthropic": Claude API (requires ANTHROPIC_API_KEY)
// - "ollama": Local LLM via Ollama (requires OLLAMA_MODEL)
//
// The factory validates provider-specific requirements and returns
// an error if the provider is unsupported or misconfigured.
//
// Circuit breaker protection:
// - Fails fast after 5 consecutive failures (prevents cost/time waste)
// - Reopens after 30s timeout to test recovery
// - Logs state transitions for monitoring
func NewAgent(cfg *config.Config) (agent.Agent, error) {
	var baseAgent agent.Agent
	var err error

	switch cfg.AIProvider {
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("Anthropic API key is required for provider 'anthropic'")
		}
		baseAgent = anthropic.NewClient(cfg.AnthropicAPIKey)

	case "ollama":
		if cfg.OllamaModel == "" {
			return nil, fmt.Errorf("Ollama model is required for provider 'ollama'")
		}
		if cfg.OllamaBaseURL == "" {
			return nil, fmt.Errorf("Ollama base URL is required for provider 'ollama'")
		}
		baseAgent = ollama.NewClient(cfg.OllamaBaseURL, cfg.OllamaModel)

	default:
		return nil, fmt.Errorf("unsupported AI provider: %s (supported: anthropic, ollama)", cfg.AIProvider)
	}

	if err != nil {
		return nil, err
	}

	// Wrap agent with circuit breaker for reliability
	cbConfig := circuitbreaker.Config{
		MaxFailures:           5,              // Open after 5 consecutive failures
		Timeout:               30 * time.Second, // Test recovery after 30s
		MaxConcurrentHalfOpen: 1,              // Only one test request when recovering
	}

	return agent.NewCircuitBreakerAgent(baseAgent, cbConfig), nil
}
