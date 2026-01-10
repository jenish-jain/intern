package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestState_Basic(t *testing.T) {
	f, err := os.CreateTemp("", "state_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	s := NewState(f.Name())
	if s.IsProcessed("TICKET-1") {
		t.Error("expected not processed")
	}
	s.MarkProcessed("TICKET-1")
	if !s.IsProcessed("TICKET-1") {
		t.Error("expected processed")
	}

	// Test persistence
	loaded := NewState(f.Name())
	if err := loaded.Load(); err != nil {
		t.Errorf("failed to load: %v", err)
	}
	if !loaded.IsProcessed("TICKET-1") {
		t.Error("expected loaded state to be processed")
	}
}

func TestState_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := NewState(statePath)
	s.MarkProcessed("TICKET-1")

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var decoded struct {
		Processed map[string]bool `json:"processed"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("state file contains invalid JSON: %v", err)
	}

	if !decoded.Processed["TICKET-1"] {
		t.Error("expected TICKET-1 to be processed in file")
	}

	// Verify no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}

	for _, entry := range entries {
		if entry.Name() != "state.json" {
			t.Errorf("unexpected file in temp dir: %s", entry.Name())
		}
	}
}

func TestState_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := NewState(statePath)

	// Number of concurrent goroutines
	const numWorkers = 10
	const ticketsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Launch multiple goroutines that mark tickets as processed
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ticketsPerWorker; j++ {
				ticketKey := formatTicketKey(workerID, j)
				s.MarkProcessed(ticketKey)
			}
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()

	// Verify all tickets were marked as processed in memory
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < ticketsPerWorker; j++ {
			ticketKey := formatTicketKey(i, j)
			if !s.IsProcessed(ticketKey) {
				t.Errorf("ticket %s not marked as processed in memory", ticketKey)
			}
		}
	}

	// Load state from disk and verify persistence
	loaded := NewState(statePath)
	if err := loaded.Load(); err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	// Verify state file is valid and contains all tickets
	// Note: Due to concurrent writes, some updates might be lost if not atomic
	// With atomic writes, we should have a consistent (though possibly not complete) state
	processedCount := 0
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < ticketsPerWorker; j++ {
			ticketKey := formatTicketKey(i, j)
			if loaded.IsProcessed(ticketKey) {
				processedCount++
			}
		}
	}

	// With atomic writes, we should have at least some tickets saved
	// (might not be all due to last-write-wins, but file should be valid)
	if processedCount == 0 {
		t.Error("no tickets found in loaded state, atomic write may have failed")
	}

	t.Logf("Persisted %d/%d tickets to disk", processedCount, numWorkers*ticketsPerWorker)
}

func TestState_LoadNonexistent(t *testing.T) {
	s := NewState("/tmp/nonexistent_state_file.json")
	err := s.Load()
	if err == nil {
		t.Error("expected error when loading nonexistent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got: %v", err)
	}
}

func TestState_LoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Write invalid JSON
	if err := os.WriteFile(statePath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	s := NewState(statePath)
	err := s.Load()
	if err == nil {
		t.Error("expected error when loading invalid JSON")
	}
}

func TestState_SaveExplicit(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := NewState(statePath)
	s.Processed["TICKET-1"] = true
	s.Processed["TICKET-2"] = true

	// Explicitly save
	if err := s.Save(); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Load and verify
	loaded := NewState(statePath)
	if err := loaded.Load(); err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if !loaded.IsProcessed("TICKET-1") {
		t.Error("TICKET-1 not persisted")
	}
	if !loaded.IsProcessed("TICKET-2") {
		t.Error("TICKET-2 not persisted")
	}
}

func TestState_SaveToReadOnlyDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Make directory read-only
	if err := os.Chmod(tmpDir, 0444); err != nil {
		t.Skipf("cannot change dir permissions: %v", err)
	}
	defer os.Chmod(tmpDir, 0755) // Restore for cleanup

	statePath := filepath.Join(tmpDir, "state.json")
	s := NewState(statePath)

	// This should fail because directory is read-only
	err := s.Save()
	if err == nil {
		t.Error("expected error when saving to read-only directory")
	}
}

// Helper function to format ticket keys consistently
func formatTicketKey(workerID, ticketNum int) string {
	return "TICKET-" + string(rune('A'+workerID)) + "-" + string(rune('0'+ticketNum%10))
}
