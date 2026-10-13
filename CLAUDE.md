# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an autonomous AI engineering assistant (Go-based) that:
- Reads JIRA tickets assigned to an agent username
- Analyzes target repositories and generates code changes using AI providers (Anthropic Claude or local LLMs via Ollama)
- Creates GitHub branches, commits, pushes, and opens Pull Requests
- Updates JIRA ticket status automatically

The agent runs in a polling loop, processing tickets concurrently with configurable worker pools.

## Build & Development Commands

### Building
```bash
make build              # Build binary to build/agent
make build-race         # Build with race detector
make install            # Install to $GOPATH/bin
```

### Running
```bash
make run                # Build and run
make dev                # Run with go run (faster for development)
make run-init           # Generate sample config files (.env.example, agent_state.jsonc)
```

### Testing
```bash
make test               # Run all tests
make test-v             # Verbose test output
make test-race          # Run with race detector
make test-cover         # Generate coverage report (coverage.html)
go test ./internal/orchestrator -run TestBackoff  # Run single test
```

### Code Quality
```bash
make fmt                # Format code
make vet                # Run go vet
make lint               # Run golangci-lint (requires installation)
make check              # Run vet + tests
make quality            # Run all quality checks (fmt, vet, lint, test)
```

### Setup & Dependencies
```bash
make setup              # Setup dev environment (installs mockgen, golangci-lint)
make deps               # Download dependencies
make tidy               # Tidy go.mod
make mocks              # Generate mocks (requires mockgen)
```

## Architecture

### Core Components

**Orchestrator** (`internal/orchestrator/coordinator.go`)
- Main control loop that polls JIRA, prepares repository, and spawns workers
- Worker pool pattern with semaphore (honors `MAX_CONCURRENT_TICKETS`)
- Pipeline: fetch tickets → create branch → build context → AI planning → apply changes → quality gates → push → PR → update ticket
- State tracking via `agent_state.jsonc` to avoid reprocessing tickets

**Ticketing** (`internal/ticketing/`)
- Interface-based facade with JIRA implementation
- Currently using `jira-raw` client (raw HTTP) as fallback for go-jira issues
- Handles ticket fetching and status transitions

**Repository** (`internal/repository/`)
- Git operations via go-git and GitHub PR creation via go-github
- Manages: clone, branch, commit, push, PR creation
- Working directory structure: `{WORKING_DIR}/{GITHUB_REPO}/`

**AI Provider** (`internal/ai/`, `internal/provider/`)
- `agent.Agent` interface with multiple implementations:
  - **Anthropic** (`ai/agent/anthropic/`): Claude Sonnet 4 via API ($3-15 per M tokens)
  - **Ollama** (`ai/agent/ollama/`): Local LLMs (qwen2.5-coder, deepseek-coder) - Free
- Factory pattern (`provider/factory.go`) instantiates provider based on `AI_PROVIDER` config
- `PlanChanges()` returns `[]CodeChange` (file paths + content)
- Context builder limits repo context by file count and byte size to control token usage
- Uses structured prompts from `agent/templates.go`

**Config** (`internal/config/config.go`)
- Loads from environment variables via viper (supports .env files)
- Validates required fields on startup
- Key settings: polling interval, concurrency, branch prefix, context limits, quality gate flags

### Data Flow
1. Poll JIRA for tickets assigned to `AGENT_USERNAME`
2. Prepare local repo (clone if needed, sync with remote, switch to base branch)
3. For each unprocessed ticket whose "is blocked by" tickets are all Done (merged):
   - Create feature branch
   - Run the developer job through the configured `runner.Runner` (the legacy runner builds repo context, asks the model for JSON edits and applies them)
   - Gate the branch with git (`internal/gate`): path allowlist, file count, protected paths, file size, credential scan
   - Commit, then run the target repo's verify commands from `.intern/project.yaml` (or `go vet`/`go test` per the legacy flags)
   - On verify failure, hand the output back to the runner up to `VERIFY_FIX_ATTEMPTS` times
   - Push branch and create PR
   - Move ticket to "In Review" (it moves to "Done" when the PR merges)
   - Mark ticket as processed in state file

