package orchestrator

import (
	"context"
	"time"

	"ai-intern-agent/internal/ai"
	"ai-intern-agent/internal/config"
	"ai-intern-agent/internal/github"
	"ai-intern-agent/internal/jira"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Jira   jira.Client
	GitHub github.Client
	AI     ai.Client
	Cfg    *config.Config
	State  *State
}

func NewCoordinator(jira jira.Client, github github.Client, ai ai.Client, cfg *config.Config, state *State) *Coordinator {
	return &Coordinator{Jira: jira, GitHub: github, AI: ai, Cfg: cfg, State: state}
}

func (c *Coordinator) Run(ctx context.Context) {
	interval, err := time.ParseDuration(c.Cfg.PollingInterval)
	if err != nil {
		interval = 30 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			tickets, err := jira.GetAssignedTickets(ctx, c.Jira, c.Cfg.AgentUsername, c.Cfg.JiraProject)
			if err != nil {
				logger.Error("Failed to fetch tickets: %v", err)
				break
			}
			for _, t := range tickets {
				if c.State.IsProcessed(t.Key) {
					continue
				}
				logger.Info("Processing ticket: %s", t.Key)
				err = jira.UpdateTicketStatus(ctx, c.Jira, t.Key, "In Progress", c.Cfg.JiraTransitions)
				if err != nil {
					logger.Error("Failed to update ticket status: %v", err)
					break
				}
				// Placeholder: implement full workflow (branch, generate, commit, PR, etc.)
				c.State.MarkProcessed(t.Key)
			}
			time.Sleep(interval)
		}
	}
}
