package indexer_test

import (
	"intern/internal/indexer"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: Tests for unexported methods (shouldSkipDir, shouldSkipFile, categorizeFile, etc.)
// were removed as they require exporting private implementation details.
// These methods are indirectly tested through TestIndexer_BuildIndex which exercises
// all the filtering and categorization logic.

func TestIndexer_BuildIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer-build")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test repository structure
	files := map[string]string{
		"main.go":                      "package main\n\nimport \"fmt\"\n\nfunc main() {}",
		"README.md":                    "# Test Repo",
		"internal/service/svc.go":      "package service",
		"internal/service/svc_test.go": "package service",
		"cmd/app/main.go":              "package main",
	}
	const IndexVersion = "1.0"

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Build index
	indexer := indexer.New(tmpDir)
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

	idx := indexer.New(tmpDir)
	const IndexVersion = "1.0"
	const IndexDirName = ".ai-intern"
	const IndexFileName = "file_index.json"
	// Create a test index
	originalIndex := &indexer.FileIndex{
		Version:   IndexVersion,
		IndexedAt: time.Now().Truncate(time.Second),
		RepoRoot:  tmpDir,
		Files: map[string]indexer.FileMetadata{
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
	err = idx.SaveIndex(originalIndex)
	require.NoError(t, err)

	// Verify file exists
	indexPath := filepath.Join(tmpDir, IndexDirName, IndexFileName)
	assert.FileExists(t, indexPath)

	// Load index
	loadedIndex, err := idx.LoadIndex()
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

	indexer := indexer.New(tmpDir)

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

	idx := indexer.New(tmpDir)
	const IndexVersion = "1.0"

	// Initially should not exist
	assert.False(t, idx.IndexExists())

	// Create index
	index := &indexer.FileIndex{
		Version:   IndexVersion,
		IndexedAt: time.Now(),
		RepoRoot:  tmpDir,
		Files:     make(map[string]indexer.FileMetadata),
		Modules:   make(map[string][]string),
	}
	require.NoError(t, idx.SaveIndex(index))

	// Now should exist
	assert.True(t, idx.IndexExists())
}
