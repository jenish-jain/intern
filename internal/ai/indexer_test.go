package ai

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIndexer(t *testing.T) {
	indexer := NewIndexer("/test/repo")
	assert.NotNil(t, indexer)
	assert.Equal(t, "/test/repo", indexer.repoRoot)
}

func TestIndexer_ShouldSkipDir(t *testing.T) {
	indexer := NewIndexer("/repo")

	tests := []struct {
		path     string
		expected bool
	}{
		{".git", true},
		{".git/objects", true},
		{"vendor", true},
		{"vendor/package", true},
		{"node_modules", true},
		{".idea", true},
		{".vscode", true},
		{"build", true},
		{"dist", true},
		{"workspace", true},
		{".ai-intern", true},
		{"internal", false},
		{"cmd", false},
		{"pkg", false},
		{"docs", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := indexer.shouldSkipDir(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIndexer_ShouldSkipFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	// Create test files
	smallFile := filepath.Join(tmpDir, "small.go")
	require.NoError(t, os.WriteFile(smallFile, []byte("package main"), 0644))

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.go", false},
		{"README.md", false},
		{"image.png", true},
		{"photo.jpg", true},
		{"doc.pdf", true},
		{"archive.zip", true},
		{"binary.exe", true},
		{"lib.so", true},
		{"lib.dylib", true},
		{"video.mp4", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := indexer.shouldSkipFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIndexer_CategorizeFile(t *testing.T) {
	indexer := NewIndexer("/repo")

	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "core"},
		{"cmd/agent/main.go", "core"},
		{"internal/orchestrator/coordinator.go", "core"},
		{"internal/ai/agent.go", "core"},
		{"internal/config/config.go", "config"},
		{"internal/util/helpers.go", "util"},
		{"README.md", "doc"},
		{"docs/guide.md", "doc"},
		{"service_test.go", "test"},
		{"internal/test/helper.go", "test"},
		{"config.yaml", "config"},
		{".env", "config"},
		{"random/file.go", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := indexer.categorizeFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIndexer_CalculateImportance(t *testing.T) {
	indexer := NewIndexer("/repo")

	tests := []struct {
		path     string
		minScore float64
		maxScore float64
	}{
		{"cmd/agent/main.go", 9.0, 10.0},
		{"internal/orchestrator/coordinator.go", 9.0, 10.0},
		{"internal/ai/agent.go", 7.0, 8.0},
		{"internal/config/config.go", 5.0, 7.0},
		{"README.md", 3.0, 5.0},
		{"service_test.go", 2.0, 4.0},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			score := indexer.calculateImportance(tt.path)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore)
			assert.GreaterOrEqual(t, score, 0.0)
			assert.LessOrEqual(t, score, 10.0)
		})
	}
}

func TestIndexer_ExtractDependencies(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-deps")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	// Test file with imports
	goFile := filepath.Join(tmpDir, "test.go")
	content := `package main

import (
	"fmt"
	"os"
	"intern/internal/ai"
)

func main() {
	fmt.Println("test")
}
`
	require.NoError(t, os.WriteFile(goFile, []byte(content), 0644))

	deps := indexer.extractDependencies(goFile, "test.go")
	assert.NotNil(t, deps)
	assert.Contains(t, deps, "fmt")
	assert.Contains(t, deps, "os")
	assert.Contains(t, deps, "intern/internal/ai")
}

func TestIndexer_ExtractDependencies_NonGoFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-nongo")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	mdFile := filepath.Join(tmpDir, "README.md")
	require.NoError(t, os.WriteFile(mdFile, []byte("# Test"), 0644))

	deps := indexer.extractDependencies(mdFile, "README.md")
	assert.Nil(t, deps)
}

func TestIndexer_ExtractModule(t *testing.T) {
	indexer := NewIndexer("/repo")

	tests := []struct {
		path     string
		expected string
	}{
		{"internal/orchestrator/coordinator.go", "orchestrator"},
		{"internal/ai/agent.go", "ai"},
		{"cmd/agent/main.go", "agent"},
		{"pkg/util/helper.go", "util"},
		{"README.md", "README.md"},
		{"main.go", "main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := indexer.extractModule(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIndexer_GenerateSummary(t *testing.T) {
	indexer := NewIndexer("/repo")

	tests := []struct {
		path     string
		contains string
	}{
		{"coordinator.go", "coordinator"},
		{"context_builder.go", "context builder"},
		{"file-manager.go", "file manager"},
		{"internal/coordinator.go", "coordinator"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			summary := indexer.generateSummary(tt.path)
			assert.Contains(t, summary, tt.contains)
		})
	}
}

func TestIndexer_BuildIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-build")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test repository structure
	files := map[string]string{
		"main.go":                     "package main\n\nimport \"fmt\"\n\nfunc main() {}",
		"README.md":                   "# Test Repo",
		"internal/service/svc.go":     "package service",
		"internal/service/svc_test.go": "package service",
		"cmd/app/main.go":             "package main",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Build index
	indexer := NewIndexer(tmpDir)
	index, err := indexer.BuildIndex()
	require.NoError(t, err)
	require.NotNil(t, index)

	// Verify index structure
	assert.Equal(t, IndexVersion, index.Version)
	assert.Equal(t, tmpDir, index.RepoRoot)
	assert.WithinDuration(t, time.Now(), index.IndexedAt, 5*time.Second)
	assert.NotEmpty(t, index.Files)
	assert.NotEmpty(t, index.Modules)

	// Verify files are indexed
	assert.Contains(t, index.Files, "main.go")
	assert.Contains(t, index.Files, "README.md")
	assert.Contains(t, index.Files, "internal/service/svc.go")

	// Verify file metadata
	mainFile := index.Files["main.go"]
	assert.Equal(t, "main.go", mainFile.Path)
	assert.Greater(t, mainFile.Size, int64(0))
	assert.Equal(t, "core", mainFile.Category)
	assert.Greater(t, mainFile.Importance, 5.0)

	// Verify modules
	assert.Contains(t, index.Modules, "service")
	assert.Contains(t, index.Modules["service"], "internal/service/svc.go")
}

func TestIndexer_SaveAndLoadIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-saveload")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	// Create a test index
	originalIndex := &FileIndex{
		Version:   IndexVersion,
		IndexedAt: time.Now().Truncate(time.Second),
		RepoRoot:  tmpDir,
		Files: map[string]FileMetadata{
			"test.go": {
				Path:       "test.go",
				Size:       100,
				Importance: 5.0,
				Category:   "core",
			},
		},
		Modules: map[string][]string{
			"test": {"test.go"},
		},
	}

	// Save index
	err = indexer.SaveIndex(originalIndex)
	require.NoError(t, err)

	// Verify file exists
	indexPath := filepath.Join(tmpDir, IndexDirName, IndexFileName)
	assert.FileExists(t, indexPath)

	// Load index
	loadedIndex, err := indexer.LoadIndex()
	require.NoError(t, err)
	require.NotNil(t, loadedIndex)

	// Verify loaded data
	assert.Equal(t, originalIndex.Version, loadedIndex.Version)
	assert.Equal(t, originalIndex.RepoRoot, loadedIndex.RepoRoot)
	assert.Equal(t, originalIndex.IndexedAt.Unix(), loadedIndex.IndexedAt.Unix())
	assert.Len(t, loadedIndex.Files, 1)
	assert.Contains(t, loadedIndex.Files, "test.go")
	assert.Equal(t, float64(5.0), loadedIndex.Files["test.go"].Importance)
}

func TestIndexer_LoadIndex_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-notfound")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	// Try to load non-existent index
	index, err := indexer.LoadIndex()
	assert.Error(t, err)
	assert.Nil(t, index)
	assert.Contains(t, err.Error(), "index not found")
}

func TestIndexer_IndexExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-exists")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	indexer := NewIndexer(tmpDir)

	// Initially should not exist
	assert.False(t, indexer.IndexExists())

	// Create index
	index := &FileIndex{
		Version:   IndexVersion,
		IndexedAt: time.Now(),
		RepoRoot:  tmpDir,
		Files:     make(map[string]FileMetadata),
		Modules:   make(map[string][]string),
	}
	require.NoError(t, indexer.SaveIndex(index))

	// Now should exist
	assert.True(t, indexer.IndexExists())
}
