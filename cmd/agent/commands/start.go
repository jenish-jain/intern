package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// StartCmd represents the start command to run the agent
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the AI Intern Agent",
	Long:  `Start the AI Intern Agent and begin processing tickets.`,
	RunE:  startAgent,
}

func startAgent(cmd *cobra.Command, args []string) error {
	logger.Info("Starting AI Intern Agent...")

	// Initialize all dependencies
	deps, err := InitDependencies(context.Background())
	if err != nil {
		logger.Error("Failed to initialize dependencies: %v", err)
		return err
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal, shutting down gracefully...")
		cancel()
	}()

	logger.Info("Starting AI Intern Agent MVP...")
	deps.Coordinator.Run(ctx)

	return nil
}
