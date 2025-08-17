# AI Intern Agent - Step-by-Step Implementation Plan

## Project Structure Setup

### 1. Initialize Go Project
```
intern/
├── cmd/
│   └── agent/
│       └── main.go
├── internal/
│   ├── ticketing/
│   │   └── jira/
│   ├── repository/
│   │   └── github/
│   ├── ai/
│   │   ├── agent.go
│   │   ├── context_builder.go
│   │   ├── types.go
│   │   └── anthropic/
│   │       └── client.go
│   ├── orchestrator/
│   │   ├── coordinator.go
│   │   └── branch.go
│   ├── config/
│   └── testing/
├── pkg/
│   └── types/
├── configs/
├── scripts/
├── docs/
├── .env.example
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
└── README.md
```

## Phase 1: Foundation (Weeks 1-2)

### Step 1: Project Initialization
**Goal**: Set up basic project structure and dependencies

**Tasks**:
1. **Initialize Go Module**
   ```bash
   go mod init intern
   ```

2. **Add Core Dependencies**
   ```bash
   go get github.com/go-resty/resty/v2          # HTTP client
   go get github.com/google/go-github/v58       # GitHub API
   go get github.com/andygrunwald/go-jira       # JIRA API
   go get github.com/joho/godotenv              # Environment variables
   go get github.com/sirupsen/logrus            # Logging
   go get github.com/spf13/viper                # Configuration
   go get gopkg.in/yaml.v3                      # YAML parsing
   go install go.uber.org/mock/mockgen@latest   # Mocks
   ```

3. **Create Basic Configuration System**
   - `internal/config/config.go` - Configuration struct and loading
   - `.env.example` - Template for environment variables

**Deliverable**: Working Go project with configuration management

### Step 2: JIRA Integration
**Goal**: Connect to JIRA and read assigned tickets

**Tasks**:
1. **JIRA Client** (`internal/ticketing/jira/jira_client.go`)
   - Authentication handling
   - Basic API connection (health check)
   - Transitions API to move issues across statuses

2. **Ticket Operations** (`internal/ticketing/service.go`)
   - Get assigned tickets
   - Update ticket status using transition mapping

3. **Types** (`internal/ticketing/types.go`)
   - Ticket struct; fields: key, summary, description, status, priority, etc.

**Deliverable**: Agent can read and update JIRA tickets

### Step 3: GitHub Integration
**Goal**: Repository operations (local and remote)

**Tasks**:
1. **Repository Client** (`internal/repository/github/client.go`)
   - Clone repository (go-git)
   - Sync (pull) with remote
   - Create/switch branches
   - Add/commit/push changes
   - Create PRs (go-github)
   - Detect local changes (worktree status)

**Deliverable**: Branch/commit/PR flow working

### Step 4: Basic AI Integration
**Goal**: Connect to Anthropic API via a facade

**Tasks**:
1. **AI Facade** (`internal/ai/agent.go`, `internal/ai/types.go`)
   - `Agent` interface: `PlanChanges(ctx, key, summary, description, context) ([]CodeChange, error)`
   - `CodeChange` supports `content` and `content_b64`
2. **Anthropic Provider** (`internal/ai/anthropic/client.go`)
   - Prompting
   - JSON-only responses with sanitization (strip fences, base64 decode if present)
3. **Context Builder** (`internal/ai/context_builder.go`)
   - Include subset of files (skip binaries, vendor/node_modules)
   - Configurable limits: file count and bytes per file

**Deliverable**: Agent can propose code changes via AI

### Step 5: Coordinator & Pipeline
**Goal**: Coordinate the basic workflow

**Tasks**:
1. **Coordinator** (`internal/orchestrator/coordinator.go`)
   - Poll tickets
   - Prepare repo: clone/sync + switch base branch
   - Per-ticket pipeline: set In Progress → create branch → AI plan → materialize → commit → push → PR → Done
   - Skip PR if no effective changes
   - Worker pool honoring `MAX_CONCURRENT_TICKETS`
2. **Branch Utility** (`internal/orchestrator/branch.go`)
   - Build sanitized branch names with prefix

**Deliverable**: End-to-end MVP with PR creation and JIRA updates


## Phase 2: Intelligence (Weeks 3-4)

### Step 6: Advanced Code Analysis
**Goal**: Improve AI context and relevance to reduce hallucinations

**Tasks**:
1. **Code Graph & Indexing (Design)**
   - Build a lightweight index: packages, exported APIs, file-to-package mapping
   - Extract public functions/method signatures to include in context
2. **Context Heuristics**
   - Focus context around paths referenced in the ticket description (heuristics: keywords, file globs)
   - Include related tests when present
3. **Configurable Limits**
   - Move context limits (file count, bytes/file) to config; set defaults (e.g., 40 files, 32KB/file)

**Acceptance Criteria**:
- Context builder produces targeted context slices for typical tickets (<1MB)
- Unit tests validating inclusion/exclusion behavior

### Step 7: Enhanced AI Planning
**Goal**: Improve planning accuracy and safety

**Tasks**:
1. **Prompt Templates**
   - Add provider-agnostic prompt templates with strict JSON requirements
   - Add system content that emphasizes minimal changes and no markdown
