package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"intern/internal/ai"
	"intern/internal/ai/agent"
	"intern/internal/config"
	"intern/internal/indexer"
	"intern/internal/repository"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Ticketing  *ticketing.TicketingService
	Repository *repository.RepositoryService
	Agent      agent.Agent
	Cfg        *config.Config
	State      *State
	Metrics    *Metrics
}

func NewCoordinator(ticketing *ticketing.TicketingService, repository *repository.RepositoryService, agent agent.Agent, cfg *config.Config, state *State) *Coordinator {
	return &Coordinator{Ticketing: ticketing, Repository: repository, Agent: agent, Cfg: cfg, State: state, Metrics: NewMetrics()}
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

	// Start metrics server if enabled
	if c.Cfg.MetricsEnabled {
		metricsServer := NewMetricsServer(c.Metrics, c.Cfg.MetricsPort)
		go func() {
			if err := metricsServer.Start(ctx); err != nil {
				logger.Error("Metrics server failed", "error", err)
			}
		}()
	}

	// Print final summary and save metrics on shutdown
	defer func() {
		snapshot := c.Metrics.Snapshot()

		// Skip if no tickets were processed
		if snapshot.TicketsProcessed == 0 {
			logger.Info("Agent shutting down (no tickets processed)")
			return
		}

		logger.Info("Agent shutting down - generating final report")

		// Print summary report to console
		report := GenerateReport(snapshot)
		fmt.Println("\n" + report)

		// Save metrics to JSON
		repoRoot := filepath.Join(workingDir, c.Cfg.GitHubRepo)
		// Note: We don't have access to individual ticket metrics here yet
		// This will be enhanced in a future iteration to collect them
		metricsFile, err := SaveMetrics(snapshot, []TicketMetrics{}, repoRoot)
		if err != nil {
			logger.Error("Failed to save metrics", "error", err)
		} else {
			fmt.Printf("\nDetailed metrics saved to: %s\n\n", metricsFile)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Ensure local repo is up to date before each cycle
			if err := c.prepareRepository(ctx); err != nil {
				logger.Error("Repository preparation failed", "error", err)
				backoffSleep(interval)
				continue
			}

			tickets, err := func() ([]ticketing.Ticket, error) {
				var out []ticketing.Ticket
				err, attempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
					t, e := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
					if e != nil {
						return MakeTransient(e)
					}
					out = t
					return nil
				})
				c.Metrics.AddRetries(attempts)
				return out, err
			}()
			if err != nil {
				logger.Error("Failed to fetch tickets", "error", err)
				backoffSleep(interval)
				continue
			}
			if len(tickets) == 0 {
				logger.Info("No tickets to process; sleeping", "interval", interval.String())
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
						logger.Error("Failed processing ticket", "key", key, "error", err)
						return
					}
					c.State.MarkProcessed(key)
				}(t.Key, t.Summary, t.Description)
			}
			wg.Wait()
			// log metrics summary
			s := c.Metrics.Snapshot()
			logger.Info("Run summary", "tickets", s.TicketsProcessed, "prs", s.PRsCreated, "retries", s.Retries, "ai_failures", s.AIPlanFailures)
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
		logger.Error("Sync failed", "error", err)
	}
	return nil
}

