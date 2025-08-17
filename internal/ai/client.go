package ai

import (
	"context"
)

type Client interface {
	GenerateCode(ctx context.Context, prompt string) (string, error)
}

type anthropicClient struct {
	apiKey string
}

func NewClient(apiKey string) Client {
	return &anthropicClient{apiKey: apiKey}
}

func (c *anthropicClient) GenerateCode(ctx context.Context, prompt string) (string, error) {
	// Placeholder: actual HTTP call to Anthropic Claude API
	return "// generated code placeholder", nil
}
