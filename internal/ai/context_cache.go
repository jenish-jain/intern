package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jenish-jain/logger"
)

// ParseCacheTTL parses a duration string (e.g., "1h", "30m") into time.Duration
func ParseCacheTTL(ttlStr string) time.Duration {
	if ttlStr == "" {
		return 1 * time.Hour
	}
	duration, err := time.ParseDuration(ttlStr)
	if err != nil {
		logger.Warn("Invalid cache TTL, using default 1h", "ttl", ttlStr, "error", err)
		return 1 * time.Hour
	}
	return duration
}

// ContextCache stores prebuilt context to avoid rebuilding common files repeatedly.
// The base context includes core files that rarely change (config, types, interfaces).
type ContextCache struct {
	BaseContext    string    `json:"base_context"`     // Common repository context
	RepoPath       string    `json:"repo_path"`        // Repository path for validation
	GitCommitHash  string    `json:"git_commit_hash"`  // Git commit when cache was built
	CreatedAt      time.Time `json:"created_at"`       // When cache was created
	FilesIncluded  []string  `json:"files_included"`   // List of files in base context
	ContextBytes   int       `json:"context_bytes"`    // Size of base context
}

// CacheConfig configures context caching behavior
type CacheConfig struct {
	Enabled   bool          // Enable/disable caching
	TTL       time.Duration // Time-to-live for cache (default: 1 hour)
	CacheFile string        // Path to cache file (default: .ai-intern/context_cache.json)
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:   true,
		TTL:       1 * time.Hour,
		CacheFile: ".ai-intern/context_cache.json",
	}
}

// ContextCacheManager handles context caching operations
type ContextCacheManager struct {
	config CacheConfig
}

// NewContextCacheManager creates a new cache manager with given config
func NewContextCacheManager(config CacheConfig) *ContextCacheManager {
	return &ContextCacheManager{config: config}
}

// LoadCache loads the context cache from disk
func (m *ContextCacheManager) LoadCache(repoPath string) (*ContextCache, error) {
	if !m.config.Enabled {
		return nil, fmt.Errorf("cache disabled")
	}

	cacheFilePath := filepath.Join(repoPath, m.config.CacheFile)
	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache ContextCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// SaveCache saves the context cache to disk
func (m *ContextCacheManager) SaveCache(cache *ContextCache) error {
	if !m.config.Enabled {
		return nil // No-op if disabled
	}

	cacheFilePath := filepath.Join(cache.RepoPath, m.config.CacheFile)

	// Ensure directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cacheFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// IsStale checks if the cache is stale and needs rebuilding
func (m *ContextCacheManager) IsStale(cache *ContextCache, repoPath string) bool {
	if cache == nil {
		return true
	}

	// Check if cache is too old
	if time.Since(cache.CreatedAt) > m.config.TTL {
		logger.Debug("Cache is stale: exceeded TTL",
			"age", time.Since(cache.CreatedAt),
			"ttl", m.config.TTL)
		return true
	}

	// Check if git commit changed
	currentCommit := getGitCommitHash(repoPath)
	if currentCommit != "" && currentCommit != cache.GitCommitHash {
		logger.Debug("Cache is stale: git commit changed",
			"cached_commit", cache.GitCommitHash[:8],
			"current_commit", currentCommit[:8])
		return true
	}

	// Check if repo path matches
	if cache.RepoPath != repoPath {
		logger.Debug("Cache is stale: repo path mismatch")
		return true
	}

	return false
}

// BuildBaseContext creates a base context from core repository files
// These are files that typically don't change often and are useful for most tickets
func (m *ContextCacheManager) BuildBaseContext(repoPath string, maxBytes int) (*ContextCache, error) {
	var sb strings.Builder
	var filesIncluded []string

	// Core patterns to include in base context (adjust for your project)
	corePatterns := []string{
		"internal/config/*.go",       // Configuration files
		"internal/*/types.go",         // Type definitions
		"internal/**/interface*.go",   // Interface definitions
		"cmd/*/main.go",              // Entry points
		"pkg/**/*.go",                // Public packages (if any)
		"go.mod",                     // Dependencies
		"README.md",                  // Project overview
	}

	totalBytes := 0
	maxBaseBytes := maxBytes / 5 // Use only 20% of max bytes for base context

	for _, pattern := range corePatterns {
		matches, err := filepath.Glob(filepath.Join(repoPath, pattern))
		if err != nil {
			continue
		}

		for _, filePath := range matches {
			if totalBytes >= maxBaseBytes {
				break
			}

			relPath, err := filepath.Rel(repoPath, filePath)
			if err != nil {
				continue
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			// Limit individual file size
			if len(data) > 10*1024 { // Max 10KB per file in base context
				data = data[:10*1024]
			}

			sb.WriteString(fmt.Sprintf("\n\n# FILE: %s\n", relPath))
			sb.Write(data)

			filesIncluded = append(filesIncluded, relPath)
			totalBytes += len(data)
		}
	}

	cache := &ContextCache{
		BaseContext:   sb.String(),
		RepoPath:      repoPath,
		GitCommitHash: getGitCommitHash(repoPath),
		CreatedAt:     time.Now(),
		FilesIncluded: filesIncluded,
		ContextBytes:  totalBytes,
	}

	logger.Info("Built base context cache",
		"files", len(filesIncluded),
		"bytes", totalBytes,
		"commit", cache.GitCommitHash[:8])

	return cache, nil
}

// GetOrBuildBaseContext retrieves cached base context or builds a new one
func (m *ContextCacheManager) GetOrBuildBaseContext(repoPath string, maxBytes int) (*ContextCache, error) {
	// Try to load existing cache
	cache, err := m.LoadCache(repoPath)
	if err == nil && !m.IsStale(cache, repoPath) {
		logger.Debug("Using cached base context",
			"files", len(cache.FilesIncluded),
			"bytes", cache.ContextBytes,
			"age", time.Since(cache.CreatedAt))
		return cache, nil
	}

	// Build new cache
	logger.Info("Building new base context cache")
	cache, err = m.BuildBaseContext(repoPath, maxBytes)
	if err != nil {
		return nil, err
	}

	// Save cache
	if err := m.SaveCache(cache); err != nil {
		logger.Warn("Failed to save context cache", "error", err)
		// Continue anyway - cache saving is not critical
	}

	return cache, nil
}

// getGitCommitHash returns the current git commit hash
func getGitCommitHash(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
