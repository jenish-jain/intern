package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"intern/internal/repository"

	logger "github.com/jenish-jain/logger"
)

var unsafeDirChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// worktreeRoot is where per-job worktrees live: next to the main clone,
// not inside it, so cleaning the clone never touches them.
func (c *Coordinator) worktreeRoot() string {
	return c.RepoPaths.Root() + "-worktrees"
}

// addWorktree creates a fresh worktree for a new ticket's branch, reset to
// the local base branch. Each job gets its own worktree, so developers,
// reviewers and testers can run in parallel without sharing a checkout.
func (c *Coordinator) addWorktree(ctx context.Context, branch string) (string, func(), error) {
	return c.newWorktree(ctx, branch, "-B", branch, c.baseBranch())
}

// flowWorktree creates a worktree on an open PR's branch at its remote
// head, for a review, fix or test round.
func (c *Coordinator) flowWorktree(ctx context.Context, f *Flow) (string, func(), error) {
	c.gitMu.Lock()
	err := c.Repository.FetchBranch(ctx, f.Branch)
	c.gitMu.Unlock()
	if err != nil && err != repository.ErrUnsupported {
		return "", nil, fmt.Errorf("fetch branch: %w", err)
	}
	return c.newWorktree(ctx, f.Branch, f.Branch)
}

// newWorktree runs `git worktree add <dir> <args...>` in a directory named
// after branch, clearing whatever a crashed run left there, and returns
// the directory and a cleanup function that removes it. Git commands on
// the shared repository hold gitMu; the job itself runs without it.
func (c *Coordinator) newWorktree(ctx context.Context, branch string, args ...string) (string, func(), error) {
	main := c.RepoPaths.Root()
	dir := filepath.Join(c.worktreeRoot(), unsafeDirChars.ReplaceAllString(branch, "_"))

	c.gitMu.Lock()
	defer c.gitMu.Unlock()
	_, _ = runCommandCapture(ctx, main, "git", "worktree", "remove", "--force", dir)
	_ = os.RemoveAll(dir)
	_, _ = runCommandCapture(ctx, main, "git", "worktree", "prune")

	if err := os.MkdirAll(c.worktreeRoot(), 0755); err != nil {
		return "", nil, err
	}
	if err := ensureCommitIdentity(ctx, main); err != nil {
		return "", nil, err
	}
	cmd := append([]string{"worktree", "add", "-q"}, args[:len(args)-1]...)
	cmd = append(cmd, dir, args[len(args)-1])
	if out, err := runCommandCapture(ctx, main, "git", cmd...); err != nil {
		return "", nil, fmt.Errorf("git worktree add %s: %w: %s", branch, err, out)
	}
	cleanup := func() {
		c.gitMu.Lock()
		defer c.gitMu.Unlock()
		// Use a fresh context: cleanup must run even after cancellation.
		if out, err := runCommandCapture(context.Background(), main, "git", "worktree", "remove", "--force", dir); err != nil {
			logger.Warn("Failed to remove worktree", "dir", dir, "error", err, "output", out)
			_ = os.RemoveAll(dir)
			_, _ = runCommandCapture(context.Background(), main, "git", "worktree", "prune")
		}
	}
	return dir, cleanup, nil
}

// push pushes branch from the shared repository under gitMu. force is for
// a ticket's first push, whose branch intern rebuilds on every attempt.
func (c *Coordinator) push(ctx context.Context, branch string, force bool) error {
	c.gitMu.Lock()
	defer c.gitMu.Unlock()
	if force {
		return c.Repository.ForcePush(ctx, branch)
	}
	return c.Repository.Push(ctx, branch)
}

// ensureCommitIdentity sets intern's commit identity in the clone's
// repository config, which every worktree shares. Jobs run git themselves
// (a developer merging the base branch to resolve a conflict, say) in an
// environment with no global git config, where commits fail without it.
func ensureCommitIdentity(ctx context.Context, repo string) error {
	for key, want := range map[string]string{"user.name": commitAuthorName, "user.email": commitAuthorEmail} {
		if got, _ := runCommandCapture(ctx, repo, "git", "config", "--local", "--get", key); strings.TrimSpace(got) == want {
			continue
		}
		if out, err := runCommandCapture(ctx, repo, "git", "config", "--local", key, want); err != nil {
			return fmt.Errorf("set git %s: %w: %s", key, err, out)
		}
	}
	return nil
}
