# Next Features Roadmap

This document outlines the next phase of improvements for the AI Intern Agent, focusing on reliability, performance, and developer experience.

## Selected Features

1. **Self-Healing & Iterative Refinement** - AI fixes its own mistakes
2. **Incremental Index Updates** - Fast, efficient index rebuilding
3. **Context Caching & Reuse** - Reduce redundant context building
4. **Metrics Dashboard** - Track performance and costs
5. **Better Logging & Tracing** - Improved debugging and observability
6. **Interactive CLI** - Manual control and review workflows

---

## Feature 1: Self-Healing & Iterative Refinement ⭐⭐⭐

### Problem
AI-generated code often fails quality gates (tests, vet, build) on first attempt, requiring manual intervention.

### Solution
Implement a retry loop that feeds failures back to the AI for automatic fixes.

### Implementation

**New Component:** `internal/orchestrator/self_heal.go`

```go
type SelfHealConfig struct {
    MaxAttempts      int  // Default: 3
    EnableForTests   bool // Retry on test failures
    EnableForVet     bool // Retry on vet failures
    EnableForBuild   bool // Retry on build failures
}

type HealResult struct {
    Attempt      int
    Success      bool
    ErrorType    string // "test", "vet", "build"
    ErrorOutput  string
    FixedChanges []agent.CodeChange
}

func (c *Coordinator) selfHealingPipeline(
    ctx context.Context,
    ticket *ticketing.Ticket,
    initialChanges []agent.CodeChange,
) ([]agent.CodeChange, error)
```

**Workflow:**
1. Apply initial AI-generated changes
2. Run quality gates (build → vet → tests)
3. If failures detected:
   - Extract error messages
   - Create healing prompt: "Fix these errors: {errorOutput}"
   - Ask AI to generate fixes
   - Apply fixes
   - Repeat (max 3 attempts)
4. If all attempts fail, create PR with failures documented

**New Prompt:** `agent/templates.go`
```go
func BuildFixErrorsPrompt(
    originalChanges []CodeChange,
    errorType string,
    errorOutput string,
) string
```

**Configuration:**
```bash
# .env additions
SELF_HEAL_ENABLED=true
SELF_HEAL_MAX_ATTEMPTS=3
SELF_HEAL_ON_TESTS=true
SELF_HEAL_ON_VET=true
SELF_HEAL_ON_BUILD=false  # Usually not needed for Go
```

**Metrics to Track:**
- Heal attempts per ticket
- Success rate after healing
- Most common error types
- Cost per heal attempt

**Estimated Impact:**
- 60-80% reduction in failed PRs
- 40% increase in AI cost (extra healing calls)
- 2x improvement in "ready to merge" rate

**Estimated Effort:** 2-3 days

**Files to Create/Modify:**
- `internal/orchestrator/self_heal.go` (new)
- `internal/orchestrator/coordinator.go` (modify)
- `internal/ai/agent/templates.go` (add healing prompt)
- `internal/config/config.go` (add config)

---

## Feature 2: Incremental Index Updates ⭐⭐⭐

### Problem
Rebuilding the entire file index is slow (10-30s for large repos), wasting time on every index rebuild.

### Solution
Track changed files since last index and only update those entries.

### Implementation

**Enhanced Index:** `internal/indexer/incremental.go`

```go
type IndexMetadata struct {
    LastBuildTime time.Time
    GitCommitHash string
    FilesIndexed  int
}

func (idx *Indexer) IncrementalUpdate() (*FileIndex, error) {
    // 1. Load existing index
    existing := idx.LoadIndex()

    // 2. Get changed files since last index
    changedFiles := idx.getChangedFiles(existing.Metadata.GitCommitHash)

    // 3. Update only changed files
    for _, file := range changedFiles {
        if deleted {
            delete(existing.Files, file)
        } else {
            existing.Files[file] = idx.indexFile(file)
        }
    }

    // 4. Update metadata
    existing.Metadata.LastBuildTime = time.Now()
    existing.Metadata.GitCommitHash = currentCommitHash()

    return existing, nil
}

func (idx *Indexer) getChangedFiles(sinceCommit string) []string {
    // Use git diff to find changed files
    // git diff --name-only {sinceCommit}..HEAD
}
```

**Smart Index Strategy:**
```go
func (idx *Indexer) SmartBuild() (*FileIndex, error) {
    existing, err := idx.LoadIndex()
    if err != nil || idx.shouldFullRebuild(existing) {
        return idx.BuildIndex() // Full rebuild
    }
    return idx.IncrementalUpdate() // Fast incremental
}

func (idx *Indexer) shouldFullRebuild(existing *FileIndex) bool {
    // Rebuild if:
    // - Index is too old (>7 days)
    // - Too many changes (>20% of files)
    // - Git history was rewritten
}
```

