package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"intern/internal/orchestrator"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// StatusCmd represents the status command
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current agent status and metrics",
	Long:  `Display the current configuration, processed tickets, and latest metrics.`,
	RunE:  showStatus,
}

func showStatus(cmd *cobra.Command, args []string) error {
	logger.Info("Checking agent status...")

	// Load config and paths
	cfg, repoPaths, err := InitPartialDependencies()
	if err != nil {
		logger.Warn("Failed to initialize dependencies: %v", err)
		fmt.Println("\n=== AI Intern Agent Status ===")
		fmt.Println("Error: Could not determine configuration")
		return nil
	}

	// Load state
	stateFile := "agent_state.jsonc"
	state := NewState(stateFile)
	if err := state.Load(); err != nil {
		logger.Warn("Failed to load state", "error", err)
	}

	metricsPath := filepath.Join(repoPaths.Root(), ".ai-intern", "metrics.json")

	fmt.Println("\n=== AI Intern Agent Status ===")
	fmt.Printf("\nConfiguration:\n")
	fmt.Printf("  AI Provider:     %s\n", cfg.AIProvider)
	if cfg.AIProvider == "ollama" {
		fmt.Printf("  Ollama Model:    %s\n", cfg.OllamaModel)
		fmt.Printf("  Ollama URL:      %s\n", cfg.OllamaBaseURL)
	}
	fmt.Printf("  GitHub Repo:     %s/%s\n", cfg.GitHubOwner, cfg.GitHubRepo)
	fmt.Printf("  JIRA Project:    %s\n", cfg.JiraProject)
	fmt.Printf("  Working Dir:     %s\n", cfg.WorkingDir)
	fmt.Printf("  Max Concurrent:  %d\n", cfg.MaxConcurrentTickets)
	fmt.Printf("  Polling Interval: %s\n", cfg.PollingInterval)

	fmt.Printf("\nFeatures:\n")
	fmt.Printf("  Context Caching:  %v\n", cfg.ContextCacheEnabled)
	fmt.Printf("  Self-Healing:     %v\n", cfg.SelfHealEnabled)
	fmt.Printf("  Metrics Server:   %v", cfg.MetricsEnabled)
	if cfg.MetricsEnabled {
		fmt.Printf(" (port %d)", cfg.MetricsPort)
	}
	fmt.Println()
	fmt.Printf("  Dry Run Mode:     %v\n", cfg.DryRun)

	fmt.Printf("\nProcessed Tickets: %d\n", len(state.Processed))

	// Try to show latest metrics if available
	if _, err := os.Stat(metricsPath); err == nil {
		output, err := orchestrator.LoadMetrics(metricsPath)
		if err == nil {
			fmt.Printf("\nLatest Metrics (from %s):\n", output.RunMetadata.Timestamp)
			fmt.Printf("  Tickets Processed: %d\n", output.Summary.TicketsProcessed)
			fmt.Printf("  PRs Created:       %d\n", output.Summary.PRsCreated)
			fmt.Printf("  Total Cost:        $%.2f\n", output.Summary.TotalCost)
		}
	}

	fmt.Println("\n=== End Status ===")
	return nil
}
