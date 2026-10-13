package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"intern/internal/ai"
	"intern/internal/ai/agent"
	"intern/internal/indexer"
	"intern/internal/journal"
	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// legacyRunner is the original single-shot engine behind the runner
// contract. The orchestrator picks the context (keyword-scored files,
// signatures only), the model returns JSON edits, and they are applied
// here. A job with Feedback asks the model to fix verify failures.
type legacyRunner struct {
	c *Coordinator

	mu sync.Mutex
	// applied remembers what was applied per ticket so a fix round can
	// show the model its own previous changes.
	applied map[string][]agent.CodeChange
}

var _ runner.Runner = (*legacyRunner)(nil)

func newLegacyRunner(c *Coordinator) *legacyRunner {
	return &legacyRunner{c: c, applied: map[string][]agent.CodeChange{}}
}

func (r *legacyRunner) Run(ctx context.Context, job runner.Job) (runner.Result, error) {
	if job.Role != runner.RoleDeveloper {
		return runner.Result{}, fmt.Errorf("legacy runner only supports the developer role, got %q", job.Role)
	}
	if job.Feedback != "" {
		return r.fix(ctx, job)
	}
	return r.plan(ctx, job)
}

// plan builds repo context, asks the model for changes, validates and
// applies them.
func (r *legacyRunner) plan(ctx context.Context, job runner.Job) (runner.Result, error) {
	c := r.c
	repoRoot := job.Workdir
	key, summary, description := job.TicketKey, job.Summary, job.Description

	idx := indexer.New(repoRoot)
	fileIndex, wasUpdated, indexErr := idx.RebuildIfStale()
	if indexErr != nil {
		logger.Warn("Failed to build/update index, smart context may fall back to simple", "error", indexErr)
	} else if wasUpdated {
		logger.Info("Index built/updated successfully", "files", len(fileIndex.Files))
		if saveErr := idx.SaveIndex(fileIndex); saveErr != nil {
			logger.Warn("Failed to save index", "error", saveErr)
		}
	}

	// Prior-work continuity: surface recent related tickets (and whether
	// their PRs have merged) so the model builds on existing work.
	priorWork := ""
	if c.Journal != nil {
		priorWork = journal.Render(c.Journal.Relevant(summary+" "+description, 3))
	}

	usedSmartContext := false
	ctxStr, ctxErr := ai.BuildSmartRepoContext(repoRoot, description, c.Cfg.ContextMaxFiles, nil)
	if ctxErr != nil {
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
	planOnce := func(repoContext string) ([]agent.CodeChange, []string, *agent.UsageMetrics, error) {
		var changes []agent.CodeChange
		var needFiles []string
		var usage *agent.UsageMetrics
		err, attempts := Retry(ctx, planBackoff, func() error {
			ch, nf, m, e := c.Agent.PlanChanges(ctx, key, summary, description, repoContext)
			if e != nil {
				return MakeTransient(e)
			}
			changes, needFiles, usage = ch, nf, m
			return nil
		})
		c.Metrics.AddRetries(attempts)
		if err != nil {
			c.Metrics.IncAIPlanFailures()
		}
		return changes, needFiles, usage, err
	}

	changes, needFiles, usageMetrics, err := planOnce(ctxStr)
	if err != nil {
		return runner.Result{}, fmt.Errorf("AI planning failed: %w", err)
	}

	// Retrieval pass: the model asked for the full content of files shown
	// signatures-only. Rebuild context with those files promoted and plan
	// again, accumulating requests across a bounded number of rounds.
	const maxRetrievalRounds = 3
	fullContentFiles := needFiles
	for round := 0; len(fullContentFiles) > 0 && usedSmartContext && round < maxRetrievalRounds; round++ {
		logger.Info("AI requested full content for additional files", "ticket", key, "files", fullContentFiles, "round", round+1)
		ctxStr2, ctxErr2 := ai.BuildSmartRepoContext(repoRoot, description, c.Cfg.ContextMaxFiles, fullContentFiles)
		if ctxErr2 != nil {
			logger.Warn("Failed to rebuild context for requested files, proceeding without further retrieval", "ticket", key, "error", ctxErr2)
			break
		}
		changes2, needFiles2, usage2, err := planOnce(priorWork + ctxStr2)
		if err != nil {
			return runner.Result{}, fmt.Errorf("AI planning failed (retrieval pass): %w", err)
		}
		changes = changes2
		usageMetrics = sumUsageMetrics(usageMetrics, usage2)
		fullContentFiles = mergeUnique(fullContentFiles, needFiles2)
	}

	if usageMetrics != nil {
		usageMetrics.ContextStats.Strategy = "simple"
		if usedSmartContext {
			usageMetrics.ContextStats.Strategy = "smart"
		}
		if tm, ok := c.LastTicketMetrics(key); ok {
			tm.ApplyUsage(usageMetrics)
		}
	}

	valid, err := validatePlannedChanges(repoRoot, changes, c.Cfg.AllowedWriteDirs, c.Cfg.PlanMaxFiles)
	if err != nil {
		return runner.Result{Usage: toRunnerUsage(usageMetrics)}, fmt.Errorf("validation failed: %w", err)
	}
	for _, ch := range valid {
		if err := applyCodeChange(repoRoot, ch); err != nil {
			return runner.Result{Usage: toRunnerUsage(usageMetrics)}, err
		}
	}
	r.remember(key, valid)

	res := runner.Result{
		Status:  runner.StatusDone,
		Summary: fmt.Sprintf("Applied %d planned file changes.", len(valid)),
		Usage:   toRunnerUsage(usageMetrics),
	}
	for _, ch := range valid {
		if ch.Note != "" {
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %s", ch.Path, ch.Note))
		}
	}
	return res, nil
}

// fix asks the model to repair verify failures given the current content
// of the files it touched and the files named in the error output.
func (r *legacyRunner) fix(ctx context.Context, job runner.Job) (runner.Result, error) {
	c := r.c
	previous := r.previous(job.TicketKey)
	fileContents := collectFileContents(job.Workdir, previous, job.Feedback)

	fixes, usage, err := c.Agent.FixErrors(ctx, job.TicketKey, job.Summary, "verify", job.Feedback, previous, fileContents)
	if err != nil {
		return runner.Result{}, fmt.Errorf("AI fix generation failed: %w", err)
	}
	valid, err := validatePlannedChanges(job.Workdir, fixes, c.Cfg.AllowedWriteDirs, c.Cfg.PlanMaxFiles)
	if err != nil {
		return runner.Result{Usage: toRunnerUsage(usage)}, fmt.Errorf("fix validation failed: %w", err)
	}
	for _, ch := range valid {
		if err := applyCodeChange(job.Workdir, ch); err != nil {
			return runner.Result{Usage: toRunnerUsage(usage)}, fmt.Errorf("failed to apply fix for %s: %w", ch.Path, err)
		}
	}
	r.remember(job.TicketKey, valid)
	return runner.Result{
		Status:  runner.StatusDone,
		Summary: fmt.Sprintf("Applied %d fixes for verify failures.", len(valid)),
		Usage:   toRunnerUsage(usage),
	}, nil
}

func (r *legacyRunner) remember(key string, changes []agent.CodeChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied[key] = append(r.applied[key], changes...)
}

func (r *legacyRunner) previous(key string) []agent.CodeChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agent.CodeChange(nil), r.applied[key]...)
}

