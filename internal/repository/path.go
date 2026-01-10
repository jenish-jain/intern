package repository

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RepositoryPath manages all repository path construction in a centralized,
// testable manner. This eliminates scattered path logic and dependency on
// environment variables.
type RepositoryPath struct {
	workingDir string // Base working directory (e.g., "./workspace")
	repoName   string // Repository name (e.g., "testrepo")
}

// NewRepositoryPath creates a new path manager for the given working directory
// and repository name. Both parameters are required and will be validated.
//
// Example:
//
//	paths := NewRepositoryPath("./workspace", "myrepo")
//	repoRoot := paths.Root() // "./workspace/myrepo"
func NewRepositoryPath(workingDir, repoName string) (*RepositoryPath, error) {
	if workingDir == "" {
		return nil, fmt.Errorf("working directory cannot be empty")
	}
	if repoName == "" {
		return nil, fmt.Errorf("repository name cannot be empty")
	}

	// Clean paths to normalize separators and remove redundant elements
	cleanWorkingDir := filepath.Clean(workingDir)
	cleanRepoName := filepath.Clean(repoName)

	// Security: prevent path traversal
	if strings.Contains(cleanRepoName, "..") {
		return nil, fmt.Errorf("repository name contains path traversal: %s", repoName)
	}

	return &RepositoryPath{
		workingDir: cleanWorkingDir,
		repoName:   cleanRepoName,
	}, nil
}

// Root returns the absolute path to the repository root directory.
// This is the base path for all repository operations.
//
// Example: "./workspace/myrepo"
func (p *RepositoryPath) Root() string {
	return filepath.Join(p.workingDir, p.repoName)
}

// GitDir returns the path to the .git directory within the repository.
// Useful for checking if a repository has been cloned.
//
// Example: "./workspace/myrepo/.git"
func (p *RepositoryPath) GitDir() string {
	return filepath.Join(p.Root(), ".git")
}

// File returns the absolute path to a file within the repository.
// The filePath parameter should be relative to the repository root.
//
// Example:
//
//	paths.File("internal/config/config.go")
//	// Returns: "./workspace/myrepo/internal/config/config.go"
func (p *RepositoryPath) File(filePath string) string {
	return filepath.Join(p.Root(), filePath)
}

// Subdir returns the absolute path to a subdirectory within the repository.
// The subdirPath parameter should be relative to the repository root.
//
// Example:
//
//	paths.Subdir("internal/config")
//	// Returns: "./workspace/myrepo/internal/config"
func (p *RepositoryPath) Subdir(subdirPath string) string {
	return filepath.Join(p.Root(), subdirPath)
}

// WorkingDir returns the base working directory (without repository name).
//
// Example: "./workspace"
func (p *RepositoryPath) WorkingDir() string {
	return p.workingDir
}

// RepoName returns the repository name.
//
// Example: "myrepo"
func (p *RepositoryPath) RepoName() string {
	return p.repoName
}
