package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"intern/internal/indexer"
)

const journalFile = ".ai-intern/journal.json"
const maxEntries = 200 // ring buffer; oldest pruned

type Entry struct {
	TicketKey    string    `json:"ticket_key"`
	Summary      string    `json:"summary"`
	Branch       string    `json:"branch"`
	PRURL        string    `json:"pr_url,omitempty"`
	Merged       bool      `json:"merged"` // updated lazily, see below
	FilesChanged []string  `json:"files_changed"`
	PublicAPIs   []string  `json:"public_apis,omitempty"` // new exported symbols
	Notes        string    `json:"notes,omitempty"`       // model-written, <=5 lines
	Keywords     []string  `json:"keywords"`              // precomputed at write time
	Timestamp    time.Time `json:"timestamp"`
}

type Journal struct {
	mu      sync.Mutex
	path    string
	Entries []Entry `json:"entries"`
}

func Load(repoRoot string) *Journal {
	j := &Journal{path: filepath.Join(repoRoot, journalFile)}
	if data, err := os.ReadFile(j.path); err == nil {
		_ = json.Unmarshal(data, j) // corrupt journal = empty journal, never fatal
	}
	return j
}

func (j *Journal) Append(e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	e.Keywords = indexer.ExtractKeywords(e.Summary + " " + e.Notes + " " + strings.Join(e.FilesChanged, " "))
	j.Entries = append(j.Entries, e)
	if len(j.Entries) > maxEntries {
		j.Entries = j.Entries[len(j.Entries)-maxEntries:]
	}
	return j.saveLocked()
}

// Reconcile checks unmerged entries against the VCS via checkMerged and flips
// Merged to true for any whose PR has since been merged. checkMerged errors
// are treated as transient (entry left unmerged, retried next cycle).
// Returns the number of entries newly marked merged.
func (j *Journal) Reconcile(ctx context.Context, checkMerged func(ctx context.Context, prURL string) (bool, error)) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	updated := 0
	for i := range j.Entries {
		e := &j.Entries[i]
		if e.Merged || e.PRURL == "" {
			continue
		}
		merged, err := checkMerged(ctx, e.PRURL)
		if err != nil || !merged {
			continue
		}
		e.Merged = true
		updated++
	}

	if updated > 0 {
		if err := j.saveLocked(); err != nil {
			return updated, fmt.Errorf("save journal after reconcile: %w", err)
		}
	}
	return updated, nil
}

// Find returns the most recent entry for a ticket key, if one exists.
// Used by request-driven callers (e.g. the Slack handler) that need the
// PR URL for a ticket after ProcessTicket returns, since the ticketing.Client
// UpdateTicketStatus callback only carries the status string, not the PR URL.
func (j *Journal) Find(ticketKey string) (Entry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.Entries) - 1; i >= 0; i-- {
		if j.Entries[i].TicketKey == ticketKey {
			return j.Entries[i], true
		}
	}
	return Entry{}, false
}

// saveLocked performs an atomic write of the journal file (temp file +
// rename, mirroring State.saveUnlocked). Must be called with j.mu held.
func (j *Journal) saveLocked() error {
	dir := filepath.Dir(j.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create journal dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".journal_tmp_*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(j); err != nil {
		return fmt.Errorf("encode journal: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tmpFile = nil

	if err := os.Rename(tmpPath, j.path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// Relevant returns up to n entries scored against the new ticket's text,
// most recent first among ties. Recency matters: weight newer entries up.
func (j *Journal) Relevant(ticketText string, n int) []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()

	keywords := indexer.ExtractKeywords(ticketText)
	kwSet := map[string]bool{}
	for _, k := range keywords {
		kwSet[k] = true
	}

	type scored struct {
		e Entry
		s float64
	}
	var out []scored
	for i, e := range j.Entries {
		s := 0.0
		for _, k := range e.Keywords {
			if kwSet[k] {
				s += 1.0
			}
		}
		// file-path overlap is the strongest continuity signal
		for _, f := range e.FilesChanged {
			if strings.Contains(ticketText, filepath.Base(f)) || strings.Contains(ticketText, f) {
				s += 5.0
			}
		}
		// recency bonus: linear decay over the buffer
		s += float64(i) / float64(len(j.Entries)) * 2.0
		if s > 1.0 { // noise floor
			out = append(out, scored{e, s})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].s > out[b].s })
	if len(out) > n {
		out = out[:n]
	}
	res := make([]Entry, len(out))
	for i, x := range out {
		res[i] = x.e
	}
	return res
}

// Render formats entries for inclusion in the planning prompt.
func Render(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Recent related work in this repository (your own prior tickets)\n")
	b.WriteString("# Build on this work; do not duplicate or contradict it.\n\n")
	for _, e := range entries {
		b.WriteString("## " + e.TicketKey + ": " + e.Summary + "\n")
		merged := "PR open (unmerged - this code may NOT be on the base branch yet)"
		if e.Merged {
			merged = "merged"
		}
		b.WriteString("- Status: " + merged + " | Branch: " + e.Branch + "\n")
		b.WriteString("- Files: " + strings.Join(e.FilesChanged, ", ") + "\n")
		if len(e.PublicAPIs) > 0 {
			b.WriteString("- New APIs: " + strings.Join(e.PublicAPIs, ", ") + "\n")
		}
		if e.Notes != "" {
			b.WriteString("- Notes: " + e.Notes + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
