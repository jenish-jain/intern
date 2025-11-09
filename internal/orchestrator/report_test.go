package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateReport_BasicReport(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed:  5,
		PRsCreated:        4,
		TicketsFailed:     1,
		TotalInputTokens:  25000,
		TotalOutputTokens: 10000,
		TotalCost:         0.225,
		AvgCostPerTicket:  0.045,
		SmartContextUsed:  4,
		SimpleContextUsed: 1,
		TotalFilesChanged: 15,
		AvgFilesPerTicket: 3.0,
		TotalRuntime:      15 * time.Minute,
	}

	report := GenerateReport(snapshot)

	// Verify report contains key sections
	assert.Contains(t, report, "AI Intern Agent - Run Summary")
	assert.Contains(t, report, "Execution Summary")
	assert.Contains(t, report, "Cost Analysis")
	assert.Contains(t, report, "Context Strategy")
	assert.Contains(t, report, "Performance")

	// Verify specific values
	assert.Contains(t, report, "Tickets Processed:       5")
	assert.Contains(t, report, "PRs Created:            4")
	assert.Contains(t, report, "Failed:                 1")
	assert.Contains(t, report, "25,000") // Input tokens
	assert.Contains(t, report, "10,000") // Output tokens
	assert.Contains(t, report, "$0.225") // Total cost
	assert.Contains(t, report, "$0.045") // Avg cost
	assert.Contains(t, report, "Smart Context Used:     4 tickets (80%)")
	assert.Contains(t, report, "Simple Fallback:        1 tickets (20%)")
	assert.Contains(t, report, "Total Files Changed:    15")
}

func TestGenerateReport_ZeroTickets(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed: 0,
		TotalRuntime:     1 * time.Minute,
	}

	report := GenerateReport(snapshot)

	// Should still generate valid report
	assert.Contains(t, report, "AI Intern Agent - Run Summary")
	assert.Contains(t, report, "Tickets Processed:       0")
	assert.NotContains(t, report, "Avg Cost per Ticket") // No average when zero tickets
}

func TestGenerateReport_NoFailures(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed: 3,
		PRsCreated:       3,
		TicketsFailed:    0,
		TotalCost:        0.135,
		TotalRuntime:     10 * time.Minute,
	}

	report := GenerateReport(snapshot)

	assert.Contains(t, report, "Tickets Processed:       3")
	assert.NotContains(t, report, "Failed:") // Don't show failed if zero
}

func TestGenerateReport_BoxDrawing(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed: 1,
		TotalRuntime:     1 * time.Minute,
	}

	report := GenerateReport(snapshot)

	// Verify box drawing characters are used
	assert.Contains(t, report, "╔")
	assert.Contains(t, report, "╗")
	assert.Contains(t, report, "╚")
	assert.Contains(t, report, "╝")
	assert.Contains(t, report, "║")
	assert.Contains(t, report, "╠")
	assert.Contains(t, report, "╣")
	assert.Contains(t, report, "═")
}

func TestCenterText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		contains string
	}{
		{
			name:     "centered text",
			text:     "Hello",
			width:    11,
			contains: "   Hello   ",
		},
		{
			name:     "text longer than width",
			text:     "Very Long Text",
			width:    5,
			contains: "Very ",
		},
		{
			name:     "exact width",
			text:     "Exact",
			width:    5,
			contains: "Exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := centerText(tt.text, tt.width)
			assert.Equal(t, tt.width, len(result))
			assert.Contains(t, result, tt.contains[:len(result)])
		})
	}
}

func TestFormatMetricLine(t *testing.T) {
	tests := []struct {
		name     string
		metricName string
		value    string
		width    int
		expected int // Expected total length
	}{
		{
			name:       "normal metric",
			metricName: "Tickets:",
			value:      "5",
			width:      30,
			expected:   30,
		},
		{
			name:       "long metric name",
			metricName: "Very Long Metric Name:",
			value:      "Value",
			width:      40,
			expected:   40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMetricLine(tt.metricName, tt.value, tt.width)
			assert.Equal(t, tt.expected, len(result))
			assert.True(t, strings.HasPrefix(result, tt.metricName))
			assert.True(t, strings.HasSuffix(result, tt.value))
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "2m 30s",
		},
		{
			name:     "hours minutes seconds",
			duration: 1*time.Hour + 23*time.Minute + 45*time.Second,
			expected: "1h 23m 45s",
		},
		{
			name:     "exact minutes",
			duration: 5 * time.Minute,
			expected: "5m 0s",
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateReport_WithRetries(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed: 3,
		Retries:          5,
		AIPlanFailures:   2,
		TotalRuntime:     10 * time.Minute,
	}

	report := GenerateReport(snapshot)

	assert.Contains(t, report, "Reliability")
	assert.Contains(t, report, "Total Retries:          5")
	assert.Contains(t, report, "AI Plan Failures:       2")
}

func TestGenerateReport_NoRetries(t *testing.T) {
	snapshot := MetricsSnapshot{
		TicketsProcessed: 3,
		Retries:          0,
		AIPlanFailures:   0,
		TotalRuntime:     10 * time.Minute,
	}

	report := GenerateReport(snapshot)

	// Should not show Reliability section if no retries/failures
	assert.NotContains(t, report, "Reliability")
}
