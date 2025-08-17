package orchestrator

import (
	"os"
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
