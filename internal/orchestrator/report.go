package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"intern/internal/ai"
)

// GenerateReport creates a formatted summary report of the agent's run.
// Returns a pretty ASCII table with all key metrics.
func GenerateReport(snapshot MetricsSnapshot) string {
	var sb strings.Builder

	// Box drawing characters
	const (
		topLeft     = "╔"
		topRight    = "╗"
		bottomLeft  = "╚"
		bottomRight = "╝"
		horizontal  = "═"
		vertical    = "║"
		leftT       = "╠"
		rightT      = "╣"
	)

	width := 62
	line := strings.Repeat(horizontal, width-2)

	// Header
	sb.WriteString(topLeft + line + topRight + "\n")
	sb.WriteString(vertical + centerText("AI Intern Agent - Run Summary", width-2) + vertical + "\n")
	sb.WriteString(leftT + line + rightT + "\n")

	// Execution Summary
	sb.WriteString(vertical + " Execution Summary" + strings.Repeat(" ", width-19) + vertical + "\n")
	sb.WriteString(vertical + formatMetricLine("  Tickets Processed:", fmt.Sprintf("%d", snapshot.TicketsProcessed), width-2) + vertical + "\n")
	sb.WriteString(vertical + formatMetricLine("  PRs Created:", fmt.Sprintf("%d", snapshot.PRsCreated), width-2) + vertical + "\n")

	if snapshot.TicketsFailed > 0 {
		sb.WriteString(vertical + formatMetricLine("  Failed:", fmt.Sprintf("%d", snapshot.TicketsFailed), width-2) + vertical + "\n")
	}

	sb.WriteString(vertical + formatMetricLine("  Total Runtime:", formatDuration(snapshot.TotalRuntime), width-2) + vertical + "\n")

	// Cost Analysis
	sb.WriteString(leftT + line + rightT + "\n")
	sb.WriteString(vertical + " Cost Analysis" + strings.Repeat(" ", width-16) + vertical + "\n")
	sb.WriteString(vertical + formatMetricLine("  Input Tokens:", ai.FormatTokens(int(snapshot.TotalInputTokens)), width-2) + vertical + "\n")
	sb.WriteString(vertical + formatMetricLine("  Output Tokens:", ai.FormatTokens(int(snapshot.TotalOutputTokens)), width-2) + vertical + "\n")
	sb.WriteString(vertical + formatMetricLine("  Total Cost:", ai.FormatCost(snapshot.TotalCost), width-2) + vertical + "\n")

	if snapshot.TicketsProcessed > 0 {
		sb.WriteString(vertical + formatMetricLine("  Avg Cost per Ticket:", ai.FormatCost(snapshot.AvgCostPerTicket), width-2) + vertical + "\n")
	}

	// Context Strategy
	if snapshot.SmartContextUsed > 0 || snapshot.SimpleContextUsed > 0 {
		sb.WriteString(leftT + line + rightT + "\n")
		sb.WriteString(vertical + " Context Strategy" + strings.Repeat(" ", width-18) + vertical + "\n")

		totalContext := snapshot.SmartContextUsed + snapshot.SimpleContextUsed
		if snapshot.SmartContextUsed > 0 {
			smartPct := float64(snapshot.SmartContextUsed) / float64(totalContext) * 100
			sb.WriteString(vertical + formatMetricLine("  Smart Context Used:",
				fmt.Sprintf("%d tickets (%.0f%%)", snapshot.SmartContextUsed, smartPct), width-2) + vertical + "\n")
		}
		if snapshot.SimpleContextUsed > 0 {
			simplePct := float64(snapshot.SimpleContextUsed) / float64(totalContext) * 100
			sb.WriteString(vertical + formatMetricLine("  Simple Fallback:",
				fmt.Sprintf("%d tickets (%.0f%%)", snapshot.SimpleContextUsed, simplePct), width-2) + vertical + "\n")
		}
	}

	// Performance
	if snapshot.TotalFilesChanged > 0 {
		sb.WriteString(leftT + line + rightT + "\n")
		sb.WriteString(vertical + " Performance" + strings.Repeat(" ", width-13) + vertical + "\n")

		if snapshot.TicketsProcessed > 0 {
			sb.WriteString(vertical + formatMetricLine("  Avg Time per Ticket:",
				formatDuration(snapshot.AvgExecutionTime), width-2) + vertical + "\n")
		}

		sb.WriteString(vertical + formatMetricLine("  Total Files Changed:",
			fmt.Sprintf("%d", snapshot.TotalFilesChanged), width-2) + vertical + "\n")

		if snapshot.TicketsProcessed > 0 {
			sb.WriteString(vertical + formatMetricLine("  Avg Files per Ticket:",
				fmt.Sprintf("%.1f", snapshot.AvgFilesPerTicket), width-2) + vertical + "\n")
		}
	}

	// Retries and Failures
	if snapshot.Retries > 0 || snapshot.AIPlanFailures > 0 {
		sb.WriteString(leftT + line + rightT + "\n")
		sb.WriteString(vertical + " Reliability" + strings.Repeat(" ", width-13) + vertical + "\n")

		if snapshot.Retries > 0 {
			sb.WriteString(vertical + formatMetricLine("  Total Retries:",
				fmt.Sprintf("%d", snapshot.Retries), width-2) + vertical + "\n")
		}
		if snapshot.AIPlanFailures > 0 {
			sb.WriteString(vertical + formatMetricLine("  AI Plan Failures:",
				fmt.Sprintf("%d", snapshot.AIPlanFailures), width-2) + vertical + "\n")
		}
	}

	// Footer
	sb.WriteString(bottomLeft + line + bottomRight + "\n")

	return sb.String()
}

// centerText centers text within a given width.
func centerText(text string, width int) string {
	if len(text) >= width {
		return text[:width]
	}
	leftPad := (width - len(text)) / 2
	rightPad := width - len(text) - leftPad
	return strings.Repeat(" ", leftPad) + text + strings.Repeat(" ", rightPad)
}

// formatMetricLine formats a metric name and value with proper spacing.
func formatMetricLine(name, value string, width int) string {
	// Calculate spaces needed between name and value
	nameLen := len(name)
	valueLen := len(value)
	spaces := width - nameLen - valueLen

	if spaces < 1 {
		spaces = 1
	}

	return name + strings.Repeat(" ", spaces) + value
}

// formatDuration formats a duration in human-readable format.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}

	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60

	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}

	hours := minutes / 60
	minutes = minutes % 60

	return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
}
