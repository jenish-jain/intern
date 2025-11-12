package ollama

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"intern/internal/ai/agent"
	"intern/internal/util"

	"github.com/jenish-jain/logger"
)

// Ensure Client implements agent.Agent
var _ agent.Agent = (*Client)(nil)

// Client is an implementation of the agent.Agent interface for Ollama.
// It handles communication with a local or remote Ollama instance for code generation.
type Client struct {
	BaseURL string       // Ollama server URL (e.g., "http://localhost:11434")
	Model   string       // Model name (e.g., "qwen2.5-coder:7b", "deepseek-coder:6.7b")
	HTTP    *http.Client // HTTP client with configured timeout
}

// NewClient creates a new Ollama API client with default settings.
// The client is configured with a 120-second timeout to accommodate slower local models.
func NewClient(baseURL, model string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"), // Remove trailing slash if present
		Model:   model,
		HTTP:    &http.Client{Timeout: 120 * time.Second}, // Longer timeout for local models
	}
}

// PlanChanges asks the model to emit a minimal JSON array of CodeChange items.
// Returns the code changes, usage metrics for tracking, and any error.
func (c *Client) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	prompt := agent.BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("prompt in ollama", "prompt_length", len(prompt))

	reqBody := GenerateRequest{
		Model:  c.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json", // Request JSON output format
		Options: map[string]interface{}{
			"temperature": 0.2, // Lower temperature for more deterministic code generation
			"top_p":       0.9,
			"num_predict": 16000, // Max tokens to generate
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/generate", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("ollama request failed: %w (ensure Ollama is running at %s)", err, c.BaseURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, nil, fmt.Errorf("ollama error %d: failed to read response body: %w", resp.StatusCode, readErr)
		}

		// Try to parse as error response
		var errResp ErrorResponse
		if json.Unmarshal(b, &errResp) == nil && errResp.Error != "" {
			return nil, nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, errResp.Error)
		}

		return nil, nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(b))
	}

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	if genResp.Response == "" {
		return nil, nil, fmt.Errorf("empty response from ollama")
	}

	raw := agent.SanitizeResponse(genResp.Response)
	logger.Debug("AI response (sanitized)", "length", len(raw), "preview", raw[:util.Min(500, len(raw))])

	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		// Log the full response on error for debugging
		logger.Error("Failed to parse AI response",
			"error", err,
			"response_length", len(raw),
			"response_preview", raw[:util.Min(1000, len(raw))],
			"model", c.Model)
		return nil, nil, fmt.Errorf("invalid JSON from model: %w", err)
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

	// Build usage metrics from Ollama response
	metrics := c.buildUsageMetrics(&genResp, len(repoContext))

	return changes, metrics, nil
}

// buildUsageMetrics converts Ollama-specific usage data to provider-agnostic metrics.
// Since Ollama is local, cost is always zero, but we still track token usage for monitoring.
func (c *Client) buildUsageMetrics(resp *GenerateResponse, contextBytes int) *agent.UsageMetrics {
	// Ollama is free (local execution), so cost is always 0
	inputTokens := resp.PromptEvalCount
	outputTokens := resp.EvalCount
	totalTokens := inputTokens + outputTokens

	// Calculate inference speed (tokens per second)
	var tokensPerSecond float64
	if resp.EvalDuration > 0 {
		tokensPerSecond = float64(outputTokens) / (float64(resp.EvalDuration) / 1e9)
	}

	logger.Debug("Ollama performance",
		"model", c.Model,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"total_duration_ms", resp.TotalDuration/1e6,
		"tokens_per_second", fmt.Sprintf("%.2f", tokensPerSecond))

	return &agent.UsageMetrics{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		TotalTokens:   totalTokens,
		EstimatedCost: 0.0, // Local execution is free
		ContextStats: agent.ContextStats{
			ContextBytes: contextBytes,
			// Strategy, FilesIncluded, and Keywords will be set by the orchestrator
		},
	}
}
