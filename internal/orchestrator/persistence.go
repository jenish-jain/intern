package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MetricsOutput represents the complete metrics output for JSON export.
type MetricsOutput struct {
	RunMetadata RunMetadata    `json:"run_metadata"`
	Summary     SummaryMetrics `json:"summary"`
	Tickets     []TicketResult `json:"tickets"`
}

// RunMetadata contains metadata about the agent run.
type RunMetadata struct {
	Timestamp       string  `json:"timestamp"`        // ISO 8601 format
	DurationSeconds float64 `json:"duration_seconds"` // Total runtime in seconds
	AgentVersion    string  `json:"agent_version"`    // Version identifier
}

// SummaryMetrics contains the aggregated metrics for the entire run.
type SummaryMetrics struct {
	TicketsProcessed int     `json:"tickets_processed"`
	PRsCreated       int     `json:"prs_created"`
	TicketsFailed    int     `json:"tickets_failed"`
	TotalCost        float64 `json:"total_cost"`
	TotalInputTokens int64   `json:"total_input_tokens"`
	TotalOutputTokens int64  `json:"total_output_tokens"`
	SmartContextUsed  int64  `json:"smart_context_used"`
	SimpleContextUsed int64  `json:"simple_context_used"`
	AvgCostPerTicket  float64 `json:"avg_cost_per_ticket"`
	AvgTimePerTicket  float64 `json:"avg_time_per_ticket_seconds"`
	TotalFilesChanged int64   `json:"total_files_changed"`
}

// SaveMetrics saves metrics to a JSON file in the .ai-intern directory.
// Creates the directory if it doesn't exist.
// Filename format: metrics_YYYYMMDD_HHMMSS.json
func SaveMetrics(snapshot MetricsSnapshot, tickets []TicketMetrics, repoRoot string) (string, error) {
	// Create .ai-intern directory if it doesn't exist
	metricsDir := filepath.Join(repoRoot, ".ai-intern")
	if err := os.MkdirAll(metricsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create metrics directory: %w", err)
	}

	// Generate filename with timestamp
	timestamp := time.Now()
	filename := fmt.Sprintf("metrics_%s.json", timestamp.Format("20060102_150405"))
	filepath := filepath.Join(metricsDir, filename)

	// Convert ticket metrics to results
	ticketResults := make([]TicketResult, len(tickets))
	for i, tm := range tickets {
		ticketResults[i] = tm.ToResult()
	}

	// Build output structure
	output := MetricsOutput{
		RunMetadata: RunMetadata{
			Timestamp:       timestamp.Format(time.RFC3339),
			DurationSeconds: snapshot.TotalRuntime.Seconds(),
			AgentVersion:    "1.0.0", // TODO: Make this configurable
		},
		Summary: SummaryMetrics{
			TicketsProcessed:  int(snapshot.TicketsProcessed),
			PRsCreated:        int(snapshot.PRsCreated),
			TicketsFailed:     int(snapshot.TicketsFailed),
			TotalCost:         snapshot.TotalCost,
			TotalInputTokens:  snapshot.TotalInputTokens,
			TotalOutputTokens: snapshot.TotalOutputTokens,
			SmartContextUsed:  snapshot.SmartContextUsed,
			SimpleContextUsed: snapshot.SimpleContextUsed,
			AvgCostPerTicket:  snapshot.AvgCostPerTicket,
			AvgTimePerTicket:  snapshot.AvgExecutionTime.Seconds(),
			TotalFilesChanged: snapshot.TotalFilesChanged,
		},
		Tickets: ticketResults,
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write metrics file: %w", err)
	}

	return filepath, nil
}

// LoadMetrics loads metrics from a JSON file.
// Useful for analysis or historical comparison.
func LoadMetrics(filepath string) (*MetricsOutput, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics file: %w", err)
	}

	var output MetricsOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	return &output, nil
}
