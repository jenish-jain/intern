# AI Intern Agent 
[![Go Coverage](https://github.com/jenish-jain/intern/wiki/coverage.svg)](https://raw.githack.com/wiki/jenish-jain/intern/coverage.html)

An autonomous Go-based engineering assistant that reads JIRA tickets assigned to it, analyzes the target repository, generates and applies code changes using an AI provider (Anthropic), opens a GitHub Pull Request, and updates the JIRA ticket status.

## Overview

- Ticketing integration (JIRA) to fetch and update tickets
- Repository integration (Git/GitHub) to branch, commit, push, and open PRs
- AI provider facade with Anthropic implementation to plan code changes based on ticket description and repo context
- **Smart context selection** using keyword extraction and file scoring to optimize token usage
- Orchestrator with a pipeline-like workflow per ticket
- Configurable concurrency and repository context limits

## Architecture

- `internal/orchestrator/`: Coordinates the end-to-end workflow
  - `coordinator.go`: Ticket processing loop, worker pool, pipeline
  - `branch.go`: Branch naming utilities (sanitization, prefixing)
- `internal/ticketing/`: Ticketing service facade
  - `jira/`: Concrete JIRA client implementation
- `internal/repository/`: Repository service facade
  - `github/`: Concrete GitHub client based on go-git and go-github
- `internal/ai/`: AI facade and shared types
  - `anthropic/`: Anthropic provider implementing the AI Agent interface
  - `context_builder.go`: Builds a compact repo context for prompting with smart file selection
- `internal/indexer/`: Smart context selection components
  - `indexer.go`: Builds and manages file index for the repository
  - `keywords.go`: Extracts keywords from ticket descriptions (file paths, identifiers)
  - `scorer.go`: Scores files based on keyword relevance with tiered matching
  - `parser.go`: Extracts minimal context from Go files (signatures without implementations)
- `internal/config/`: Configuration loading and validation
- `internal/util/`: Shared utility functions
- `cmd/agent/`: Entry-point wiring

### Data flow (simplified)
1. Orchestrator fetches tickets from JIRA
2. Prepares repository (clone/sync, switch to base branch)
3. For each ticket:
   - Creates a feature branch
   - **Builds smart repo context**: extracts keywords from ticket, scores files by relevance, selects top matches
   - Calls AI agent to plan changes based on minimal context (function signatures, not implementations)
   - Applies changes, commits, pushes
   - Creates a PR and updates ticket status to Done

### Smart Context Selection

The agent uses an intelligent context selection system to optimize token usage while providing relevant code context to the AI:

**How it works:**
1. **Keyword Extraction** (`keywords.go`): Extracts technical terms from ticket description
   - File paths: `internal/auth/login.go`
   - Identifiers: camelCase, PascalCase, snake_case variables/functions
   - Filters common stop words (the, and, with, etc.)

2. **File Scoring** (`scorer.go`): Ranks files by relevance using tiered matching
   - Exact path match: +15 points
   - Path contains keyword: +8 points
   - Path segment matches: +5 points
   - Segment contains keyword: +2 points
   - Category multipliers: core (1.5x), config (1.2x), test (0.7x), doc (0.5x)

3. **Minimal Context Extraction** (`parser.go`): For Go files, extracts only:
   - Package name and imports
   - Type definitions
   - Function signatures (no implementation bodies)
   - Results in 60-80% token reduction vs full file content

4. **Graceful Fallback**: If index unavailable or no keywords extracted, falls back to simple context builder

**Benefits:**
- **70% cost reduction**: From ~$0.15 to ~$0.045 per ticket (based on testing)
- **Better relevance**: Only includes files actually mentioned or related to the ticket
- **Faster processing**: Less context means faster AI responses

## Design Patterns and Practices

- **Interface-based architecture**: `ticketing`, `repository`, and `ai` use interfaces with DI-friendly services
- **Provider facade**: AI is abstracted via `ai.Agent`; Anthropic is one implementation under `ai/anthropic`
- **Pipeline/Chain of Responsibility**: Orchestrator breaks the workflow into small, testable steps
- **Worker Pool**: Concurrency honoring `MaxConcurrentTickets` with a bounded semaphore
- **Configuration-driven**: Environment variable based config with defaults; supports `WorkingDir`, `BaseBranch`, `BranchPrefix`
- **Repository context limiting**: Restricts number and size of files included in the AI prompt to control token usage
- **Guardrails**: Skips PR creation if no effective changes detected
- **Logging**: Consistent structured logging via a logger package

## Requirements

- Go 1.22+ (latest recommended)
- Access tokens for JIRA and GitHub; Anthropic API key

## Quick Start

1. Clone the repo and install dependencies:
   ```bash
   go mod tidy
   ```
2. Initialize sample config files:
   ```bash
   go run cmd/agent/main.go --init
   ```
3. Edit `.env.example` and export/envsubst variables (or create `.env`)

4. **(Optional but recommended)** Build the file index for smart context selection:
   ```bash
   go run cmd/agent/main.go --build-index
   ```
   This creates a `.ai-intern/file_index.json` in your repository with metadata about all files. The agent will use this for intelligent file selection.

5. Run the agent:
   ```bash
   go run cmd/agent/main.go
   ```

## Configuration

Environment variables (examples):

- JIRA:
  - `JIRA_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`, `JIRA_PROJECT_KEY`
  - Transitions map (via YAML or env mapping if loaded): you can provide mapping in code/config for status transitions
- GitHub:
  - `GITHUB_TOKEN`, `GITHUB_OWNER`, `GITHUB_REPO`
- Anthropic:
  - `ANTHROPIC_API_KEY`
- Agent:
  - `AGENT_USERNAME`, `POLLING_INTERVAL` (e.g., `30s`), `MAX_CONCURRENT_TICKETS`
  - `WORKING_DIR` (default `./workspace`)
  - `BASE_BRANCH` (default `main`)
  - `BRANCH_PREFIX` (e.g., `feature`)

## How It Works

- The orchestrator loops on `POLLING_INTERVAL`:
  - Prepares the local repo (clone/sync, switch to base)
  - Spawns up to `MAX_CONCURRENT_TICKETS` workers
  - Each worker processes a ticket end-to-end and marks it done when the PR is created

## Extensibility

- AI Providers: Implement `ai.Agent` and wire in via DI (see `ai/agent.go`, `ai/anthropic/client.go`)
- Ticketing Systems: Implement `ticketing.TicketingClient` and create a `TicketingService`
- VCS Providers: Implement `repository.RepositoryClient` and wrap in `RepositoryService`
- Pipeline Steps: Add steps to `processTicket` or refactor into discrete handlers

## Testing

- Unit tests for clients and orchestrator helpers are encouraged
- For mocks, use `go install go.uber.org/mock/mockgen@latest` and generate interface mocks
- Run tests:
  ```bash
  go test ./...
  ```

## Contribution Guidelines

See `CONTRIBUTING.md` for detailed guidelines on branching, coding style, commit messages, and PR checks.

## Roadmap

- Base branch auto-detection for checkout (currently applied to PR creation fallback)
- ✅ ~~Configurable repo context limits (files/bytes)~~ - Implemented with CONTEXT_MAX_FILES and smart selection
- ✅ ~~Intelligent file selection~~ - Implemented with keyword extraction and file scoring
- Exponential backoff with jitter for all remote calls
- Per-repo locking for concurrent ticket processing across the same repository
- Additional AI providers and ticketing/VCS integrations
- Incremental index updates (currently rebuilds entire index each time)