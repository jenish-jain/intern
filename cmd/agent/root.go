package main

import (
	"intern/cmd/agent/commands"

	"github.com/spf13/cobra"
)

// Version information - can be set during build with ldflags
var (
	Version   = "0.1.0"
	BuildDate = "2026-01-10"
)

// RootCmd represents the root command
var RootCmd = &cobra.Command{
	Use:   "agent",
	Short: "AI Intern Agent - Intelligent code automation for your project",
	Long: `AI Intern Agent is an AI-powered automation tool that processes tickets, 
creates code solutions, and submits pull requests to your repository.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.StartCmd.RunE(cmd, args)
	},
}

// Execute runs the root command
func Execute() error {
	return RootCmd.Execute()
}

func init() {
	// Set version info in commands
	commands.Version = Version
	commands.BuildDate = BuildDate

	// Add all subcommands
	RootCmd.AddCommand(commands.StartCmd)
	RootCmd.AddCommand(commands.ServeCmd)
	RootCmd.AddCommand(commands.InitCmd)
	RootCmd.AddCommand(commands.BuildIndexCmd)
	RootCmd.AddCommand(commands.StatusCmd)
	RootCmd.AddCommand(commands.MetricsCmd)
	RootCmd.AddCommand(commands.VersionCmd)

	// Configure default behavior - start the agent when no command is specified
	RootCmd.CompletionOptions.DisableDefaultCmd = true
}
