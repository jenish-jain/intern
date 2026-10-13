# Team mode: from a PRD to merged code

Team mode runs intern as a small software team. A product owner talks to the **architect** in Slack. The architect turns the requirements into a plan and a JIRA backlog. **Developers**, a **reviewer** and a **tester** then build, review, test and merge the tickets in dependency order. People only answer questions, approve plans and give feedback.

## How a project goes

1. **Share the PRD.** In the architect channel, mention the bot with your requirements, or write `@intern plan: <requirements>` anywhere. Paste the PRD text directly.
2. **Answer questions.** The architect replies in the thread with up to five questions, each with a recommended default. Reply in the thread; no mention is needed. Each round updates a **Project plan** PR with `docs/PRD.md`, `docs/architecture.md`, decision records, `CLAUDE.md` and `.intern/project.yaml`.
3. **Approve the backlog.** When the plan is ready, the architect posts the proposed epics and tickets. Reply `approve` to merge the plan and create the JIRA tickets, assigned to the agent and linked by their dependencies. Reply with anything else to change it.
4. **Watch it build.** The poll loop picks up tickets whose blockers are done. For each ticket a developer opens a PR, the reviewer reviews it, the tester writes acceptance tests, and intern merges once CI passes (with `AUTO_MERGE`). Reviews and tests run alongside developers building other tickets, so a PR is reviewed soon after it opens. PRs still merge in plan order: one that passes before an earlier ticket's PR waits with the `intern:queued` label, and becomes `intern:ready-to-merge` once the earlier ones have merged. Escalations and merges are announced in the notify channel.
5. **Help when asked.** When a PR needs a person, intern explains why on the PR, the ticket and the notify channel. Comment on the PR (or push fixes), then add the `intern:resume` label: comments you left since the escalation go to the developer as feedback; with no comments, the PR goes back to review. When a developer has questions about a ticket, edit the ticket's description with the answer and it is retried.
6. **Review each epic.** When every ticket of an epic has merged, intern announces it in the notify channel and in the planning thread. The architect checks the merged code against the architecture and decision records and, if it drifted or left gaps, proposes follow-up tickets in the same thread for you to approve. Try the epic and reply in the thread with feedback. Set `EPIC_REVIEW=false` to skip the architect check.
7. **Change the plan.** Message the architect in the same thread at any time. Changes become a new plan PR and new tickets; tickets already created are never rewritten.

## Configuration

`scripts/setup.sh` asks for all of this and checks it; see [SETUP.md](SETUP.md). Team mode is `agent serve` with `TICKETING_MODE=jira` and Slack configured. The JIRA poll loop runs in the background of the Slack endpoint.

| Variable | Purpose |
|---|---|
| `TICKETING_MODE=jira` | Tickets live in JIRA |
| `SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET` | Slack app credentials |
| `SLACK_ARCHITECT_CHANNEL` | Channel ID whose new threads are architect conversations (optional; `plan:` works anywhere) |
| `SLACK_NOTIFY_CHANNEL` | Channel ID for escalations and merges (optional) |
| `AI_RUNNER=claude-code` | Required for the architect, reviewer and tester |
| `REVIEW_ENABLED`, `TEST_ENABLED`, `AUTO_MERGE` | Turn on the review, test and merge stages |
| `JIRA_AGENT_ACCOUNT_ID` | Assignee for created tickets (looked up from `AGENT_USERNAME` if unset) |
| `ALLOWED_WRITE_DIRS=*` | Let developers create the project's own layout |
| `TICKET_BUDGET_USD`, `MAX_REVIEW_ROUNDS`, `VERIFY_FIX_ATTEMPTS` | Cost and loop limits per job and per PR |
| `PROJECT_BUDGET_USD` | Total spend cap across all roles. Reaching it pauses planning, new tickets, reviews and tests (announced once in the notify channel); merges of ready PRs continue |
| `MAX_CONCURRENT_TICKETS` | Jobs of each kind run at once, each in its own worktree: this many developers building tickets, and this many review, fix or test rounds on open PRs |

The Slack app needs the `app_mentions:read`, `chat:write`, `im:history` and `channels:history` scopes (add `groups:history` for private channels). Subscribe to the `app_mention`, `message.im` and `message.channels` events, with the request URL `https://<host>/slack/events`.

On Cloud Run the process must stay up for the poll loop: set minimum instances to 1 and CPU to always allocated. Cloud Run can't run Docker, so use `SANDBOX=local` there, or run on a VM or GKE for `SANDBOX=docker`.

## Starting with a human merge gate

Leave `AUTO_MERGE` off at first. Approved and tested PRs then wait with the `intern:ready-to-merge` label for a person to merge them, which is a cheap way to build trust in the reviewer and tester before letting the team merge on its own. Only the next PR in plan order carries that label; later ones wait as `intern:queued`, so merging the labelled PRs as they appear keeps the plan's order. An earlier PR that needs a person holds the ones after it until it merges or closes.

## Watching it

With `METRICS_ENABLED=true`, `/metrics` adds team metrics to the existing ones: `ai_intern_role_cost_dollars{role=...}`, `ai_intern_project_spend_dollars`, review approvals and rejections, acceptance test passes and failures, merges and escalations. A falling escalation rate and few review rounds per ticket mean the plan's tickets are well specified; rising ones usually mean tickets are too large or vague.
