package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"intern/internal/ai"
	"intern/internal/ai/agent"
	"intern/internal/config"
	"intern/internal/errors"
	"intern/internal/indexer"
	"intern/internal/journal"
	"intern/internal/repository"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Ticketing  *ticketing.Service
	Repository *repository.RepositoryService
	Agent      agent.Agent
	Cfg        *config.Config
	State      *State
	Metrics    *Metrics
	RepoPaths  *repository.RepositoryPath // Centralized path management
	Journal    *journal.Journal           // Cross-ticket continuity log

	ticketMetricsMu sync.Mutex
	ticketMetrics   map[string]*TicketMetrics // last-known metrics per ticket key, for request-driven callers (see LastTicketMetrics)
}

func NewCoordinator(ticketing *ticketing.Service, repository *repository.RepositoryService, agent agent.Agent, cfg *config.Config, state *State, repoPaths *repository.RepositoryPath) *Coordinator {
	return &Coordinator{
		Ticketing:     ticketing,
		Repository:    repository,
		Agent:         agent,
		Cfg:           cfg,
		State:         state,
		Metrics:       NewMetrics(),
		RepoPaths:     repoPaths,
		Journal:       journal.Load(repoPaths.Root()),
		ticketMetrics: make(map[string]*TicketMetrics),
	}
}

// LastTicketMetrics returns the most recently recorded metrics for a ticket
// key, if any. Populated as soon as a ticket starts processing and updated
// in place as AI usage data becomes available, so it's readable after
// ProcessTicket returns regardless of whether the ticket succeeded or
// failed partway through.
func (c *Coordinator) LastTicketMetrics(key string) (*TicketMetrics, bool) {
	c.ticketMetricsMu.Lock()
	defer c.ticketMetricsMu.Unlock()
	tm, ok := c.ticketMetrics[key]
	return tm, ok
}

func (c *Coordinator) storeTicketMetrics(key string, tm *TicketMetrics) {
	c.ticketMetricsMu.Lock()
	defer c.ticketMetricsMu.Unlock()
	c.ticketMetrics[key] = tm
}

