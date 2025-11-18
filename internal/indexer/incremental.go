package indexer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jenish-jain/logger"
)

// getGitCommitHash returns the current git commit hash for the repository
func (idx *Indexer) getGitCommitHash() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = idx.repoRoot

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	hash := strings.TrimSpace(stdout.String())
	return hash, nil
}

// getChangedFiles returns files that changed between two git commits
func (idx *Indexer) getChangedFiles(oldCommit, newCommit string) ([]string, error) {
	// Get list of changed files
	cmd := exec.Command("git", "diff", "--name-status", oldCommit, newCommit)
	cmd.Dir = idx.repoRoot

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	lines := strings.Split(stdout.String(), "\n")
	changedFiles := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse git diff output: "M\tfile.go" or "A\tfile.go" or "D\tfile.go"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			filePath := parts[1]

			// Handle renames: "R100\told.go\tnew.go"
			if strings.HasPrefix(status, "R") && len(parts) >= 3 {
				// For renames, track both old and new paths
				changedFiles = append(changedFiles, parts[1], parts[2])
			} else {
				changedFiles = append(changedFiles, filePath)
			}
		}
	}

	return changedFiles, nil
}

// UpdateIndex incrementally updates the index based on git changes
// Returns the updated index or error
func (idx *Indexer) UpdateIndex() (*FileIndex, error) {
	// Load existing index
	existingIndex, err := idx.LoadIndex()
	if err != nil {
		logger.Warn("Failed to load existing index, will build from scratch", "error", err)
		return idx.BuildIndex()
	}

	// Get current git commit
	currentCommit, err := idx.getGitCommitHash()
	if err != nil {
		logger.Warn("Failed to get git commit, will build from scratch", "error", err)
		return idx.BuildIndex()
	}

	// If index doesn't have a commit hash, rebuild
	if existingIndex.GitCommitHash == "" {
		logger.Info("Index doesn't have git commit hash, rebuilding")
		return idx.BuildIndex()
	}

	// If commits are the same, index is up to date
	if existingIndex.GitCommitHash == currentCommit {
		logger.Info("Index is up to date", "commit", currentCommit[:8])
		return existingIndex, nil
	}

	logger.Info("Updating index incrementally",
		"old_commit", existingIndex.GitCommitHash[:8],
		"new_commit", currentCommit[:8])

	// Get changed files
	changedFiles, err := idx.getChangedFiles(existingIndex.GitCommitHash, currentCommit)
	if err != nil {
		logger.Warn("Failed to get changed files, will build from scratch", "error", err)
		return idx.BuildIndex()
	}

	logger.Info("Processing changed files", "count", len(changedFiles))

	// Create updated index based on existing index
	updatedIndex := &FileIndex{
		Version:       IndexVersion,
		IndexedAt:     time.Now(),
		RepoRoot:      idx.repoRoot,
		GitCommitHash: currentCommit,
		Files:         make(map[string]FileMetadata),
		Modules:       make(map[string][]string),
	}

	// Copy existing files
	for path, metadata := range existingIndex.Files {
		updatedIndex.Files[path] = metadata
	}

	// Update changed files
	for _, relPath := range changedFiles {
		// Skip if should be excluded
		if idx.shouldSkipFile(relPath) {
			continue
		}

		absPath := filepath.Join(idx.repoRoot, relPath)

		// Check if file still exists (not deleted)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			// File was deleted, remove from index
			delete(updatedIndex.Files, relPath)
			logger.Debug("Removed deleted file from index", "path", relPath)
			continue
		}

		// File was added or modified, update metadata
		metadata := idx.analyzeFile(absPath, relPath)
		if metadata != nil {
			updatedIndex.Files[relPath] = *metadata
			logger.Debug("Updated file in index", "path", relPath)
		}
	}

	// Rebuild module mapping
	updatedIndex.Modules = make(map[string][]string)
	for relPath := range updatedIndex.Files {
		module := idx.extractModule(relPath)
		if module != "" {
			updatedIndex.Modules[module] = append(updatedIndex.Modules[module], relPath)
		}
	}

	logger.Info("Index updated successfully",
		"total_files", len(updatedIndex.Files),
		"changed_files", len(changedFiles),
		"modules", len(updatedIndex.Modules))

	return updatedIndex, nil
}

// RebuildIfStale checks if the index is stale and rebuilds/updates if needed
// Returns true if index was rebuilt/updated, false if already up to date
func (idx *Indexer) RebuildIfStale() (*FileIndex, bool, error) {
	// Check if index exists
	if !idx.IndexExists() {
		logger.Info("Index doesn't exist, building from scratch")
		index, err := idx.BuildIndex()
		if err != nil {
			return nil, false, err
		}
		return index, true, nil
	}

	// Try incremental update
	index, err := idx.UpdateIndex()
	if err != nil {
		return nil, false, err
	}

	// Check if index was already up to date
	existingIndex, loadErr := idx.LoadIndex()
	if loadErr == nil && existingIndex.GitCommitHash == index.GitCommitHash {
		// Index was already up to date
		return index, false, nil
	}

	// Index was updated
	return index, true, nil
}
