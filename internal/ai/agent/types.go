package agent

import "encoding/json"

type CodeChangeOperation string

const (
	OperationCreate CodeChangeOperation = "create"
	OperationEdit   CodeChangeOperation = "edit" // NEW: replaces "update"
	OperationDelete CodeChangeOperation = "delete"
)

// EditHunk is a single search/replace operation within a file.
// Old must match the file content exactly (including whitespace) and
// must appear exactly once. New replaces it.
type EditHunk struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type CodeChange struct {
	Path      string              `json:"path"`
	Operation CodeChangeOperation `json:"operation"`
	Content   string              `json:"content,omitempty"` // create only
	Edits     []EditHunk          `json:"edits,omitempty"`   // edit only
	// Note is an optional, AI-authored explanation of a judgment call made
	// on this change (e.g. renaming a resource to avoid a naming collision).
	// Surfaced in the PR description so a human can confirm or override it.
	Note string `json:"note,omitempty"`
}

// ParseNeedFiles checks whether raw is a retrieval request rather than a
// changes array: either the instructed {"need_files":["path", ...]} form,
// or a bare ["path", ...] array of strings, which models sometimes emit
// instead despite the prompt. The model emits this when it needs to edit a
// file that wasn't shown with full content (see BuildPlanChangesPrompt).
// Returns nil if raw is not such a request.
func ParseNeedFiles(raw string) []string {
	var req struct {
		NeedFiles []string `json:"need_files"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err == nil && len(req.NeedFiles) > 0 {
		return req.NeedFiles
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err == nil && len(paths) > 0 {
		return paths
	}
	return nil
}
