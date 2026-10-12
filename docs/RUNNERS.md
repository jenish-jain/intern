# Runners and sandboxes

A runner is the engine that does a role's work on a ticket. The orchestrator hands it a job (ticket, workspace, optional feedback) and gets back a result (status, summary, notes, questions, cost). Runners only edit files in the workspace. Pushing, opening PRs and moving tickets stay in the orchestrator, after the diff gate and verify commands have run.

## Choosing a runner

| `AI_RUNNER` | How it works | Use it when |
|---|---|---|
| `legacy` (default) | Intern picks context with keyword scoring, the model returns JSON edits in one call, intern applies them. Supports `AI_PROVIDER=ollama`. | You need local models or the old behavior. |
| `claude-code` | The Claude Code CLI runs headless. It reads the repo, edits files, runs the build and tests, and iterates until done. | Real feature work. This is the path to autonomous module delivery. |

Claude Code settings:

| Variable | Meaning | Default |
|---|---|---|
| `CLAUDE_CODE_BIN` | Path to the CLI | `claude` |
| `CLAUDE_MODEL` | Model alias or ID passed to `--model` | CLI default |
| `CLAUDE_EFFORT` | Passed to `--effort` | CLI default |
| `TICKET_BUDGET_USD` | Spend cap per job (`--max-budget-usd`) | none |
| `RUNNER_TIMEOUT` | Wall-clock cap per job | `45m` |
| `RUNNER_ALLOWED_COMMANDS` | Comma-separated Bash prefixes allowed in a local sandbox | common build/test tools |
| `ANTHROPIC_API_KEY` | Passed to the CLI | required outside local dev |

The developer persona lives in `internal/personas/developer.md` and ships in the binary. It tells the agent to read the repo's CLAUDE.md, PRD, architecture doc and ADRs, keep scope to the ticket, run the verify commands, never push, and stop with `needs_human` questions instead of inventing requirements.

## Personas

| Role | Persona | Tools | Output |
|---|---|---|---|
| Developer | `internal/personas/developer.md` | Read, edit, run build and tests | Summary, judgment-call notes, or questions |
| Reviewer | `internal/personas/reviewer.md` | Read and run verify only; edit tools and `git add`/`commit` are denied | Verdict (`approve` or `request_changes`) with line comments |
| Tester | `internal/personas/tester.md` | Read, run, and write test files only (enforced by the gate) | Whether acceptance criteria pass, with one entry per failure |

## The flow after a PR opens

With `REVIEW_ENABLED=true`, each PR the developer opens becomes a flow. Every poll cycle advances open flows before starting new tickets, so work in progress finishes first.

1. **Review.** The reviewer reads the diff against the base branch, runs verify, and posts a review on the PR. Approval moves the flow on. A request for changes sends the comments to the developer.
2. **Changes requested.** The developer addresses the comments on the PR branch. Intern gates, commits, verifies, pushes, comments a summary, and returns the PR to review.
3. **Testing** (`TEST_ENABLED=true`). After approval, the tester writes acceptance tests from the ticket's criteria and runs them with the verify commands. The gate rejects any change outside test paths (`test_paths` in `.intern/project.yaml`, or common defaults such as `*_test.go`, `tests/`, `*.spec.ts`). New tests are committed and pushed either way. Failures go back to the developer, whose fix returns the PR to review.
4. **Ready to merge.** The PR waits with the `intern:ready-to-merge` label. Without `AUTO_MERGE` a human merges it: this is the per-PR human gate. With `AUTO_MERGE=true` intern merges with `MERGE_METHOD` (default `squash`) once CI allows:
   - CI pending: wait.
   - CI failed: the failing check names and links go to the developer.
   - Merge conflict: the developer is asked to merge the base branch and resolve it.
   - Any other merge error is retried, then escalated after three failures in a row.
5. **Needs human.** After `MAX_REVIEW_ROUNDS` rejected reviews, twice that many developer fix rounds, a tester change outside test paths, or when a persona has questions, intern comments on the PR and the ticket and waits. Push fixes or answer in the PR, then add the `intern:resume` label to restart review with a fresh round budget.

The current stage is visible as an `intern:<stage>` label on the PR. Merged or closed PRs drop out of the flow; merged ones move their ticket to Done.

## Sandboxes

| `SANDBOX` | Isolation | Notes |
|---|---|---|
| `local` (default) | Same host, scrubbed environment. Credentials such as `GITHUB_TOKEN` and `JIRA_API_TOKEN` are not passed. Permissions run in `acceptEdits` mode with an allowlist of Bash commands, plus the project's own setup and verify commands. | Not a security boundary: the agent runs as the same user as intern. Fine for development and trusted tickets. The only option on Cloud Run, which can't run Docker. |
| `docker` | A fresh container per job with only the workspace mounted, no host environment, `no-new-privileges`, and the host uid so files stay owned by intern. The agent runs without permission prompts because the container is the boundary. | Set `SANDBOX_IMAGE` (build `deploy/sandbox/Dockerfile`) and optionally `SANDBOX_NETWORK`. Needs a host that can run Docker (VM or GKE, not Cloud Run). |

With Docker on macOS, `WORKING_DIR` must be on a path the Docker VM shares. Colima shares only your home directory by default, so a workspace under `/tmp` mounts as an empty directory.

`SANDBOX_NETWORK=bridge` allows all egress. To restrict the agent to the model API and package registries, attach the container to a network that routes through an egress proxy. Agent Substrate provides default-deny egress natively and is the planned upgrade path (see the roadmap).

## The project contract

The target repository describes how to verify a change in `.intern/project.yaml`:

```yaml
setup:
  - name: deps
    run: npm ci
verify:
  - name: lint
    run: npm run lint
  - name: test
    run: npm test
    timeout: 15m
protected_paths: [".github/", ".intern/"]
test_paths: ["tests/", "*.test.ts"]   # where the tester may write
```

After the runner finishes, intern gates the diff (allowed directories, file count, protected paths, file size, symlinks, credential patterns), commits, runs `setup` then `verify`, and hands any failure output back to the same runner session up to `VERIFY_FIX_ATTEMPTS` times. Without this file, intern falls back to `go vet` / `go test` according to `RUN_VET_BEFORE_PR` / `RUN_TESTS_BEFORE_PR`.

## Testing against the real CLI

Both end-to-end tests spend real resources and are opt-in:

```bash
# Claude Code developer pipeline on a tiny Go repo (about $0.01 on haiku)
INTERN_E2E_CLAUDE=1 INTERN_E2E_MODEL=haiku go test ./internal/orchestrator -run TestE2E_ClaudeCodeDeveloper -v

# Docker sandbox mechanics
docker build -t intern-sandbox:latest deploy/sandbox
INTERN_E2E_DOCKER_IMAGE=intern-sandbox:latest INTERN_E2E_DOCKER_DIR=$HOME/.cache/intern-e2e go test ./internal/sandbox -run TestDocker_Real -v
```
