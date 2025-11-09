package orchestrator

import (
	"time"

	"intern/internal/ai/agent"
)

// TicketMetrics contains detailed metrics for a single ticket.
// Used for per-ticket logging and JSON export.
type TicketMetrics struct {
	TicketKey     string        `json:"ticket_key"`
	Status        string        `json:"status"`          // "success" or "failed"
	InputTokens   int           `json:"input_tokens"`
	OutputTokens  int           `json:"output_tokens"`
	Cost          float64       `json:"cost"`            // In dollars
	ExecutionTime time.Duration `json:"execution_time_seconds"` // Will be converted to seconds in JSON
	FilesChanged  int           `json:"files_changed"`

	// Context information
	ContextStrategy   string `json:"context_strategy"`    // "smart" or "simple"
	FilesInContext    int    `json:"files_in_context"`
	ContextSizeBytes  int    `json:"context_size_bytes"`
	KeywordsExtracted int    `json:"keywords_extracted"`   // Only for smart context

	// Error information (if failed)
	ErrorMessage string `json:"error_message,omitempty"`
	RetryCount   int    `json:"retry_count"`

	// Savings estimation (smart context vs full context)
	EstimatedFullContextCost float64 `json:"estimated_full_context_cost,omitempty"`
	CostSavings             float64 `json:"cost_savings,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// TicketResult is the JSON-friendly version of TicketMetrics.
// Converts durations to seconds for easier JSON handling.
type TicketResult struct {
	TicketKey     string  `json:"ticket_key"`
	Status        string  `json:"status"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	Cost          float64 `json:"cost"`
	ExecutionTimeSeconds float64 `json:"execution_time_seconds"`
	FilesChanged  int     `json:"files_changed"`

	ContextStrategy   string `json:"context_strategy"`
	FilesInContext    int    `json:"files_in_context"`
	ContextSizeBytes  int    `json:"context_size_bytes"`
	KeywordsExtracted int    `json:"keywords_extracted,omitempty"`

	ErrorMessage string `json:"error_message,omitempty"`
	RetryCount   int    `json:"retry_count"`

	EstimatedFullContextCost float64 `json:"estimated_full_context_cost,omitempty"`
	CostSavings             float64 `json:"cost_savings,omitempty"`

	Timestamp string `json:"timestamp"` // RFC3339 format
}

// ToResult converts TicketMetrics to JSON-friendly TicketResult.
func (tm *TicketMetrics) ToResult() TicketResult {
	return TicketResult{
		TicketKey:                tm.TicketKey,
		Status:                   tm.Status,
		InputTokens:              tm.InputTokens,
		OutputTokens:             tm.OutputTokens,
		Cost:                     tm.Cost,
		ExecutionTimeSeconds:     tm.ExecutionTime.Seconds(),
		FilesChanged:             tm.FilesChanged,
		ContextStrategy:          tm.ContextStrategy,
		FilesInContext:           tm.FilesInContext,
		ContextSizeBytes:         tm.ContextSizeBytes,
		KeywordsExtracted:        tm.KeywordsExtracted,
		ErrorMessage:             tm.ErrorMessage,
		RetryCount:               tm.RetryCount,
		EstimatedFullContextCost: tm.EstimatedFullContextCost,
		CostSavings:              tm.CostSavings,
		Timestamp:                tm.Timestamp.Format(time.RFC3339),
	}
}

// NewTicketMetricsFromUsage creates a TicketMetrics from agent.UsageMetrics.
func NewTicketMetricsFromUsage(ticketKey string, usage *agent.UsageMetrics) *TicketMetrics {
	if usage == nil {
		return &TicketMetrics{
			TicketKey: ticketKey,
			Status:    "failed",
			Timestamp: time.Now(),
		}
	}

	return &TicketMetrics{
		TicketKey:         ticketKey,
		Status:            "success",
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		Cost:              usage.EstimatedCost,
		ContextStrategy:   usage.ContextStats.Strategy,
		FilesInContext:    usage.ContextStats.FilesIncluded,
		ContextSizeBytes:  usage.ContextStats.ContextBytes,
		KeywordsExtracted: usage.ContextStats.Keywords,
		Timestamp:         time.Now(),
	}
}

// MarkFailed marks the ticket as failed with an error message.
func (tm *TicketMetrics) MarkFailed(err error) {
	tm.Status = "failed"
	if err != nil {
		tm.ErrorMessage = err.Error()
	}
}

// SetExecutionTime sets the execution time for the ticket.
func (tm *TicketMetrics) SetExecutionTime(d time.Duration) {
	tm.ExecutionTime = d
}

// SetFilesChanged sets the number of files changed.
func (tm *TicketMetrics) SetFilesChanged(count int) {
	tm.FilesChanged = count
}

// SetRetryCount sets the retry count.
func (tm *TicketMetrics) SetRetryCount(count int) {
	tm.RetryCount = count
}

// SetSavingsEstimate sets the estimated savings from using smart context.
func (tm *TicketMetrics) SetSavingsEstimate(fullContextCost float64) {
	tm.EstimatedFullContextCost = fullContextCost
	if fullContextCost > tm.Cost {
		tm.CostSavings = fullContextCost - tm.Cost
	}
}