### Key Patterns
- **Dependency Injection**: Services use interfaces; concrete implementations injected at `main.go`
- **Retry with Backoff**: `orchestrator/backoff.go` provides exponential backoff with jitter for transient failures
- **Runner contract**: `internal/runner` defines `Job`/`Result`/`Runner`. Runners only edit the workdir; the orchestrator does every external side effect (push, PR, ticket transitions)
- **Diff gate**: `internal/gate` validates what actually changed in git, whichever runner produced it
- **Project contract**: `internal/project` reads `.intern/project.yaml` in the target repo (`setup`, `verify`, `protected_paths`)
- **Legacy validation**: `orchestrator/validation.go` still screens the legacy runner's JSON edits before they are applied
- **Metrics Tracking**: `orchestrator/metrics.go` tracks tickets processed, PRs created, retries, AI failures

## Configuration

Environment variables (see `.env.example` generated by `make run-init`):

**JIRA**
- `JIRA_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`, `JIRA_PROJECT_KEY`
- Transition IDs (optional; looked up by status name when unset): `JIRA_TRANSITION_TO_DO`, `JIRA_TRANSITION_IN_PROGRESS`, `JIRA_TRANSITION_IN_REVIEW`, `JIRA_TRANSITION_DONE`

**GitHub**
- `GITHUB_TOKEN`, `GITHUB_OWNER`, `GITHUB_REPO`

**AI Provider**
- `AI_PROVIDER`: "anthropic" or "ollama" (default: "anthropic")

**Runner** (see `docs/RUNNERS.md`)
- `AI_RUNNER`: "legacy" (default, single-shot JSON edits) or "claude-code" (agentic Claude Code CLI)
- `SANDBOX`: "local" (default, scrubbed env, not a security boundary) or "docker" (requires `SANDBOX_IMAGE`)
- `CLAUDE_MODEL`, `CLAUDE_EFFORT`, `RUNNER_TIMEOUT`, `RUNNER_ALLOWED_COMMANDS`, `SANDBOX_NETWORK`, `SANDBOX_PASS_ENV`

**Team mode** (see `docs/TEAM_MODE.md`): `agent serve` with `TICKETING_MODE=jira` and Slack configured runs the JIRA poll loop next to the Slack endpoint
- `SLACK_ARCHITECT_CHANNEL`: new threads there are architect conversations; `plan: ...` starts one anywhere
- `SLACK_NOTIFY_CHANNEL`: escalations and merges are posted here
- `JIRA_AGENT_ACCOUNT_ID`, `JIRA_ISSUE_TYPE`: for tickets the architect's approved backlog creates

**Review / test / merge flow** (requires `AI_RUNNER=claude-code`)
- `REVIEW_ENABLED`: run the reviewer persona on each PR; it posts a review and sends requested changes back to the developer
- `MAX_REVIEW_ROUNDS`: review rounds before the PR is handed to a human (default: 2)
- `TEST_ENABLED`, `AUTO_MERGE`, `MERGE_METHOD` (squash, merge, rebase): later stages
- Flow stages show as `intern:*` PR labels; a human adds `intern:resume` to send a needs-human PR back to review
- In-flight PRs are tracked under `flows` in `agent_state.jsonc` and are advanced before new tickets start

**Anthropic** (required if `AI_PROVIDER=anthropic`)
- `ANTHROPIC_API_KEY`: API key from console.anthropic.com
- `ANTHROPIC_MODEL`: Claude model ID (default: `claude-opus-4-5-20251101`); cost tracking uses this model's pricing

**Ollama** (required if `AI_PROVIDER=ollama`)
- `OLLAMA_BASE_URL`: Ollama server URL (default: "http://localhost:11434")
- `OLLAMA_MODEL`: Model name (e.g., "qwen2.5-coder:7b", "deepseek-coder:6.7b")
- See `docs/OLLAMA_SETUP.md` for installation and model recommendations

**Agent Behavior**
- `AGENT_USERNAME`: JIRA assignee to filter tickets
- `POLLING_INTERVAL`: Duration (e.g., "30s", "5m")
- `MAX_CONCURRENT_TICKETS`: Worker pool size (default: 1). Each developer ticket runs in its own git worktree under `{WORKING_DIR}/{GITHUB_REPO}-worktrees/`, so values above 1 are safe; independent tickets (no unfinished blockers) build in parallel
- `MAX_TICKET_ATTEMPTS`: Failed attempts before a ticket is skipped for human attention (default: 3)
- `WORKING_DIR`: Base directory for cloned repos (default: "./workspace")
- `BASE_BRANCH`: Main branch name (default: "main")
- `BRANCH_PREFIX`: Prefix for feature branches (e.g., "feature/")

