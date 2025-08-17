# Contributing to AI Intern Agent

Thanks for your interest in contributing! This document outlines how to set up your environment, the development workflow, and standards to follow.

## Getting Started

1. Fork the repository and clone your fork.
2. Install Go (1.22+). Run `go mod tidy` at the repo root.
3. Copy `.env.example` to `.env` and configure your credentials.
4. Run initialization and the agent locally:
   ```bash
   go run cmd/agent/main.go --init
   go run cmd/agent/main.go
   ```

## Branching Strategy

- Base branch: `main` (or configured `BASE_BRANCH`)
- Feature branches: `feature/<ticket-key>-short-slug`
- Bugfix branches: `fix/<ticket-key>-short-slug`

## Commit Messages

- Use conventional commits where possible:
  - `feat: add X`
  - `fix: correct Y`
  - `refactor: restructure Z`
  - `docs: update README`
  - `test: add unit tests`

## Code Style

- Follow idiomatic Go practices
- Keep functions small and focused; prefer composition over deep nesting
- Use interfaces for external dependencies; inject via constructors
- Avoid global state
- Handle errors explicitly; never ignore errors silently
- Log actionable context (ticket key, branch name, repo) and avoid logging secrets

## Testing

- Write unit tests for new modules and logic
- Use mocks for external calls (JIRA, GitHub, Anthropic)
- Run tests locally:
  ```bash
  go test ./...
  ```

## PR Process

1. Ensure your branch is up-to-date with base
2. Run `go vet` and `go test ./...`
3. Open a PR with:
   - Summary of changes
   - Rationale and context
   - Screenshots/logs if relevant
4. Link to relevant JIRA ticket
5. Address review feedback promptly

## Design Principles

- Interface-driven facades for `ticketing`, `repository`, and `ai`
- Provider-specific implementations under dedicated subpackages (e.g., `ai/anthropic`, `ticketing/jira`)
- Orchestrator uses a pipeline with small helpers and a worker pool
- Configuration-driven behavior (env or config file) with sensible defaults

## Security

- Do not commit secrets (tokens, API keys)
- Avoid logging credentials or sensitive data
- Validate file paths and disallow path traversal in any file operation

## Performance & Resilience

- Limit I/O and API calls; add backoff/retries where appropriate
- Control concurrency with worker pools; avoid overloading remote APIs
- Add guards to skip no-op work (e.g., no changes → skip PR)

## Questions

Open an issue or start a discussion if you’re unsure about the best way to implement a change. We appreciate your contributions!
