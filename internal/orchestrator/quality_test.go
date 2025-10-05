package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10", 10, "exactly10"},
		{"this is a long string that needs truncation", 20, "this is a \n...\ntruncation"},
		{"", 5, ""},
		{"a", 1, "a"},
		{"ab", 5, "ab"},
		{"abc", 5, "abc"},
	}

	for _, tt := range tests {
		result := truncateMiddle(tt.input, tt.max)
		assert.Equal(t, tt.expected, result, "truncateMiddle(%q, %d)", tt.input, tt.max)
		// Note: truncateMiddle may exceed max due to "\n...\n" separator (5 chars)
		if len(tt.input) > tt.max {
			assert.Contains(t, result, "\n...\n", "should contain separator for truncated strings")
		}
	}
}

func TestRunQualityGates_AllSkipped(t *testing.T) {
	cfg := &config.Config{
		RunVetBeforePR:   false,
		RunTestsBeforePR: false,
	}

	notes, ok := runQualityGates(context.Background(), cfg, "/fake/path")
	assert.True(t, ok)
	assert.Contains(t, notes, "go vet: skipped")
	assert.Contains(t, notes, "go test: skipped")
}

func TestRunQualityGates_WithValidGoProject(t *testing.T) {
	// Create a temporary Go project
	tmpDir, err := os.MkdirTemp("", "quality-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	require.NoError(t, err)

	// Create a simple Go file
	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644)
	require.NoError(t, err)

	// Create a simple test file
	testGo := `package main

import "testing"

func TestMain(t *testing.T) {
	// This test always passes
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte(testGo), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		RunVetBeforePR:   true,
		RunTestsBeforePR: true,
	}

	notes, ok := runQualityGates(context.Background(), cfg, tmpDir)
	assert.True(t, ok, "quality gates should pass for valid project")

	// Should have run both vet and test
	assert.Contains(t, notes, "go vet: PASSED")
	// Test output may contain summary, so check for PASSED prefix
	found := false
	for _, note := range notes {
		if strings.HasPrefix(note, "go test: PASSED") {
			found = true
			break
		}
	}
	assert.True(t, found, "should contain go test: PASSED, got: %v", notes)
}

func TestRunQualityGates_WithVetError(t *testing.T) {
	// Create a temporary Go project with vet issues
	tmpDir, err := os.MkdirTemp("", "quality-vet-error")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	require.NoError(t, err)

	// Create Go file with vet issues (unreachable code)
	badGo := `package main

import "fmt"

func main() {
	return
	fmt.Println("This is unreachable")
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(badGo), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		RunVetBeforePR:   true,
		RunTestsBeforePR: false,
	}

	notes, ok := runQualityGates(context.Background(), cfg, tmpDir)
	assert.False(t, ok, "quality gates should fail when vet fails")
	assert.Contains(t, notes, "go vet: FAILED")
	assert.Contains(t, notes, "go test: skipped")
}

func TestRunQualityGates_WithTestFailure(t *testing.T) {
	// Create a temporary Go project with failing tests
	tmpDir, err := os.MkdirTemp("", "quality-test-fail")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	require.NoError(t, err)

	// Create a simple Go file
	mainGo := `package main

func Add(a, b int) int {
	return a + b
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644)
	require.NoError(t, err)

	// Create a failing test
	testGo := `package main

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 2)
	if result != 5 { // This will fail since 2+2 != 5
		t.Errorf("Expected 5, got %d", result)
	}
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte(testGo), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		RunVetBeforePR:   false,
		RunTestsBeforePR: true,
	}

	notes, ok := runQualityGates(context.Background(), cfg, tmpDir)
	assert.False(t, ok, "quality gates should fail when tests fail")
	assert.Contains(t, notes, "go vet: skipped")
	assert.Contains(t, notes, "go test: FAILED")
}

func TestRunQualityGates_NonexistentDirectory(t *testing.T) {
	cfg := &config.Config{
		RunVetBeforePR:   true,
		RunTestsBeforePR: true,
	}

	notes, ok := runQualityGates(context.Background(), cfg, "/nonexistent/path")
	assert.False(t, ok, "quality gates should fail for nonexistent directory")

	// Both should fail
	assert.Contains(t, notes, "go vet: FAILED")
	assert.Contains(t, notes, "go test: FAILED")
}
