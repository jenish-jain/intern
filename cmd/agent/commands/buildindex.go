package commands

import (
	"os"
	"path/filepath"

	"intern/internal/indexer"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// BuildIndexCmd represents the build-index command
var BuildIndexCmd = &cobra.Command{
	Use:   "build-index",
	Short: "Build file index for smart context selection",
	Long:  `Build or update the file index for intelligent context selection during code analysis.`,
	RunE:  buildIndex,
}

func buildIndex(cmd *cobra.Command, args []string) error {
	logger.Info("Building file index for smart context selection...")

	// Load config to get repository path
	_, repoPaths, err := InitPartialDependencies()
	if err != nil {
		logger.Error("Failed to initialize dependencies: %v", err)
		return err
	}

	repoRoot := repoPaths.Root()

	// Check if repository exists
	if _, err := os.Stat(repoRoot); os.IsNotExist(err) {
		logger.Error("Repository not found", "path", repoRoot)
		logger.Info("Make sure to clone the repository first or set WORKING_DIR correctly in your config")
		return err
	}

	logger.Info("Indexing repository", "path", repoRoot)

	// Build or update index incrementally
	idx := indexer.New(repoRoot)
	fileIndex, wasUpdated, err := idx.RebuildIfStale()
	if err != nil {
		logger.Error("Failed to build index", "error", err)
		return err
	}

	if !wasUpdated {
		logger.Info("Index is already up to date")
		indexPath := filepath.Join(repoRoot, indexer.IndexDirName, indexer.IndexFileName)
		logger.Info("Using existing index", "path", indexPath)
		return nil
	}

	logger.Info("Index built successfully", "files", len(fileIndex.Files), "modules", len(fileIndex.Modules))

	// Save index
	if err := idx.SaveIndex(fileIndex); err != nil {
		logger.Error("Failed to save index", "error", err)
		return err
	}

	indexPath := filepath.Join(repoRoot, indexer.IndexDirName, indexer.IndexFileName)
	logger.Info("Index saved successfully", "path", indexPath)

	// Show some statistics
	categoryCounts := make(map[string]int)
	for _, meta := range fileIndex.Files {
		categoryCounts[meta.Category]++
	}

	logger.Info("Index statistics:")
	for category, count := range categoryCounts {
		logger.Info("  - "+category, "count", count)
	}

	return nil
}
