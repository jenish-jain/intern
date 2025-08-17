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
	AI         ai.Client
	Cfg        *config.Config
	State      *State
}

func NewCoordinator(ticketing *ticketing.TicketingService, repository *repository.RepositoryService, ai ai.Client, cfg *config.Config, state *State) *Coordinator {
	return &Coordinator{Ticketing: ticketing, Repository: repository, AI: ai, Cfg: cfg, State: state}
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
	_ = c.Repository.SwitchBranch(ctx, "main")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			tickets, err := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
			logger.Debug("fetched tickets from JIRA in coordinator", "tickets", tickets)
			logger.Debug("error in fetching tickets from JIRA in coordinator", "err", err)
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
				logger.Debug("ticket in coordinator", "ticket", t)
				logger.Debug("state in coordinator", "state", c.State)
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

				// Create a dummy file
				dummyFilePath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo, "test.md")
				_ = os.WriteFile(dummyFilePath, []byte(fmt.Sprintf("This is a test file for ticket %s", t.Key)), 0644)
				logger.Info("Added dummy file %s", dummyFilePath)

				// Add file and commit
				_ = c.Repository.AddFile(ctx, "test.md") // Add file relative to repo root
				commitMessage := fmt.Sprintf("feat(%s): Add dummy file for %s", t.Key, t.Key)
				_ = c.Repository.Commit(ctx, commitMessage)

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
				}

				logger.Info("Completed basic workflow for ticket %s", t.Key)
				c.State.MarkProcessed(t.Key)
			}

			time.Sleep(interval)
		}
	}
}
