package ai

import (
	"intern/internal/indexer"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logger "github.com/jenish-jain/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Initialize logger for tests
	logger.Init("error")
}

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

func TestBuildSmartRepoContext(t *testing.T) {
	// Create a test repository
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"internal/auth/login.go": `package auth

type LoginService struct {
	username string
}

func (s *LoginService) Authenticate(user, pass string) error {
	// implementation
	return nil
}
`,
		"internal/auth/service.go": `package auth

type AuthService struct{}

func NewAuthService() *AuthService {
	return &AuthService{}
}
`,
		"cmd/main.go": `package main

func main() {
	println("Hello")
}
`,
		"README.md": "# Test Project\n",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Build and save index
	idx := indexer.New(tmpDir)
	fileIndex, err := idx.BuildIndex()
	require.NoError(t, err)
	err = idx.SaveIndex(fileIndex)
	require.NoError(t, err)

	tests := []struct {
		name               string
		ticketDescription  string
		expectedInContext  []string
		notExpectedInContext []string
	}{
		{
			name:              "auth-related ticket",
			ticketDescription: "Fix authentication bug in login service",
			expectedInContext: []string{
				"internal/auth/login.go",
				"LoginService",
				"Authenticate",
			},
			notExpectedInContext: []string{},
		},
		{
			name:              "specific file mentioned",
			ticketDescription: "Update internal/auth/service.go to add new method",
			expectedInContext: []string{
				"internal/auth/service.go",
				"AuthService",
			},
			notExpectedInContext: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, err := BuildSmartRepoContext(tmpDir, tt.ticketDescription, 5, nil)
			require.NoError(t, err)

			for _, expected := range tt.expectedInContext {
				assert.Contains(t, context, expected,
					"Context should contain '%s'", expected)
			}

			for _, notExpected := range tt.notExpectedInContext {
				assert.NotContains(t, context, notExpected,
					"Context should not contain '%s'", notExpected)
			}
		})
	}
}

func TestBuildSmartRepoContext_NoIndex(t *testing.T) {
	// Create a test repository without index
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte("package test"), 0644)
	require.NoError(t, err)

	// Should return error when index is missing
	context, err := BuildSmartRepoContext(tmpDir, "Fix test bug", 10, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file index not found")
	assert.Empty(t, context)
}

func TestBuildSmartRepoContext_FallbackOnError(t *testing.T) {
	// Create a test repository with invalid index
	tmpDir := t.TempDir()

	// Create invalid index
	indexDir := filepath.Join(tmpDir, ".ai-intern")
	err := os.MkdirAll(indexDir, 0755)
	require.NoError(t, err)
	indexFile := filepath.Join(indexDir, "file_index.json")
	err = os.WriteFile(indexFile, []byte("invalid json"), 0644)
	require.NoError(t, err)

	// Should return error for invalid index
	context, err := BuildSmartRepoContext(tmpDir, "Fix bug", 10, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load index")
	assert.Empty(t, context)
}

func TestMin(t *testing.T) {
	assert.Equal(t, 1, min(1, 2))
	assert.Equal(t, 1, min(2, 1))
	assert.Equal(t, 5, min(5, 5))
	assert.Equal(t, -1, min(-1, 0))
}
