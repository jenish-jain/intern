package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicClient struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

type codeGenRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []messagePart `json:"messages"`
}

type messagePart struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type codeGenResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func NewAnthropicClient(apiKey string) *AnthropicClient {
	return &AnthropicClient{
		APIKey: apiKey,
		Model:  "claude-3-5-sonnet-20240620",
		HTTP:   &http.Client{Timeout: 60 * time.Second},
	}
}

// PlanChanges asks the model to emit a minimal JSON array of CodeChange items.
func (c *AnthropicClient) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]CodeChange, error) {
	prompt := fmt.Sprintf(`You are a senior Go engineer. Follow instructions exactly.
Ticket: %s - %s
Description:
%s

Repository context (truncated):
%s

Strictly output a JSON array of code changes with shape:
[
  {"path":"relative/path.ext","operation":"create|update","content":"file content or full updated content"}
]
Do not include explanations, markdown, or comments. Only JSON. Only the minimal changes required by the description.`, ticketKey, ticketSummary, ticketDescription, repoContext)

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
	raw := cg.Content[0].Text
	var changes []CodeChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		return nil, fmt.Errorf("invalid JSON from model: %w", err)
	}
	return changes, nil
}
