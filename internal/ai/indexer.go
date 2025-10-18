package ai

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	IndexVersion      = "1.0"
	IndexFileName     = "file_index.json"
	IndexDirName      = ".ai-intern"
	ProjectIndexName  = "PROJECT_INDEX.md"
)

// Indexer generates and manages repository file indexes
type Indexer struct {
	repoRoot string
}

// NewIndexer creates a new repository indexer
func NewIndexer(repoRoot string) *Indexer {
	return &Indexer{repoRoot: repoRoot}
}

// BuildIndex scans the repository and creates a complete file index
func (idx *Indexer) BuildIndex() (*FileIndex, error) {
	index := &FileIndex{
		Version:   IndexVersion,
		IndexedAt: time.Now(),
		RepoRoot:  idx.repoRoot,
		Files:     make(map[string]FileMetadata),
		Modules:   make(map[string][]string),
	}

	err := filepath.WalkDir(idx.repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		relPath, rErr := filepath.Rel(idx.repoRoot, path)
		if rErr != nil {
			return nil
		}

		// Skip excluded directories
		if d.IsDir() {
			if idx.shouldSkipDir(relPath) {
				return fs.SkipDir
			}
			return nil
		}

		// Skip excluded files
		if idx.shouldSkipFile(relPath) {
			return nil
		}

		// Analyze file and add to index
		metadata := idx.analyzeFile(path, relPath)
		if metadata != nil {
			index.Files[relPath] = *metadata

			// Group by module
			module := idx.extractModule(relPath)
			if module != "" {
				index.Modules[module] = append(index.Modules[module], relPath)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return index, nil
}

// shouldSkipDir determines if a directory should be excluded from indexing
func (idx *Indexer) shouldSkipDir(relPath string) bool {
	lower := strings.ToLower(relPath)
	excludedDirs := []string{
		".git", "vendor", "node_modules", ".idea", ".vscode",
		"build", "dist", "out", ".ai-intern", "workspace",
	}

	for _, excluded := range excludedDirs {
		if lower == excluded || strings.HasPrefix(lower, excluded+"/") {
			return true
		}
	}

	return false
}

// shouldSkipFile determines if a file should be excluded from indexing
func (idx *Indexer) shouldSkipFile(relPath string) bool {
	lower := strings.ToLower(relPath)

	// Skip binary and media files
	binaryExts := []string{
		".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip",
		".exe", ".bin", ".mp4", ".mov", ".dll", ".so", ".dylib",
		".tar", ".gz", ".bz2", ".7z", ".rar",
	}

	for _, ext := range binaryExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	// Skip very large files (>1MB)
	info, err := os.Stat(filepath.Join(idx.repoRoot, relPath))
	if err == nil && info.Size() > 1*1024*1024 {
		return true
	}

	return false
}

// analyzeFile examines a file and generates metadata
func (idx *Indexer) analyzeFile(absPath, relPath string) *FileMetadata {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil
	}

	metadata := &FileMetadata{
		Path:         relPath,
		Size:         info.Size(),
		LastModified: info.ModTime(),
		Category:     idx.categorizeFile(relPath),
		Importance:   idx.calculateImportance(relPath),
		Dependencies: idx.extractDependencies(absPath, relPath),
		Summary:      idx.generateSummary(relPath),
	}

	return metadata
}

// categorizeFile assigns a category to a file
func (idx *Indexer) categorizeFile(relPath string) string {
	lower := strings.ToLower(relPath)

	if strings.Contains(lower, "_test.go") || strings.Contains(lower, "/test/") {
		return "test"
	}
	if strings.HasSuffix(lower, ".md") || strings.Contains(lower, "/docs/") {
		return "doc"
	}
	if strings.Contains(lower, "config") || strings.Contains(lower, ".env") ||
	   strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") ||
	   strings.HasSuffix(lower, ".json") {
		return "config"
	}
	if strings.Contains(lower, "/cmd/") || strings.Contains(lower, "main.go") {
		return "core"
	}
	if strings.Contains(lower, "internal/orchestrator/") ||
	   strings.Contains(lower, "internal/ai/") ||
	   strings.Contains(lower, "internal/repository/") ||
	   strings.Contains(lower, "internal/ticketing/") {
		return "core"
	}
	if strings.Contains(lower, "internal/") {
		return "util"
	}

	return "other"
}

// calculateImportance assigns an importance score (0-10)
func (idx *Indexer) calculateImportance(relPath string) float64 {
	score := 5.0 // Default importance

	lower := strings.ToLower(relPath)

	// High importance for entry points
	if strings.Contains(lower, "main.go") {
		score += 5.0
	}

	// High importance for core orchestrator
	if strings.Contains(lower, "/orchestrator/coordinator.go") {
		score += 4.0
	}

	// High importance for core modules
	if strings.Contains(lower, "/orchestrator/") || strings.Contains(lower, "/ai/") {
		score += 2.0
	}

	// Medium importance for other internal packages
	if strings.Contains(lower, "/internal/") {
		score += 1.0
	}

	// Lower importance for tests
	if strings.Contains(lower, "_test.go") {
		score -= 2.0
	}

	// Lower importance for docs
	if strings.HasSuffix(lower, ".md") {
		score -= 1.0
	}

	// Clamp to 0-10 range
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}

	return score
}

// extractDependencies finds Go imports or module dependencies
func (idx *Indexer) extractDependencies(absPath, relPath string) []string {
	if !strings.HasSuffix(relPath, ".go") {
		return nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	// Extract import statements using regex
	importRegex := regexp.MustCompile(`import\s+(?:\(([^)]+)\)|"([^"]+)")`)
	matches := importRegex.FindAllStringSubmatch(string(content), -1)

	deps := make(map[string]bool)
	for _, match := range matches {
		// Handle both single import and import blocks
		if match[1] != "" {
			// Import block
			lines := strings.Split(match[1], "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "\"") && strings.HasSuffix(line, "\"") {
					dep := strings.Trim(line, "\"")
					if dep != "" {
						deps[dep] = true
					}
				}
			}
		} else if match[2] != "" {
			// Single import
			deps[match[2]] = true
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	return result
}

// extractModule determines the module name from file path
func (idx *Indexer) extractModule(relPath string) string {
	// For paths like "internal/orchestrator/coordinator.go", return "orchestrator"
	parts := strings.Split(relPath, "/")

	if len(parts) >= 2 {
		if parts[0] == "internal" || parts[0] == "cmd" || parts[0] == "pkg" {
			return parts[1]
		}
	}

	if len(parts) >= 1 {
		return parts[0]
	}

	return ""
}

// generateSummary creates a brief summary from the file path
func (idx *Indexer) generateSummary(relPath string) string {
	base := filepath.Base(relPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Convert snake_case or kebab-case to words
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")

	// Add context from directory
	dir := filepath.Dir(relPath)
	if dir != "." && !strings.Contains(dir, "/") {
		return dir + " - " + name
	}

	return name
}

// SaveIndex writes the index to disk
func (idx *Indexer) SaveIndex(index *FileIndex) error {
	indexDir := filepath.Join(idx.repoRoot, IndexDirName)
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return err
	}

	indexPath := filepath.Join(indexDir, IndexFileName)
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return err
	}

	return nil
}

// LoadIndex reads the index from disk
func (idx *Indexer) LoadIndex() (*FileIndex, error) {
	indexPath := filepath.Join(idx.repoRoot, IndexDirName, IndexFileName)

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("index not found, run with --reindex to create")
		}
		return nil, err
	}

	var index FileIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

// IndexExists checks if an index file exists
func (idx *Indexer) IndexExists() bool {
	indexPath := filepath.Join(idx.repoRoot, IndexDirName, IndexFileName)
	_, err := os.Stat(indexPath)
	return err == nil
}
