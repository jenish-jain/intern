package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"intern/internal/orchestrator"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// MetricsCmd represents the metrics command
var MetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show detailed metrics summary",
	Long:  `Display comprehensive metrics about agent performance, costs, and processing statistics.`,
	RunE:  showMetrics,
}

func showMetrics(cmd *cobra.Command, args []string) error {
	logger.Info("Loading metrics...")

	// Load config to find metrics file
	_, repoPaths, err := InitPartialDependencies()
	if err != nil {
		logger.Error("Failed to initialize dependencies: %v", err)
		return err
	}

	metricsPath := filepath.Join(repoPaths.Root(), ".ai-intern", "metrics.json")

	// Check if metrics file exists
	if _, err := os.Stat(metricsPath); os.IsNotExist(err) {
		logger.Error("No metrics file found", "path", metricsPath)
		fmt.Println("\nNo metrics available yet. Run the agent to generate metrics.")
		return fmt.Errorf("metrics file not found")
	}

	// Load metrics
	output, err := orchestrator.LoadMetrics(metricsPath)
	if err != nil {
		logger.Error("Failed to load metrics", "error", err)
		return err
	}

	// Print metrics summary
	fmt.Println("\n=== AI Intern Agent Metrics ===")
	fmt.Printf("\nRun Metadata:\n")
	fmt.Printf("  Timestamp:       %s\n", output.RunMetadata.Timestamp)
	fmt.Printf("  Duration:        %.1f seconds\n", output.RunMetadata.DurationSeconds)
	fmt.Printf("  Version:         %s\n", output.RunMetadata.AgentVersion)

	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Tickets Processed: %d\n", output.Summary.TicketsProcessed)
	fmt.Printf("  PRs Created:       %d\n", output.Summary.PRsCreated)
	fmt.Printf("  Tickets Failed:    %d\n", output.Summary.TicketsFailed)

	fmt.Printf("\nCost Metrics:\n")
	fmt.Printf("  Total Cost:        $%.2f\n", output.Summary.TotalCost)
	fmt.Printf("  Avg Cost/Ticket:   $%.3f\n", output.Summary.AvgCostPerTicket)
	fmt.Printf("  Input Tokens:      %d\n", output.Summary.TotalInputTokens)
	fmt.Printf("  Output Tokens:     %d\n", output.Summary.TotalOutputTokens)

	fmt.Printf("\nContext Strategy:\n")
	fmt.Printf("  Smart Context:     %d\n", output.Summary.SmartContextUsed)
	fmt.Printf("  Simple Context:    %d\n", output.Summary.SimpleContextUsed)

	fmt.Printf("\nPerformance:\n")
	fmt.Printf("  Avg Time/Ticket:   %.1f seconds\n", output.Summary.AvgTimePerTicket)
	fmt.Printf("  Files Changed:     %d\n", output.Summary.TotalFilesChanged)

	fmt.Println("\n=== End Metrics ===")
	return nil
}
