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
}

// ParseNeedFiles checks whether raw is a {"need_files":["path", ...]}
// retrieval request rather than a changes array. The model emits this when
// it needs to edit a file that was shown signatures-only (see
// BuildPlanChangesPrompt). Returns nil if raw is not such a request.
func ParseNeedFiles(raw string) []string {
	var req struct {
		NeedFiles []string `json:"need_files"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return nil
	}
	return req.NeedFiles
}
