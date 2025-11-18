package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"intern/internal/ai/agent"

	"github.com/jenish-jain/logger"
)

// HealResult represents the result of a single healing attempt
type HealResult struct {
	Attempt      int                  // Attempt number (1-based)
	Success      bool                 // Whether the healing succeeded
	ErrorType    string               // Type of error ("test", "vet", "build")
	ErrorOutput  string               // Error output from quality gate
	FixedChanges []agent.CodeChange   // Changes applied to fix the error
	Metrics      *agent.UsageMetrics  // AI usage metrics for this healing attempt
}

// SelfHealingResult contains the overall result of the self-healing process
type SelfHealingResult struct {
	Attempts      []HealResult // All healing attempts
	Success       bool         // Whether healing ultimately succeeded
	TotalAttempts int          // Total number of attempts made
	TotalCost     float64      // Total cost of all healing attempts
}

// runQualityGate executes a quality gate command and returns the output and error
func runQualityGate(ctx context.Context, repoPath, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	return output, err
}

// runGoVet runs go vet on the repository
func runGoVet(ctx context.Context, repoPath string) (string, error) {
	return runQualityGate(ctx, repoPath, "go", "vet", "./...")
}

// runGoTest runs go test on the repository
func runGoTest(ctx context.Context, repoPath string) (string, error) {
	return runQualityGate(ctx, repoPath, "go", "test", "./...")
}

// runGoBuild runs go build on the repository
func runGoBuild(ctx context.Context, repoPath string) (string, error) {
	return runQualityGate(ctx, repoPath, "go", "build", "./...")
}