**Background Indexing:**
```go
// In coordinator.go
func (c *Coordinator) startBackgroundIndexer(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    go func() {
        for {
            select {
            case <-ticker.C:
                idx.IncrementalUpdate()
            case <-ctx.Done():
                return
            }
        }
    }()
}
```

**Estimated Impact:**
- 90% faster index updates (30s → 3s)
- Always fresh index without manual rebuilds
- Better context selection accuracy

**Estimated Effort:** 2-3 days

**Files to Create/Modify:**
- `internal/indexer/incremental.go` (new)
- `internal/indexer/indexer.go` (modify)
- `internal/indexer/types.go` (add metadata)
- `internal/orchestrator/coordinator.go` (background indexer)

---

## Feature 3: Context Caching & Reuse ⭐⭐

### Problem
Rebuilding repository context for every ticket is expensive (time and tokens).

### Solution
Cache common context and reuse across tickets, only adding ticket-specific files.

### Implementation

**Context Cache:** `internal/ai/context_cache.go`

```go
type ContextCache struct {
    BaseContext     string          // Common files (config, types, interfaces)
    TicketContext   map[string]string // Ticket-specific context
    CacheTime       time.Time
    RepoCommitHash  string
}

func (cb *ContextBuilder) BuildWithCache(
    repoPath string,
    ticketKey string,
    keywords []string,
) (string, error) {
    // 1. Check if base context is fresh
    cache := cb.loadCache()
    if cache.isStale() {
        cache.BaseContext = cb.buildBaseContext(repoPath)
        cache.save()
    }

    // 2. Build ticket-specific context
    ticketFiles := cb.selectTicketFiles(keywords)
    ticketContext := cb.buildContextFromFiles(ticketFiles)

    // 3. Combine
    return cache.BaseContext + "\n" + ticketContext, nil
}

func (cb *ContextBuilder) buildBaseContext(repoPath string) string {
    // Always include:
    // - Main package files
    // - Core types/interfaces
    // - Config files
    // - Common utilities
    // Limit to ~20% of CONTEXT_MAX_BYTES
}
```

**Cache Invalidation:**
```go
func (c *ContextCache) isStale() bool {
    // Invalidate if:
    // - Older than 1 hour
    // - Git commit changed
    // - Config files modified
}
```

**Estimated Impact:**
- 40-50% faster context building
- 30-40% token savings on input
- Consistent core context across tickets

**Estimated Effort:** 1 day

**Files to Create/Modify:**
- `internal/ai/context_cache.go` (new)
- `internal/ai/context_builder.go` (modify)

---

## Feature 5: Metrics Dashboard 📊

### Problem
No visibility into agent performance, costs, success rates, or failure patterns.

### Solution
Track key metrics and expose via Prometheus endpoint + simple web UI.

### Implementation

**Metrics Package:** `internal/metrics/collector.go`

```go
type Metrics struct {
    // Ticket processing
    TicketsProcessed     prometheus.Counter
    TicketsSuccessful    prometheus.Counter
    TicketsFailed        prometheus.Counter
    ProcessingDuration   prometheus.Histogram

    // AI usage
    AICallsTotal         prometheus.Counter
    AITokensInput        prometheus.Counter
    AITokensOutput       prometheus.Counter
    AICostTotal          prometheus.Counter  // In cents
    AIProviderLatency    prometheus.Histogram

    // Quality gates
    VetFailures          prometheus.Counter
    TestFailures         prometheus.Counter
    HealAttempts         prometheus.Counter
    HealSuccesses        prometheus.Counter

    // PRs
    PRsCreated           prometheus.Counter
    PRsMerged            prometheus.Counter  // Via webhook
    FilesChanged         prometheus.Histogram
}

func NewMetrics() *Metrics {
    // Initialize all Prometheus metrics
}

func (m *Metrics) RecordTicketSuccess(duration time.Duration, cost float64) {
    m.TicketsSuccessful.Inc()
    m.ProcessingDuration.Observe(duration.Seconds())
    m.AICostTotal.Add(cost * 100) // Convert to cents
}
```

**HTTP Endpoint:** `internal/metrics/server.go`

```go
func StartMetricsServer(port int) {
    http.Handle("/metrics", promhttp.Handler())
    http.Handle("/health", healthHandler())
    http.Handle("/dashboard", dashboardHandler())
    log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func dashboardHandler() http.Handler {
    // Simple HTML dashboard with:
    // - Real-time metrics
    // - Cost trends
    // - Success rate charts
    // - Recent tickets table
}
```

**Configuration:**
```bash
METRICS_ENABLED=true
METRICS_PORT=9090
```

**Dashboard Views:**

1. **Overview**
   - Tickets processed today/week/month
   - Total cost
   - Average cost per ticket
   - Success rate

