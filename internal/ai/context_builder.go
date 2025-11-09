package ai

import (
	"errors"
	"fmt"
	"intern/internal/indexer"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
func BuildSmartRepoContext(repoRoot, ticketDescription string, maxFiles int) (string, error) {
	// Try to use smart selection with index
	idx := indexer.New(repoRoot)

	// Check if index exists
	if !idx.IndexExists() {
		// No index available, fall back to simple context builder
		return BuildRepoContext(repoRoot, maxFiles, 32*1024), nil
	}

	// Load index
	fileIndex, err := idx.LoadIndex()
	if err != nil {
		// Failed to load index, fall back
		return BuildRepoContext(repoRoot, maxFiles, 32*1024), nil
	}

	// Extract keywords from ticket description
	keywords := indexer.ExtractKeywords(ticketDescription)

	// If no keywords, fall back to simple selection
	if len(keywords) == 0 {
		return BuildRepoContext(repoRoot, maxFiles, 32*1024), nil
	}

	// Score files based on keywords
	scores := indexer.ScoreFiles(fileIndex, keywords)

	// Select top files
	topScores := indexer.SelectTopFiles(scores, maxFiles)

	// Build context from top files
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Repository Context (Smart Selection)\n"))
	sb.WriteString(fmt.Sprintf("# Based on keywords: %v\n", keywords[:min(5, len(keywords))]))
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
