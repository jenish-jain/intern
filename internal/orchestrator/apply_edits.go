package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"intern/internal/ai/agent"
)

// applyEditChange applies search/replace hunks to an existing file.
// Returns a descriptive error suitable for feeding back to the AI on failure.
func applyEditChange(repoRoot string, ch agent.CodeChange) error {
	abs := filepath.Join(repoRoot, ch.Path)
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("edit target %s: %w (did you mean operation=create?)", ch.Path, err)
	}
	content := string(data)

	for i, hunk := range ch.Edits {
		if strings.TrimSpace(hunk.Old) == "" {
			return fmt.Errorf("%s hunk %d: empty old block", ch.Path, i+1)
		}
		switch n := strings.Count(content, hunk.Old); n {
		case 1:
			content = strings.Replace(content, hunk.Old, hunk.New, 1)
		case 0:
			// Fallback: match ignoring leading/trailing whitespace per line
			replaced, ok := replaceNormalized(content, hunk.Old, hunk.New)
			if !ok {
				return fmt.Errorf(
					"%s hunk %d: old block not found in file. The file may have changed; "+
						"re-read the current content. Old block was:\n%s",
					ch.Path, i+1, truncateForErr(hunk.Old, 400))
			}
			content = replaced
		default:
			return fmt.Errorf(
				"%s hunk %d: old block matches %d locations; include more surrounding lines to disambiguate",
				ch.Path, i+1, n)
		}
	}

	return os.WriteFile(abs, []byte(content), 0644)
}

// replaceNormalized retries the match with per-line whitespace trimmed,
// then maps back to the original byte range. Returns ok=false if zero or
// multiple normalized matches exist.
func replaceNormalized(content, old, new string) (string, bool) {
	cLines := strings.Split(content, "\n")
	oLines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	if len(oLines) == 0 || len(oLines) > len(cLines) {
		return content, false
	}

	matchStart := -1
	for i := 0; i+len(oLines) <= len(cLines); i++ {
		ok := true
		for j := range oLines {
			if strings.TrimSpace(cLines[i+j]) != strings.TrimSpace(oLines[j]) {
				ok = false
				break
			}
		}
		if ok {
			if matchStart != -1 {
				return content, false // ambiguous
			}
			matchStart = i
		}
	}
	if matchStart == -1 {
		return content, false
	}

	out := append([]string{}, cLines[:matchStart]...)
	out = append(out, strings.Split(strings.TrimRight(new, "\n"), "\n")...)
	out = append(out, cLines[matchStart+len(oLines):]...)
	return strings.Join(out, "\n"), true
}

func truncateForErr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...(truncated)"
}