2. **Performance**
   - Processing time distribution
   - AI latency trends
   - Token usage over time

3. **Quality**
   - Test failure rate
   - Vet failure rate
   - Heal success rate
   - Files changed per ticket

4. **Cost Analysis**
   - Cost per day/week/month
   - Cost by provider (Anthropic vs Ollama)
   - Token usage trends
   - Cost per ticket type

**Estimated Impact:**
- Full visibility into agent performance
- Data-driven optimization decisions
- Cost tracking and budgeting

**Estimated Effort:** 2-3 days

**Files to Create:**
- `internal/metrics/collector.go` (new)
- `internal/metrics/server.go` (new)
- `internal/metrics/dashboard.html` (new)
- `cmd/agent/main.go` (start metrics server)

---

## Feature 6: Better Logging & Tracing 🔍

### Problem
Hard to debug issues, no correlation between log lines for same ticket, verbose output.

### Solution
Structured logging with correlation IDs, log levels, and contextual information.

### Implementation

**Enhanced Logging:** `internal/logger/context.go`

```go
type TicketContext struct {
    TicketKey    string
    CorrelationID string
    StartTime    time.Time
    Provider     string
}

func WithTicketContext(ctx context.Context, ticket *Ticket) context.Context {
    return context.WithValue(ctx, ticketContextKey, &TicketContext{
        TicketKey:    ticket.Key,
        CorrelationID: generateCorrelationID(),
        StartTime:    time.Now(),
    })
}

func LogInfo(ctx context.Context, msg string, fields ...interface{}) {
    tctx := getTicketContext(ctx)
    logger.Info(msg,
        append(fields,
            "ticket", tctx.TicketKey,
            "correlation_id", tctx.CorrelationID,
            "elapsed", time.Since(tctx.StartTime),
        )...,
    )
}
```

**Structured Logs:**
```json
{
  "level": "info",
  "msg": "AI planning complete",
  "ticket": "PROJ-123",
  "correlation_id": "abc-123-def",
  "elapsed": "2.5s",
  "provider": "ollama",
  "model": "qwen2.5-coder:7b",
  "files_changed": 3,
  "tokens_input": 2500,
  "tokens_output": 800,
  "cost": 0.0,
  "timestamp": "2025-01-12T10:30:45Z"
}
```

**Performance Tracing:**
```go
func TraceOperation(ctx context.Context, operation string, fn func() error) error {
    start := time.Now()
    LogInfo(ctx, "Starting operation", "operation", operation)

    err := fn()

    duration := time.Since(start)
    if err != nil {
        LogError(ctx, "Operation failed",
            "operation", operation,
            "duration", duration,
            "error", err,
        )
    } else {
        LogInfo(ctx, "Operation complete",
            "operation", operation,
            "duration", duration,
        )
    }
    return err
}
```

**Log Levels:**
```bash
LOG_LEVEL=info  # debug, info, warn, error
LOG_FORMAT=json # json or text
LOG_FILE=/var/log/ai-intern.log  # Optional file output
```

**Estimated Impact:**
- 10x faster debugging
- Easy correlation of ticket lifecycle
- Production-ready logging

**Estimated Effort:** 1 day

**Files to Create/Modify:**
- `internal/logger/context.go` (new)
- `internal/logger/trace.go` (new)
- All files with logging (use contextual logger)

---

## Feature 16: Interactive CLI 🎮

### Problem
No way to manually review, approve, or reject AI changes before creating PRs.

### Solution
Interactive commands for manual control and review workflows.

### Implementation

**New Commands:**

```bash
# Review changes before creating PR
make review TICKET=PROJ-123
# Shows: diff, cost estimate, files changed, test results
# Prompts: [A]pprove, [R]eject, [E]dit, [C]ancel

# Approve and create PR
make approve TICKET=PROJ-123

# Reject with feedback (feeds back to AI)
make reject TICKET=PROJ-123 REASON="Use bcrypt instead of md5"

# Dry-run mode (no PR creation)
make dry-run TICKET=PROJ-123

# Reprocess a ticket (ignores state)
make reprocess TICKET=PROJ-123

# Show ticket status
make status TICKET=PROJ-123
```

**Implementation:** `cmd/cli/`

```go
// cmd/cli/review.go
func ReviewCommand(ticketKey string) error {
    // 1. Load saved changes from state
    changes := state.GetChanges(ticketKey)

    // 2. Show diff
    showDiff(changes)

    // 3. Show metadata
    fmt.Printf("Cost: $%.3f\n", changes.Cost)
    fmt.Printf("Files: %d\n", len(changes.Files))
    fmt.Printf("Tests: %s\n", changes.TestStatus)

    // 4. Prompt for action
    action := promptUser("Approve, Reject, Edit, Cancel?")

    switch action {
    case "approve":
        createPR(changes)
    case "reject":
        reason := promptUser("Rejection reason:")
        rejectChanges(ticketKey, reason)
    case "edit":
        // Open changes in $EDITOR
    }
}
```

