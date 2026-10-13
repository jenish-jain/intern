package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	logger "github.com/jenish-jain/logger"
)

type State struct {
	Processed map[string]bool `json:"processed"`
	// Failures counts failed processing attempts per ticket key. The poll
	// loop stops retrying a ticket once this reaches MAX_TICKET_ATTEMPTS,
	// so a ticket that can never succeed doesn't burn AI spend every cycle.
	Failures map[string]int `json:"failures,omitempty"`
	// FailureFingerprints records the ticket text fingerprint the failures
	// were counted against. Editing the ticket changes the fingerprint and
	// resets the count, so clarifying a ticket is how a human asks for a retry.
	FailureFingerprints map[string]string `json:"failure_fingerprints,omitempty"`
	// Flows tracks tickets whose PR is open and moving through review,
	// test and merge.
	Flows map[string]*Flow `json:"flows,omitempty"`
	// BacklogKeys maps backlog ticket IDs to the ticket keys created for
	// them, so re-importing a revised backlog never duplicates tickets.
	BacklogKeys map[string]string `json:"backlog_keys,omitempty"`
	// Conversations are architect threads with the product owner.
	Conversations map[string]*Conversation `json:"conversations,omitempty"`
	// SpendUSD is the total model spend for the project across all roles.
	SpendUSD float64 `json:"spend_usd,omitempty"`
	// ProjectConversation is the architect thread that approved the plan.
	ProjectConversation string `json:"project_conversation,omitempty"`
	// CompletedEpics are epic IDs already announced as complete.
	CompletedEpics map[string]bool `json:"completed_epics,omitempty"`
	mu             sync.Mutex      `json:"-"`
	filePath       string          `json:"-"`
}

func NewState(filePath string) *State {
	return &State{
		Processed:           make(map[string]bool),
		Failures:            make(map[string]int),
		FailureFingerprints: make(map[string]string),
		filePath:            filePath,
	}
}

// Attempts returns how many times processing has failed for key against
// the given ticket fingerprint. A different fingerprint means the ticket was
// edited since those failures, so the count is treated as zero.
func (s *State) Attempts(key, fingerprint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailureFingerprints[key] != fingerprint {
		return 0
	}
	return s.Failures[key]
}

// RecordFailure increments and persists the failure count for key,
// restarting from zero if the ticket fingerprint changed, and returns the
// new count.
func (s *State) RecordFailure(key, fingerprint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Failures == nil {
		s.Failures = make(map[string]int)
	}
	if s.FailureFingerprints == nil {
		s.FailureFingerprints = make(map[string]string)
	}
	if s.FailureFingerprints[key] != fingerprint {
		s.Failures[key] = 0
		s.FailureFingerprints[key] = fingerprint
	}
	s.Failures[key]++
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "ticket", key)
	}
	return s.Failures[key]
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
		Processed           map[string]bool          `json:"processed"`
		Failures            map[string]int           `json:"failures"`
		FailureFingerprints map[string]string        `json:"failure_fingerprints"`
		Flows               map[string]*Flow         `json:"flows"`
		BacklogKeys         map[string]string        `json:"backlog_keys"`
		Conversations       map[string]*Conversation `json:"conversations"`
		SpendUSD            float64                  `json:"spend_usd"`
		ProjectConversation string                   `json:"project_conversation"`
		CompletedEpics      map[string]bool          `json:"completed_epics"`
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
	if temp.Failures != nil {
		s.Failures = temp.Failures
	} else {
		s.Failures = make(map[string]int)
	}
	s.Flows = temp.Flows
	s.BacklogKeys = temp.BacklogKeys
	s.Conversations = temp.Conversations
	s.SpendUSD = temp.SpendUSD
	s.ProjectConversation = temp.ProjectConversation
	s.CompletedEpics = temp.CompletedEpics
	if temp.FailureFingerprints != nil {
		s.FailureFingerprints = temp.FailureFingerprints
	} else {
		s.FailureFingerprints = make(map[string]string)
	}

	return nil
}

// SaveFlow stores a copy of f and persists the state.
func (s *State) SaveFlow(f Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Flows == nil {
		s.Flows = make(map[string]*Flow)
	}
	cp := f
	s.Flows[f.TicketKey] = &cp
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "ticket", f.TicketKey)
	}
}

// HasFlow reports whether a flow is tracking key's PR.
func (s *State) HasFlow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.Flows[key]
	return ok
}

// DeleteFlow removes the flow for key and persists the state.
func (s *State) DeleteFlow(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Flows[key]; !ok {
		return
	}
	delete(s.Flows, key)
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "ticket", key)
	}
}

// ListFlows returns copies of all flows, oldest first.
func (s *State) ListFlows() []Flow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Flow, 0, len(s.Flows))
	for _, f := range s.Flows {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// BacklogKeyMap returns a copy of the backlog ID to ticket key mapping.
func (s *State) BacklogKeyMap() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.BacklogKeys))
	for k, v := range s.BacklogKeys {
		out[k] = v
	}
	return out
}

// SetBacklogKey records the ticket key created for a backlog ID.
func (s *State) SetBacklogKey(id, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.BacklogKeys == nil {
		s.BacklogKeys = make(map[string]string)
	}
	s.BacklogKeys[id] = key
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "backlog_id", id)
	}
}

// GetConversation returns a copy of the conversation with id.
func (s *State) GetConversation(id string) (Conversation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.Conversations[id]
	if !ok {
		return Conversation{}, false
	}
	return *c, true
}

// SaveConversation stores a copy of conv and persists the state.
func (s *State) SaveConversation(conv Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Conversations == nil {
		s.Conversations = make(map[string]*Conversation)
	}
	cp := conv
	s.Conversations[conv.ID] = &cp
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err, "conversation", conv.ID)
	}
}

// AddSpend adds usd to the project total, persists it, and returns the new total.
func (s *State) AddSpend(usd float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SpendUSD += usd
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err)
	}
	return s.SpendUSD
}

// Spend returns the project's total model spend.
func (s *State) Spend() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SpendUSD
}

// SetProjectConversation records the planning thread for the project.
func (s *State) SetProjectConversation(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ProjectConversation = id
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err)
	}
}

// ProjectConversationID returns the planning thread for the project.
func (s *State) ProjectConversationID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ProjectConversation
}

// EpicCompleted reports whether an epic was already announced.
func (s *State) EpicCompleted(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CompletedEpics[id]
}

// MarkEpicComplete records an epic as announced. It returns false if it
// already was.
func (s *State) MarkEpicComplete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CompletedEpics[id] {
		return false
	}
	if s.CompletedEpics == nil {
		s.CompletedEpics = make(map[string]bool)
	}
	s.CompletedEpics[id] = true
	if err := s.saveUnlocked(); err != nil {
		logger.Error("Failed to save state file", "error", err)
	}
	return true
}
