package orchestrator

import (
	"context"
	"time"

	"intern/internal/ai"
	"intern/internal/config"
	"intern/internal/github"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Ticketing *ticketing.TicketingService
	GitHub    github.Client
	AI        ai.Client
	Cfg       *config.Config
	State     *State
}

func NewCoordinator(ticketing *ticketing.TicketingService, github github.Client, ai ai.Client, cfg *config.Config, state *State) *Coordinator {
	return &Coordinator{Ticketing: ticketing, GitHub: github, AI: ai, Cfg: cfg, State: state}
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
			tickets, err := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
			if err != nil {
				logger.Error("Failed to fetch tickets: %v", err)
				break
			}
			for _, t := range tickets {
				if c.State.IsProcessed(t.Key) {
					continue
				}
				logger.Info("Processing ticket: %s", t.Key)
				err = c.Ticketing.UpdateTicketStatus(ctx, t.Key, "In Progress", c.Cfg.JiraTransitions)
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
