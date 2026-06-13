package agent

import (
	"context"
	"fmt"
	"intern/internal/circuitbreaker"

	logger "github.com/jenish-jain/logger"
)

// CircuitBreakerAgent wraps an Agent with circuit breaker protection
// to prevent cascading failures and excessive costs when AI calls fail repeatedly.
type CircuitBreakerAgent struct {
	agent          Agent
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// NewCircuitBreakerAgent creates a new circuit breaker-protected agent
func NewCircuitBreakerAgent(agent Agent, config circuitbreaker.Config) *CircuitBreakerAgent {
	// Set up state change logging if not provided
	if config.OnStateChange == nil {
		config.OnStateChange = func(from, to circuitbreaker.State) {
			logger.Warn("AI circuit breaker state changed",
				"from", from.String(),
				"to", to.String())

			// Log additional context based on state
			switch to {
			case circuitbreaker.StateOpen:
				logger.Error("AI circuit breaker OPEN - failing fast to prevent costs",
					"reason", "too many consecutive failures",
					"recommendation", "check AI provider status and logs")
			case circuitbreaker.StateHalfOpen:
				logger.Info("AI circuit breaker HALF_OPEN - testing if service recovered")
			case circuitbreaker.StateClosed:
				logger.Info("AI circuit breaker CLOSED - normal operation resumed")
			}
		}
	}

	cb := circuitbreaker.New(config)
	return &CircuitBreakerAgent{
		agent:          agent,
		circuitBreaker: cb,
	}
}

// PlanChanges wraps the underlying agent's PlanChanges with circuit breaker protection
func (a *CircuitBreakerAgent) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]CodeChange, []string, *UsageMetrics, error) {
	var changes []CodeChange
	var needFiles []string
	var metrics *UsageMetrics
	var err error

	cbErr := a.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		changes, needFiles, metrics, err = a.agent.PlanChanges(ctx, ticketKey, ticketSummary, ticketDescription, repoContext)
		return err
	})

	// If circuit breaker itself returned an error (circuit open), return it
	if cbErr != nil && (cbErr == circuitbreaker.ErrCircuitOpen || cbErr == circuitbreaker.ErrTooManyRequests) {
		logger.Error("AI request rejected by circuit breaker",
			"ticket", ticketKey,
			"state", a.circuitBreaker.State().String(),
			"failures", a.circuitBreaker.Failures(),
			"error", cbErr)
		return nil, nil, nil, fmt.Errorf("AI service unavailable (circuit breaker %s): %w", a.circuitBreaker.State().String(), cbErr)
	}

	// Return the actual result (might still be an error from the AI call itself)
	return changes, needFiles, metrics, err
}

// FixErrors wraps the underlying agent's FixErrors with circuit breaker protection
func (a *CircuitBreakerAgent) FixErrors(ctx context.Context, ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []CodeChange, fileContents map[string]string) ([]CodeChange, *UsageMetrics, error) {
	var changes []CodeChange
	var metrics *UsageMetrics
	var err error

	cbErr := a.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		changes, metrics, err = a.agent.FixErrors(ctx, ticketKey, ticketSummary, errorType, errorOutput, previousChanges, fileContents)
		return err
	})

	// If circuit breaker itself returned an error (circuit open), return it
	if cbErr != nil && (cbErr == circuitbreaker.ErrCircuitOpen || cbErr == circuitbreaker.ErrTooManyRequests) {
		logger.Error("AI fix request rejected by circuit breaker",
			"ticket", ticketKey,
			"error_type", errorType,
			"state", a.circuitBreaker.State().String(),
			"failures", a.circuitBreaker.Failures(),
			"error", cbErr)
		return nil, nil, fmt.Errorf("AI service unavailable (circuit breaker %s): %w", a.circuitBreaker.State().String(), cbErr)
	}

	// Return the actual result (might still be an error from the AI call itself)
	return changes, metrics, err
}

// Stats returns current circuit breaker statistics
func (a *CircuitBreakerAgent) Stats() circuitbreaker.Stats {
	return a.circuitBreaker.Stats()
}

// Reset manually resets the circuit breaker (for testing or manual intervention)
func (a *CircuitBreakerAgent) Reset() {
	logger.Info("Manually resetting AI circuit breaker")
	a.circuitBreaker.Reset()
}
