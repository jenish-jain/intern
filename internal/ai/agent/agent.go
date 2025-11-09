package agent

import "context"

// Agent is the interface that AI providers must implement.
// It abstracts the code generation capability across different providers (Anthropic, OpenAI, etc.)
type Agent interface {
	// PlanChanges generates code changes based on a ticket and repository context.
	// Returns the list of changes, usage metrics for cost tracking, and any error.
	PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]CodeChange, *UsageMetrics, error)
}

// UsageMetrics contains token usage and cost information for an AI operation.
// This is provider-agnostic and can be used with any AI service.
type UsageMetrics struct {
	InputTokens   int     // Number of tokens in the input prompt
	OutputTokens  int     // Number of tokens in the generated response
	TotalTokens   int     // InputTokens + OutputTokens
	EstimatedCost float64 // Estimated cost in USD based on provider pricing
	ContextStats  ContextStats
}

// ContextStats contains information about the context used for code generation.
// This helps track the effectiveness of smart context selection.
type ContextStats struct {
	Strategy      string // "smart" or "simple" - which context building strategy was used
	FilesIncluded int    // Number of files included in the context
	ContextBytes  int    // Total size of context in bytes
	Keywords      int    // Number of keywords extracted (only for smart context)
}
