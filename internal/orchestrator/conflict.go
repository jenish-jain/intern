package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"intern/internal/project"
	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// conflictsWith reports whether merging base into the checked-out branch
// would conflict, without touching the worktree.
func conflictsWith(ctx context.Context, dir, base string) bool {
	// git merge-tree exits 1 when the merge has conflicts.
	_, err := runCommandCapture(ctx, dir, "git", "merge-tree", "--write-tree", "HEAD", base)
	return err != nil
}

// resolveConflict brings a PR branch up to date with the base branch. Intern
// does the merge itself: a clean merge is committed and pushed with no
// model involved. Only when real conflicts remain does a developer get the
// list of conflicted files to resolve, on a model a tier stronger than the
// ticket's, since a resolution has to keep two changes' intent at once.
func (c *Coordinator) resolveConflict(ctx context.Context, f *Flow, dir string) error {
	base := c.baseBranch()
	baseSHA, err := runCommandCapture(ctx, dir, "git", "rev-parse", base)
	if err != nil {
		return fmt.Errorf("resolve %s: %w: %s", base, err, baseSHA)
	}
	baseSHA = strings.TrimSpace(baseSHA)
	if limit := 2 * c.Cfg.MaxReviewRounds; f.FixRounds >= limit {
		c.escalate(ctx, f, fmt.Sprintf("still not passing after %d developer fix rounds", f.FixRounds))
		return nil
	}

	mergeOut, mergeErr := runCommandCapture(ctx, dir, "git", "merge", "--no-edit", "--no-ff", base)
	if mergeErr == nil {
		if err := c.push(ctx, f.Branch, false); err != nil {
			return fmt.Errorf("push: %w", err)
		}
		logger.Info("Merged the base branch into a PR cleanly", "ticket", f.TicketKey, "base", base)
		c.commentPR(ctx, f, fmt.Sprintf("**intern: merged `%s` into this branch.** It merged without conflicts, so this PR goes back through review and testing on the updated code.", base))
		f.Conflict, f.Feedback = false, ""
		c.moveFlow(ctx, f, c.firstStage())
		return nil
	}
	conflicted := gitLines(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if len(conflicted) == 0 {
		_, _ = runCommandCapture(ctx, dir, "git", "merge", "--abort")
		return fmt.Errorf("merge %s failed: %w: %s", base, mergeErr, truncateNotes(mergeOut, 500))
	}

	f.FixRounds++
	feedback := fmt.Sprintf("Intern started merging %[1]s into this branch, and the merge has conflicts in:\n- %[2]s\n\n"+
		"Resolve every conflict so both sides' intent is kept: this ticket's change must keep working alongside what %[1]s added since the branch was made. "+
		"Read both versions, remove all conflict markers, `git add` the resolved files, and conclude the merge with `git commit --no-edit`. "+
		"Do not abort the merge, reset or rebase the branch. Make sure verify passes on the merged result.",
		base, strings.Join(conflicted, "\n- "))
	if f.Feedback != "" {
		feedback += "\n\nAlso address this earlier feedback:\n" + f.Feedback
	}
	// The gate compares against the base branch: after the merge, the
	// change is exactly what this PR contributes on top of it.
	out, err := c.developAndVerify(ctx, runner.Job{
		Role: runner.RoleDeveloper, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: dir, BaseBranch: base, Feedback: feedback,
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
		Model:  c.modelFor(runner.RoleDeveloper, f.TicketKey, f.Description, f.FixRounds),
	}, baseSHA)
	f.CostUSD += out.Result.Usage.CostUSD
	c.recordSpend(ctx, runner.RoleDeveloper, out.Result.Usage.CostUSD)
	if err == nil {
		err = mergeConcluded(ctx, dir, baseSHA, conflicted)
	}
	if err != nil {
		reason := "the developer could not resolve the merge conflicts with " + base + ": " + truncateNotes(err.Error(), 500)
		if s := strings.TrimSpace(out.Result.Summary); s != "" && !strings.Contains(err.Error(), s) {
			reason += "\nThe developer reported: " + truncateNotes(s, 400)
		}
		c.escalate(ctx, f, reason)
		return nil
	}
	if err := c.push(ctx, f.Branch, false); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	msg := fmt.Sprintf("**intern developer: resolved merge conflicts with `%s`** in %s\n\n%s", base, strings.Join(conflicted, ", "), out.Result.Summary)
	if checks := project.Summary(out.Checks); len(checks) > 0 {
		msg += "\n\nVerify:\n- " + strings.Join(checks, "\n- ")
	}
	c.commentPR(ctx, f, msg)
	f.Conflict, f.Feedback = false, ""
	c.moveFlow(ctx, f, c.firstStage())
	return nil
}

// mergeConcluded checks a developer really finished a merge: the base is
// part of the branch, no merge is still in progress, and no conflict
// markers are left in the files that conflicted.
func mergeConcluded(ctx context.Context, dir, baseSHA string, conflicted []string) error {
	if _, err := runCommandCapture(ctx, dir, "git", "merge-base", "--is-ancestor", baseSHA, "HEAD"); err != nil {
		return fmt.Errorf("the merge was not completed: the base branch is not part of the branch")
	}
	if _, err := runCommandCapture(ctx, dir, "git", "rev-parse", "-q", "--verify", "MERGE_HEAD"); err == nil {
		return fmt.Errorf("the merge is still in progress")
	}
	for _, f := range conflicted {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue // resolved by deleting the file
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") {
				return fmt.Errorf("conflict markers are left in %s", f)
			}
		}
	}
	return nil
}

// gitLines runs a git command and returns its non-empty output lines.
func gitLines(ctx context.Context, dir string, args ...string) []string {
	out, err := runCommandCapture(ctx, dir, "git", args...)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// commentPR posts a comment on the flow's PR, logging a failure.
func (c *Coordinator) commentPR(ctx context.Context, f *Flow, body string) {
	if err := c.Repository.CommentPR(ctx, f.PRURL, body); err != nil {
		logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
	}
}

// verifyPasses runs the project's verify commands in dir.
func (c *Coordinator) verifyPasses(ctx context.Context, dir string) bool {
	_, cmds, err := c.verifyCommands(dir)
	if err != nil {
		return false
	}
	_, ok := project.RunCommands(ctx, dir, cmds)
	return ok
}
