package orchestrator

import (
	"context"

	logger "github.com/jenish-jain/logger"
)

// Notifier posts a message where the team will see it, such as a Slack
// channel.
type Notifier interface {
	Notify(ctx context.Context, text string) error
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
