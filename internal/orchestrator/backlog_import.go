package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"intern/internal/backlog"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

// ImportResult reports what an import created.
type ImportResult struct {
	// Created maps backlog IDs to new ticket keys, in creation order.
	Created []ImportedTicket
	// Existing is how many backlog tickets were already imported.
	Existing int
}

// ImportedTicket is one created ticket.
type ImportedTicket struct {
	ID  string
	Key string
}

// ImportBacklog creates a ticket for every backlog item in repoRoot that
// hasn't been imported, assigned to the agent, in dependency order, with
// "blocks" links. The ID-to-key mapping is saved after each ticket, so an
// interrupted import resumes without duplicates.
func (c *Coordinator) ImportBacklog(ctx context.Context, repoRoot string) (ImportResult, error) {
	var res ImportResult
	b, found, err := backlog.Load(repoRoot)
	if err != nil {
		return res, err
	}
	if !found {
		return res, fmt.Errorf("no %s in the repository", backlog.Path)
	}
	known := c.State.BacklogKeyMap()
	if err := b.Validate(known); err != nil {
		return res, err
	}
	order, err := b.Order(known)
	if err != nil {
		return res, err
	}
	res.Existing = len(b.Tickets()) - len(order)
	if len(order) == 0 {
		return res, nil
	}

	assignee, err := c.agentAccountID(ctx)
	if err != nil {
		return res, err
	}
	issueType := c.Cfg.JiraIssueType
	if issueType == "" {
		issueType = "Task"
	}
	for _, t := range order {
		key, err := c.Ticketing.CreateTicket(ctx, ticketing.NewTicket{
			Project:     c.Cfg.JiraProject,
			Summary:     t.Title,
			Description: t.TicketDescription(),
			IssueType:   issueType,
			Labels:      []string{"intern", "epic-" + strings.ToLower(t.Epic)},
			AssigneeID:  assignee,
		})
		if err != nil {
			return res, fmt.Errorf("create ticket %s: %w", t.ID, err)
		}
		c.State.SetBacklogKey(t.ID, key)
		known[t.ID] = key
		res.Created = append(res.Created, ImportedTicket{ID: t.ID, Key: key})
		logger.Info("Imported backlog ticket", "id", t.ID, "key", key)

		for _, dep := range t.BlockedBy {
			if err := c.Ticketing.LinkBlocks(ctx, known[dep], key); err != nil {
				return res, fmt.Errorf("link %s blocks %s: %w", dep, t.ID, err)
			}
		}
	}
	return res, nil
}

// agentAccountID returns the user ID imported tickets are assigned to.
func (c *Coordinator) agentAccountID(ctx context.Context) (string, error) {
	if c.Cfg.JiraAgentAccountID != "" {
		return c.Cfg.JiraAgentAccountID, nil
	}
	id, err := c.Ticketing.FindUserID(ctx, c.Cfg.AgentUsername)
	if err != nil {
		return "", fmt.Errorf("resolve agent user %q (set JIRA_AGENT_ACCOUNT_ID to skip the lookup): %w", c.Cfg.AgentUsername, err)
	}
	return id, nil
}
