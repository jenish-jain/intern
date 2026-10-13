package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	logger "github.com/jenish-jain/logger"
)

var unsafeDirChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// worktreeRoot is where per-ticket worktrees live: next to the main clone,
// not inside it, so cleaning the clone never touches them.
func (c *Coordinator) worktreeRoot() string {
	return c.RepoPaths.Root() + "-worktrees"
}

// addWorktree creates a fresh worktree for branch, reset to the local base
// branch, and returns its path and a cleanup function that removes it.
// Each developer ticket gets its own worktree, so tickets can run in
// parallel without sharing a checkout.
func (c *Coordinator) addWorktree(ctx context.Context, branch string) (string, func(), error) {
	main := c.RepoPaths.Root()
	dir := filepath.Join(c.worktreeRoot(), unsafeDirChars.ReplaceAllString(branch, "_"))

	// Clear anything left by a crashed run.
	_, _ = runCommandCapture(ctx, main, "git", "worktree", "remove", "--force", dir)
	_ = os.RemoveAll(dir)
	_, _ = runCommandCapture(ctx, main, "git", "worktree", "prune")

	if err := os.MkdirAll(c.worktreeRoot(), 0755); err != nil {
		return "", nil, err
	}
	if out, err := runCommandCapture(ctx, main, "git", "worktree", "add", "-q", "-B", branch, dir, c.baseBranch()); err != nil {
		return "", nil, fmt.Errorf("git worktree add %s: %w: %s", branch, err, out)
	}
	cleanup := func() {
		// Use a fresh context: cleanup must run even after cancellation.
		if out, err := runCommandCapture(context.Background(), main, "git", "worktree", "remove", "--force", dir); err != nil {
			logger.Warn("Failed to remove worktree", "dir", dir, "error", err, "output", out)
			_ = os.RemoveAll(dir)
			_, _ = runCommandCapture(context.Background(), main, "git", "worktree", "prune")
		}
	}
	return dir, cleanup, nil
}