// applyCodeChange writes a code change to disk
func applyCodeChange(repoPath string, change agent.CodeChange) error {
	absPath := filepath.Join(repoPath, change.Path)

	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Write the file content
	if err := os.WriteFile(absPath, []byte(change.Content), 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// tryHealErrors attempts to fix errors by calling the AI agent's FixErrors method
func (c *Coordinator) tryHealErrors(
	ctx context.Context,
	ticketKey, ticketSummary, errorType, errorOutput string,
	previousChanges []agent.CodeChange,
	repoPath string,
) (*HealResult, error) {
	logger.Info("Attempting to heal errors",
		"ticket", ticketKey,
		"error_type", errorType,
		"previous_changes", len(previousChanges))

	// Call AI to generate fixes
	fixes, metrics, err := c.Agent.FixErrors(ctx, ticketKey, ticketSummary, errorType, errorOutput, previousChanges)
	if err != nil {
		return nil, fmt.Errorf("AI fix generation failed: %w", err)
	}

	logger.Info("AI generated fixes",
		"ticket", ticketKey,
		"fixes", len(fixes),
		"cost", metrics.EstimatedCost)

	// Apply the fixes
	for _, change := range fixes {
		if err := applyCodeChange(repoPath, change); err != nil {
			return nil, fmt.Errorf("failed to apply fix for %s: %w", change.Path, err)
		}
	}

	return &HealResult{
		Success:      false, // Will be updated after validation
		ErrorType:    errorType,
		ErrorOutput:  errorOutput,
		FixedChanges: fixes,
		Metrics:      metrics,
	}, nil
}

// selfHealingPipeline implements the self-healing retry loop
func (c *Coordinator) selfHealingPipeline(
	ctx context.Context,
	ticketKey, ticketSummary string,
	initialChanges []agent.CodeChange,
	repoPath string,
) (*SelfHealingResult, error) {
	if !c.Cfg.SelfHealEnabled {
		// Self-healing disabled, return success immediately
		return &SelfHealingResult{Success: true}, nil
	}

	result := &SelfHealingResult{
		Attempts: make([]HealResult, 0),
		Success:  true, // Assume success unless we find errors
	}

	// Track current changes (starts with initial changes)
	currentChanges := initialChanges

	// Try up to MaxAttempts times
	for attempt := 1; attempt <= c.Cfg.SelfHealMaxAttempts; attempt++ {
		logger.Info("Quality gate check",
			"ticket", ticketKey,
			"attempt", attempt,
			"max_attempts", c.Cfg.SelfHealMaxAttempts)

		// Run quality gates and check for errors
		var errorType, errorOutput string
		var hasError bool

		// Check build (if enabled)
		if c.Cfg.SelfHealOnBuild {
			output, err := runGoBuild(ctx, repoPath)
			if err != nil {
				errorType = "build"
				errorOutput = output
				hasError = true
				logger.Warn("Build failed, attempting heal",
					"ticket", ticketKey,
					"attempt", attempt)
			}
		}

		// Check vet (if enabled and no build error)
		if !hasError && c.Cfg.SelfHealOnVet {
			output, err := runGoVet(ctx, repoPath)
			if err != nil {
				errorType = "vet"
				errorOutput = output
				hasError = true
				logger.Warn("Vet failed, attempting heal",
					"ticket", ticketKey,
					"attempt", attempt)
			}
		}

		// Check tests (if enabled and no other errors)
		if !hasError && c.Cfg.SelfHealOnTests {
			output, err := runGoTest(ctx, repoPath)
			if err != nil {
				errorType = "test"
				errorOutput = output
				hasError = true
				logger.Warn("Tests failed, attempting heal",
					"ticket", ticketKey,
					"attempt", attempt)
			}
		}

		// If no errors, we're done!
		if !hasError {
			logger.Info("Quality gates passed",
				"ticket", ticketKey,
				"attempt", attempt,
				"total_heal_attempts", len(result.Attempts))
			result.Success = true
			break
		}

		// If we have an error and haven't exceeded max attempts, try to heal
		if hasError && attempt < c.Cfg.SelfHealMaxAttempts {
			healResult, err := c.tryHealErrors(ctx, ticketKey, ticketSummary, errorType, errorOutput, currentChanges, repoPath)
			if err != nil {
				logger.Error("Healing attempt failed",
					"ticket", ticketKey,
					"attempt", attempt,
					"error", err)

				// Record failed healing attempt
				result.Attempts = append(result.Attempts, HealResult{
					Attempt:     attempt,
					Success:     false,
					ErrorType:   errorType,
					ErrorOutput: errorOutput,
				})
				result.Success = false
				break
			}

			// Record successful healing attempt (fixes were applied)
			healResult.Attempt = attempt
			result.Attempts = append(result.Attempts, *healResult)
			result.TotalCost += healResult.Metrics.EstimatedCost

			// Update current changes to include the fixes
			currentChanges = healResult.FixedChanges

			logger.Info("Healing attempt complete, will retry quality gates",
				"ticket", ticketKey,
				"attempt", attempt,
				"cost", healResult.Metrics.EstimatedCost)

			// Continue to next attempt (will re-run quality gates)
			continue
		}

		// If we've exceeded max attempts, record failure
		if attempt >= c.Cfg.SelfHealMaxAttempts {
			logger.Error("Exceeded max healing attempts",
				"ticket", ticketKey,
				"max_attempts", c.Cfg.SelfHealMaxAttempts,
				"error_type", errorType)

			result.Attempts = append(result.Attempts, HealResult{
				Attempt:     attempt,
				Success:     false,
				ErrorType:   errorType,
				ErrorOutput: errorOutput,
			})
			result.Success = false
			break
		}
	}

	result.TotalAttempts = len(result.Attempts)

	// Log final result
	if result.Success {
		logger.Info("Self-healing succeeded",
			"ticket", ticketKey,
			"total_attempts", result.TotalAttempts,
			"total_cost", result.TotalCost)
	} else {
		logger.Error("Self-healing failed",
			"ticket", ticketKey,
			"total_attempts", result.TotalAttempts,
			"total_cost", result.TotalCost)
	}

	return result, nil
}

// extractErrorSummary extracts a concise error summary from full error output
// This is useful for logging and metrics
func extractErrorSummary(errorOutput string) string {
	lines := strings.Split(errorOutput, "\n")

	// Take first few error lines (up to 3)
	summary := make([]string, 0, 3)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Look for common error indicators
		if strings.Contains(trimmed, "FAIL") ||
		   strings.Contains(trimmed, "error") ||
		   strings.Contains(trimmed, "Error") ||
		   strings.Contains(trimmed, "undefined") {
			summary = append(summary, trimmed)
			if len(summary) >= 3 {
				break
			}
		}
	}

	if len(summary) == 0 {
		// No specific errors found, take first non-empty line
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				return trimmed
			}
		}
		return "Unknown error"
	}

	return strings.Join(summary, "; ")
}
