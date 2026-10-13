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
