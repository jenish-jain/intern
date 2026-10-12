# Team mode: from a PRD to merged code

Team mode runs intern as a small software team. A product owner talks to the **architect** in Slack. The architect turns the requirements into a plan and a JIRA backlog. **Developers**, a **reviewer** and a **tester** then build, review, test and merge the tickets in dependency order. People only answer questions, approve plans and give feedback.

## How a project goes

1. **Share the PRD.** In the architect channel, mention the bot with your requirements, or write `@intern plan: <requirements>` anywhere. Paste the PRD text directly.
2. **Answer questions.** The architect replies in the thread with up to five questions, each with a recommended default. Reply in the thread; no mention is needed. Each round updates a **Project plan** PR with `docs/PRD.md`, `docs/architecture.md`, decision records, `CLAUDE.md` and `.intern/project.yaml`.
3. **Approve the backlog.** When the plan is ready, the architect posts the proposed epics and tickets. Reply `approve` to merge the plan and create the JIRA tickets, assigned to the agent and linked by their dependencies. Reply with anything else to change it.
4. **Watch it build.** The poll loop picks up tickets whose blockers are done. For each ticket a developer opens a PR, the reviewer reviews it, the tester writes acceptance tests, and intern merges once CI passes (with `AUTO_MERGE`). Escalations and merges are announced in the notify channel.
5. **Help when asked.** When a PR needs a person, intern explains why on the PR, the ticket and the notify channel. Fix or answer on the PR, then add the `intern:resume` label. When a developer has questions about a ticket, edit the ticket's description with the answer and it is retried.
6. **Change the plan.** Message the architect in the same thread at any time. Changes become a new plan PR and new tickets; tickets already created are never rewritten.

## Configuration

Team mode is `agent serve` with `TICKETING_MODE=jira` and Slack configured. The JIRA poll loop runs in the background of the Slack endpoint.

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
| `TICKET_BUDGET_USD`, `MAX_REVIEW_ROUNDS`, `VERIFY_FIX_ATTEMPTS` | Cost and loop limits |

The Slack app needs the `app_mentions:read`, `chat:write`, `im:history` and `channels:history` scopes (add `groups:history` for private channels). Subscribe to the `app_mention`, `message.im` and `message.channels` events, with the request URL `https://<host>/slack/events`.

On Cloud Run the process must stay up for the poll loop: set minimum instances to 1 and CPU to always allocated. Cloud Run can't run Docker, so use `SANDBOX=local` there, or run on a VM or GKE for `SANDBOX=docker`.

## Starting with a human merge gate

Leave `AUTO_MERGE` off at first. Approved and tested PRs then wait with the `intern:ready-to-merge` label for a person to merge them, which is a cheap way to build trust in the reviewer and tester before letting the team merge on its own.
