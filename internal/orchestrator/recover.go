package orchestrator

import (
	"context"
	"strings"
	"time"

	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

// trackReviewTicket makes sure an In Review ticket's PR is tracked. Flows
// live in the process's state, which a restart or deploy loses: without
// this, a PR merged afterwards never moves its ticket to Done (so its
// dependents stay blocked), and an open PR is never reviewed, tested or
// merged. The PR is found by the ticket's branch, and an open one resumes
// at the stage its intern:* label shows.
func (c *Coordinator) trackReviewTicket(ctx context.Context, t ticketing.Ticket) {
	if c.State.HasFlow(t.Key) {
		return
	}
	branch := buildBranchName(c.Cfg.BranchPrefix, t.Key)
	prURL, err := c.Repository.FindPR(ctx, branch)
	if err != nil {
		logger.Warn("Could not look up the PR for an In Review ticket", "ticket", t.Key, "branch", branch, "error", err)
		return
	}
	if prURL == "" {
		return // In Review by hand, without an intern PR
	}
	pr, err := c.Repository.GetPR(ctx, prURL)
	if err != nil {
		logger.Warn("Could not read the PR for an In Review ticket", "ticket", t.Key, "pr", prURL, "error", err)
		return
	}
	switch {
	case pr.Merged:
		logger.Info("PR merged; moving ticket to Done", "ticket", t.Key, "pr", prURL)
		c.setStatus(ctx, t.Key, ticketing.StatusDone)
		return
	case pr.Closed:
		logger.Warn("PR closed without merging; leaving the ticket for a person", "ticket", t.Key, "pr", prURL)
		return
	}

	stage := stageFromLabels(pr.Labels)
	if stage == "" {
		stage = c.firstStage()
	}
	if stage == "" {
		return // no review, test or merge stages: a person merges
	}
	// advanceFlow re-checks plan order for ready and queued PRs.
	now := time.Now()
	f := Flow{
		TicketKey: t.Key, Summary: t.Summary, Description: t.Description,
		Branch: branch, PRURL: prURL, CreatedAt: now,
	}
	if stageFromLabels(pr.Labels) == stage {
		// The label already shows the stage; just track it again.
		f.Stage, f.UpdatedAt = stage, now
		c.State.SaveFlow(f)
	} else {
		c.moveFlow(ctx, &f, stage)
	}
	logger.Info("Tracking PR again after a restart", "ticket", t.Key, "pr", prURL, "stage", stage)
}

// stageFromLabels reads a flow stage from a PR's intern:* label.
func stageFromLabels(labels []string) Stage {
	for _, l := range labels {
		s, ok := strings.CutPrefix(l, "intern:")
		if !ok {
			continue
		}
		switch st := Stage(s); st {
		case StageReview, StageChangesRequested, StageTest, StageMerge, StageQueued, StageNeedsHuman:
			return st
		}
	}
	return ""
}
