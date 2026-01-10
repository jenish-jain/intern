package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version information - imported from root.go
var Version string
var BuildDate string

// VersionCmd represents the version command
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the agent version, build information, and go runtime details.`,
	RunE:  showVersion,
}

func showVersion(cmd *cobra.Command, args []string) error {
	fmt.Println("\n=== AI Intern Agent ===")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Build Date: %s\n", BuildDate)
	fmt.Printf("Go Version: %s\n", runtime.Version())
	fmt.Printf("OS:         %s\n", runtime.GOOS)
	fmt.Printf("Arch:       %s\n", runtime.GOARCH)
	fmt.Println("=== End Version ===\n")
	return nil
}
