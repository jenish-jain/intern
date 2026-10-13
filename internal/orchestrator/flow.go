package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"intern/internal/gate"
	"intern/internal/project"
	"intern/internal/repository"
	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// Stage is where an open PR is in the review, test and merge flow.
type Stage string

const (
	StageReview           Stage = "review"
	StageChangesRequested Stage = "changes-requested"
	StageTest             Stage = "testing"
	StageMerge            Stage = "ready-to-merge"
	StageNeedsHuman       Stage = "needs-human"
	// StageQueued is approved and tested, waiting for earlier tickets in
	// the plan to merge first, so PRs merge in plan order.
	StageQueued Stage = "queued"
)

// LabelResume is added by a human to a needs-human PR to send it back
// to review with a fresh round budget.
const LabelResume = "intern:resume"

// stageLabel is the PR label that shows a stage on GitHub.
func stageLabel(s Stage) string { return "intern:" + string(s) }

// Flow is a ticket whose PR is open and moving toward merge.
type Flow struct {
	TicketKey    string    `json:"ticket_key"`
	Summary      string    `json:"summary"`
	Description  string    `json:"description"`
	Branch       string    `json:"branch"`
	PRURL        string    `json:"pr_url"`
	Stage        Stage     `json:"stage"`
	ReviewRounds int       `json:"review_rounds"`
	FixRounds    int       `json:"fix_rounds"`
	MergeErrors  int       `json:"merge_errors,omitempty"` // consecutive unexplained merge failures
	Feedback     string    `json:"feedback,omitempty"`     // input for the next developer round
	Reason       string    `json:"reason,omitempty"`       // why a human is needed
	EscalatedAt  time.Time `json:"escalated_at,omitempty"`
	CostUSD      float64   `json:"cost_usd"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// firstStage is where a new PR starts, given which stages are enabled.
// Empty means no flow: a human reviews and merges.
func (c *Coordinator) firstStage() Stage {
	switch {
	case c.Cfg.ReviewEnabled:
		return StageReview
	case c.Cfg.TestEnabled:
		return StageTest
	case c.Cfg.AutoMerge:
		return StageMerge
	default:
		return ""
	}
}

// stageAfterReview is where an approved PR goes next. StageMerge without
// AUTO_MERGE waits, labelled ready-to-merge, for a human to merge.
func (c *Coordinator) stageAfterReview() Stage {
	if c.Cfg.TestEnabled {
		return StageTest
	}
	return StageMerge
}

// startFlow begins tracking a newly opened PR.
func (c *Coordinator) startFlow(ctx context.Context, key, summary, description, branch, prURL string, cost float64) {
	stage := c.firstStage()
	if stage == "" {
		return
	}
	now := time.Now()
	f := Flow{
		TicketKey: key, Summary: summary, Description: description,
		Branch: branch, PRURL: prURL, CostUSD: cost, CreatedAt: now,
	}
	c.moveFlowInOrder(ctx, &f, stage)
}

// moveFlowInOrder is moveFlow, except that a PR becomes ready to merge only
// when no earlier ticket in the plan is still open; until then it waits as
// queued. A small PR reviewed and tested first still merges in plan order.
func (c *Coordinator) moveFlowInOrder(ctx context.Context, f *Flow, to Stage) {
	if to != StageMerge {
		c.moveFlow(ctx, f, to)
		return
	}
	blocker := c.mergeBlocker(f.TicketKey)
	if blocker == "" {
		c.moveFlow(ctx, f, StageMerge)
		return
	}
	if f.Stage != StageQueued {
		msg := fmt.Sprintf("**intern: queued.** This PR is approved and tested, and waits for %s to merge first so PRs merge in plan order. It will be marked `%s` then.", blocker, stageLabel(StageMerge))
		if err := c.Repository.CommentPR(ctx, f.PRURL, msg); err != nil {
			logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
		}
		logger.Info("PR queued behind an earlier ticket", "ticket", f.TicketKey, "waiting_for", blocker)
	}
	c.moveFlow(ctx, f, StageQueued)
}

// moveFlow sets the flow's stage, swaps the PR label and persists it.
// An empty stage ends the flow (a human takes over the merge).
func (c *Coordinator) moveFlow(ctx context.Context, f *Flow, to Stage) {
	from := f.Stage
	f.Stage = to
	f.UpdatedAt = time.Now()
	if from != "" && from != to {
		if err := c.Repository.RemoveLabel(ctx, f.PRURL, stageLabel(from)); err != nil {
			logger.Warn("Failed to remove PR label", "pr", f.PRURL, "error", err)
		}
	}
	if to == "" {
		c.State.DeleteFlow(f.TicketKey)
		return
	}
	if from != to {
		if err := c.Repository.AddLabels(ctx, f.PRURL, stageLabel(to)); err != nil {
			logger.Warn("Failed to add PR label", "pr", f.PRURL, "error", err)
		}
	}
	c.State.SaveFlow(*f)
	logger.Info("Flow stage", "ticket", f.TicketKey, "from", from, "to", to)
}

// escalate hands a flow to a human with a reason posted on the PR and
// the ticket.
func (c *Coordinator) escalate(ctx context.Context, f *Flow, reason string) {
	f.Reason = reason
	f.EscalatedAt = time.Now()
	msg := fmt.Sprintf("I need a human to look at this PR: %s\n\nPush fixes or comment, then add the `%s` label to send it back to review.", reason, LabelResume)
	if err := c.Repository.CommentPR(ctx, f.PRURL, msg); err != nil {
		logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
	}
	c.comment(ctx, f.TicketKey, "Needs human attention on "+f.PRURL+": "+reason)
	c.team.count(&c.team.escalations)
	c.notify(ctx, fmt.Sprintf(":raising_hand: %s needs a human: %s\n%s", f.TicketKey, reason, f.PRURL))
	c.moveFlow(ctx, f, StageNeedsHuman)
}

func (c *Coordinator) advanceFlow(ctx context.Context, f *Flow) error {
	pr, err := c.Repository.GetPR(ctx, f.PRURL)
	if err != nil {
		return fmt.Errorf("read PR: %w", err)
	}
	if pr.Merged || pr.Closed {
		// Merged PRs are moved to Done by reconcileMerged; closed ones are
		// abandoned by a human.
		c.State.DeleteFlow(f.TicketKey)
		return nil
	}
	if c.budgetExceeded() && (f.Stage == StageReview || f.Stage == StageChangesRequested || f.Stage == StageTest) {
		return nil // paused until the budget is raised; merges still run
	}
	switch f.Stage {
	case StageReview:
		return c.runReview(ctx, f)
	case StageChangesRequested:
		return c.runDeveloperFix(ctx, f)
	case StageTest:
		return c.runTest(ctx, f)
	case StageQueued:
		if c.mergeBlocker(f.TicketKey) == "" {
			logger.Info("Earlier tickets merged; PR is next in plan order", "ticket", f.TicketKey)
			c.moveFlow(ctx, f, StageMerge)
		}
		return nil
	case StageMerge:
		// An earlier ticket's PR can reappear, e.g. tracked again after a
		// restart; never merge ahead of it.
		if c.mergeBlocker(f.TicketKey) != "" {
			c.moveFlowInOrder(ctx, f, StageMerge)
			return nil
		}
		if c.Cfg.AutoMerge {
			return c.runMerge(ctx, f, pr)
		}
		return nil // waiting for a human to merge
	case StageNeedsHuman:
		if pr.HasLabel(LabelResume) {
			_ = c.Repository.RemoveLabel(ctx, f.PRURL, LabelResume)
			f.ReviewRounds, f.FixRounds, f.Reason, f.Feedback = 0, 0, "", ""
			// What people wrote on the PR while it waited is the most
			// direct guidance the developer can get.
			if fb := c.humanFeedback(ctx, f); fb != "" {
				f.Feedback = fb
				c.moveFlow(ctx, f, StageChangesRequested)
				return nil
			}
			c.moveFlow(ctx, f, c.firstStage())
		}
		return nil
	default:
		return nil
	}
}

func (c *Coordinator) baseBranch() string {
	if c.Cfg.BaseBranch != "" {
		return c.Cfg.BaseBranch
	}
	return "main"
}

// runReview runs the reviewer and posts its verdict.
func (c *Coordinator) runReview(ctx context.Context, f *Flow) error {
	repoRoot, cleanup, err := c.flowWorktree(ctx, f)
	if err != nil {
		return err
	}
	defer cleanup()
	res, err := c.runModel(ctx, runner.Job{
		Role: runner.RoleReviewer, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: repoRoot, BaseBranch: c.baseBranch(),
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	})
	f.CostUSD += res.Usage.CostUSD
	c.recordSpend(ctx, runner.RoleReviewer, res.Usage.CostUSD)
	if err != nil {
		return err
	}
	switch {
	case res.Status == runner.StatusNeedsHuman:
		c.escalate(ctx, f, "the reviewer has questions: "+strings.Join(res.Questions, "; "))
		return nil
	case res.Status != runner.StatusDone || res.Review == nil:
		return fmt.Errorf("reviewer did not reach a verdict: %s", res.Summary)
	}

	f.ReviewRounds++
	verdict := "Approved"
	if !res.Review.Approve {
		verdict = "Changes requested"
	}
	body := fmt.Sprintf("**intern reviewer: %s** (round %d)\n\n%s", verdict, f.ReviewRounds, res.Summary)
	if len(res.Notes) > 0 {
		body += "\n\nNotes:\n- " + strings.Join(res.Notes, "\n- ")
	}
	comments := make([]repository.ReviewComment, len(res.Review.Comments))
	for i, cm := range res.Review.Comments {
		comments[i] = repository.ReviewComment{Path: cm.Path, Line: cm.Line, Body: cm.Body}
	}
	if err := c.Repository.ReviewPR(ctx, f.PRURL, body, comments); err != nil {
		logger.Warn("Failed to post review", "pr", f.PRURL, "error", err)
	}

	if res.Review.Approve {
		c.team.count(&c.team.approvals)
		c.moveFlowInOrder(ctx, f, c.stageAfterReview())
		return nil
	}
	c.team.count(&c.team.rejections)
	if f.ReviewRounds > c.Cfg.MaxReviewRounds {
		c.escalate(ctx, f, fmt.Sprintf("review still requests changes after %d rounds", f.ReviewRounds))
		return nil
	}
	f.Feedback = reviewFeedback(res)
	c.moveFlow(ctx, f, StageChangesRequested)
	return nil
}

// reviewFeedback formats a review for the developer.
func reviewFeedback(res runner.Result) string {
	var b strings.Builder
	b.WriteString("The reviewer requested changes before this can merge. Address every comment, keep the rest of the change intact, and make sure verify still passes.\n\n")
	b.WriteString("Review summary: " + res.Summary + "\n")
	for _, cm := range res.Review.Comments {
		fmt.Fprintf(&b, "\n- %s:%d: %s", cm.Path, cm.Line, cm.Body)
	}
	return b.String()
}

// runDeveloperFix has the developer address feedback on the PR branch,
// then pushes and sends the PR back to review.
func (c *Coordinator) runDeveloperFix(ctx context.Context, f *Flow) error {
	repoRoot, cleanup, err := c.flowWorktree(ctx, f)
	if err != nil {
		return err
	}
	defer cleanup()
	base, err := gate.HeadSHA(ctx, repoRoot)
	if err != nil {
		return err
	}
	if limit := 2 * c.Cfg.MaxReviewRounds; f.FixRounds >= limit {
		c.escalate(ctx, f, fmt.Sprintf("still not passing after %d developer fix rounds", f.FixRounds))
		return nil
	}
	f.FixRounds++
	out, err := c.developAndVerify(ctx, runner.Job{
		Role: runner.RoleDeveloper, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: repoRoot, BaseBranch: c.baseBranch(), Feedback: f.Feedback,
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	}, base)
	f.CostUSD += out.Result.Usage.CostUSD
	c.recordSpend(ctx, runner.RoleDeveloper, out.Result.Usage.CostUSD)
	if err != nil {
		c.escalate(ctx, f, "the developer could not address the feedback: "+truncateNotes(err.Error(), 500))
		return nil
	}
	if err := c.push(ctx, f.Branch, false); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	msg := fmt.Sprintf("**intern developer: addressed feedback** (round %d)\n\n%s", f.FixRounds, out.Result.Summary)
	if checks := project.Summary(out.Checks); len(checks) > 0 {
		msg += "\n\nVerify:\n- " + strings.Join(checks, "\n- ")
	}
	if err := c.Repository.CommentPR(ctx, f.PRURL, msg); err != nil {
		logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
	}
	f.Feedback = ""
	c.moveFlow(ctx, f, c.firstStage())
	return nil
}

// runTest has the tester write and run acceptance tests on the PR branch.
// Tests it adds are committed and pushed whatever the outcome, so the
// developer sees exactly what failed.
func (c *Coordinator) runTest(ctx context.Context, f *Flow) error {
	repoRoot, cleanup, err := c.flowWorktree(ctx, f)
	if err != nil {
		return err
	}
	defer cleanup()
	base, err := gate.HeadSHA(ctx, repoRoot)
	if err != nil {
		return err
	}
	res, err := c.runModel(ctx, runner.Job{
		Role: runner.RoleTester, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: repoRoot, BaseBranch: c.baseBranch(),
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	})
	f.CostUSD += res.Usage.CostUSD
	c.recordSpend(ctx, runner.RoleTester, res.Usage.CostUSD)
	if err != nil {
		return err
	}
	if res.Status == runner.StatusNeedsHuman {
		c.escalate(ctx, f, "the tester has questions: "+strings.Join(res.Questions, "; "))
		return nil
	}
	if res.Status != runner.StatusDone || res.Test == nil {
		return fmt.Errorf("tester did not report a result: %s", res.Summary)
	}

	proj, cmds, err := c.verifyCommands(repoRoot)
	if err != nil {
		return err
	}
	testPaths := proj.TestPaths
	if len(testPaths) == 0 {
		testPaths = gate.DefaultTestPaths
	}
	changes, violations, err := gate.Check(ctx, repoRoot, base, gate.Policy{
		OnlyPaths: testPaths, ProtectedPaths: proj.ProtectedPaths, MaxFiles: c.Cfg.PlanMaxFiles,
	})
	if err != nil {
		return fmt.Errorf("gate: %w", err)
	}
	if len(violations) > 0 {
		msgs := make([]string, len(violations))
		for i, v := range violations {
			msgs[i] = v.String()
		}
		c.escalate(ctx, f, "the tester changed files it may not touch: "+strings.Join(msgs, "; "))
		return nil
	}
	if len(changes) > 0 {
		if err := c.commitStaged(ctx, repoRoot, fmt.Sprintf("test(%s): acceptance tests", f.TicketKey)); err != nil {
			return err
		}
	}
	checks, verifyOK := project.RunCommands(ctx, repoRoot, cmds)
	if len(changes) > 0 {
		if err := c.push(ctx, f.Branch, false); err != nil {
			return fmt.Errorf("push: %w", err)
		}
	}

	passed := res.Test.Passed && verifyOK
	verdict := "Acceptance tests passed"
	if !passed {
		verdict = "Acceptance tests failed"
	}
	msg := fmt.Sprintf("**intern tester: %s**\n\n%s", verdict, res.Summary)
	if len(res.Test.Failures) > 0 {
		msg += "\n\nFailures:\n- " + strings.Join(res.Test.Failures, "\n- ")
	}
	if lines := project.Summary(checks); len(lines) > 0 {
		msg += "\n\nVerify:\n- " + strings.Join(lines, "\n- ")
	}
	if err := c.Repository.CommentPR(ctx, f.PRURL, msg); err != nil {
		logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
	}

	if passed {
		c.team.count(&c.team.testPasses)
		c.moveFlowInOrder(ctx, f, StageMerge)
		return nil
	}
	c.team.count(&c.team.testFails)
	var fb strings.Builder
	fb.WriteString("Acceptance testing found problems. The tester's tests are committed on this branch; make the application code satisfy them without weakening or deleting them.\n\n")
	fb.WriteString("Tester summary: " + res.Summary + "\n")
	for _, fail := range res.Test.Failures {
		fb.WriteString("\n- " + fail)
	}
	if !verifyOK {
		fb.WriteString("\n\n" + project.FailureReport(checks))
	}
	f.Feedback = fb.String()
	c.moveFlow(ctx, f, StageChangesRequested)
	return nil
}

// maxMergeErrors is how many consecutive unexplained merge failures are
// retried before a human is asked to look.
const maxMergeErrors = 3

// runMerge merges a ready PR once CI allows it. Failing CI and conflicts
// go back to the developer; other merge errors are retried, then escalated.
func (c *Coordinator) runMerge(ctx context.Context, f *Flow, pr repository.PRState) error {
	switch pr.Checks {
	case repository.ChecksPending:
		return nil // wait for CI
	case repository.ChecksFailure:
		f.Feedback = "CI failed on this pull request. Find the cause, fix it, and make sure verify passes.\n\nFailing checks:\n- " +
			strings.Join(pr.FailedChecks, "\n- ")
		c.moveFlow(ctx, f, StageChangesRequested)
		return nil
	}

	err := c.Repository.MergePR(ctx, f.PRURL, c.Cfg.MergeMethod)
	switch {
	case err == nil:
		logger.Info("Merged PR", "ticket", f.TicketKey, "pr", f.PRURL, "flow_cost", f.CostUSD)
		if cerr := c.Repository.CommentPR(ctx, f.PRURL, fmt.Sprintf("**intern: merged** after %d review and %d fix rounds. Total model cost for this ticket: $%.2f.", f.ReviewRounds, f.FixRounds, f.CostUSD)); cerr != nil {
			logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", cerr)
		}
		c.team.count(&c.team.merges)
		c.notify(ctx, fmt.Sprintf(":white_check_mark: Merged %s: %s (%s, $%.2f)", f.TicketKey, f.Summary, f.PRURL, f.CostUSD))
		// reconcileMerged moves the ticket to Done on the next cycle.
		c.State.DeleteFlow(f.TicketKey)
		return nil
	case errors.Is(err, repository.ErrMergeConflict):
		f.MergeErrors = 0
		f.Feedback = fmt.Sprintf("This pull request conflicts with %[1]s. Merge the local %[1]s branch into this branch (`git merge %[1]s`), resolve every conflict so both sides' intent is kept, and make sure verify passes.", c.baseBranch())
		c.moveFlow(ctx, f, StageChangesRequested)
		return nil
	default:
		f.MergeErrors++
		if f.MergeErrors >= maxMergeErrors {
			c.escalate(ctx, f, "merging keeps failing: "+truncateNotes(err.Error(), 300))
			return nil
		}
		c.State.SaveFlow(*f)
		return err
	}
}

// humanFeedback collects PR comments people left since the flow was
// escalated, formatted for the developer. Empty when there are none.
func (c *Coordinator) humanFeedback(ctx context.Context, f *Flow) string {
	since := f.EscalatedAt
	if since.IsZero() {
		since = f.UpdatedAt
	}
	comments, err := c.Repository.ListComments(ctx, f.PRURL, since)
	if err != nil {
		if err != repository.ErrUnsupported {
			logger.Warn("Could not read PR comments", "pr", f.PRURL, "error", err)
		}
		return ""
	}
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("A person reviewed this pull request and left the comments below. Address every one of them, and make sure verify still passes.\n")
	for _, cm := range comments {
		if cm.Path != "" {
			fmt.Fprintf(&b, "\n- %s on %s:%d: %s", cm.Author, cm.Path, cm.Line, cm.Body)
		} else {
			fmt.Fprintf(&b, "\n- %s: %s", cm.Author, cm.Body)
		}
	}
	return b.String()
}