func (c *Coordinator) processTicket(ctx context.Context, key, summary, description string) error {
	startTime := time.Now()

	branchName := buildBranchName(c.Cfg.BranchPrefix, key)
	logger.Info("Creating branch", "branch", branchName)
	if err := c.Repository.CreateBranch(ctx, branchName); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}
	_ = c.Repository.SwitchBranch(ctx, branchName)

	repoRoot := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.Cfg.GitHubRepo)

	// Build or update index for smart context selection
	idx := indexer.New(repoRoot)
	fileIndex, wasUpdated, indexErr := idx.RebuildIfStale()
	if indexErr != nil {
		logger.Warn("Failed to build/update index, smart context may fall back to simple", "error", indexErr)
	} else {
		if wasUpdated {
			logger.Info("Index built/updated successfully", "files", len(fileIndex.Files))
			// Save the updated index
			if saveErr := idx.SaveIndex(fileIndex); saveErr != nil {
				logger.Warn("Failed to save index", "error", saveErr)
			}
		} else {
			logger.Debug("Index already up to date")
		}
	}

	// Use smart context builder with ticket description for better file selection
	usedSmartContext := false
	ctxStr, ctxErr := ai.BuildSmartRepoContext(repoRoot, description, c.Cfg.ContextMaxFiles)
	if ctxErr != nil {
		// Fall back to simple context builder on error
		logger.Warn("Smart context builder failed, falling back to simple builder", "error", ctxErr)
		ctxStr = ai.BuildRepoContext(repoRoot, c.Cfg.ContextMaxFiles, c.Cfg.ContextMaxBytes)
		c.Metrics.IncSimpleContextUsed()
	} else {
		usedSmartContext = true
		logger.Info("Smart context selection succeeded", "context_size", len(ctxStr))
		c.Metrics.IncSmartContextUsed()
	}

	var changes []agent.CodeChange
	var usageMetrics *agent.UsageMetrics
	planErr, attempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		ch, metrics, e := c.Agent.PlanChanges(ctx, key, summary, description, ctxStr)
		if e != nil {
			return MakeTransient(e)
		}
		changes = ch
		usageMetrics = metrics
		return nil
	})
	c.Metrics.AddRetries(attempts)
	if planErr != nil {
		c.Metrics.IncAIPlanFailures()
		return fmt.Errorf("AI planning failed: %w", planErr)
	}

	// Update context strategy in usage metrics
	if usageMetrics != nil {
		if usedSmartContext {
			usageMetrics.ContextStats.Strategy = "smart"
		} else {
			usageMetrics.ContextStats.Strategy = "simple"
		}
	}

	// Create per-ticket metrics for tracking
	ticketMetrics := NewTicketMetricsFromUsage(key, usageMetrics)
	ticketMetrics.SetRetryCount(attempts)

	// Estimate savings if smart context was used
	if usageMetrics != nil && usedSmartContext {
		// Estimate what full context would have cost
		// Rough approximation: assume full context would be 3x larger
		// (based on typical repository file counts vs selected files)
		estimatedFullContextTokens := usageMetrics.InputTokens * 3
		estimatedFullContextCost := ai.CalculateCost(estimatedFullContextTokens, usageMetrics.OutputTokens, &ai.ClaudeSonnet4)

		// Save savings estimate
		ticketMetrics.SetSavingsEstimate(estimatedFullContextCost)

		logger.Debug("Smart context savings",
			"ticket", key,
			"estimated_full_cost", ai.FormatCost(estimatedFullContextCost),
			"actual_cost", ai.FormatCost(usageMetrics.EstimatedCost),
			"savings", ai.FormatCost(ticketMetrics.CostSavings))
	}

	// Update global metrics with token usage
	if usageMetrics != nil {
		c.Metrics.AddTokenUsage(
			usageMetrics.InputTokens,
			usageMetrics.OutputTokens,
			usageMetrics.EstimatedCost,
		)

		// Log per-ticket cost and metrics
		logger.Info("AI generated code",
			"ticket", key,
			"cost", ai.FormatCost(usageMetrics.EstimatedCost),
			"input_tokens", ai.FormatTokens(usageMetrics.InputTokens),
			"output_tokens", ai.FormatTokens(usageMetrics.OutputTokens),
			"context", usageMetrics.ContextStats.Strategy)
	}
	valid, verr := validatePlannedChanges(repoRoot, changes, c.Cfg.AllowedWriteDirs, c.Cfg.PlanMaxFiles)
	if verr != nil {
		return fmt.Errorf("validation failed: %w", verr)
	}
	for _, ch := range valid {
		abs := filepath.Join(repoRoot, ch.Path)
		if ch.Operation == agent.OperationDelete {
			// Delete the file
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", ch.Path, err)
			}
			// Stage the deletion in git
			if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
				return fmt.Errorf("git add (delete) %s: %w", ch.Path, err)
			}
			logger.Debug("Deleted file", "path", ch.Path)
		} else {
			// Create or update the file
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := os.WriteFile(abs, []byte(ch.Content), 0644); err != nil {
				return fmt.Errorf("write: %w", err)
			}
			if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
				return fmt.Errorf("git add %s: %w", ch.Path, err)
			}
		}
	}
	if len(valid) > 0 {
		if err := c.Repository.Commit(ctx, fmt.Sprintf("feat(%s): apply planned changes", key)); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
	}
	changed, err := c.Repository.HasLocalChanges(ctx)
	if err != nil {
		logger.Error("status failed", "error", err)
	}
	if !changed && len(valid) == 0 {
		logger.Info("No effective changes; skipping push/PR", "key", key)
		return nil
	}

	// Run self-healing pipeline (includes quality gates)
	healResult, err := c.selfHealingPipeline(ctx, key, summary, valid, repoRoot)
	if err != nil {
		logger.Error("Self-healing pipeline failed", "key", key, "error", err)
		return fmt.Errorf("self-healing failed: %w", err)
	}

	// Track healing metrics
	if len(healResult.Attempts) > 0 {
		c.Metrics.AddHealAttempts(len(healResult.Attempts))
		if healResult.Success {
			c.Metrics.IncHealSuccesses()
		} else {
			c.Metrics.IncHealFailures()
		}
	}

	// If healing failed, skip push/PR
	if !healResult.Success {
		logger.Error("Quality gates failed after healing attempts; skipping push/PR",
			"key", key,
			"attempts", healResult.TotalAttempts,
			"cost", healResult.TotalCost)
		return nil
	}

	// If healing was needed and succeeded, commit the fixes
	if len(healResult.Attempts) > 0 {
		if err := c.Repository.Commit(ctx, fmt.Sprintf("fix(%s): self-healing fixes after %d attempts", key, healResult.TotalAttempts)); err != nil {
			logger.Warn("Failed to commit healing fixes", "error", err)
			// Continue anyway - fixes are already applied
		}
		logger.Info("Self-healing succeeded, fixes committed",
			"key", key,
			"attempts", healResult.TotalAttempts,
			"cost", healResult.TotalCost)
	}

	// Run final quality gates check for PR notes (should pass now)
	notes, ok := runQualityGates(ctx, c.Cfg, repoRoot)
	if !ok {
		// This shouldn't happen after successful healing, but check anyway
		logger.Error("Quality gates failed after successful healing; skipping push/PR", "key", key)
		return nil
	}
	// Check for dry-run mode
	if c.Cfg.DryRun {
		logger.Warn("DRY RUN MODE: Skipping push and PR creation",
			"ticket", key,
			"branch", branchName,
			"files_changed", len(valid),
			"cost", usageMetrics.EstimatedCost)

		// In dry-run, log what would have been done
		logger.Info("DRY RUN: Would have created PR",
			"ticket", key,
			"branch", branchName,
			"base_branch", c.Cfg.BaseBranch,
			"files", len(valid),
			"summary", summary)

		// Mark Done even in dry-run (to avoid reprocessing)
		if err := c.Ticketing.UpdateTicketStatus(ctx, key, "Done", c.Cfg.JiraTransitions); err != nil {
			logger.Error("Failed to move ticket to Done", "error", err)
		}

		// Update state to avoid reprocessing
		// MarkProcessed automatically saves the state, so no need to call Save() explicitly
		c.State.MarkProcessed(key)

		return nil // Exit early without creating PR
	}

	if err := c.Repository.Push(ctx, branchName); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	base := c.Cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	title := buildPRTitle(key, summary)
	body := buildPRBody(key, summary, description, valid, notes)
	var prURL string
	prErr, prAttempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		u, e := c.Repository.CreatePullRequest(ctx, base, branchName, title, body)
		if e != nil {
			return MakeTransient(e)
		}
		prURL = u
		return nil
	})
	c.Metrics.AddRetries(prAttempts)
	if prErr != nil {
		return fmt.Errorf("create PR: %w", prErr)
	}
	logger.Info("Created PR", "url", prURL)
	c.Metrics.IncPRsCreated()

	// Mark Done
	if err := c.Ticketing.UpdateTicketStatus(ctx, key, "Done", c.Cfg.JiraTransitions); err != nil {
		logger.Error("Failed to move ticket to Done", "error", err)
	}

	// Update metrics with execution time and files changed
	executionTime := time.Since(startTime)
	filesChanged := len(valid)

	ticketMetrics.SetExecutionTime(executionTime)
	ticketMetrics.SetFilesChanged(filesChanged)

	c.Metrics.IncTicketsProcessed()
	c.Metrics.AddExecutionTime(executionTime)
	c.Metrics.AddFilesChanged(filesChanged)

	// Log final ticket summary
	logger.Info("Ticket completed",
		"ticket", key,
		"duration", executionTime.Round(time.Second),
		"files_changed", filesChanged,
		"pr_url", prURL)

	return nil
}
