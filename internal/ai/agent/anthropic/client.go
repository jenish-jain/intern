package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"intern/internal/ai"
	"intern/internal/ai/agent"
	"intern/internal/util"

	"github.com/jenish-jain/logger"
)

// Ensure Client implements agent.Agent
var _ agent.Agent = (*Client)(nil)

const url = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"
const model = "claude-sonnet-4-20250514"

// Client is an implementation of the agent.Agent interface for Anthropic's Claude API.
// It handles communication with the Anthropic API for code generation tasks.
type Client struct {
	APIKey string       // Anthropic API key for authentication
	Model  string       // Claude model identifier to use (e.g., "claude-sonnet-4-20250514")
	HTTP   *http.Client // HTTP client with configured timeout
}

// NewClient creates a new Anthropic API client with default settings.
// The client is configured with a 180-second timeout (3 minutes) to handle large contexts
// from smart context selection, and uses the latest Claude model.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Model:  model,
		HTTP:   &http.Client{Timeout: 180 * time.Second},
	}
}

// PlanChanges asks the model to emit a minimal JSON array of CodeChange items.
// Returns the code changes, usage metrics for cost tracking, and any error.
func (c *Client) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	prompt := agent.BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("prompt in anthropic", "prompt", prompt)

	reqBody := codeGenRequest{
		Model:     c.Model,
		MaxTokens: 16000, // Increased for complex tickets (e.g., Next.js initialization with multiple files)
		Messages:  []messagePart{{Role: "user", Content: prompt}},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, nil, fmt.Errorf("anthropic error %d: failed to read response body: %w", resp.StatusCode, readErr)
		}
		return nil, nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(b))
	}
	var cg codeGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&cg); err != nil {
		return nil, nil, err
	}
	if len(cg.Content) == 0 {
		return nil, nil, fmt.Errorf("empty anthropic response")
	}
	raw := agent.SanitizeResponse(cg.Content[0].Text)
	logger.Debug("AI response (sanitized)", "length", len(raw), "preview", raw[:util.Min(500, len(raw))])

	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		// Try to fix truncated JSON (common when model stops mid-generation)
		fixed := attemptJSONFix(raw)
		if fixErr := json.Unmarshal([]byte(fixed), &changes); fixErr == nil {
			logger.Warn("JSON was truncated, auto-completed successfully",
				"original_length", len(raw),
				"fixed_length", len(fixed))
		} else {
			// Log the full response on error for debugging
			logger.Error("Failed to parse AI response",
				"error", err,
				"response_length", len(raw),
				"response_preview", raw[:util.Min(1000, len(raw))],
				"stop_reason", cg.StopReason)
			return nil, nil, fmt.Errorf("invalid JSON from model: %w", err)
		}
	}
	// Decode base64 content if provided
	for i := range changes {
		if changes[i].Content == "" && changes[i].ContentB64 != "" {
			data, derr := base64.StdEncoding.DecodeString(changes[i].ContentB64)
			if derr == nil {
				changes[i].Content = string(data)
			}
		}
	}

	// Build usage metrics from API response
	metrics := c.buildUsageMetrics(&cg.Usage, len(repoContext))

	return changes, metrics, nil
}

// FixErrors generates fixes for errors in previously generated code.
// This is used by the self-healing system to iteratively improve code that fails quality gates.
func (c *Client) FixErrors(ctx context.Context, ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []agent.CodeChange) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	prompt := agent.BuildFixErrorsPrompt(ticketKey, ticketSummary, errorType, errorOutput, previousChanges, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("fix errors prompt in anthropic", "prompt", prompt)

	reqBody := codeGenRequest{
		Model:     c.Model,
		MaxTokens: 16000,
		Messages:  []messagePart{{Role: "user", Content: prompt}},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, nil, fmt.Errorf("anthropic error %d: failed to read response body: %w", resp.StatusCode, readErr)
		}
		return nil, nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(b))
	}
	var cg codeGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&cg); err != nil {
		return nil, nil, err
	}
	if len(cg.Content) == 0 {
		return nil, nil, fmt.Errorf("empty anthropic response")
	}
	raw := agent.SanitizeResponse(cg.Content[0].Text)
	logger.Debug("AI fix response (sanitized)", "length", len(raw), "preview", raw[:util.Min(500, len(raw))])

	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		// Try to fix truncated JSON (common when model stops mid-generation)
		fixed := attemptJSONFix(raw)
		if fixErr := json.Unmarshal([]byte(fixed), &changes); fixErr == nil {
			logger.Warn("JSON was truncated, auto-completed successfully",
				"original_length", len(raw),
				"fixed_length", len(fixed))
		} else {
			logger.Error("Failed to parse AI fix response",
				"error", err,
				"response_length", len(raw),
				"response_preview", raw[:util.Min(1000, len(raw))],
				"stop_reason", cg.StopReason)
			return nil, nil, fmt.Errorf("invalid JSON from model: %w", err)
		}
	}

	// Decode base64 content if provided
	for i := range changes {
		if changes[i].Content == "" && changes[i].ContentB64 != "" {
			data, derr := base64.StdEncoding.DecodeString(changes[i].ContentB64)
			if derr == nil {
				changes[i].Content = string(data)
			}
		}
	}

	// Build usage metrics - using error output length as context size
	metrics := c.buildUsageMetrics(&cg.Usage, len(errorOutput))

	return changes, metrics, nil
}

// attemptJSONFix tries to fix common JSON truncation issues.
// Returns the fixed JSON string, or the original if unable to fix.
func attemptJSONFix(raw string) string {
	// Common pattern: JSON array truncated mid-object
	// Example: [{"path":"foo","content":"bar...  (missing closing "}]

	// Count braces and brackets
	openBraces := 0
	closeBraces := 0
	openBrackets := 0
	closeBrackets := 0
	inString := false
	escaped := false

	for _, char := range raw {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		switch char {
		case '{':
			openBraces++
		case '}':
			closeBraces++
		case '[':
			openBrackets++
		case ']':
			closeBrackets++
		}
	}

	// If we're still in a string, close it
	result := raw
	if inString {
		result += "\""
	}

	// Close any unclosed braces and brackets
	for i := closeBraces; i < openBraces; i++ {
		result += "}"
	}
	for i := closeBrackets; i < openBrackets; i++ {
		result += "]"
	}

	return result
}

// buildUsageMetrics converts Anthropic-specific usage data to provider-agnostic metrics.
// Calculates cost using the current Claude Sonnet 4 pricing model.
func (c *Client) buildUsageMetrics(usage *Usage, contextBytes int) *agent.UsageMetrics {
	// Calculate cost using the Anthropic pricing model
	cost := ai.CalculateCost(usage.InputTokens, usage.OutputTokens, &ai.ClaudeSonnet4)

	return &agent.UsageMetrics{
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		TotalTokens:   usage.InputTokens + usage.OutputTokens,
		EstimatedCost: cost,
		ContextStats: agent.ContextStats{
			ContextBytes: contextBytes,
			// Strategy, FilesIncluded, and Keywords will be set by the orchestrator
			// when it builds the context
		},
	}
}
