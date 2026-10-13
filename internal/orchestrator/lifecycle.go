package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

// maxCommentErrorLen bounds the error text posted to a ticket comment.
const maxCommentErrorLen = 2000

// ticketFingerprint identifies the ticket's current text. Failure counts
// are tied to it, so editing a ticket resets its retry budget.
func ticketFingerprint(t ticketing.Ticket) string {
	sum := sha256.Sum256([]byte(t.Summary + "\n" + t.Description))
	return hex.EncodeToString(sum[:8])
}

// setStatus moves a ticket to status. Failures are logged, not returned:
// a board missing a status (e.g. no "In Review" column) must not stop the
// pipeline. Dry-run mode never touches the board.
func (c *Coordinator) setStatus(ctx context.Context, key, status string) {
	if c.Cfg.DryRun {
		logger.Info("DRY RUN: would move ticket", "ticket", key, "status", status)
		return
	}
	if err := c.Ticketing.UpdateTicketStatus(ctx, key, status, c.Cfg.JiraTransitions); err != nil {
		logger.Warn("Failed to move ticket", "ticket", key, "status", status, "error", err)
	}
}

// comment posts a comment on the ticket when the backend supports it.
func (c *Coordinator) comment(ctx context.Context, key, body string) {
	if c.Cfg.DryRun {
		return
	}
	if err := c.Ticketing.AddComment(ctx, key, body); err != nil && err != ticketing.ErrUnsupported {
		logger.Warn("Failed to comment on ticket", "ticket", key, "error", err)
	}
}

// startTickets starts process for each eligible ticket while developer
// slots allow, each in its own goroutine, and returns without waiting; the
// returned group waits for the jobs it started. Tickets already running,
// processed, at their attempt limit, blocked by unfinished tickets, or
// deferred by the journal are skipped, and In Review tickets have their PR
// tracked instead.
func (c *Coordinator) startTickets(ctx context.Context, tickets []ticketing.Ticket, process func(ctx context.Context, key, summary, description string) error) (*sync.WaitGroup, map[string]bool) {
	var wg sync.WaitGroup
	started := map[string]bool{}
	if _, paused := c.modelPaused(); paused {
		return &wg, started
	}
	for _, t := range tickets {
		if ctx.Err() != nil {
			break
		}
		// Polls no longer wait for jobs, so a ticket can be In Progress
		// because this process is building it right now.
		if c.sched.isRunning(t.Key) {
			continue
		}
		if t.Status == ticketing.StatusInReview {
			c.trackReviewTicket(ctx, t)
			continue
		}
		if c.State.IsProcessed(t.Key) {
			continue
		}
		fp := ticketFingerprint(t)
		if n := c.State.Attempts(t.Key, fp); n >= c.Cfg.MaxTicketAttempts {
			logger.Debug("Skipping ticket - attempt limit reached; edit the ticket to retry",
				"ticket", t.Key, "attempts", n, "limit", c.Cfg.MaxTicketAttempts)
			continue
		}
		if open := t.OpenBlockers(); len(open) > 0 {
			logger.Info("Deferring ticket - blocked by unfinished tickets", "ticket", t.Key, "blocked_by", open)
			continue
		}
		// Explicit links are authoritative. The journal's keyword-overlap
		// guess only applies to tickets with no dependency links at all.
		if c.Journal != nil && len(t.Blockers) == 0 {
			if blocker := c.journalBlocker(t.Summary + " " + t.Description); blocker != "" {
				logger.Info("Deferring ticket - related work not yet merged", "ticket", t.Key, "waiting_on", blocker)
				continue
			}
		}
		t := t
		ok := c.spawn(t.Key, jobDeveloper, &wg, func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Worker panic recovered", "ticket", t.Key, "panic", r)
					c.Metrics.IncTicketsFailed()
					c.handleTicketFailure(ctx, t, fp, fmt.Errorf("panic: %v", r))
				}
			}()
			if t.Status == ticketing.StatusInProgress {
				logger.Info("Resuming ticket left In Progress by an interrupted run", "ticket", t.Key)
			}
			if err := process(ctx, t.Key, t.Summary, t.Description); err != nil {
				c.Metrics.IncTicketsFailed()
				c.handleTicketFailure(ctx, t, fp, err)
				return
			}
			c.State.MarkProcessed(t.Key)
		})
		if !ok {
			break // developer slots are full; the next poll continues
		}
		started[t.Key] = true
	}
	return &wg, started
}

