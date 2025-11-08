package orchestrator

import (
	"fmt"
	"intern/internal/ai/agent"
	"path/filepath"
	"strings"

	logger "github.com/jenish-jain/logger"
)

func validatePlannedChanges(root string, changes []agent.CodeChange, allowedDirs []string, maxFiles int) ([]agent.CodeChange, error) {
	logger.Debug("Validating planned changes", "total_changes", len(changes), "allowed_dirs", allowedDirs)

	if len(changes) > maxFiles {
		logger.Debug("Truncating changes due to max files limit", "original", len(changes), "max", maxFiles)
		changes = changes[:maxFiles]
	}
	var out []agent.CodeChange
	for _, ch := range changes {
		p := strings.TrimSpace(ch.Path)
		if p == "" {
			logger.Debug("Skipping empty path")
			continue
		}
		// No absolute paths
		if filepath.IsAbs(p) {
			logger.Debug("Skipping absolute path", "path", p)
			continue
		}
		// Normalize and guard traversal
		clean := filepath.Clean(p)
		if strings.HasPrefix(clean, "..") {
			logger.Debug("Skipping path with traversal", "path", clean)
			continue
		}
		// Enforce allowlist
		first := firstSegment(clean)
		// Allow root-level files if "." is in allowedDirs
		if !inList(first, allowedDirs) && !(first == clean && inList(".", allowedDirs)) {
			logger.Debug("Skipping path not in allowed directories", "path", clean, "first_segment", first, "allowed_dirs", allowedDirs)
			continue
		}
		// Ensure content is present (content or content_b64 decoded earlier)
		if strings.TrimSpace(ch.Content) == "" {
			logger.Debug("Skipping file with empty content", "path", clean)
			continue
		}
		logger.Debug("Accepting change", "path", clean, "operation", ch.Operation)
		out = append(out, agent.CodeChange{Path: clean, Operation: ch.Operation, Content: ch.Content})
	}
	logger.Debug("Validation complete", "accepted_changes", len(out), "rejected_changes", len(changes)-len(out))
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid changes after validation")
	}
	return out, nil
}

func firstSegment(p string) string {
	i := strings.IndexByte(p, filepath.Separator)
	if i == -1 {
		return p
	}
	return p[:i]
}

func inList(s string, list []string) bool {
	for _, x := range list {
		if s == x {
			return true
		}
	}
	return false
}
