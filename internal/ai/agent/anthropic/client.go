package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
const model = "claude-opus-4-5-20251101"

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
// Returns the code changes (or needFiles, if the model requested full content
// for additional files), usage metrics for cost tracking, and any error.
func (c *Client) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]agent.CodeChange, []string, *agent.UsageMetrics, error) {
	prompt := agent.BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("prompt in anthropic", "prompt", prompt)

	reqBody := codeGenRequest{
		Model:     c.Model,
		MaxTokens: 16000, // Increased for complex tickets (e.g., Next.js initialization with multiple files)
		Messages:  []messagePart{{Role: "user", Content: prompt}},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("anthropic error %d: failed to read response body: %w", resp.StatusCode, readErr)
		}
		return nil, nil, nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(b))
	}
	var cg codeGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&cg); err != nil {
		return nil, nil, nil, err
	}
	if len(cg.Content) == 0 {
		return nil, nil, nil, fmt.Errorf("empty anthropic response")
	}
	if cg.StopReason == "max_tokens" {
		return nil, nil, nil, fmt.Errorf(
			"response truncated at max_tokens (%d output tokens) - plan too large, reduce scope or split the ticket",
			cg.Usage.OutputTokens)
	}
	raw := agent.SanitizeResponse(cg.Content[0].Text)
	logger.Debug("AI response (sanitized)", "length", len(raw), "preview", raw[:util.Min(500, len(raw))])

	// Build usage metrics from API response
	metrics := c.buildUsageMetrics(&cg.Usage, len(repoContext))

	// The model may ask for full content of files currently shown
	// signatures-only instead of producing changes - see BuildPlanChangesPrompt.
	if needFiles := agent.ParseNeedFiles(raw); len(needFiles) > 0 {
		logger.Info("Model requested full content for additional files", "files", needFiles)
		return nil, needFiles, metrics, nil
	}

	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		repaired := false
		if cg.StopReason == "end_turn" {
			// Last resort: the model reported a normal stop but emitted
			// cosmetically malformed JSON (e.g. an extra brace). Genuine
			// truncation is already handled above via stop_reason ==
			// "max_tokens", so reaching here with a parse error means
			// something is off with the model's output - log loudly.
			fixed := attemptJSONFix(raw)
			if fixErr := json.Unmarshal([]byte(fixed), &changes); fixErr == nil {
				logger.Error("JSON required cosmetic repair despite stop_reason=end_turn - investigate model output",
					"original_length", len(raw),
					"fixed_length", len(fixed),
					"original_preview", raw[:util.Min(500, len(raw))])
				repaired = true
			}
		}
		if !repaired {
			logger.Error("Failed to parse AI response",
				"error", err,
				"response_length", len(raw),
				"response_preview", raw[:util.Min(1000, len(raw))],
				"stop_reason", cg.StopReason)
			return nil, nil, nil, fmt.Errorf("invalid JSON from model: %w", err)
		}
	}

	return changes, nil, metrics, nil
}

// FixErrors generates fixes for errors in previously generated code.
// This is used by the self-healing system to iteratively improve code that fails quality gates.
func (c *Client) FixErrors(ctx context.Context, ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []agent.CodeChange, fileContents map[string]string) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	prompt := agent.BuildFixErrorsPrompt(ticketKey, ticketSummary, errorType, errorOutput, previousChanges, fileContents, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("fix errors prompt in anthropic", "prompt", prompt)

	reqBody := codeGenRequest{
		Model:     c.Model,
		MaxTokens: 8000, // Reduced for fix generation to get more focused, simpler fixes
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
	if cg.StopReason == "max_tokens" {
		return nil, nil, fmt.Errorf(
			"response truncated at max_tokens (%d output tokens) - plan too large, reduce scope or split the ticket",
			cg.Usage.OutputTokens)
	}
	raw := agent.SanitizeResponse(cg.Content[0].Text)
	logger.Debug("AI fix response (sanitized)", "length", len(raw), "preview", raw[:util.Min(500, len(raw))])

	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		repaired := false
		if cg.StopReason == "end_turn" {
			// Last resort: the model reported a normal stop but emitted
			// cosmetically malformed JSON (e.g. an extra brace). Genuine
			// truncation is already handled above via stop_reason ==
			// "max_tokens", so reaching here with a parse error means
			// something is off with the model's output - log loudly.
			fixed := attemptJSONFix(raw)
			if fixErr := json.Unmarshal([]byte(fixed), &changes); fixErr == nil {
				logger.Error("JSON required cosmetic repair despite stop_reason=end_turn - investigate model output",
					"original_length", len(raw),
					"fixed_length", len(fixed),
					"original_preview", raw[:util.Min(500, len(raw))])
				repaired = true
			}
		}
		if !repaired {
			logger.Error("Failed to parse AI fix response",
				"error", err,
				"response_length", len(raw),
				"response_preview", raw[:util.Min(1000, len(raw))],
				"stop_reason", cg.StopReason)
			return nil, nil, fmt.Errorf("invalid JSON from model: %w", err)
		}
	}

	// Build usage metrics - using error output length as context size
	metrics := c.buildUsageMetrics(&cg.Usage, len(errorOutput))

	return changes, metrics, nil
}

