package agent

import "context"

// Agent is the interface that AI providers must implement.
// It abstracts the code generation capability across different providers (Anthropic, OpenAI, etc.)
type Agent interface {
	// PlanChanges generates code changes based on a ticket and repository context.
	// If the model needs the full content of a file that was shown
	// signatures-only, it instead returns needFiles (a non-empty list of
	// repo-relative paths) with an empty changes slice; the caller should
	// rebuild the context with those files promoted to full content and call
	// PlanChanges again.
	// Returns the list of changes (or needFiles), usage metrics for cost tracking, and any error.
	PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) (changes []CodeChange, needFiles []string, metrics *UsageMetrics, err error)

	// FixErrors generates fixes for errors in previously generated code.
	// Used by the self-healing system to iteratively improve code that fails quality gates.
	// fileContents maps repo-relative paths to their current on-disk content, giving the
	// model the ground truth it needs to emit accurate edit hunks.
	// Returns the fixed changes, usage metrics, and any error.
	FixErrors(ctx context.Context, ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []CodeChange, fileContents map[string]string) ([]CodeChange, *UsageMetrics, error)
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
