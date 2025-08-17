package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"intern/internal/ai"
	"intern/internal/config"
	"intern/internal/repository"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Ticketing  *ticketing.TicketingService
	Repository *repository.RepositoryService
	Agent      ai.Agent
	Cfg        *config.Config
	State      *State
}

func NewCoordinator(ticketing *ticketing.TicketingService, repository *repository.RepositoryService, agent ai.Agent, cfg *config.Config, state *State) *Coordinator {
	return &Coordinator{Ticketing: ticketing, Repository: repository, Agent: agent, Cfg: cfg, State: state}
}

func (c *Coordinator) Run(ctx context.Context) {
	interval, err := time.ParseDuration(c.Cfg.PollingInterval)
	if err != nil {
		interval = 30 * time.Second
	}

	// Ensure working directory exists
	workingDir := "./workspace" // TODO: Make configurable via Cfg.Agent.WorkingDir
	_ = os.MkdirAll(workingDir, 0755)
	_ = os.Setenv("AGENT_WORKING_DIR", workingDir)

	// Initial repository setup (clone or sync)
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo) // Assuming repo name is Cfg.GitHubRepo
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
		logger.Info("Cloning repository...")
		if err := c.Repository.CloneRepository(ctx, repoPath); err != nil {
			logger.Error("Failed to clone repository: %v", err)
			return // Exit if initial clone fails
		}
	} else {
		logger.Info("Syncing repository...")
		if err := c.Repository.SyncWithRemote(ctx); err != nil {
			logger.Error("Failed to sync repository: %v", err)
		}
	}
	// Try to switch to base branch before feature work (best-effort)
	_ = c.Repository.SwitchBranch(ctx, "master")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			tickets, err := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
			if err != nil {
				logger.Error("Failed to fetch tickets: %v", err)
				time.Sleep(interval)
				continue
			}

			if len(tickets) == 0 {
				logger.Info("No tickets to process; sleeping %s", interval.String())
				time.Sleep(interval)
				continue
			}

			for _, t := range tickets {
				if c.State.IsProcessed(t.Key) {
					continue
				}
				logger.Info("Processing ticket: %s", t.Key)
				if err := c.Ticketing.UpdateTicketStatus(ctx, t.Key, "In Progress", c.Cfg.JiraTransitions); err != nil {
					logger.Error("Failed to update ticket status: %v", err)
					continue // Continue to next ticket if status update fails
				}

				// Implement basic workflow steps
				branchName := fmt.Sprintf("feature/%s", t.Key)
				logger.Info("Creating branch %s", branchName)
				if err := c.Repository.CreateBranch(ctx, branchName); err != nil {
					logger.Error("Failed to create branch: %v", err)
					_ = c.Ticketing.UpdateTicketStatus(ctx, t.Key, "To Do", c.Cfg.JiraTransitions) // Revert status
					continue
				}
				_ = c.Repository.SwitchBranch(ctx, branchName) // Ignore error for now

				// Build AI context and plan changes (minimal)
				repoRoot := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo)
				ctxStr := ai.BuildRepoContext(repoRoot, 50, 32*1024)
				changes, planErr := c.Agent.PlanChanges(ctx, t.Key, t.Summary, t.Description, ctxStr)
				if planErr != nil {
					logger.Error("AI planning failed: %v", planErr)
				} else {
					// Materialize changes
					for _, ch := range changes {
						abs := filepath.Join(repoRoot, ch.Path)
						// Ensure parent dir
						_ = os.MkdirAll(filepath.Dir(abs), 0755)
						// Write full content
						_ = os.WriteFile(abs, []byte(ch.Content), 0644)
						_ = c.Repository.AddFile(ctx, ch.Path)
					}
					_ = c.Repository.Commit(ctx, fmt.Sprintf("feat(%s): apply planned changes", t.Key))
				}

				// Push the branch
				_ = c.Repository.Push(ctx, branchName)

				// Create a PR against main
				title := fmt.Sprintf("%s: %s", t.Key, t.Summary)
				body := fmt.Sprintf("Automated changes for %s\n\nTicket: %s\n\nDescription:\n%s", t.Key, t.Key, t.Description)
				prURL, prErr := c.Repository.CreatePullRequest(ctx, "main", branchName, title, body)
				if prErr != nil {
					logger.Error("Failed to create PR: %v", prErr)
				} else {
					logger.Info("Created PR: %s", prURL)
					// Update JIRA status to Done after successful PR creation
					if err := c.Ticketing.UpdateTicketStatus(ctx, t.Key, "Done", c.Cfg.JiraTransitions); err != nil {
						logger.Error("Failed to move ticket to Done: %v", err)
					}
				}

				logger.Info("Completed basic workflow for ticket %s", t.Key)
				c.State.MarkProcessed(t.Key)
			}

			time.Sleep(interval)
		}
	}
}
