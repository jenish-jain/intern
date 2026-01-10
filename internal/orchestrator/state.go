package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	logger "github.com/jenish-jain/logger"
)

type State struct {
	Processed map[string]bool `json:"processed"`
	mu        sync.Mutex      `json:"-"`
	filePath  string          `json:"-"`
}

func NewState(filePath string) *State {
	return &State{
		Processed: make(map[string]bool),
		filePath:  filePath,
	}
}

func (s *State) IsProcessed(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Processed[key]
}

func (s *State) MarkProcessed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Processed[key] = true

	// Attempt to save, log error if it fails
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "ticket", key)
		// Don't panic - we'll retry on next save
		// The in-memory state is still updated
	}
}

// saveUnlocked performs atomic write of state file.
// Must be called with s.mu held.
// Uses temp file + rename pattern to ensure atomicity.
func (s *State) saveUnlocked() error {
	// Create temp file in same directory as target file
	// This ensures rename is atomic (same filesystem)
	dir := filepath.Dir(s.filePath)
	tmpFile, err := os.CreateTemp(dir, ".state_tmp_*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Cleanup temp file on error
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Encode state to temp file
	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ") // Pretty print for readability
	if err := encoder.Encode(s); err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	// Ensure data is written to disk before rename
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	// Close temp file before rename
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tmpFile = nil // Prevent defer from closing again

	// Atomic rename - this is the critical operation
	// On POSIX systems, rename is atomic if source and dest are on same filesystem
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// Save persists the current state to disk atomically.
// This is a public method that can be called explicitly if needed.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked()
}

func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Decode into a temporary structure first
	var temp struct {
		Processed map[string]bool `json:"processed"`
	}

	if err := json.NewDecoder(f).Decode(&temp); err != nil {
		return fmt.Errorf("decode state file: %w", err)
	}

	// Only update our map if decode succeeded
	if temp.Processed != nil {
		s.Processed = temp.Processed
	} else {
		// Initialize empty map if null in JSON
		s.Processed = make(map[string]bool)
	}

	return nil
}
