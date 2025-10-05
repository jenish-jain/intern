package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRepoContext(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "repo-context-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := map[string]string{
		"main.go":                  "package main\n\nfunc main() {}\n",
		"README.md":                "# Test Repo\n",
		"internal/service.go":      "package internal\n\ntype Service struct{}\n",
		"internal/service_test.go": "package internal\n\nfunc TestService(t *testing.T) {}\n",
		"vendor/lib.go":            "// vendor file",          // should be skipped
		".git/config":              "git config",              // should be skipped
		"image.png":                "fake binary",             // should be skipped
		"large.txt":                strings.Repeat("x", 1000), // will be truncated
	}

	for relPath, content := range testFiles {
		fullPath := filepath.Join(tmpDir, relPath)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Test basic functionality
	context := BuildRepoContext(tmpDir, 10, 500)

	// Should include main.go
	assert.Contains(t, context, "# FILE: main.go")
	assert.Contains(t, context, "package main")

	// Should include README.md
	assert.Contains(t, context, "# FILE: README.md")
	assert.Contains(t, context, "# Test Repo")

	// Should include internal files
	assert.Contains(t, context, "# FILE: internal/service.go")
	assert.Contains(t, context, "# FILE: internal/service_test.go")

	// Should skip vendor
	assert.NotContains(t, context, "vendor/lib.go")

	// Should skip .git
	assert.NotContains(t, context, ".git/config")

	// Should skip binary files
	assert.NotContains(t, context, "image.png")

	// Should include large.txt but truncated
	assert.Contains(t, context, "# FILE: large.txt")
	// Content should be truncated to 500 bytes
	largeSection := strings.Split(context, "# FILE: large.txt")[1]
	if len(largeSection) > 0 {
		nextFileIdx := strings.Index(largeSection, "# FILE:")
		if nextFileIdx > 0 {
			largeSection = largeSection[:nextFileIdx]
		}
		// Should not contain the full 1000 x's
		assert.True(t, len(strings.TrimSpace(largeSection)) <= 500)
	}
}

func TestBuildRepoContext_MaxFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-context-maxfiles")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create more files than the limit
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".go")
		err := os.WriteFile(filename, []byte("content"), 0644)
		require.NoError(t, err)
	}

	// Limit to 3 files
	context := BuildRepoContext(tmpDir, 3, 1000)

	// Should only include 3 files
	fileCount := strings.Count(context, "# FILE:")
	assert.LessOrEqual(t, fileCount, 3)
}

func TestBuildRepoContext_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-context-empty")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	context := BuildRepoContext(tmpDir, 10, 1000)
	assert.Equal(t, "", context)
}

func TestHasAnySuffix(t *testing.T) {
	tests := []struct {
		input    string
		suffixes []string
		expected bool
	}{
		{"file.go", []string{".go", ".py"}, true},
		{"file.py", []string{".go", ".py"}, true},
		{"file.txt", []string{".go", ".py"}, false},
		{"", []string{".go"}, false},
		{"file.go", []string{}, false},
	}

	for _, tt := range tests {
		result := hasAnySuffix(tt.input, tt.suffixes...)
		assert.Equal(t, tt.expected, result, "hasAnySuffix(%q, %v)", tt.input, tt.suffixes)
	}
}
