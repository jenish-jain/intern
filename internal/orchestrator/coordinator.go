package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

	workingDir := c.Cfg.WorkingDir
	if workingDir == "" {
		workingDir = "./workspace"
	}
	_ = os.MkdirAll(workingDir, 0755)
	_ = os.Setenv("AGENT_WORKING_DIR", workingDir)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Ensure local repo is up to date before each cycle
			if err := c.prepareRepository(ctx); err != nil {
				logger.Error("Repository preparation failed: %v", err)
				backoffSleep(interval)
				continue
			}

			tickets, err := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
			if err != nil {
				logger.Error("Failed to fetch tickets: %v", err)
				backoffSleep(interval)
				continue
			}
			if len(tickets) == 0 {
				logger.Info("No tickets to process; sleeping %s", interval.String())
				time.Sleep(interval)
				continue
			}

			maxWorkers := c.Cfg.MaxConcurrentTickets
			if maxWorkers <= 0 {
				maxWorkers = 1
			}
			sem := make(chan struct{}, maxWorkers)
			var wg sync.WaitGroup
			for _, t := range tickets {
				if c.State.IsProcessed(t.Key) {
					continue
				}
				sem <- struct{}{}
				wg.Add(1)
				go func(key, summary, description string) {
					defer wg.Done()
					defer func() { <-sem }()
					if err := c.processTicket(ctx, key, summary, description); err != nil {
						logger.Error("Failed processing %s: %v", key, err)
						return
					}
					c.State.MarkProcessed(key)
				}(t.Key, t.Summary, t.Description)
			}
			wg.Wait()
			time.Sleep(interval)
		}
	}
}

func backoffSleep(base time.Duration) {
	t := base
	if t < time.Second*5 {
		t = time.Second * 5
	}
	time.Sleep(t)
}

func (c *Coordinator) prepareRepository(ctx context.Context) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo)
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
		logger.Info("Cloning repository...")
		if err := c.Repository.CloneRepository(ctx, repoPath); err != nil {
			return err
		}
	}
	base := c.Cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	_ = c.Repository.SwitchBranch(ctx, base)
	if err := c.Repository.SyncWithRemote(ctx); err != nil {
		logger.Error("Sync failed: %v", err)
	}
	return nil
}

func (c *Coordinator) processTicket(ctx context.Context, key, summary, description string) error {
	branchName := buildBranchName(c.Cfg.BranchPrefix, key)
	logger.Info("Creating branch %s", branchName)
	if err := c.Repository.CreateBranch(ctx, branchName); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}
	_ = c.Repository.SwitchBranch(ctx, branchName)

	repoRoot := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo)
	ctxStr := ai.BuildRepoContext(repoRoot, c.Cfg.ContextMaxFiles, c.Cfg.ContextMaxBytes)
	logger.Debug("context string", "ctxStr", ctxStr)
	changes, planErr := c.Agent.PlanChanges(ctx, key, summary, description, ctxStr)
	if planErr != nil {
		return fmt.Errorf("AI planning failed: %w", planErr)
	}
	valid, verr := validatePlannedChanges(repoRoot, changes, c.Cfg.AllowedWriteDirs, c.Cfg.PlanMaxFiles)
	if verr != nil {
		return fmt.Errorf("validation failed: %w", verr)
	}
	for _, ch := range valid {
		abs := filepath.Join(repoRoot, ch.Path)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(abs, []byte(ch.Content), 0644); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
	}
	if len(valid) > 0 {
		if err := c.Repository.Commit(ctx, fmt.Sprintf("feat(%s): apply planned changes", key)); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
	}
	changed, err := c.Repository.HasLocalChanges(ctx)
	if err != nil {
		logger.Error("status failed: %v", err)
	}
	if !changed && len(valid) == 0 {
		logger.Info("No effective changes for %s; skipping push/PR", key)
		return nil
	}
	if err := c.Repository.Push(ctx, branchName); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	base := c.Cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	title := buildPRTitle(key, summary)
	body := buildPRBody(key, summary, description, valid, nil)
	prURL, prErr := c.Repository.CreatePullRequest(ctx, base, branchName, title, body)
	if prErr != nil {
		return fmt.Errorf("create PR: %w", prErr)
	}
	logger.Info("Created PR: %s", prURL)
	// Mark Done
	if err := c.Ticketing.UpdateTicketStatus(ctx, key, "Done", c.Cfg.JiraTransitions); err != nil {
		logger.Error("Failed to move ticket to Done: %v", err)
	}
	return nil
}