// dispatchTickets runs each eligible ticket once and waits for them all,
// as many at a time as there are developer slots.
func (c *Coordinator) dispatchTickets(ctx context.Context, tickets []ticketing.Ticket, process func(ctx context.Context, key, summary, description string) error) {
	for len(tickets) > 0 && ctx.Err() == nil {
		wg, started := c.startTickets(ctx, tickets, process)
		wg.Wait()
		if len(started) == 0 {
			return
		}
		rest := tickets[:0:0]
		for _, t := range tickets {
			if !started[t.Key] {
				rest = append(rest, t)
			}
		}
		tickets = rest
	}
}

// handleTicketFailure records the failure, explains it on the ticket, and
// returns the ticket to To Do so the next poll can retry it. Once the
// attempt limit is reached the comment says retries have stopped.
func (c *Coordinator) handleTicketFailure(ctx context.Context, t ticketing.Ticket, fingerprint string, err error) {
	var outage *ModelUnavailableError
	if errors.As(err, &outage) {
		// Not the ticket's fault and not an attempt: put it back for when
		// the API is available again. The outage was announced once.
		logger.Warn("Ticket paused: model API unavailable", "ticket", t.Key, "reason", outage.Reason)
		c.setStatus(ctx, t.Key, ticketing.StatusToDo)
		return
	}
	n := c.State.RecordFailure(t.Key, fingerprint)
	limit := c.Cfg.MaxTicketAttempts
	logger.Error("Failed processing ticket", "key", t.Key, "error", err, "attempt", n, "limit", limit)

	var body string
	var nh *NeedsHumanError
	if errors.As(err, &nh) {
		body = "I need more information before I can finish this ticket:\n"
		for _, q := range nh.Questions {
			body += "- " + q + "\n"
		}
		body += "\nAdd the answers to the ticket description and I'll pick it up again on the next poll."
	} else {
		msg := err.Error()
		if len(msg) > maxCommentErrorLen {
			msg = msg[:maxCommentErrorLen] + "…"
		}
		body = fmt.Sprintf("intern attempt %d of %d failed:\n%s", n, limit, msg)
		if n >= limit {
			body += "\n\nI've stopped retrying this ticket. Edit its summary or description to clarify it, and I'll try again on the next poll."
			c.notify(ctx, fmt.Sprintf(":x: %s failed %d times and intern stopped retrying it: %s\nEdit the ticket to retry.", t.Key, n, msg))
		}
	}
	if nh != nil {
		c.notify(ctx, fmt.Sprintf(":raising_hand: %s needs more information before intern can finish it; the questions are on the ticket.", t.Key))
	}
	c.comment(ctx, t.Key, body)
	c.setStatus(ctx, t.Key, ticketing.StatusToDo)
}

// reconcileMerged marks journal entries whose PRs have merged and moves
// those tickets to Done. Done therefore means "merged into the base
// branch", which is what later tickets that build on this one need.
func (c *Coordinator) reconcileMerged(ctx context.Context) {
	if c.Journal == nil {
		return
	}
	merged, err := c.Journal.Reconcile(ctx, c.Repository.IsPRMerged)
	if err != nil {
		logger.Warn("Journal reconciliation failed", "error", err)
	}
	for _, key := range merged {
		logger.Info("PR merged; moving ticket to Done", "ticket", key)
		c.setStatus(ctx, key, ticketing.StatusDone)
	}
	if len(merged) > 0 {
		c.checkEpics(ctx)
	}
}

// agentStateDir holds intern's own files inside the target clone (journal,
// index). It is never discarded by discardLocalChanges.
const agentStateDir = ".ai-intern"

// discardLocalChanges resets the agent-owned clone to a clean state:
// tracked edits are reverted and untracked files removed, except intern's
// own state directory. A failed ticket can leave partial edits behind, and
// without this the next switch to the base branch fails or carries them
// into an unrelated ticket.
func discardLocalChanges(ctx context.Context, repoRoot string) error {
	out, err := runCommandCapture(ctx, repoRoot, "git", "status", "--porcelain", "--", ".", ":(exclude)"+agentStateDir)
	if err != nil {
		return fmt.Errorf("git status: %w: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	logger.Warn("Discarding leftover local changes in agent clone", "repo", repoRoot)
	if out, err := runCommandCapture(ctx, repoRoot, "git", "reset", "--hard"); err != nil {
		return fmt.Errorf("git reset: %w: %s", err, out)
	}
	if out, err := runCommandCapture(ctx, repoRoot, "git", "clean", "-fd", "-e", agentStateDir); err != nil {
		return fmt.Errorf("git clean: %w: %s", err, out)
	}
	return nil
}

func runCommandCapture(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
