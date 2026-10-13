package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"intern/internal/backlog"

	logger "github.com/jenish-jain/logger"
)

// checkEpics announces epics whose tickets have all merged. When the
// project has a planning thread and EPIC_REVIEW is on, the architect
// reviews the merged epic against the architecture and replies there,
// proposing follow-up tickets if needed. Must hold the repository lock.
func (c *Coordinator) checkEpics(ctx context.Context) {
	if c.Journal == nil || c.RepoPaths == nil {
		return
	}
	b, found, err := backlog.Load(c.RepoPaths.Root())
	if err != nil || !found {
		return
	}
	keys := c.State.BacklogKeyMap()
	for _, e := range b.Epics {
		if c.State.EpicCompleted(e.ID) || len(e.Tickets) == 0 {
			continue
		}
		var merged []string
		complete := true
		for _, t := range e.Tickets {
			key := keys[t.ID]
			entry, ok := c.Journal.Find(key)
			if key == "" || !ok || !entry.Merged {
				complete = false
				break
			}
			merged = append(merged, key)
		}
		if !complete || !c.State.MarkEpicComplete(e.ID) {
			continue
		}
		announce := fmt.Sprintf(":tada: Epic %s (%s) is complete: %s merged.", e.ID, e.Title, strings.Join(merged, ", "))
		logger.Info("Epic complete", "epic", e.ID)
		c.notify(ctx, announce)

		convID := c.State.ProjectConversationID()
		if convID == "" || !c.Cfg.EpicReview || c.Cfg.AIRunner != "claude-code" || c.budgetExceeded() {
			continue
		}
		msg := fmt.Sprintf("Epic %s (%s) is complete: tickets %s have merged into the base branch. "+
			"Review the merged code for this epic against docs/architecture.md and the ADRs. "+
			"If it drifted from the architecture or left gaps against the PRD, add follow-up tickets to the backlog and explain why. "+
			"If it is on track, say so briefly and change nothing.", e.ID, e.Title, strings.Join(merged, ", "))
		reply, err := c.converse(ctx, convID, msg)
		if err != nil {
			logger.Warn("Epic review failed", "epic", e.ID, "error", err)
			continue
		}
		c.notifyConversation(ctx, convID, announce+" Please try it and tell me what to change.\n\n*Architecture check*\n"+reply)
	}
}
