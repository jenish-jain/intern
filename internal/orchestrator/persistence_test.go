package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveMetrics(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	snapshot := MetricsSnapshot{
		TicketsProcessed:  3,
		PRsCreated:        2,
		TicketsFailed:     1,
		TotalInputTokens:  15000,
		TotalOutputTokens: 7000,
		TotalCost:         0.135,
		SmartContextUsed:  2,
		SimpleContextUsed: 1,
		AvgCostPerTicket:  0.045,
		TotalRuntime:      10 * time.Minute,
		AvgExecutionTime:  200 * time.Second,
		TotalFilesChanged: 9,
	}

	tickets := []TicketMetrics{
		{
			TicketKey:    "PROJ-123",
			Status:       "success",
			InputTokens:  5000,
			OutputTokens: 2000,
			Cost:         0.045,
			Timestamp:    time.Now(),
		},
		{
			TicketKey:    "PROJ-456",
			Status:       "success",
			InputTokens:  10000,
			OutputTokens: 5000,
			Cost:         0.090,
			Timestamp:    time.Now(),
		},
	}

	// Save metrics
	filepath, err := SaveMetrics(snapshot, tickets, tempDir)
	require.NoError(t, err)

	// Verify file was created
	assert.FileExists(t, filepath)

	// Verify file is in .ai-intern directory
	assert.Contains(t, filepath, ".ai-intern")
	assert.Contains(t, filepath, "metrics_")
	assert.Contains(t, filepath, ".json")

	// Read and verify JSON content
	data, err := os.ReadFile(filepath)
	require.NoError(t, err)

	var output MetricsOutput
	err = json.Unmarshal(data, &output)
	require.NoError(t, err)

	// Verify run metadata
	assert.NotEmpty(t, output.RunMetadata.Timestamp)
	assert.InDelta(t, 600.0, output.RunMetadata.DurationSeconds, 1.0) // 10 minutes

	// Verify summary
	assert.Equal(t, 3, output.Summary.TicketsProcessed)
	assert.Equal(t, 2, output.Summary.PRsCreated)
	assert.Equal(t, 1, output.Summary.TicketsFailed)
	assert.Equal(t, 0.135, output.Summary.TotalCost)
	assert.Equal(t, int64(15000), output.Summary.TotalInputTokens)
	assert.Equal(t, int64(7000), output.Summary.TotalOutputTokens)
	assert.Equal(t, int64(2), output.Summary.SmartContextUsed)
	assert.Equal(t, int64(1), output.Summary.SimpleContextUsed)

	// Verify tickets
	assert.Len(t, output.Tickets, 2)
	assert.Equal(t, "PROJ-123", output.Tickets[0].TicketKey)
	assert.Equal(t, "success", output.Tickets[0].Status)
	assert.Equal(t, 5000, output.Tickets[0].InputTokens)
}

func TestSaveMetrics_CreatesDirectory(t *testing.T) {
	// Create temp directory without .ai-intern
	tempDir := t.TempDir()

	snapshot := MetricsSnapshot{
		TicketsProcessed: 1,
		TotalRuntime:     1 * time.Minute,
	}

	filepath, err := SaveMetrics(snapshot, []TicketMetrics{}, tempDir)
	require.NoError(t, err)

	// Verify .ai-intern directory was created
	metricsDir := filepath[:len(filepath)-len("/metrics_")-len("20060102_150405.json")]
	assert.DirExists(t, metricsDir)
}

func TestLoadMetrics(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Save some metrics first
	snapshot := MetricsSnapshot{
		TicketsProcessed: 2,
		PRsCreated:       2,
		TotalCost:        0.090,
		TotalRuntime:     5 * time.Minute,
	}

	tickets := []TicketMetrics{
		{
			TicketKey:    "PROJ-789",
			Status:       "success",
			InputTokens:  3000,
			OutputTokens: 1500,
			Cost:         0.045,
			Timestamp:    time.Now(),
		},
	}

	filepath, err := SaveMetrics(snapshot, tickets, tempDir)
	require.NoError(t, err)

	// Load metrics
	loaded, err := LoadMetrics(filepath)
	require.NoError(t, err)

	// Verify loaded data
	assert.Equal(t, 2, loaded.Summary.TicketsProcessed)
	assert.Equal(t, 2, loaded.Summary.PRsCreated)
	assert.Equal(t, 0.090, loaded.Summary.TotalCost)
	assert.Len(t, loaded.Tickets, 1)
	assert.Equal(t, "PROJ-789", loaded.Tickets[0].TicketKey)
	assert.Equal(t, 3000, loaded.Tickets[0].InputTokens)
}

func TestLoadMetrics_FileNotFound(t *testing.T) {
	_, err := LoadMetrics("/nonexistent/path/metrics.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read metrics file")
}

func TestLoadMetrics_InvalidJSON(t *testing.T) {
	// Create temp file with invalid JSON
	tempDir := t.TempDir()
	invalidFile := filepath.Join(tempDir, "invalid.json")
	err := os.WriteFile(invalidFile, []byte("not valid json"), 0644)
	require.NoError(t, err)

	_, err = LoadMetrics(invalidFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal metrics")
}

func TestMetricsOutput_JSONSerialization(t *testing.T) {
	now := time.Now()

	output := MetricsOutput{
		RunMetadata: RunMetadata{
			Timestamp:       now.Format(time.RFC3339),
			DurationSeconds: 600.0,
			AgentVersion:    "1.0.0",
		},
		Summary: SummaryMetrics{
			TicketsProcessed: 3,
			PRsCreated:       3,
			TotalCost:        0.135,
		},
		Tickets: []TicketResult{
			{
				TicketKey:    "PROJ-111",
				Status:       "success",
				InputTokens:  5000,
				OutputTokens: 2000,
				Cost:         0.045,
				Timestamp:    now.Format(time.RFC3339),
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(output)
	require.NoError(t, err)

	// Unmarshal back
	var loaded MetricsOutput
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)

	// Verify round-trip
	assert.Equal(t, output.RunMetadata.AgentVersion, loaded.RunMetadata.AgentVersion)
	assert.Equal(t, output.Summary.TicketsProcessed, loaded.Summary.TicketsProcessed)
	assert.Equal(t, output.Tickets[0].TicketKey, loaded.Tickets[0].TicketKey)
}

func TestSaveMetrics_EmptyTickets(t *testing.T) {
	tempDir := t.TempDir()

	snapshot := MetricsSnapshot{
		TicketsProcessed: 0,
		TotalRuntime:     1 * time.Minute,
	}

	filepath, err := SaveMetrics(snapshot, []TicketMetrics{}, tempDir)
	require.NoError(t, err)

	// Load and verify
	loaded, err := LoadMetrics(filepath)
	require.NoError(t, err)

	assert.Equal(t, 0, loaded.Summary.TicketsProcessed)
	assert.Empty(t, loaded.Tickets)
}
