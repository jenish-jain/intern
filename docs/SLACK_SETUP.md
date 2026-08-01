# Slack Setup

Lets you assign tasks to the agent and get status updates from a Slack conversation
instead of requiring a JIRA account. This is the `TICKETING_MODE=slack` path,
served over HTTP via `agent serve` (see `docs/CLOUD_RUN_DEPLOY.md` for deploying it).

## 1. Create the Slack app

1. Go to https://api.slack.com/apps → **Create New App** → **From scratch**.
2. Name it (e.g. "AI Intern") and pick your workspace.

## 2. Add bot scopes

Under **OAuth & Permissions → Scopes → Bot Token Scopes**, add:

- `app_mentions:read` — see `@AI Intern <ask>` mentions in channels
- `chat:write` — post status replies
- `im:history` — read DMs sent directly to the bot (optional, for DM-based asks)
- `im:read` — required alongside `im:history` for DM support

Install the app to your workspace, then copy the **Bot User OAuth Token**
(`xoxb-...`) — this is `SLACK_BOT_TOKEN`.

## 3. Subscribe to events

Under **Event Subscriptions**:

1. Toggle **Enable Events** on.
2. **Request URL**: `https://<your-deployed-url>/slack/events`
   - Slack sends a `url_verification` challenge here as soon as you enter the
     URL — the agent must already be reachable (deploy first, see
     `docs/CLOUD_RUN_DEPLOY.md`) for this step to succeed.
3. Under **Subscribe to bot events**, add:
   - `app_mention` — for `@AI Intern` mentions in channels
   - `message.im` — for direct messages (optional)

## 4. Get the signing secret

Under **Basic Information → App Credentials**, copy the **Signing Secret** —
this is `SLACK_SIGNING_SECRET`. The agent uses it to verify that incoming
webhook requests actually came from Slack (HMAC-SHA256 over the request body,
per Slack's request-signing scheme).

## 5. Configure the agent

```bash
TICKETING_MODE=slack
SLACK_BOT_TOKEN=xoxb-...
SLACK_SIGNING_SECRET=...
PORT=8080
```

GitHub and AI provider configuration (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, etc.)
are still required — only the ticketing source changes. `JIRA_*` and
`AGENT_USERNAME`/`POLLING_INTERVAL` are not required in `slack` mode.

## 6. Run it

```bash
go run ./cmd/agent serve
```

Then in Slack: `@AI Intern add input validation to the signup handler`.
The bot replies in-thread with an ack, then again with the PR link (or an
error) once done — this can take several minutes for a real ticket, since it
runs the full AI planning + quality-gate + PR pipeline.

## Notes / limits (MVP)

- One "ticket" per Slack thread (keyed by channel + thread timestamp), so
  replying in the same thread does **not** currently continue the same
  ticket — each mention starts a new one.
- No access control yet: any user who can message the bot can trigger a
  ticket. Restrict who can DM/invite the bot at the Slack app or channel
  level until this is addressed.
