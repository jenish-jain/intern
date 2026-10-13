package orchestrator

import (
	"context"
	"errors"
	"fmt"

	logger "github.com/jenish-jain/logger"
)

// Notifier posts a message where the team will see it, such as a Slack
// channel.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// ThreadNotifier is a Notifier that can also reply in an architect
// conversation's thread.
type ThreadNotifier interface {
	NotifyConversation(ctx context.Context, convID, text string) error
}

// notifyConversation replies in the conversation's thread when the
// notifier supports it, and falls back to the channel otherwise.
func (c *Coordinator) notifyConversation(ctx context.Context, convID, text string) {
	if tn, ok := c.Notifier.(ThreadNotifier); ok && convID != "" {
		err := tn.NotifyConversation(ctx, convID, text)
		if err == nil {
			return
		}
		logger.Warn("Failed to post in conversation thread", "conversation", convID, "error", err)
	}
	c.notify(ctx, text)
}

// notify sends text through the configured notifier, if any.
func (c *Coordinator) notify(ctx context.Context, text string) {
	if c.Notifier == nil {
		return
	}
	if err := c.Notifier.Notify(ctx, text); err != nil {
		logger.Warn("Failed to send notification", "error", err)
	}
}

// HasConversation reports whether an architect conversation exists.
func (c *Coordinator) HasConversation(id string) bool {
	_, ok := c.State.GetConversation(id)
	return ok
}

// PRURLFor returns the PR opened for a ticket, or "".
func (c *Coordinator) PRURLFor(key string) string {
	if c.Journal == nil {
		return ""
	}
	if e, ok := c.Journal.Find(key); ok {
		return e.PRURL
	}
	return ""
}

// flowStepFailed logs a failed review, fix, test or merge step and
// announces it in Slack the first time a PR's step fails with a given
// error, so a stuck PR is visible without reading logs. An outage is
// announced by runModel instead.
func (c *Coordinator) flowStepFailed(ctx context.Context, f Flow, err error) {
	logger.Error("Flow step failed", "ticket", f.TicketKey, "stage", f.Stage, "error", err)
	var outage *ModelUnavailableError
	if errors.As(err, &outage) {
		return
	}
	msg := truncateNotes(err.Error(), 300)
	if _, seen := c.notified.LoadOrStore(f.TicketKey+"|"+string(f.Stage)+"|"+msg, true); seen {
		return
	}
	c.notify(ctx, fmt.Sprintf(":warning: %s's %s step failed and will be retried on the next poll: %s\n%s", f.TicketKey, f.Stage, msg, f.PRURL))
}
