package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"intern/internal/config"
)

func runCommandCapture(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func truncateMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max / 2
	tail := max - head
	return s[:head] + "\n...\n" + s[len(s)-tail:]
}

// runQualityGates executes optional go vet and go test before PR.
// Returns notes to include in PR body and ok=false when any enabled gate fails.
func runQualityGates(ctx context.Context, cfg *config.Config, repoRoot string) ([]string, bool) {
	notes := []string{}
	ok := true

	// Use a short timeout per command to avoid hanging
	perCmdTimeout := 10 * time.Minute

	if cfg.RunVetBeforePR {
		ctxVet, cancel := context.WithTimeout(ctx, perCmdTimeout)
		out, err := runCommandCapture(ctxVet, repoRoot, "go", "vet", "./...")
		cancel()
		if err != nil {
			notes = append(notes, "go vet: FAILED")
			notes = append(notes, fmt.Sprintf("```\n%s\n```", truncateMiddle(strings.TrimSpace(out), 8000)))
			ok = false
		} else {
			notes = append(notes, "go vet: PASSED")
		}
	} else {
		notes = append(notes, "go vet: skipped")
	}

	if cfg.RunTestsBeforePR {
		ctxTest, cancel := context.WithTimeout(ctx, perCmdTimeout)
		out, err := runCommandCapture(ctxTest, repoRoot, "go", "test", "./...")
		cancel()
		if err != nil {
			notes = append(notes, "go test: FAILED")
			notes = append(notes, fmt.Sprintf("```\n%s\n```", truncateMiddle(strings.TrimSpace(out), 8000)))
			ok = false
		} else {
			// keep summary short
			summary := out
			if idx := strings.LastIndex(summary, "\n"); idx > -1 {
				summary = summary[idx+1:]
			}
			notes = append(notes, fmt.Sprintf("go test: PASSED (%s)", strings.TrimSpace(summary)))
		}
	} else {
		notes = append(notes, "go test: skipped")
	}

	return notes, ok
}