2. **Validation Layer**
   - Validate `CodeChange` entries: path traversal guard, writeable directories, non-empty content
   - Limit number of changed files per ticket (configurable)
3. **Plan Auditing**
   - Log summarized plan (file list only) for traceability

**Acceptance Criteria**:
- Plans rejected if they violate path or file limits
- Plans applied only when valid; unsafe output is skipped with clear logs

### Step 8: Testing & Quality Gates
**Goal**: Add gates before PR

**Tasks**:
1. **Hooks**
   - Optional: run `go vet`, `go test ./...` before commit/push (config flag)
   - Optional linters (gofumpt/golangci-lint) if configured
2. **PR Template**
   - Autogenerate PR body with: ticket key, summary, file list, any test results

**Acceptance Criteria**:
- When enabled, PRs include test/quality summaries
- Fail-fast: do not push/PR on test failure when gate is enabled

### Step 9: Robustness & Backoff
**Goal**: Improve resilience on network/API errors

**Tasks**:
1. **Backoff & Retry**
   - Add exponential backoff with jitter for JIRA, GitHub, and Anthropic calls (configurable max retries)
2. **Error Taxonomy**
   - Classify errors: transient vs. permanent
   - Skip vs. retry decisions documented and implemented

**Acceptance Criteria**:
- Transient failures retry within limits; permanent failures logged with context
- Unit tests for retry policy behavior


## Phase 3: Autonomy (Weeks 5-6)

### Step 10: Priority & Scheduling
**Goal**: Intelligent ticket prioritization and controlled concurrency

**Tasks**:
1. **Priority Engine**
   - Extend ticket model with priority scoring from JIRA fields
   - Sort queue using weighted priority + recency + size (estimated by plan file count)
2. **Concurrency Controls**
   - Per-repo mutex: prevent concurrent writes to the same repo/branch
   - Rate limiters for remote APIs

**Acceptance Criteria**:
- No concurrent conflicting operations on the same repo
- High-priority tickets generally processed first

### Step 11: State Machine & Recovery
**Goal**: Introduce formal states and recoverability

**Tasks**:
1. **State Machine**
   - States per ticket: `Queued → Planning → Applying → Testing → PRCreated → Done | Failed`
   - Persist state and last action timestamp in agent state file (JSON)
2. **Recovery**
   - On startup, resume non-terminal tickets from last state
   - Idempotent operations: skip work already done (branch exists, PR exists)

**Acceptance Criteria**:
- Restarting agent resumes work without duplication
- State transitions logged; invalid transitions prevented

### Step 12: Observability & Reporting
**Goal**: Metrics and reporting

**Tasks**:
1. **Metrics**
   - Counters: tickets processed, PRs created, retries, failures
   - Timings: ticket processing time
2. **Reporting**
   - Summary logs per day/run; optional structured export (JSON)

**Acceptance Criteria**:
- Basic metrics visible in logs; optional export endpoint/design


## Implementation Checklist

### Phase 1 Checklist
- [x] Go project initialization
- [x] Basic configuration system
- [x] JIRA client and ticket reading/updating
- [x] GitHub client and repository operations
- [x] AI code planning (Anthropic)
- [x] Orchestrator workflow with worker pool
- [x] PR creation and JIRA Done update

### Phase 2 Checklist
- [ ] Advanced context builder (graph/index and heuristics)
- [ ] Provider-agnostic prompt templates and validation
- [ ] Testing/quality gates and PR template generation
- [ ] Backoff/retry policies with error taxonomy

### Phase 3 Checklist
- [ ] Priority engine and improved scheduling
- [ ] Per-repo locking and rate limiting
- [ ] State machine with recovery and idempotency
- [ ] Observability metrics and reporting


## Development Best Practices

### Code Quality
- Interface-first design; inject dependencies
- Keep functions small; pipeline-style orchestration
- Never ignore errors; log actionable context

### Security Considerations
- Never log secrets
- Guard against path traversal in file writes
- Minimal privileges for tokens

### Performance Optimization
- Rate limit remote APIs
- Limit AI context (file count/bytes)
- Cache repository context inputs when feasible

### Testing Strategy
- Unit tests for business logic and clients
- Mocks for external calls
- Optional gates before PRs (`go test`, lint)


## AI Prompting Notes (Appendix)

- Always request JSON-only output and provide a strict schema
- Instruct model to use `content_b64` for complex content
- Reject/skip plans that contain invalid paths, excessive files, or exceed limits


## Configuration Additions (Appendix)

- `WORKING_DIR` (default `./workspace`)
- `BASE_BRANCH` (default `main`)
- `BRANCH_PREFIX` (e.g., `feature`)
- Future: `CONTEXT_MAX_FILES`, `CONTEXT_MAX_BYTES`, `RETRY_MAX_ATTEMPTS`


This updated plan reflects the current codebase (Phase 1 complete) and provides detailed, AI-agent-friendly tasks for Phase 2 and Phase 3 to improve accuracy, reliability, and maintainability. The plan emphasizes interface-driven design, robust error handling, and controlled concurrency for safe autonomous operation.