**Context & Limits**
- `CONTEXT_MAX_FILES`: Max files to include in AI prompt (default: 40)
- `CONTEXT_MAX_BYTES`: Max bytes per file for context (default: 32KB)
- `PLAN_MAX_FILES`: Max files AI can modify in one ticket (default: 20)
- `ALLOWED_WRITE_DIRS`: Comma-separated allowed directories (default: "internal,cmd,pkg,docs,config,.")

**Verify**
- Preferred: a `.intern/project.yaml` in the target repo with `setup` and `verify` commands (any language) and `protected_paths`
- Fallback when that file is absent: `RUN_VET_BEFORE_PR` (`go vet ./...`) and `RUN_TESTS_BEFORE_PR` (`go test ./...`), both default false
- `VERIFY_FIX_ATTEMPTS`: Times a failing verify is handed back to the runner (default: `SELF_HEAL_MAX_ATTEMPTS` if `SELF_HEAL_ENABLED`, else 0). `SELF_HEAL_ON_*` flags are no longer used
- `TICKET_BUDGET_USD`: Per-ticket model spend cap for runners that support it (0 = none)

## Important Implementation Details

### Branch Naming
Branches are created with format: `{BRANCH_PREFIX}{ticketKey}` (sanitized)
- Example: `feature/PROJ-123`
- See `orchestrator/branch.go` for sanitization logic

### PR Title and Body
- **Title**: `[{ticketKey}] {ticketSummary}` (see `orchestrator/pr.go`)
- **Body**: Includes ticket summary, description, list of changed files, and quality gate results

### State Management
- `agent_state.jsonc` tracks processed ticket keys
- Prevents reprocessing tickets on subsequent polling cycles
- State is loaded at startup and updated after each successful PR creation

### Validation Guardrails
- Path traversal prevention: rejects paths with `..`
- Directory allowlist: only writes to configured `ALLOWED_WRITE_DIRS`
- File count limit: enforces `PLAN_MAX_FILES` to prevent excessive changes
- No-op detection: skips push/PR if no effective changes detected

### Error Handling
- Transient errors wrapped with `MakeTransient()` trigger retries
- Retries use exponential backoff with jitter (configurable via `BackoffConfig`)
- Failures logged with context (ticket key, error type)
- Metrics track retry counts and AI planning failures

## Testing Strategy

- Unit tests for orchestrator helpers (validation, backoff, metrics, quality gates)
- Context builder tests verify file filtering and size limiting
- Mock generation via `go.uber.org/mock/mockgen`
- To add new mocks: add `//go:generate mockgen` directive and run `make mocks`

## Common Development Patterns

### Adding a New AI Provider
1. Implement `agent.Agent` interface (see `internal/ai/agent/agent.go`)
2. Create new package under `internal/ai/agent/{provider}/` (e.g., `openai`, `azure`)
3. Add pricing model in `internal/ai/cost.go` if applicable
4. Update factory in `internal/provider/factory.go` to support new provider
5. Add configuration fields in `internal/config/config.go`
6. Document setup in `docs/{PROVIDER}_SETUP.md`

**Example Providers:**
- Anthropic: `internal/ai/agent/anthropic/` - Cloud API with Claude models
- Ollama: `internal/ai/agent/ollama/` - Local LLM execution

### Adding a New Ticketing System
1. Implement `ticketing.TicketingClient` interface
2. Create package under `internal/ticketing/{system}/`
3. Update `ticketing.NewTicketingService()` or wire in `main.go`

### Adding a New VCS Provider
1. Implement `repository.RepositoryClient` interface
2. Create package under `internal/repository/{vcs}/`
3. Update `repository.NewRepositoryService()` or wire in `main.go`

### Extending the Pipeline
- Add new steps in `coordinator.processTicket()`
- Keep steps small and testable
- Use helper functions in separate files (e.g., `orchestrator/validation.go`, `orchestrator/quality.go`)

## Current Branch & Workflow

- **Main branch**: `master` (not `main` - configure with `BASE_BRANCH=master`)
- **Current branch**: `phase-2` (development branch)
- When creating PRs manually, target `master`
- Recent commits show ongoing work on JIRA client fixes, retries, and enhanced PR formatting
