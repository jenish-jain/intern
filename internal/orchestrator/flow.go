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
	c.moveFlow(ctx, &f, stage)
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
	msg := fmt.Sprintf("I need a human to look at this PR: %s\n\nPush fixes or comment, then add the `%s` label to send it back to review.", reason, LabelResume)
	if err := c.Repository.CommentPR(ctx, f.PRURL, msg); err != nil {
		logger.Warn("Failed to comment on PR", "pr", f.PRURL, "error", err)
	}
	c.comment(ctx, f.TicketKey, "Needs human attention on "+f.PRURL+": "+reason)
	c.notify(ctx, fmt.Sprintf(":raising_hand: %s needs a human: %s\n%s", f.TicketKey, reason, f.PRURL))
	c.moveFlow(ctx, f, StageNeedsHuman)
}

// advanceFlows moves every open flow one step. It runs before new tickets
// are started so in-flight work finishes first.
func (c *Coordinator) advanceFlows(ctx context.Context) {
	flows := c.State.ListFlows()
	if len(flows) == 0 {
		return
	}
	for _, f := range flows {
		if ctx.Err() != nil {
			break
		}
		f := f
		if err := c.advanceFlow(ctx, &f); err != nil {
			logger.Error("Flow step failed", "ticket", f.TicketKey, "stage", f.Stage, "error", err)
		}
	}
	// New ticket branches are created from HEAD, so leave the clone on the
	// base branch rather than on whichever PR branch ran last.
	if err := discardLocalChanges(ctx, c.RepoPaths.Root()); err != nil {
		logger.Warn("Could not clean agent clone", "error", err)
	}
	if err := c.Repository.SwitchBranch(ctx, c.baseBranch()); err != nil {
		logger.Error("Failed to return to base branch after flows", "error", err)
	}
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
	switch f.Stage {
	case StageReview:
		return c.runReview(ctx, f)
	case StageChangesRequested:
		return c.runDeveloperFix(ctx, f)
	case StageTest:
		return c.runTest(ctx, f)
	case StageMerge:
		if c.Cfg.AutoMerge {
			return c.runMerge(ctx, f, pr)
		}
		return nil // waiting for a human to merge
	case StageNeedsHuman:
		if pr.HasLabel(LabelResume) {
			_ = c.Repository.RemoveLabel(ctx, f.PRURL, LabelResume)
			f.ReviewRounds, f.FixRounds, f.Reason, f.Feedback = 0, 0, "", ""
			c.moveFlow(ctx, f, c.firstStage())
		}
		return nil
	default:
		return nil
	}
}

// checkoutFlowBranch puts the shared clone on the flow's branch at the
// remote head.
func (c *Coordinator) checkoutFlowBranch(ctx context.Context, f *Flow) (string, error) {
	repoRoot := c.RepoPaths.Root()
	if err := discardLocalChanges(ctx, repoRoot); err != nil {
		logger.Warn("Could not clean agent clone", "error", err)
	}
	if err := c.Repository.FetchBranch(ctx, f.Branch); err != nil && err != repository.ErrUnsupported {
		return "", fmt.Errorf("fetch branch: %w", err)
	}
	if err := c.Repository.SwitchBranch(ctx, f.Branch); err != nil {
		return "", fmt.Errorf("switch to %s: %w", f.Branch, err)
	}
	return repoRoot, nil
}

func (c *Coordinator) baseBranch() string {
	if c.Cfg.BaseBranch != "" {
		return c.Cfg.BaseBranch
	}
	return "main"
}

// runReview runs the reviewer and posts its verdict.
func (c *Coordinator) runReview(ctx context.Context, f *Flow) error {
	repoRoot, err := c.checkoutFlowBranch(ctx, f)
	if err != nil {
		return err
	}
	res, err := c.Runner.Run(ctx, runner.Job{
		Role: runner.RoleReviewer, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: repoRoot, BaseBranch: c.baseBranch(),
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	})
	f.CostUSD += res.Usage.CostUSD
	// Reviewers can't edit, but never let stray files reach a later stage.
	_ = discardLocalChanges(ctx, repoRoot)
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
		c.moveFlow(ctx, f, c.stageAfterReview())
		return nil
	}
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
	repoRoot, err := c.checkoutFlowBranch(ctx, f)
	if err != nil {
		return err
	}
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
	if err != nil {
		c.escalate(ctx, f, "the developer could not address the feedback: "+truncateNotes(err.Error(), 500))
		return nil
	}
	if err := c.Repository.Push(ctx, f.Branch); err != nil {
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
	repoRoot, err := c.checkoutFlowBranch(ctx, f)
	if err != nil {
		return err
	}
	base, err := gate.HeadSHA(ctx, repoRoot)
	if err != nil {
		return err
	}
	res, err := c.Runner.Run(ctx, runner.Job{
		Role: runner.RoleTester, TicketKey: f.TicketKey, Summary: f.Summary, Description: f.Description,
		Workdir: repoRoot, BaseBranch: c.baseBranch(),
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	})
	f.CostUSD += res.Usage.CostUSD
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
		if err := c.Repository.Push(ctx, f.Branch); err != nil {
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
		c.moveFlow(ctx, f, StageMerge)
		return nil
	}
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
