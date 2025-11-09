package orchestrator

import (
	"errors"
	"testing"
	"time"

	"intern/internal/ai/agent"

	"github.com/stretchr/testify/assert"
)

func TestNewTicketMetricsFromUsage(t *testing.T) {
	t.Run("with valid usage metrics", func(t *testing.T) {
		usage := &agent.UsageMetrics{
			InputTokens:   5000,
			OutputTokens:  2000,
			TotalTokens:   7000,
			EstimatedCost: 0.045,
			ContextStats: agent.ContextStats{
				Strategy:      "smart",
				FilesIncluded: 12,
				ContextBytes:  24576,
				Keywords:      5,
			},
		}

		tm := NewTicketMetricsFromUsage("PROJ-123", usage)

		assert.Equal(t, "PROJ-123", tm.TicketKey)
		assert.Equal(t, "success", tm.Status)
		assert.Equal(t, 5000, tm.InputTokens)
		assert.Equal(t, 2000, tm.OutputTokens)
		assert.Equal(t, 0.045, tm.Cost)
		assert.Equal(t, "smart", tm.ContextStrategy)
		assert.Equal(t, 12, tm.FilesInContext)
		assert.Equal(t, 24576, tm.ContextSizeBytes)
		assert.Equal(t, 5, tm.KeywordsExtracted)
	})

	t.Run("with nil usage metrics", func(t *testing.T) {
		tm := NewTicketMetricsFromUsage("PROJ-456", nil)

		assert.Equal(t, "PROJ-456", tm.TicketKey)
		assert.Equal(t, "failed", tm.Status)
		assert.Equal(t, 0, tm.InputTokens)
		assert.Equal(t, 0, tm.OutputTokens)
	})
}

func TestTicketMetrics_MarkFailed(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey: "PROJ-789",
		Status:    "success",
	}

	err := errors.New("AI planning failed")
	tm.MarkFailed(err)

	assert.Equal(t, "failed", tm.Status)
	assert.Equal(t, "AI planning failed", tm.ErrorMessage)
}

func TestTicketMetrics_MarkFailedWithNilError(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey: "PROJ-999",
		Status:    "success",
	}

	tm.MarkFailed(nil)

	assert.Equal(t, "failed", tm.Status)
	assert.Empty(t, tm.ErrorMessage)
}

func TestTicketMetrics_SetExecutionTime(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey: "PROJ-111",
	}

	duration := 2*time.Minute + 30*time.Second
	tm.SetExecutionTime(duration)

	assert.Equal(t, duration, tm.ExecutionTime)
}

func TestTicketMetrics_SetFilesChanged(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey: "PROJ-222",
	}

	tm.SetFilesChanged(5)

	assert.Equal(t, 5, tm.FilesChanged)
}

func TestTicketMetrics_SetRetryCount(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey: "PROJ-333",
	}

	tm.SetRetryCount(3)

	assert.Equal(t, 3, tm.RetryCount)
}

func TestTicketMetrics_SetSavingsEstimate(t *testing.T) {
	t.Run("with savings", func(t *testing.T) {
		tm := &TicketMetrics{
			TicketKey: "PROJ-444",
			Cost:      0.045,
		}

		tm.SetSavingsEstimate(0.150)

		assert.Equal(t, 0.150, tm.EstimatedFullContextCost)
		assert.InDelta(t, 0.105, tm.CostSavings, 0.001)
	})

	t.Run("no savings when smart is more expensive", func(t *testing.T) {
		tm := &TicketMetrics{
			TicketKey: "PROJ-555",
			Cost:      0.150,
		}

		tm.SetSavingsEstimate(0.045) // Full context cheaper (shouldn't happen, but handle it)

		assert.Equal(t, 0.045, tm.EstimatedFullContextCost)
		assert.Equal(t, 0.0, tm.CostSavings) // No negative savings
	})
}

func TestTicketMetrics_ToResult(t *testing.T) {
	now := time.Now()
	tm := &TicketMetrics{
		TicketKey:         "PROJ-666",
		Status:            "success",
		InputTokens:       5000,
		OutputTokens:      2000,
		Cost:              0.045,
		ExecutionTime:     2*time.Minute + 30*time.Second,
		FilesChanged:      3,
		ContextStrategy:   "smart",
		FilesInContext:    12,
		ContextSizeBytes:  24576,
		KeywordsExtracted: 5,
		RetryCount:        1,
		EstimatedFullContextCost: 0.150,
		CostSavings:              0.105,
		Timestamp:                now,
	}

	result := tm.ToResult()

	assert.Equal(t, "PROJ-666", result.TicketKey)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 5000, result.InputTokens)
	assert.Equal(t, 2000, result.OutputTokens)
	assert.Equal(t, 0.045, result.Cost)
	assert.InDelta(t, 150.0, result.ExecutionTimeSeconds, 0.1) // 2m 30s = 150s
	assert.Equal(t, 3, result.FilesChanged)
	assert.Equal(t, "smart", result.ContextStrategy)
	assert.Equal(t, 12, result.FilesInContext)
	assert.Equal(t, 24576, result.ContextSizeBytes)
	assert.Equal(t, 5, result.KeywordsExtracted)
	assert.Equal(t, 1, result.RetryCount)
	assert.Equal(t, 0.150, result.EstimatedFullContextCost)
	assert.InDelta(t, 0.105, result.CostSavings, 0.001)
	assert.Equal(t, now.Format(time.RFC3339), result.Timestamp)
}

func TestTicketMetrics_ToResultWithError(t *testing.T) {
	tm := &TicketMetrics{
		TicketKey:    "PROJ-777",
		Status:       "failed",
		ErrorMessage: "Validation failed",
		Timestamp:    time.Now(),
	}

	result := tm.ToResult()

	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "Validation failed", result.ErrorMessage)
}
