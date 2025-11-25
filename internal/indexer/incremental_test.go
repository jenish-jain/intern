package indexer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	logger "github.com/jenish-jain/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init("error")
}

func TestUpdateIndex_NoChanges(t *testing.T) {
	// Create a temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("git not available, skipping test")
	}

	// Configure git for test
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Commit the file
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Build initial index
	idx := New(tmpDir)
	initialIndex, err := idx.BuildIndex()
	if err != nil {
		t.Fatalf("Failed to build initial index: %v", err)
	}

	if err := idx.SaveIndex(initialIndex); err != nil {
		t.Fatalf("Failed to save initial index: %v", err)
	}

	// Update index (should detect no changes)
	updatedIndex, err := idx.UpdateIndex()
	if err != nil {
		t.Fatalf("Failed to update index: %v", err)
	}

	// Verify commits are the same
	if updatedIndex.GitCommitHash != initialIndex.GitCommitHash {
		t.Errorf("Expected same commit hash, got %s vs %s",
			updatedIndex.GitCommitHash, initialIndex.GitCommitHash)
	}

	// Verify file count is the same
	if len(updatedIndex.Files) != len(initialIndex.Files) {
		t.Errorf("Expected %d files, got %d", len(initialIndex.Files), len(updatedIndex.Files))
	}
}

func TestUpdateIndex_FileAdded(t *testing.T) {
	// Create a temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("git not available, skipping test")
	}

	// Configure git for test
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create initial test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Commit the file
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	cmd.Run()

	// Build initial index
	idx := New(tmpDir)
	initialIndex, err := idx.BuildIndex()
	if err != nil {
		t.Fatalf("Failed to build initial index: %v", err)
	}
	if err := idx.SaveIndex(initialIndex); err != nil {
		t.Fatalf("Failed to save initial index: %v", err)
	}

	// Add a new file
	newFile := filepath.Join(tmpDir, "new.go")
	newContent := []byte("package main\n\nfunc foo() {}\n")
	if err := os.WriteFile(newFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	// Commit the new file
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Add new file")
	cmd.Dir = tmpDir
	cmd.Run()

	// Sleep briefly to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Update index
	updatedIndex, err := idx.UpdateIndex()
	if err != nil {
		t.Fatalf("Failed to update index: %v", err)
	}

	// Verify new file is in index
	if _, ok := updatedIndex.Files["new.go"]; !ok {
		t.Errorf("Expected new.go to be in updated index")
	}

	// Verify file count increased
	if len(updatedIndex.Files) != len(initialIndex.Files)+1 {
		t.Errorf("Expected %d files, got %d", len(initialIndex.Files)+1, len(updatedIndex.Files))
	}

	// Verify commit hash changed
	if updatedIndex.GitCommitHash == initialIndex.GitCommitHash {
		t.Errorf("Expected different commit hash after update")
	}
}

func TestUpdateIndex_FileDeleted(t *testing.T) {
	// Create a temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("git not available, skipping test")
	}

	// Configure git for test
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create test files
	testFile1 := filepath.Join(tmpDir, "test1.go")
	testFile2 := filepath.Join(tmpDir, "test2.go")
	content := []byte("package main\n\nfunc main() {}\n")
	os.WriteFile(testFile1, content, 0644)
	os.WriteFile(testFile2, content, 0644)

	// Commit the files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	cmd.Run()

	// Build initial index
	idx := New(tmpDir)
	initialIndex, err := idx.BuildIndex()
	if err != nil {
		t.Fatalf("Failed to build initial index: %v", err)
	}
	if err := idx.SaveIndex(initialIndex); err != nil {
		t.Fatalf("Failed to save initial index: %v", err)
	}

	// Delete one file
	cmd = exec.Command("git", "rm", "test2.go")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Delete test2.go")
	cmd.Dir = tmpDir
	cmd.Run()

	// Update index
	updatedIndex, err := idx.UpdateIndex()
	if err != nil {
		t.Fatalf("Failed to update index: %v", err)
	}

	// Verify deleted file is not in index
	if _, ok := updatedIndex.Files["test2.go"]; ok {
		t.Errorf("Expected test2.go to be removed from index")
	}

	// Verify file count decreased
	if len(updatedIndex.Files) != len(initialIndex.Files)-1 {
		t.Errorf("Expected %d files, got %d", len(initialIndex.Files)-1, len(updatedIndex.Files))
	}
}

func TestUpdateIndex_FileModified(t *testing.T) {
	// Create a temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("git not available, skipping test")
	}

	// Configure git for test
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package main\n\nfunc main() {}\n")
	os.WriteFile(testFile, content, 0644)

	// Commit the file
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	cmd.Run()

	// Build initial index
	idx := New(tmpDir)
	initialIndex, err := idx.BuildIndex()
	if err != nil {
		t.Fatalf("Failed to build initial index: %v", err)
	}
	if err := idx.SaveIndex(initialIndex); err != nil {
		t.Fatalf("Failed to save initial index: %v", err)
	}

	// Sleep to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Modify the file
	newContent := []byte("package main\n\nfunc main() {}\nfunc foo() {}\n")
	os.WriteFile(testFile, newContent, 0644)

	// Commit the modification
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "Modify test.go")
	cmd.Dir = tmpDir
	cmd.Run()

	// Update index
	updatedIndex, err := idx.UpdateIndex()
	if err != nil {
		t.Fatalf("Failed to update index: %v", err)
	}

	// Verify file is still in index
	updatedMeta, ok := updatedIndex.Files["test.go"]
	if !ok {
		t.Fatalf("Expected test.go to be in updated index")
	}

	// Verify size changed
	initialMeta := initialIndex.Files["test.go"]
	if updatedMeta.Size == initialMeta.Size {
		t.Errorf("Expected file size to change after modification")
	}

	// Verify last modified changed
	if !updatedMeta.LastModified.After(initialMeta.LastModified) {
		t.Errorf("Expected last modified time to be later")
	}
}

func TestRebuildIfStale_NoIndex(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("git not available, skipping test")
	}

	// Create test file
	testFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(testFile, []byte("package main\n"), 0644)

	idx := New(tmpDir)

	// RebuildIfStale should build new index
	index, wasUpdated, err := idx.RebuildIfStale()
	if err != nil {
		t.Fatalf("Failed to rebuild: %v", err)
	}

	if !wasUpdated {
		t.Errorf("Expected index to be rebuilt")
	}

	if index == nil {
		t.Errorf("Expected index to be returned")
	}
}
