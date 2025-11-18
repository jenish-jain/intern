package ai

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"intern/internal/indexer"
	"intern/internal/util"

	logger "github.com/jenish-jain/logger"
)

// BuildRepoContext reads a subset of files (small text/code files) to provide
// a lightweight context string for the LLM. It skips binaries, vendor, node_modules, and large files.
func BuildRepoContext(repoRoot string, maxFiles int, maxBytesPerFile int) string {
	var b strings.Builder
	count := 0
	stop := errors.New("stop-walk")
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rErr := filepath.Rel(repoRoot, path)
		if rErr != nil {
			return nil
		}
		lower := strings.ToLower(rel)
		// Skip common large/noise directories early
		if d.IsDir() {
			if lower == ".git" || strings.HasPrefix(lower, ".git/") ||
				lower == "vendor" || strings.HasPrefix(lower, "vendor/") ||
				lower == "node_modules" || strings.HasPrefix(lower, "node_modules/") ||
				lower == ".idea" || lower == ".vscode" ||
				lower == "build" || lower == "dist" || lower == "out" {
				return fs.SkipDir
			}
			return nil
		}
		if count >= maxFiles {
			return stop
		}
		// Skip obvious binaries
		if hasAnySuffix(lower, ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".exe", ".bin", ".mp4", ".mov", ".dll") {
			return nil
		}
		// Read up to maxBytesPerFile
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if len(data) > maxBytesPerFile {
			data = data[:maxBytesPerFile]
		}
		b.WriteString("\n\n# FILE: ")
		b.WriteString(rel)
		b.WriteString("\n")
		b.Write(data)
		count++
		return nil
	})
	return b.String()
}

func hasAnySuffix(s string, suff ...string) bool {
	for _, x := range suff {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

// BuildSmartRepoContext builds repository context using intelligent file selection
// based on keywords extracted from ticket description.
// Falls back to BuildRepoContext if index is not available or keywords are empty.
// Uses context caching to avoid rebuilding common files repeatedly.
func BuildSmartRepoContext(repoRoot, ticketDescription string, maxFiles int) (string, error) {
	return BuildSmartRepoContextWithCache(repoRoot, ticketDescription, maxFiles, DefaultCacheConfig())
}

// BuildSmartRepoContextWithCache builds repository context with caching support.
// It combines a cached base context (core files) with ticket-specific context (relevant files).
func BuildSmartRepoContextWithCache(repoRoot, ticketDescription string, maxFiles int, cacheConfig CacheConfig) (string, error) {
	var baseContext string
	maxBytesPerFile := 32 * 1024

	// Try to get cached base context if caching is enabled
	if cacheConfig.Enabled {
		cacheMgr := NewContextCacheManager(cacheConfig)
		cache, err := cacheMgr.GetOrBuildBaseContext(repoRoot, maxFiles*maxBytesPerFile)
		if err == nil && cache != nil {
			baseContext = cache.BaseContext
			// Reduce maxFiles for ticket-specific context since we already have base context
			maxFiles = maxFiles - len(cache.FilesIncluded)
			if maxFiles < 5 {
				maxFiles = 5 // Ensure at least 5 ticket-specific files
			}
		}
	}

	// Try to use smart selection with index for ticket-specific files
	idx := indexer.New(repoRoot)

	// Check if index exists
	if !idx.IndexExists() {
		// No index available, return error to signal fallback needed
		return "", fmt.Errorf("file index not found, smart context unavailable")
	}

	// Load index
	fileIndex, err := idx.LoadIndex()
	if err != nil {
		// Failed to load index, return error
		return "", fmt.Errorf("failed to load index: %w", err)
	}

	// Extract keywords from ticket description
	keywords := indexer.ExtractKeywords(ticketDescription)

	// If no keywords, return error to signal fallback needed
	if len(keywords) == 0 {
		return "", fmt.Errorf("no keywords extracted from ticket description, smart context unavailable")
	}

	// Score files based on keywords
	scores := indexer.ScoreFiles(fileIndex, keywords)

	// Filter out low-relevance files (score < 18.0) to reduce context size
	// This helps prevent timeouts while keeping highly relevant files
	const minRelevanceThreshold = 18.0
	filteredScores := make([]indexer.FileScore, 0, len(scores))
	for _, score := range scores {
		if score.Score >= minRelevanceThreshold {
			filteredScores = append(filteredScores, score)
		}
	}

	// Log filtering results for debugging
	if len(scores) > len(filteredScores) {
		logger.Info("Filtered low-relevance files",
			"total", len(scores),
			"filtered", len(filteredScores),
			"removed", len(scores)-len(filteredScores))
	}

	// Select top files from filtered results
	topScores := indexer.SelectTopFiles(filteredScores, maxFiles)

	// Build ticket-specific context from top files
	var sb strings.Builder

	// Add base context first if available
	if baseContext != "" {
		sb.WriteString("# Base Repository Context (Cached)\n")
		sb.WriteString(baseContext)
		sb.WriteString("\n\n")
	}

	// Add ticket-specific context
	sb.WriteString(fmt.Sprintf("# Ticket-Specific Context (Smart Selection)\n"))
	sb.WriteString(fmt.Sprintf("# Based on keywords: %v\n", keywords[:util.Min(5, len(keywords))]))
	sb.WriteString(fmt.Sprintf("# Selected %d most relevant files\n\n", len(topScores)))

	for _, fileScore := range topScores {
		filePath := filepath.Join(repoRoot, fileScore.Path)

		sb.WriteString(fmt.Sprintf("\n## FILE: %s (relevance: %.1f)\n", fileScore.Path, fileScore.Score))

		// Extract minimal context for Go files, full content for others
		context, err := indexer.ExtractMinimalContext(filePath)
		if err != nil {
			// If extraction fails, try reading file directly
			data, readErr := os.ReadFile(filePath)
			if readErr == nil {
				sb.Write(data)
			}
			continue
		}

		sb.WriteString(context)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
