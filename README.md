# AI Intern Agent

An autonomous Go-based engineering assistant that reads JIRA tickets assigned to it, analyzes the target repository, generates and applies code changes using an AI provider (Anthropic), opens a GitHub Pull Request, and updates the JIRA ticket status.

## Overview

- Ticketing integration (JIRA) to fetch and update tickets
- Repository integration (Git/GitHub) to branch, commit, push, and open PRs
- AI provider facade with Anthropic implementation to plan code changes based on ticket description and repo context
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
  - `context_builder.go`: Builds a compact repo context for prompting
- `internal/config/`: Configuration loading and validation
- `cmd/agent/`: Entry-point wiring

### Data flow (simplified)
1. Orchestrator fetches tickets from JIRA
2. Prepares repository (clone/sync, switch to base branch)
3. For each ticket:
   - Creates a feature branch
   - Builds repo context and calls AI agent to plan changes
   - Applies changes, commits, pushes
   - Creates a PR and updates ticket status to Done

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
3. Edit `.env.example` and export/envsubst variables (or create `.env`), then run:
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
- Configurable repo context limits (files/bytes)
- Exponential backoff with jitter for all remote calls
- Per-repo locking for concurrent ticket processing across the same repository
- Additional AI providers and ticketing/VCS integrations