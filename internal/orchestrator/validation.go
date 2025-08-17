package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"

	"intern/internal/ai"
)

func validatePlannedChanges(root string, changes []ai.CodeChange, allowedDirs []string, maxFiles int) ([]ai.CodeChange, error) {
	if len(changes) > maxFiles {
		changes = changes[:maxFiles]
	}
	var out []ai.CodeChange
	for _, ch := range changes {
		p := strings.TrimSpace(ch.Path)
		if p == "" {
			continue
		}
		// No absolute paths
		if filepath.IsAbs(p) {
			continue
		}
		// Normalize and guard traversal
		clean := filepath.Clean(p)
		if strings.HasPrefix(clean, "..") {
			continue
		}
		// Enforce allowlist
		first := firstSegment(clean)
		if !inList(first, allowedDirs) {
			continue
		}
		// Ensure content is present (content or content_b64 decoded earlier)
		if strings.TrimSpace(ch.Content) == "" {
			continue
		}
		out = append(out, ai.CodeChange{Path: clean, Operation: ch.Operation, Content: ch.Content})
	}
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
