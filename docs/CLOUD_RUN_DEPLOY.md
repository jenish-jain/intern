# Cloud Run Deployment

Deploys the agent as a request-driven service: it scales to zero when idle
and spins up on the first Slack event. Requires `TICKETING_MODE=slack`
(see `docs/SLACK_SETUP.md`) — the JIRA polling loop (`agent start`) is a
long-running process and isn't a fit for Cloud Run's request model.

## Why `--no-cpu-throttling`

Slack requires an HTTP ack within ~3 seconds, but a ticket (AI planning, git
operations, quality gates, PR creation) takes minutes. The handler acks
immediately and finishes the work in a background goroutine. By default
Cloud Run throttles CPU to near-zero once a response is sent, which would
stall that goroutine. `--no-cpu-throttling` keeps CPU allocated for the life
of the container instance, not just the request, so background work
actually runs to completion.

This is an MVP tradeoff: under very sparse traffic, Cloud Run can still
scale an idle instance down between the ack and the background work
finishing. If that becomes a problem in practice, the fix is to hand off
processing to Cloud Tasks (webhook enqueues a task, a second endpoint does
the work with its own request lifetime) instead of a bare goroutine — not
implemented here, note it as a follow-up if reliability issues show up.

## 1. Build and push the image

```bash
PROJECT_ID=your-gcp-project
REGION=us-central1
IMAGE=us-central1-docker.pkg.dev/$PROJECT_ID/ai-intern/agent:latest

gcloud auth configure-docker $REGION-docker.pkg.dev
docker build -t $IMAGE .
docker push $IMAGE
```

(Requires an Artifact Registry repo — `gcloud artifacts repositories create ai-intern --repository-format=docker --location=$REGION` if you don't have one yet.)

## 2. Store secrets

```bash
echo -n "xoxb-..."      | gcloud secrets create slack-bot-token --data-file=-
echo -n "..."           | gcloud secrets create slack-signing-secret --data-file=-
echo -n "ghp_..."       | gcloud secrets create github-token --data-file=-
echo -n "sk-ant-..."    | gcloud secrets create anthropic-api-key --data-file=-
```

## 3. Deploy

```bash
gcloud run deploy ai-intern-agent \
  --image=$IMAGE \
  --region=$REGION \
  --platform=managed \
  --no-cpu-throttling \
  --timeout=3600 \
  --min-instances=0 \
  --max-instances=5 \
  --concurrency=5 \
  --memory=1Gi \
  --set-env-vars="TICKETING_MODE=slack,GITHUB_OWNER=your-org,GITHUB_REPO=your-repo,AI_PROVIDER=anthropic,BASE_BRANCH=main,WORKING_DIR=/tmp/workspace" \
  --set-secrets="SLACK_BOT_TOKEN=slack-bot-token:latest,SLACK_SIGNING_SECRET=slack-signing-secret:latest,GITHUB_TOKEN=github-token:latest,ANTHROPIC_API_KEY=anthropic-api-key:latest" \
  --allow-unauthenticated
```

Notes:

- `--allow-unauthenticated` is required so Slack can reach `/slack/events`
  directly; the handler authenticates requests itself via Slack's request
  signature (`SLACK_SIGNING_SECRET`), so this doesn't leave the endpoint open.
- `WORKING_DIR=/tmp/workspace`: Cloud Run's filesystem is writable only
  under `/tmp` (in-memory, cleared per instance) unless you mount a volume;
  `/tmp` is fine here since each request re-clones/syncs the repo anyway.
- `--concurrency=5` bounds how many Slack events one instance handles at
  once; each is a full clone+AI+PR pipeline, so keep this low relative to
  `--memory`.

## 4. Point Slack at it

```bash
gcloud run services describe ai-intern-agent --region=$REGION --format='value(status.url)'
```

Use that URL + `/slack/events` as the Event Subscriptions Request URL
(step 3 in `docs/SLACK_SETUP.md`).

## 5. Verify

```bash
curl https://<service-url>/healthz
```
