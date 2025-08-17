package ai

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BuildRepoContext reads a subset of files (small text/code files) to provide
// a lightweight context string for the LLM. It skips binaries, vendor, node_modules, and large files.
func BuildRepoContext(repoRoot string, maxFiles int, maxBytesPerFile int) string {
	var b strings.Builder
	count := 0
	stop := errors.New("stop-walk")
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rErr := filepath.Rel(repoRoot, path)
		if rErr != nil {
			return nil
		}
		lower := strings.ToLower(rel)
		// Skip common large/noise directories early
		if d.IsDir() {
			if lower == ".git" || strings.HasPrefix(lower, ".git/") ||
				lower == "vendor" || strings.HasPrefix(lower, "vendor/") ||
				lower == "node_modules" || strings.HasPrefix(lower, "node_modules/") ||
				lower == ".idea" || lower == ".vscode" ||
				lower == "build" || lower == "dist" || lower == "out" {
				return fs.SkipDir
			}
			return nil
		}
		if count >= maxFiles {
			return stop
		}
		// Skip obvious binaries
		if hasAnySuffix(lower, ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".exe", ".bin", ".mp4", ".mov", ".dll") {
			return nil
		}
		// Read up to maxBytesPerFile
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if len(data) > maxBytesPerFile {
			data = data[:maxBytesPerFile]
		}
		b.WriteString("\n\n# FILE: ")
		b.WriteString(rel)
		b.WriteString("\n")
		b.Write(data)
		count++
		return nil
	})
	return b.String()
}

func hasAnySuffix(s string, suff ...string) bool {
	for _, x := range suff {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}
