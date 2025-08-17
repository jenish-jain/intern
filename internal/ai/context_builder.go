package ai

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BuildRepoContext reads a subset of files (small text/code files) to provide
// a lightweight context string for the LLM. It skips binaries and large files.
func BuildRepoContext(repoRoot string, maxFiles int, maxBytesPerFile int) string {
	var b strings.Builder
	count := 0
	filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if count >= maxFiles {
			return filepath.SkipDir
		}
		// Skip obvious binaries and vendored modules
		lower := strings.ToLower(path)
		if strings.HasPrefix(lower, ".git/") || strings.Contains(lower, "/.git/") {
			return nil
		}
		if strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".gif") || strings.HasSuffix(lower, ".pdf") || strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".bin") {
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
		rel, _ := filepath.Rel(repoRoot, path)
		b.WriteString("\n\n# FILE: ")
		b.WriteString(rel)
		b.WriteString("\n")
		b.Write(data)
		count++
		return nil
	})
	return b.String()
}
