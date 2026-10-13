package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRepositoryPath_Success(t *testing.T) {
	paths, err := NewRepositoryPath("./workspace", "myrepo")
	if err != nil {
		t.Fatalf("NewRepositoryPath failed: %v", err)
	}

	if want := absWorkspace(t); paths.WorkingDir() != want { // relative paths resolve against the cwd
		t.Errorf("WorkingDir = %s, want %s", paths.WorkingDir(), want)
	}
	if paths.RepoName() != "myrepo" {
		t.Errorf("RepoName = %s, want myrepo", paths.RepoName())
	}
}

func TestNewRepositoryPath_EmptyWorkingDir(t *testing.T) {
	_, err := NewRepositoryPath("", "myrepo")
	if err == nil {
		t.Error("Expected error for empty working directory")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("Error should mention working directory, got: %v", err)
	}
}

func TestNewRepositoryPath_EmptyRepoName(t *testing.T) {
	_, err := NewRepositoryPath("./workspace", "")
	if err == nil {
		t.Error("Expected error for empty repository name")
	}
	if !strings.Contains(err.Error(), "repository name") {
		t.Errorf("Error should mention repository name, got: %v", err)
	}
}

func TestNewRepositoryPath_PathTraversal(t *testing.T) {
	testCases := []struct {
		name     string
		repoName string
	}{
		{"parent directory", "../etc"},
		{"nested traversal", "foo/../../../etc"},
		{"hidden traversal", "foo/../../bar"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRepositoryPath("./workspace", tc.repoName)
			if err == nil {
				t.Errorf("Expected error for path traversal: %s", tc.repoName)
			}
			if !strings.Contains(err.Error(), "traversal") {
				t.Errorf("Error should mention path traversal, got: %v", err)
			}
		})
	}
}

func TestRepositoryPath_Root(t *testing.T) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")
	root := paths.Root()

	expected := filepath.Join(absWorkspace(t), "myrepo")
	if root != expected {
		t.Errorf("Root() = %s, want %s", root, expected)
	}
}

func TestRepositoryPath_GitDir(t *testing.T) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")
	gitDir := paths.GitDir()

	expected := filepath.Join(absWorkspace(t), "myrepo", ".git")
	if gitDir != expected {
		t.Errorf("GitDir() = %s, want %s", gitDir, expected)
	}
}

func TestRepositoryPath_File(t *testing.T) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")

	testCases := []struct {
		input    string
		expected string
	}{
		{"config.go", filepath.Join(absWorkspace(t), "myrepo", "config.go")},
		{"internal/config/config.go", filepath.Join(absWorkspace(t), "myrepo", "internal", "config", "config.go")},
		{"./README.md", filepath.Join(absWorkspace(t), "myrepo", "README.md")},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := paths.File(tc.input)
			if result != tc.expected {
				t.Errorf("File(%s) = %s, want %s", tc.input, result, tc.expected)
			}
		})
	}
}

func TestRepositoryPath_Subdir(t *testing.T) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")

	testCases := []struct {
		input    string
		expected string
	}{
		{"internal", filepath.Join(absWorkspace(t), "myrepo", "internal")},
		{"internal/config", filepath.Join(absWorkspace(t), "myrepo", "internal", "config")},
		{"./docs", filepath.Join(absWorkspace(t), "myrepo", "docs")},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := paths.Subdir(tc.input)
			if result != tc.expected {
				t.Errorf("Subdir(%s) = %s, want %s", tc.input, result, tc.expected)
			}
		})
	}
}

func TestRepositoryPath_PathCleaning(t *testing.T) {
	// Test that paths are normalized (redundant separators removed, etc.)
	paths, _ := NewRepositoryPath("./workspace//", "myrepo")

	root := paths.Root()
	expected := filepath.Join(absWorkspace(t), "myrepo")
	if root != expected {
		t.Errorf("Root() with redundant separators = %s, want %s", root, expected)
	}
}

func TestRepositoryPath_AbsolutePaths(t *testing.T) {
	// Test with absolute path (should work)
	paths, err := NewRepositoryPath("/tmp/workspace", "myrepo")
	if err != nil {
		t.Fatalf("NewRepositoryPath with absolute path failed: %v", err)
	}

	root := paths.Root()
	expected := filepath.Join("/tmp/workspace", "myrepo")
	if root != expected {
		t.Errorf("Root() with absolute working dir = %s, want %s", root, expected)
	}
}

func TestRepositoryPath_NestedRepoNames(t *testing.T) {
	// Test repository names with subdirectories (valid use case)
	// Example: GitHub Enterprise might have paths like "org/repo"
	paths, _ := NewRepositoryPath("./workspace", "myorg/myrepo")

	root := paths.Root()
	expected := filepath.Join(absWorkspace(t), "myorg", "myrepo")
	if root != expected {
		t.Errorf("Root() with nested repo name = %s, want %s", root, expected)
	}
}

// Benchmark path construction
func BenchmarkRepositoryPath_Root(b *testing.B) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")
	for i := 0; i < b.N; i++ {
		_ = paths.Root()
	}
}

func BenchmarkRepositoryPath_File(b *testing.B) {
	paths, _ := NewRepositoryPath("./workspace", "myrepo")
	for i := 0; i < b.N; i++ {
		_ = paths.File("internal/config/config.go")
	}
}

// absWorkspace is ./workspace resolved against the test's working directory.
func absWorkspace(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "workspace")
}
