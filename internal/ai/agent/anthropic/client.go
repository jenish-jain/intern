package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"intern/internal/ai/agent"

	"github.com/jenish-jain/logger"
)

// Ensure Client implements agent.Agent
var _ agent.Agent = (*Client)(nil)

type Client struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Model:  "claude-sonnet-4-20250514",
		HTTP:   &http.Client{Timeout: 60 * time.Second},
	}
}

// sanitizeJSON tries to strip code fences and extract the JSON array
func sanitizeJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	// Extract first JSON array if extra text present
	re := regexp.MustCompile(`(?s)\[.*\]`)
	if m := re.FindString(s); m != "" {
		return m
	}
	return s
}

// PlanChanges asks the model to emit a minimal JSON array of CodeChange items.
func (c *Client) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]agent.CodeChange, error) {
	prompt := agent.BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext, agent.PlanPromptOptions{AllowBase64: true})
	logger.Debug("prompt in anthropic", "prompt", prompt)

	reqBody := codeGenRequest{
		Model:     c.Model,
		MaxTokens: 4000,
		Messages:  []messagePart{{Role: "user", Content: prompt}},
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(b))
	}
	var cg codeGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&cg); err != nil {
		return nil, err
	}
	if len(cg.Content) == 0 {
		return nil, fmt.Errorf("empty anthropic response")
	}
	raw := sanitizeJSON(cg.Content[0].Text)
	var changes []agent.CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		return nil, fmt.Errorf("invalid JSON from model: %w", err)
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
	return changes, nil
}
