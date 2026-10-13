# Setting up intern

Setup takes three commands: one to configure, one to check, one to deploy. The wizard asks only for what your chosen mode needs, and `agent doctor` tells you exactly what's wrong before you find out in Slack.

```bash
git clone https://github.com/jenish-jain/intern && cd intern
scripts/setup.sh                 # ask, write .env, check every credential
scripts/deploy-cloudrun.sh       # or run locally: build/agent serve
```

`scripts/setup.sh` needs Go 1.23+ or Docker. Everything else (`gcloud` for Cloud Run, the Claude Code CLI for local runs) is checked and explained when it's needed.

## 1. Choose a mode

| Mode | Use it to | Runs as | Needs |
|---|---|---|---|
| **Team** | Plan a project with the architect in Slack; developers, reviewer and tester build the JIRA backlog | `agent serve` | GitHub, JIRA, Slack, Anthropic |
| **JIRA** | Build tickets assigned to the agent, one PR each | `agent start` | GitHub, JIRA, Anthropic |
| **Slack** | Hand out one-off tasks with `@intern`, one PR each | `agent serve` | GitHub, Slack, Anthropic |

## 2. Collect credentials

Have these ready before running the wizard. Each one is checked by `agent doctor`.

**GitHub.** The repo intern writes code in. For a new project, create an empty repo with one commit on its default branch (the architect needs a branch to open its plan PR against). Create a [fine-grained token](https://github.com/settings/personal-access-tokens/new) for that repo with **Contents**, **Pull requests** and **Issues** set to read and write.

**JIRA** (team and JIRA modes).

- An [API token](https://id.atlassian.com/manage-profile/security/api-tokens) for the account intern signs in as.
- A project whose **Task** workflow has the statuses **To Do**, **In Progress**, **In Review** and **Done**. Intern moves tickets by status name; if your names differ, set the `JIRA_TRANSITION_*` transition IDs in `.env`.
- A JIRA user for the agent (`AGENT_USERNAME`). Intern picks up tickets assigned to it, and assigns the tickets the architect creates to it.

**Slack** (team and Slack modes). The wizard writes `slack-app-manifest.yaml` and tells you when to use it:

1. At [api.slack.com/apps](https://api.slack.com/apps), choose **Create New App → From an app manifest**, pick your workspace and paste the manifest.
2. **Install to Workspace**, then copy the **Bot User OAuth Token** (`xoxb-…`) and, from **Basic Information**, the **Signing Secret**.
3. For team mode, create a planning channel (e.g. `#intern-architect`) and a notifications channel (e.g. `#intern-notify`). Copy each channel's ID from **channel details → About** (it starts with `C`).

The first manifest has no events URL, because Slack verifies the URL when the manifest is saved and intern isn't running yet. Step 4 adds it.

**Anthropic.** An [API key](https://console.anthropic.com/settings/keys). The Claude Code runner and the architect, reviewer and tester personas all use it.

## 3. Run the wizard

```bash
scripts/setup.sh         # or: make configure
```

It builds intern, asks for each setting with a sensible default, and writes `.env` (keeping any previous file as `.env.bak`). Re-running it uses your current values as defaults, so pressing enter keeps them; secrets are typed without echo. It finishes by running `agent doctor`:

```
✓ Configuration: valid
✓ git: installed
✓ Claude Code CLI: 2.1.0 (Claude Code)
✓ GitHub: can read acme/todo-app
✓ Base branch: main
✓ JIRA: signed in as bot@acme.com
✓ JIRA project: TODO
! JIRA workflow: missing statuses: In Review
    Fix: Add them to the project's workflow, or set the matching JIRA_TRANSITION_* IDs.
✓ JIRA agent user: ai-intern
✓ Slack: bot @intern in Acme
✓ Anthropic: API key works
```

`✗` blocks intern from working; `!` is a warning worth fixing. A check that isn't OK lists each setting it used (`Using:`), with secrets masked and whether the value came from `.env` or from your shell (a variable exported in the shell overrides `.env`). Run `agent doctor` (or `make doctor`) any time after editing `.env`.

## 4. Deploy

### Cloud Run

```bash
gcloud auth login && gcloud config set project <your-project>
scripts/deploy-cloudrun.sh           # or: REGION=asia-south1 SERVICE=intern scripts/deploy-cloudrun.sh
```

The script:

- updates an existing service in its own region and keeps its service account (set `REGION`, `SERVICE` or `SERVICE_ACCOUNT` to override);
- enables the Cloud Run, Secret Manager, Cloud Build and Artifact Registry APIs;
- stores `GITHUB_TOKEN`, `JIRA_API_TOKEN`, `ANTHROPIC_API_KEY`, `SLACK_BOT_TOKEN` and `SLACK_SIGNING_SECRET` in Secret Manager (a new version only when a value changed) and lets the service's account read them;
- passes every other `.env` setting as an environment variable, replacing the service's previous ones, so `.env` is the source of truth;
- builds the image from the repo with Cloud Build (or deploys `IMAGE=…`);
- in team mode, runs exactly one always-on instance, because the JIRA poll loop and the conversation state live in that process;
- writes the service URL to `.env` as `PUBLIC_URL` and regenerates `slack-app-manifest.yaml` with the events URL.

Then finish the Slack app: open **your app → App Manifest**, paste the new `slack-app-manifest.yaml` and save. Slack verifies `https://<service>/slack/events` against the running service. Invite the bot to your channels with `/invite @intern`.

Preview the commands without changing anything with `DRY_RUN=1 scripts/deploy-cloudrun.sh` (secret values are hidden). If `gcloud` refuses because a secret name already exists as a plain environment variable on the service, deploy once with `SECRETS=env`, which passes secrets as environment variables instead.

Cloud Run can't run Docker, so the agent runs with `SANDBOX=local` there. For `SANDBOX=docker`, run on a VM or GKE.

### Locally or on a VM

```bash
build/agent serve        # team and Slack modes
build/agent start        # JIRA mode
```

Slack must reach `/slack/events`, so expose the port with a tunnel such as `cloudflared tunnel --url http://localhost:8080` or `ngrok http 8080`, then run `build/agent slack-manifest --url <tunnel-url>` and paste the result into the app's **App Manifest** page.

## 5. Try it

Team mode: in the planning channel, post

```
@intern plan: A to-do app with sign-in, lists, and due-date reminders by email.
```

The architect replies in the thread with questions. Answer them in the thread, then reply `approve` once the backlog looks right. [TEAM_MODE.md](TEAM_MODE.md) walks through the rest of the workflow.

Slack mode: `@intern add a /healthz endpoint that returns 200`. JIRA mode: assign a ticket to the agent user.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `legacy runner only supports the developer role` | The architect needs `AI_RUNNER=claude-code`. The wizard sets it for team mode; re-run it, then redeploy. |
| Slack shows "Your URL didn't respond" | Intern isn't reachable yet. Deploy first, then paste the manifest with the URL. |
| The bot doesn't answer in a channel | Invite it with `/invite @intern`, and check the app subscribes to `message.channels` (`message.groups` for private channels). |
| Tickets never start | Check `agent doctor`: the agent user, the statuses, and that tickets are assigned to `AGENT_USERNAME`. Tickets also wait for every ticket that blocks them to be done. |
| Planning stopped after a redeploy | Conversation state lives in the instance; a new revision starts empty. Start a new thread with the latest requirements. |
| `The project budget is used up` | Raise `PROJECT_BUDGET_USD` in `.env` and redeploy. |
| Slack says intern is going to sleep | The model API refused every request, for example because the Anthropic spend limit was reached. Fix the cause, then run the wake command from the message. See [TEAM_MODE.md](TEAM_MODE.md#sleeping-after-an-outage). |

## Changing settings

Re-run `scripts/setup.sh` (or edit `.env`), check with `agent doctor`, then `scripts/deploy-cloudrun.sh` again. More detail: [TEAM_MODE.md](TEAM_MODE.md) for the team workflow and every setting, [RUNNERS.md](RUNNERS.md) for runners, personas and sandboxes.
