package ai

import "time"

// FileIndex represents the complete index of a repository
type FileIndex struct {
	Version   string                 `json:"version"`
	IndexedAt time.Time              `json:"indexed_at"`
	RepoRoot  string                 `json:"repo_root"`
	Files     map[string]FileMetadata `json:"files"`
	Modules   map[string][]string    `json:"modules"`
}

// FileMetadata contains metadata about a single file
type FileMetadata struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	Importance   float64   `json:"importance"`   // 0-10 scale, higher = more important
	Category     string    `json:"category"`     // "core", "util", "test", "config", "doc"
	Dependencies []string  `json:"dependencies"` // List of imported packages/modules
	LastModified time.Time `json:"last_modified"`
	Summary      string    `json:"summary"` // Brief description of file purpose
}

// ContextStrategy defines how context should be loaded
type ContextStrategy string

const (
	ContextStrategyFull  ContextStrategy = "full"  // Load all files (current behavior)
	ContextStrategySmart ContextStrategy = "smart" // Smart selection with scoring
)

// ContextTier represents different levels of context detail
type ContextTier int

const (
	ContextTierBaseline ContextTier = 0 // PROJECT_INDEX.md + CLAUDE.md (~5-10KB)
	ContextTierSmart    ContextTier = 1 // Smart subset based on ticket (~50-100KB)
	ContextTierFull     ContextTier = 2 // Full context fallback (~500KB-1MB)
)

// ContextBuildOptions configures how context should be built
type ContextBuildOptions struct {
	Strategy          ContextStrategy
	Tier              ContextTier
	MaxFiles          int
	MaxBytesPerFile   int
	IncludeRecent     int  // Number of recent commits to prioritize
	UseCache          bool
	TicketDescription string // Used for smart context selection
}

// FileScore represents a file's relevance score for a given ticket
type FileScore struct {
	Path  string
	Score float64
}
