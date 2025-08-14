package ai

import (
	"context"
	"fmt"
	"os"
)

type Generator struct {
	AIClient Client
}

func (g *Generator) GenerateFile(ctx context.Context, prompt, filePath string) error {
	code, err := g.AIClient.GenerateCode(ctx, prompt)
	if err != nil {
		return fmt.Errorf("AI code generation failed: %w", err)
	}
	return os.WriteFile(filePath, []byte(code), 0644)
}

func (g *Generator) GenerateFunction(ctx context.Context, prompt string) (string, error) {
	return g.AIClient.GenerateCode(ctx, prompt)
}