func (c *Coordinator) Run(ctx context.Context) {
	interval, err := time.ParseDuration(c.Cfg.PollingInterval)
	if err != nil {
		interval = 30 * time.Second
	}

	// Ensure working directory exists
	_ = os.MkdirAll(c.RepoPaths.WorkingDir(), 0755)

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
		repoRoot := c.RepoPaths.Root()
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

			// Reconcile journal entries: flip Merged for PRs that landed since
			// the last cycle, so deferred tickets can unblock.
			if updated, err := c.Journal.Reconcile(ctx, c.Repository.IsPRMerged); err != nil {
				logger.Warn("Journal reconciliation failed", "error", err)
			} else if updated > 0 {
				logger.Info("Journal reconciliation: marked PRs as merged", "count", updated)
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
				if blocker := c.journalBlocker(t.Summary + " " + t.Description); blocker != "" {
					logger.Info("Deferring ticket - related work not yet merged", "ticket", t.Key, "waiting_on", blocker)
					continue
				}
				sem <- struct{}{}
				wg.Add(1)
				go func(key, summary, description string) {
					// Ensure cleanup happens even on panic
					defer wg.Done()
					defer func() { <-sem }()

					// Panic recovery - catch and log panics without crashing agent
					defer func() {
						if r := recover(); r != nil {
							logger.Error("Worker panic recovered", "ticket", key, "panic", r)
							c.Metrics.IncTicketsFailed()
							// Panic recovered - ticket will not be marked as processed
							// It will be retried in next polling cycle
						}
					}()

					// Process the ticket
					if err := c.processTicket(ctx, key, summary, description); err != nil {
						logger.Error("Failed processing ticket", "key", key, "error", err)
						c.Metrics.IncTicketsFailed()
						return
					}

					// Only mark as processed if successful
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

// checkContext checks if the context has been cancelled and returns an appropriate error.
// This allows for graceful shutdown at key checkpoints in ticket processing.
func checkContext(ctx context.Context, ticketKey, checkpoint string) error {
	select {
	case <-ctx.Done():
		logger.Info("Context cancelled, stopping ticket processing",
			"ticket", ticketKey,
			"checkpoint", checkpoint)
		return fmt.Errorf("context cancelled at %s: %w", checkpoint, ctx.Err())
	default:
		return nil
	}
}

// PrepareRepository clones/syncs the target repository. Exported for
// request-driven callers (e.g. the Slack webhook handler) that process a
// single ticket outside of Run()'s polling loop and need to ensure the repo
// is ready before ProcessTicket runs.
func (c *Coordinator) PrepareRepository(ctx context.Context) error {
	return c.prepareRepository(ctx)
}

// ProcessTicket runs the full ticket pipeline (branch, plan, apply, quality
// gates, push, PR, status update) for a single ticket. Exported so request-
// driven trigger sources (Slack webhook, Cloud Run handler) can invoke it
// directly for one ticket without going through Run()'s JIRA polling loop.
func (c *Coordinator) ProcessTicket(ctx context.Context, key, summary, description string) error {
	if err := c.processTicket(ctx, key, summary, description); err != nil {
		return err
	}
	c.State.MarkProcessed(key)
	return nil
}

func (c *Coordinator) prepareRepository(ctx context.Context) error {
	repoPath := c.RepoPaths.Root()
	if _, err := os.Stat(c.RepoPaths.GitDir()); os.IsNotExist(err) {
		logger.Info("Cloning repository...")
		if err := c.Repository.CloneRepository(ctx, repoPath); err != nil {
			return err
		}
	}
	base := c.Cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	if err := c.Repository.SwitchBranch(ctx, base); err != nil {
		// Switching to base branch is critical - we need to be on the right branch
		// before creating feature branches
		return errors.NewRepoBranchError(err, base, "switch to").
			WithContext("operation", "prepareRepository")
	}
	if err := c.Repository.SyncWithRemote(ctx); err != nil {
		// Log but don't fail - sync is best-effort
		// We can still work with slightly stale code
		logger.Warn("Sync with remote failed (continuing with local state)", "error", err)
	}
	return nil
}

func (c *Coordinator) processTicket(ctx context.Context, key, summary, description string) error {
	startTime := time.Now()

	// Recorded early and updated in place (rather than replaced) as the
	// pipeline progresses, so cost data is available via LastTicketMetrics
	// even if a later step fails - Status only flips to "success" if the
	// full pipeline completes.
	ticketMetrics := &TicketMetrics{TicketKey: key, Status: "failed", Timestamp: startTime}
	c.storeTicketMetrics(key, ticketMetrics)

	branchName := buildBranchName(c.Cfg.BranchPrefix, key)
	logger.Info("Creating branch", "branch", branchName)
	if err := c.Repository.CreateBranch(ctx, branchName); err != nil {
		return errors.NewRepoBranchError(err, branchName, "create").
			WithContext("ticket_key", key)
	}
	if err := c.Repository.SwitchBranch(ctx, branchName); err != nil {
		return errors.NewRepoBranchError(err, branchName, "switch to").
			WithContext("ticket_key", key)
	}

	// Checkpoint 1: Check for cancellation before expensive operations
	if err := checkContext(ctx, key, "after branch setup"); err != nil {
		return err
	}

	repoRoot := c.RepoPaths.Root()

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

	// Prior-work continuity: surface recent related tickets (and whether their
	// PRs have merged) so the model builds on existing work instead of
	// duplicating or contradicting it.
	priorWork := journal.Render(c.Journal.Relevant(summary+" "+description, 3))

	// Use smart context builder with ticket description for better file selection
	usedSmartContext := false
	ctxStr, ctxErr := ai.BuildSmartRepoContext(repoRoot, description, c.Cfg.ContextMaxFiles, nil)
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
	ctxStr = priorWork + ctxStr

	planBackoff := BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}

	var changes []agent.CodeChange
	var needFiles []string
	var usageMetrics *agent.UsageMetrics
	planErr, attempts := Retry(ctx, planBackoff, func() error {
		ch, nf, metrics, e := c.Agent.PlanChanges(ctx, key, summary, description, ctxStr)
		if e != nil {
			return MakeTransient(e)
		}
		changes = ch
		needFiles = nf
		usageMetrics = metrics
		return nil
	})
	c.Metrics.AddRetries(attempts)
	if planErr != nil {
		c.Metrics.IncAIPlanFailures()
		return fmt.Errorf("AI planning failed: %w", planErr)
	}

	// Retrieval pass: the model asked to see the full content of files shown
	// signatures-only (responded with {"need_files":[...]} instead of
	// changes). Rebuild context with those files promoted to the
	// full-content tier and plan again - this is cheap since a need_files
	// response is just a short list. Bounded and iterative: each round's
	// need_files accumulate on top of prior rounds', so a model that still
	// needs another file after seeing the first batch can ask again instead
	// of being forced to guess at content it was never shown.
	const maxRetrievalRounds = 3
	fullContentFiles := needFiles
	for round := 0; len(fullContentFiles) > 0 && usedSmartContext && round < maxRetrievalRounds; round++ {
		logger.Info("AI requested full content for additional files", "ticket", key, "files", fullContentFiles, "round", round+1)

		ctxStr2, ctxErr2 := ai.BuildSmartRepoContext(repoRoot, description, c.Cfg.ContextMaxFiles, fullContentFiles)
		if ctxErr2 != nil {
			logger.Warn("Failed to rebuild context for requested files, proceeding without further retrieval",
				"ticket", key, "error", ctxErr2)
			break
		}
		ctxStr = priorWork + ctxStr2

		var changes2 []agent.CodeChange
		var needFiles2 []string
		var usageMetrics2 *agent.UsageMetrics
		planErr2, attempts2 := Retry(ctx, planBackoff, func() error {
			ch, nf, metrics, e := c.Agent.PlanChanges(ctx, key, summary, description, ctxStr)
			if e != nil {
				return MakeTransient(e)
			}
			changes2 = ch
			needFiles2 = nf
			usageMetrics2 = metrics
			return nil
		})
		c.Metrics.AddRetries(attempts2)
		if planErr2 != nil {
			c.Metrics.IncAIPlanFailures()
			return fmt.Errorf("AI planning failed (retrieval pass): %w", planErr2)
		}
		changes = changes2
		usageMetrics = sumUsageMetrics(usageMetrics, usageMetrics2)

		fullContentFiles = mergeUnique(fullContentFiles, needFiles2)
	}

	// Checkpoint 2: Check for cancellation after AI planning (expensive operation)
	if err := checkContext(ctx, key, "after AI planning"); err != nil {
		return err
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
	ticketMetrics.ApplyUsage(usageMetrics)
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

	// Snapshot exported APIs of edited Go files before applying changes, so
	// the journal entry can record which public APIs are new (see
	// diffExportedAPIs below).
	beforeAPIs := capturePublicAPIs(repoRoot, valid)

	for _, ch := range valid {
		abs := filepath.Join(repoRoot, ch.Path)
		switch ch.Operation {
		case agent.OperationDelete:
			// Delete the file
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", ch.Path, err)
			}
			// Stage the deletion in git
			if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
				return fmt.Errorf("git add (delete) %s: %w", ch.Path, err)
			}
			logger.Debug("Deleted file", "path", ch.Path)
		case agent.OperationEdit:
			if err := applyEditChange(repoRoot, ch); err != nil {
				return fmt.Errorf("edit %s: %w", ch.Path, err)
			}
			if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
				return fmt.Errorf("git add %s: %w", ch.Path, err)
			}
			logger.Debug("Edited file", "path", ch.Path)
		case agent.OperationCreate:
			if _, err := os.Stat(abs); err == nil {
				return fmt.Errorf("create %s: file already exists (use operation=edit)", ch.Path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", ch.Path, err)
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := os.WriteFile(abs, []byte(ch.Content), 0644); err != nil {
				return fmt.Errorf("write %s: %w", ch.Path, err)
			}
			if err := c.Repository.AddFile(ctx, ch.Path); err != nil {
				return fmt.Errorf("git add %s: %w", ch.Path, err)
			}
			logger.Debug("Created file", "path", ch.Path)
		default:
			return fmt.Errorf("%s: unknown operation %q", ch.Path, ch.Operation)
		}
	}

	// Checkpoint 3: Check for cancellation after file operations (before commit)
	if err := checkContext(ctx, key, "after file operations"); err != nil {
		return err
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

	// Checkpoint 4: Check for cancellation before push/PR (point of no return)
	if err := checkContext(ctx, key, "before push/PR"); err != nil {
		return err
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

	pushErr, pushAttempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		return MakeTransient(c.Repository.Push(ctx, branchName))
	})
	c.Metrics.AddRetries(pushAttempts)
	if pushErr != nil {
		return fmt.Errorf("push: %w", pushErr)
	}
	base := c.Cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	// Surface any judgment calls the AI made while planning (e.g. renaming a
	// resource to avoid a naming collision) so a human can confirm or
	// override them, rather than the ticket silently failing on ambiguity.
	for _, ch := range valid {
		if ch.Note != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", ch.Path, ch.Note))
		}
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

	// Record this ticket's work in the journal for future tickets'
	// continuity injection and dependency deferral.
	if err := c.Journal.Append(journal.Entry{
		TicketKey:    key,
		Summary:      summary,
		Branch:       branchName,
		PRURL:        prURL,
		Merged:       false,
		FilesChanged: changedPaths(valid),
		PublicAPIs:   diffPublicAPIs(repoRoot, valid, beforeAPIs),
		Timestamp:    time.Now(),
	}); err != nil {
		logger.Warn("Failed to append journal entry", "ticket", key, "error", err)
	}

	// Mark Done
	if err := c.Ticketing.UpdateTicketStatus(ctx, key, "Done", c.Cfg.JiraTransitions); err != nil {
		logger.Error("Failed to move ticket to Done", "error", err)
	}

	// Update metrics with execution time and files changed
	executionTime := time.Since(startTime)
	filesChanged := len(valid)

	ticketMetrics.SetExecutionTime(executionTime)
	ticketMetrics.SetFilesChanged(filesChanged)
	ticketMetrics.Status = "success"

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

// changedPaths returns the repo-relative paths touched by changes, in order.
func changedPaths(changes []agent.CodeChange) []string {
	paths := make([]string, len(changes))
	for i, ch := range changes {
		paths[i] = ch.Path
	}
	return paths
}

// capturePublicAPIs snapshots the exported symbols of Go files about to be
// edited, before any changes are applied. Created files have no "before"
// state (absent from the map, treated as empty by diffPublicAPIs); deleted
// files aren't tracked.
func capturePublicAPIs(repoRoot string, changes []agent.CodeChange) map[string][]string {
	before := make(map[string][]string)
	for _, ch := range changes {
		if ch.Operation != agent.OperationEdit || !strings.HasSuffix(ch.Path, ".go") {
			continue
		}
		before[ch.Path] = readExportedAPIs(filepath.Join(repoRoot, ch.Path))
	}
	return before
}

// diffPublicAPIs compares each changed Go file's exported symbols against its
// pre-change snapshot (from capturePublicAPIs) and returns the newly added
// ones as "path.Symbol", for recording in the journal.
func diffPublicAPIs(repoRoot string, changes []agent.CodeChange, before map[string][]string) []string {
	var added []string
	for _, ch := range changes {
		if ch.Operation == agent.OperationDelete || !strings.HasSuffix(ch.Path, ".go") {
			continue
		}
		prior := make(map[string]bool, len(before[ch.Path]))
		for _, sym := range before[ch.Path] {
			prior[sym] = true
		}
		for _, sym := range readExportedAPIs(filepath.Join(repoRoot, ch.Path)) {
			if !prior[sym] {
				added = append(added, ch.Path+"."+sym)
			}
		}
	}
	return added
}

// readExportedAPIs returns the exported top-level symbols declared in the Go
// file at path, or nil if it can't be read or parsed.
func readExportedAPIs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	symbols, err := indexer.ExtractExportedSymbols(data)
	if err != nil {
		return nil
	}
	return symbols
}

// journalBlocker returns the ticket key of a related prior entry whose PR
// hasn't been merged yet, or "" if this ticket has no such dependency.
// Tickets with a blocker are deferred until that PR merges, avoiding the
// "ticket N+1 doesn't see ticket N's work" class of failures.
func (c *Coordinator) journalBlocker(ticketText string) string {
	for _, e := range c.Journal.Relevant(ticketText, 3) {
		if !e.Merged {
			return e.TicketKey
		}
	}
	return ""
}

// sumUsageMetrics combines usage metrics from the initial PlanChanges call and
// a retrieval-pass call triggered by a need_files response into one total.
// Token counts and cost are summed across both calls; ContextStats reflects
// the final (retrieval-pass) context, with ContextBytes summed across both.
func sumUsageMetrics(a, b *agent.UsageMetrics) *agent.UsageMetrics {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &agent.UsageMetrics{
		InputTokens:   a.InputTokens + b.InputTokens,
		OutputTokens:  a.OutputTokens + b.OutputTokens,
		TotalTokens:   a.TotalTokens + b.TotalTokens,
		EstimatedCost: a.EstimatedCost + b.EstimatedCost,
		ContextStats: agent.ContextStats{
			Strategy:      b.ContextStats.Strategy,
			FilesIncluded: b.ContextStats.FilesIncluded,
			ContextBytes:  a.ContextStats.ContextBytes + b.ContextStats.ContextBytes,
			Keywords:      b.ContextStats.Keywords,
		},
	}
}

// mergeUnique returns the union of a and b, preserving a's order and
// appending any elements of b not already present in a.
func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	out := make([]string, 0, len(a)+len(b))
	for _, v := range a {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range b {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