func toRunnerUsage(u *agent.UsageMetrics) runner.Usage {
	if u == nil {
		return runner.Usage{}
	}
	return runner.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CostUSD: u.EstimatedCost}
}

// applyCodeChange writes one planned change to disk.
func applyCodeChange(repoPath string, change agent.CodeChange) error {
	absPath := filepath.Join(repoPath, change.Path)

	switch change.Operation {
	case agent.OperationDelete:
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete %s: %w", change.Path, err)
		}
		return nil
	case agent.OperationEdit:
		if err := applyEditChange(repoPath, change); err != nil {
			return fmt.Errorf("edit %s: %w", change.Path, err)
		}
		return nil
	case agent.OperationCreate:
		if _, err := os.Stat(absPath); err == nil {
			return fmt.Errorf("create %s: file already exists (use operation=edit)", change.Path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", change.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(absPath, []byte(change.Content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", change.Path, err)
		}
		return nil
	default:
		return fmt.Errorf("%s: unknown operation %q", change.Path, change.Operation)
	}
}

// goFileRefRe matches Go compiler/test output lines like
// "./internal/foo/bar.go:23:4: undefined: baz" and captures the file path.
var goFileRefRe = regexp.MustCompile(`([A-Za-z0-9_./\-]+\.go):\d+`)

// extractFilePathsFromErrors returns the de-duplicated set of repo-relative
// .go file paths referenced in verify output.
func extractFilePathsFromErrors(errorOutput string) []string {
	matches := goFileRefRe.FindAllStringSubmatch(errorOutput, -1)
	seen := make(map[string]bool, len(matches))
	var paths []string
	for _, m := range matches {
		p := strings.TrimPrefix(m[1], "./")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
}

// collectFileContents reads the current on-disk content of every file in
// previousChanges plus every file referenced in errorOutput, so the model
// sees what's actually on disk. Missing or unreadable files are skipped.
func collectFileContents(repoPath string, previousChanges []agent.CodeChange, errorOutput string) map[string]string {
	fileContents := make(map[string]string)
	addFile := func(relPath string) {
		clean := filepath.Clean(strings.TrimPrefix(relPath, "./"))
		if clean == "" || clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return
		}
		if _, exists := fileContents[clean]; exists {
			return
		}
		data, err := os.ReadFile(filepath.Join(repoPath, clean))
		if err != nil {
			return
		}
		fileContents[clean] = string(data)
	}
	for _, ch := range previousChanges {
		addFile(ch.Path)
	}
	for _, p := range extractFilePathsFromErrors(errorOutput) {
		addFile(p)
	}
	return fileContents
}
