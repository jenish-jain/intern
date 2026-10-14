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
	"intern/internal/gate"
	"intern/internal/indexer"
	"intern/internal/journal"
	"intern/internal/project"
	"intern/internal/repository"
	"intern/internal/runner"
	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

type Coordinator struct {
	Ticketing  *ticketing.Service
	Repository *repository.RepositoryService
	Agent      agent.Agent   // model client used by the legacy runner
	Runner     runner.Runner // engine that does the developer's work
	Cfg        *config.Config
	State      *State
	Metrics    *Metrics
	RepoPaths  *repository.RepositoryPath // Centralized path management
	Journal    *journal.Journal           // Cross-ticket continuity log

	// Notifier announces events a person should see (escalations,
	// merges). Optional.
	Notifier Notifier
	// Sleeper scales the service to zero after an unrecoverable outage.
	// Optional; see outage.go.
	Sleeper Sleeper

	// repoMu serializes use of the shared clone's working tree between the
	// poll loop and request-driven work (Slack asks, architect
	// conversations). Jobs never hold it: they run in their own worktrees.
	repoMu sync.Mutex
	// gitMu serializes git operations on the shared repository (sync,
	// fetch, worktree add/remove, push), which all jobs' worktrees share.
	// It's held only for those commands, never for a whole job. Lock order:
	// repoMu before gitMu.
	gitMu sync.Mutex

	// sched tracks running jobs; see scheduler.go.
	sched scheduler
	// outage pauses model jobs while the API is unavailable; see outage.go.
	outage outage
	// notified dedupes repeated failure notifications; see notify.go.
	notified sync.Map
	// ticketMeta holds each ticket's priority and attempts for model
	// routing; see models.go.
	ticketMeta sync.Map

	team       teamMetrics
	budgetOnce sync.Once

	ticketMetricsMu sync.Mutex
	ticketMetrics   map[string]*TicketMetrics // last-known metrics per ticket key, for request-driven callers (see LastTicketMetrics)
}

func NewCoordinator(ticketing *ticketing.Service, repository *repository.RepositoryService, agent agent.Agent, cfg *config.Config, state *State, repoPaths *repository.RepositoryPath) *Coordinator {
	c := &Coordinator{
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
	c.Runner = newLegacyRunner(c)
	return c
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
		metricsServer.Team = c.Team
		metricsServer.Spend = c.State.Spend
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
			// Jobs see the cancelled context; let them stop cleanly.
			c.sched.wait()
			return
		default:
			c.runCycle(ctx, interval)
		}
	}
}

// runCycle is one poll iteration followed by the wait before the next.
// Jobs the cycle started keep running through the wait.
func (c *Coordinator) runCycle(ctx context.Context, interval time.Duration) {
	sleep := c.cycle(ctx, interval)
	select {
	case <-ctx.Done():
	case <-time.After(sleep):
	}
}

// cycle does the work of one poll iteration and returns how long to wait
// before the next. It starts jobs and returns without waiting for them:
// reviews and tests of open PRs run alongside developers building new
// tickets, each in its own worktree, and the next poll starts whatever has
// become ready meanwhile.
func (c *Coordinator) cycle(ctx context.Context, interval time.Duration) time.Duration {
	if e, asleep := c.modelPaused(); asleep {
		// Asleep after an unrecoverable outage: do nothing until a person
		// wakes intern, which restarts the process.
		logger.Info("Asleep: model API unavailable until a person fixes it", "reason", e.Reason)
		return time.Hour
	}
	c.repoMu.Lock()
	// Ensure local repo is up to date before each cycle
	err := c.prepareRepository(ctx)
	if err == nil {
		// Flip Merged for PRs that landed since the last cycle, so deferred
		// tickets can unblock, and move those tickets to Done.
		c.reconcileMerged(ctx)
	}
	c.repoMu.Unlock()
	if err != nil {
		logger.Error("Repository preparation failed", "error", err)
		return backoffDuration(interval)
	}

	// Move open PRs along (review, fixes, test, merge) before starting new
	// tickets, so finishing work gets the first free slots.
	c.startFlows(ctx)

	if c.budgetExceeded() {
		logger.Warn("Project budget reached; not starting new tickets", "spent", c.State.Spend(), "budget", c.Cfg.ProjectBudgetUSD)
		return interval
	}

	var tickets []ticketing.Ticket
	err, attempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		t, e := c.Ticketing.GetTickets(ctx, c.Cfg.AgentUsername, c.Cfg.JiraProject)
		if e != nil {
			return MakeTransient(e)
		}
		tickets = t
		return nil
	})
	c.Metrics.AddRetries(attempts)
	if err != nil {
		logger.Error("Failed to fetch tickets", "error", err)
		return backoffDuration(interval)
	}
	if len(tickets) == 0 {
		logger.Info("No tickets to process; sleeping", "interval", interval.String())
		return interval
	}

	c.startTickets(ctx, tickets, c.processTicket)
	s := c.Metrics.Snapshot()
	logger.Info("Run summary", "tickets", s.TicketsProcessed, "prs", s.PRsCreated, "retries", s.Retries, "ai_failures", s.AIPlanFailures)
	return interval
}

