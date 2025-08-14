package orchestrator

import (
	"encoding/json"
	"os"
	"sync"
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
	s.save()
}

func (s *State) save() {
	f, err := os.Create(s.filePath)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(s)
}

func (s *State) Load() error {
	f, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(s)
}