// attemptJSONFix tries to fix common JSON truncation and malformation issues.
// Returns the fixed JSON string, or the original if unable to fix.
func attemptJSONFix(raw string) string {
	// Common pattern: JSON array truncated mid-object
	// Example: [{"path":"foo","content":"bar...  (missing closing "}]

	// Handle empty or too-short strings
	if len(raw) < 2 {
		return raw
	}

	// Trim any trailing whitespace first
	trimmed := strings.TrimSpace(raw)

	// Fix common AI generation errors first
	trimmed = fixCommonMalformations(trimmed)

	// Strategy 1: Try to complete the truncated JSON by counting braces
	result := attemptSimpleFix(trimmed)

	// Validate the result is at least syntactically balanced
	if isBalanced(result) {
		return result
	}

	// Strategy 2: More aggressive - find last complete object and truncate there
	return attemptAggressiveFix(trimmed)
}

// fixCommonMalformations fixes common AI-generated JSON errors
func fixCommonMalformations(s string) string {
	// Pattern 1: Extra closing brace before array close: "}}] instead of "}]
	// This happens when AI generates [{...}}}] instead of [{...}]
	if strings.HasSuffix(s, "}}]") {
		s = s[:len(s)-3] + "}]"
	}

	// Pattern 2: Extra closing brace after array close: "}]] instead of "}]
	if strings.HasSuffix(s, "}]]") {
		s = s[:len(s)-3] + "}]"
	}

	return s
}

// attemptSimpleFix tries to complete truncated JSON by adding missing closing characters
func attemptSimpleFix(trimmed string) string {
	openBraces := 0
	closeBraces := 0
	openBrackets := 0
	closeBrackets := 0
	inString := false
	escaped := false

	for _, char := range trimmed {
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
		if !inString {
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
	}

	result := trimmed

	// Close any open string
	if inString {
		result += "\""
	}

	// Close objects before arrays
	for i := closeBraces; i < openBraces; i++ {
		result += "}"
	}

	// Close arrays
	for i := closeBrackets; i < openBrackets; i++ {
		result += "]"
	}

	return result
}

// attemptAggressiveFix finds the last complete object and truncates everything after
func attemptAggressiveFix(trimmed string) string {
	// Find the last '}' that completes an object
	lastCompleteIdx := -1
	depth := 0
	inString := false
	escaped := false

	for i, char := range trimmed {
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
		if !inString {
			if char == '{' {
				depth++
			} else if char == '}' {
				depth--
				if depth == 1 { // We're at array level with one object closed
					lastCompleteIdx = i
				}
			}
		}
	}

	// If we found a complete object, truncate there and close the array
	if lastCompleteIdx > 0 {
		return trimmed[:lastCompleteIdx+1] + "]"
	}

	// Fallback: just return the simple fix
	return attemptSimpleFix(trimmed)
}

// isBalanced checks if the JSON has balanced braces and brackets
func isBalanced(s string) bool {
	braces := 0
	brackets := 0
	inString := false
	escaped := false

	for _, char := range s {
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
		if !inString {
			switch char {
			case '{':
				braces++
			case '}':
				braces--
			case '[':
				brackets++
			case ']':
				brackets--
			}
		}
	}

	return braces == 0 && brackets == 0 && !inString
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