// backoffDuration is the wait after a failed cycle: at least 5s.
func backoffDuration(base time.Duration) time.Duration {
	if base < 5*time.Second {
		return 5 * time.Second
	}
	return base
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
	c.repoMu.Lock()
	defer c.repoMu.Unlock()
	return c.prepareRepository(ctx)
}

// ProcessTicket runs the full ticket pipeline (branch, plan, apply, quality
// gates, push, PR, status update) for a single ticket. Exported so request-
// driven trigger sources (Slack webhook, Cloud Run handler) can invoke it
// directly for one ticket without going through Run()'s JIRA polling loop.
func (c *Coordinator) ProcessTicket(ctx context.Context, key, summary, description string) error {
	c.repoMu.Lock()
	defer c.repoMu.Unlock()
	if err := c.processTicket(ctx, key, summary, description); err != nil {
		return err
	}
	c.State.MarkProcessed(key)
	return nil
}

func (c *Coordinator) prepareRepository(ctx context.Context) error {
	c.gitMu.Lock()
	defer c.gitMu.Unlock()
	repoPath := c.RepoPaths.Root()
	if _, err := os.Stat(c.RepoPaths.GitDir()); os.IsNotExist(err) {
		logger.Info("Cloning repository...")
		if err := c.Repository.CloneRepository(ctx, repoPath); err != nil {
			return err
		}
	}
	if err := discardLocalChanges(ctx, repoPath); err != nil {
		logger.Warn("Could not clean agent clone", "error", err)
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

	c.setStatus(ctx, key, ticketing.StatusInProgress)

	branchName := buildBranchName(c.Cfg.BranchPrefix, key)
	logger.Info("Creating branch in a dedicated worktree", "branch", branchName)
	repoRoot, cleanup, err := c.addWorktree(ctx, branchName)
	if err != nil {
		return errors.NewRepoBranchError(err, branchName, "create").
			WithContext("ticket_key", key)
	}
	defer cleanup()
	if err := checkContext(ctx, key, "after branch setup"); err != nil {
		return err
	}

	base, err := gate.HeadSHA(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("read base commit: %w", err)
	}

	job := runner.Job{
		Role:        runner.RoleDeveloper,
		TicketKey:   key,
		Summary:     summary,
		Description: description,
		Workdir:     repoRoot,
		Budget:      runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
		// A ticket that already failed gets a stronger model.
		Model: c.modelFor(runner.RoleDeveloper, key, description, c.metaOf(key).Attempts),
	}
	outcome, workErr := c.developAndVerify(ctx, job, base)

	usage := outcome.Result.Usage
	ticketMetrics.InputTokens = usage.InputTokens
	ticketMetrics.OutputTokens = usage.OutputTokens
	ticketMetrics.Cost = usage.CostUSD
	c.Metrics.AddTokenUsage(usage.InputTokens, usage.OutputTokens, usage.CostUSD)
	c.recordSpend(ctx, runner.RoleDeveloper, usage.CostUSD)
	logger.Info("Runner finished",
		"ticket", key,
		"cost", ai.FormatCost(usage.CostUSD),
		"input_tokens", ai.FormatTokens(usage.InputTokens),
		"output_tokens", ai.FormatTokens(usage.OutputTokens))
	if workErr != nil {
		return workErr
	}

	// Checkpoint: point of no return before push/PR
	if err := checkContext(ctx, key, "before push/PR"); err != nil {
		return err
	}

	if c.Cfg.DryRun {
		logger.Warn("DRY RUN MODE: Skipping push and PR creation",
			"ticket", key,
			"branch", branchName,
			"files_changed", len(outcome.Changes),
			"cost", usage.CostUSD)
		c.State.MarkProcessed(key)
		return nil
	}

	pushErr, pushAttempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		return MakeTransient(c.push(ctx, branchName, true))
	})
	c.Metrics.AddRetries(pushAttempts)
	if pushErr != nil {
		return fmt.Errorf("push: %w", pushErr)
	}
	baseBranch := c.Cfg.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	title := buildPRTitle(key, summary)
	body := buildPRBody(prBodyInput{
		TicketKey:   key,
		Summary:     summary,
		Description: description,
		WorkSummary: outcome.Result.Summary,
		Changes:     outcome.Changes,
		Notes:       outcome.Notes,
		Checks:      project.Summary(outcome.Checks),
	})
	var prURL string
	prErr, prAttempts := Retry(ctx, BackoffConfig{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, Jitter: 0.2, MaxRetries: 3}, func() error {
		u, e := c.Repository.CreatePullRequest(ctx, baseBranch, branchName, title, body)
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
	paths := make([]string, len(outcome.Changes))
	for i, ch := range outcome.Changes {
		paths[i] = ch.Path
	}
	if c.Journal != nil {
		if err := c.Journal.Append(journal.Entry{
			TicketKey:    key,
			Summary:      summary,
			Branch:       branchName,
			PRURL:        prURL,
			FilesChanged: paths,
			PublicAPIs:   addedPublicAPIs(ctx, repoRoot, base, outcome.Changes),
			Notes:        truncateNotes(outcome.Result.Summary, 500),
			Timestamp:    time.Now(),
		}); err != nil {
			logger.Warn("Failed to append journal entry", "ticket", key, "error", err)
		}
	}

	// The ticket moves to Done only once this PR merges (see
	// reconcileMerged), so dependent tickets never start on an unmerged base.
	c.setStatus(ctx, key, ticketing.StatusInReview)
	c.startFlow(ctx, key, summary, description, branchName, prURL, usage.CostUSD)

	executionTime := time.Since(startTime)
	ticketMetrics.SetExecutionTime(executionTime)
	ticketMetrics.SetFilesChanged(len(outcome.Changes))
	ticketMetrics.Status = "success"

	c.Metrics.IncTicketsProcessed()
	c.Metrics.AddExecutionTime(executionTime)
	c.Metrics.AddFilesChanged(len(outcome.Changes))

	logger.Info("Ticket completed",
		"ticket", key,
		"duration", executionTime.Round(time.Second),
		"files_changed", len(outcome.Changes),
		"pr_url", prURL)
	return nil
}

// addedPublicAPIs returns exported Go symbols that the change added, as
// "path.Symbol", by comparing each changed Go file with its content at base.
func addedPublicAPIs(ctx context.Context, repoRoot, base string, changes []gate.Change) []string {
	var added []string
	for _, ch := range changes {
		if ch.Status == "D" || !strings.HasSuffix(ch.Path, ".go") {
			continue
		}
		prior := map[string]bool{}
		if ch.Status != "A" {
			if old, err := runCommandCapture(ctx, repoRoot, "git", "show", base+":"+ch.Path); err == nil {
				for _, sym := range exportedSymbols([]byte(old)) {
					prior[sym] = true
				}
			}
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, ch.Path))
		if err != nil {
			continue
		}
		for _, sym := range exportedSymbols(data) {
			if !prior[sym] {
				added = append(added, ch.Path+"."+sym)
			}
		}
	}
	return added
}

func exportedSymbols(src []byte) []string {
	symbols, err := indexer.ExtractExportedSymbols(src)
	if err != nil {
		return nil
	}
	return symbols
}

func truncateNotes(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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