**Dry-Run Mode:**
```go
// In config
DryRun bool

// In coordinator
if cfg.DryRun {
    logger.Info("DRY RUN: Would create PR",
        "ticket", ticket.Key,
        "files", len(changes),
        "cost", cost,
    )
    return nil // Don't create PR
}
```

**Estimated Impact:**
- Human oversight for critical tickets
- Ability to course-correct AI
- Better for learning/testing

**Estimated Effort:** 2 days

**Files to Create:**
- `cmd/cli/review.go` (new)
- `cmd/cli/approve.go` (new)
- `cmd/cli/reject.go` (new)
- `cmd/cli/status.go` (new)
- `Makefile` (add commands)

---

## Implementation Order

### Week 1: Foundation
**Day 1-2:** Context Caching (#3)
- Simple, quick win
- Reduces cost/time immediately
- Foundation for other features

**Day 3-4:** Better Logging (#6)
- Essential for debugging next features
- Structured logs help with metrics
- Needed before self-healing

**Day 5:** Dry-Run Mode (part of #16)
- Quick to implement
- Useful for testing self-healing

### Week 2: Reliability
**Day 6-8:** Self-Healing (#1)
- Most impactful feature
- Requires good logging (done)
- Core improvement

**Day 9-10:** Incremental Indexing (#2)
- Performance improvement
- Builds on existing indexer

### Week 3: Observability
**Day 11-13:** Metrics Dashboard (#5)
- Collect metrics during self-healing
- Dashboard UI
- Grafana integration (optional)

**Day 14-15:** Interactive CLI (#16)
- Review commands
- Manual workflows
- Polish and testing

---

## Testing Strategy

**For Each Feature:**

1. **Unit Tests**
   - Core logic isolated
   - Mock dependencies
   - Edge cases covered

2. **Integration Tests**
   - End-to-end workflows
   - Real AI calls (with test account)
   - Database/state persistence

3. **Manual Testing**
   - Test with real tickets
   - Monitor logs and metrics
   - Verify cost impact

4. **Rollout**
   - Deploy with feature flags
   - Monitor for issues
   - Gradual enablement

---

## Success Metrics

### Self-Healing
- ✅ 70%+ of failures auto-fixed
- ✅ <3 heal attempts average
- ✅ 50%+ reduction in manual intervention

### Incremental Indexing
- ✅ 90%+ faster index updates
- ✅ <5s for incremental update
- ✅ Background updates working

### Context Caching
- ✅ 40%+ token savings
- ✅ 30%+ faster context building
- ✅ Cache hit rate >80%

### Metrics Dashboard
- ✅ All key metrics tracked
- ✅ Dashboard accessible
- ✅ Prometheus integration working

### Better Logging
- ✅ All logs have correlation IDs
- ✅ JSON output for production
- ✅ Easy to trace ticket lifecycle

### Interactive CLI
- ✅ Review workflow functional
- ✅ Dry-run mode working
- ✅ Manual approval process smooth

---

## Configuration Summary

**New Environment Variables:**

```bash
# Self-Healing
SELF_HEAL_ENABLED=true
SELF_HEAL_MAX_ATTEMPTS=3
SELF_HEAL_ON_TESTS=true
SELF_HEAL_ON_VET=true

# Indexing
INDEX_AUTO_UPDATE=true
INDEX_UPDATE_INTERVAL=5m
INDEX_BACKGROUND=true

# Context Caching
CONTEXT_CACHE_ENABLED=true
CONTEXT_CACHE_TTL=1h

# Metrics
METRICS_ENABLED=true
METRICS_PORT=9090

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_FILE=/var/log/ai-intern.log

# CLI
DRY_RUN=false
REQUIRE_APPROVAL=false
```

---

## Dependencies

**New Go Packages:**
- `github.com/prometheus/client_golang` - Metrics
- `github.com/spf13/cobra` - CLI framework (optional)

**Optional:**
- Grafana (for visualizing Prometheus metrics)
- PostgreSQL (if we want persistent metrics storage)

---

## Documentation Updates Needed

1. **README.md**
   - Document new features
   - Update quick start
   - Add CLI commands

2. **New Docs:**
   - `docs/SELF_HEALING.md` - How healing works
   - `docs/METRICS.md` - Metrics reference
   - `docs/CLI.md` - CLI command reference

3. **CLAUDE.md**
   - Update architecture
   - Add new components
   - Update development patterns

---

*Roadmap Version: 1.0*
*Created: 2025-11-18*
*Status: Ready for Implementation*
