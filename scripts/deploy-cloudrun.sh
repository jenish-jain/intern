#!/usr/bin/env bash
# Deploy intern to Cloud Run from the .env that `agent setup` writes.
#
# Secrets go to Secret Manager and everything else to plain environment
# variables. Team mode (JIRA plus Slack) gets one always-on instance, because
# the JIRA poll loop and the conversation state live in that process.
#
# Usage:
#   scripts/deploy-cloudrun.sh
#   PROJECT=my-project REGION=asia-south1 SERVICE=intern scripts/deploy-cloudrun.sh
#   DRY_RUN=1 scripts/deploy-cloudrun.sh      # print the gcloud commands only
#
# Settings (environment):
#   ENV_FILE  config to deploy (default .env)
#   PROJECT   GCP project (default: gcloud's current project)
#   REGION    Cloud Run region (default: the existing service's region,
#             else gcloud's run/region, else us-central1)
#   SERVICE   Cloud Run service name (default ai-intern-agent)
#   IMAGE     deploy this image instead of building the repo with Cloud Build
#   SECRETS   "manager" (default) or "env" to pass secrets as plain env vars
#   SERVICE_ACCOUNT  account the service runs as (default: the existing
#             service's, else the Compute Engine default)
#   DRY_RUN   1 prints commands instead of running them
set -euo pipefail

ENV_FILE=${ENV_FILE:-.env}
REGION=${REGION:-}
SERVICE=${SERVICE:-ai-intern-agent}
IMAGE=${IMAGE:-}
SECRETS=${SECRETS:-manager}
DRY_RUN=${DRY_RUN:-0}

SECRET_KEYS=(GITHUB_TOKEN JIRA_API_TOKEN ANTHROPIC_API_KEY SLACK_BOT_TOKEN SLACK_SIGNING_SECRET)
# PORT is set by Cloud Run; PUBLIC_URL is only used to write the Slack manifest;
# DRY_RUN is this script's setting, not the agent's.
SKIP_KEYS=(PORT PUBLIC_URL DRY_RUN)

die() { echo "error: $*" >&2; exit 1; }
run() {
  if [[ $DRY_RUN == 1 ]]; then
    { printf '+'; printf ' %q' "$@"; printf '\n'; } >&2
  else
    "$@"
  fi
}
contains() { local x=$1; shift; for y in "$@"; do [[ $x == "$y" ]] && return 0; done; return 1; }

command -v gcloud >/dev/null || die "gcloud is not installed: https://cloud.google.com/sdk/docs/install"
[[ -f $ENV_FILE ]] || die "$ENV_FILE not found; run \`agent setup\` first"
PROJECT=${PROJECT:-$(gcloud config get-value project 2>/dev/null || true)}
[[ -n $PROJECT ]] || die "set PROJECT, or run \`gcloud config set project <id>\`"
if [[ -z $REGION ]]; then
  # Update an existing service where it runs, rather than creating a second
  # one in another region.
  found=$(gcloud run services list --project "$PROJECT" --filter "metadata.name=$SERVICE" --format 'value(region)' 2>/dev/null || true)
  if [[ $(printf '%s\n' "$found" | grep -c .) -gt 1 ]]; then
    die "$SERVICE exists in several regions ($(echo $found)); set REGION"
  fi
  REGION=${found:-$(gcloud config get-value run/region 2>/dev/null || true)}
  REGION=${REGION:-us-central1}
fi

# Load .env the way the agent does (single-quoted values are literal), into
# ENV_<key> variables so its settings can't change this script's own, such
# as DRY_RUN or REGION.
keys=()
while IFS= read -r key; do keys+=("$key"); done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$ENV_FILE" | cut -d= -f1 | awk '!seen[$0]++')
eval "$(env -i /bin/bash -c 'set -a; . "$1"; shift; for k in "$@"; do printf "ENV_%s=%q\n" "$k" "${!k-}"; done' _ "$ENV_FILE" "${keys[@]}")"
env_get() { local v="ENV_$1"; printf '%s' "${!v-}"; }
GITHUB_TOKEN=$(env_get GITHUB_TOKEN)
TICKETING_MODE=$(env_get TICKETING_MODE)
SLACK_BOT_TOKEN=$(env_get SLACK_BOT_TOKEN)
SANDBOX=$(env_get SANDBOX)

[[ -n ${GITHUB_TOKEN:-} ]] || die "$ENV_FILE has no GITHUB_TOKEN; run \`agent setup\`"
team=0
if [[ ${TICKETING_MODE:-jira} == jira && -n ${SLACK_BOT_TOKEN:-} ]]; then team=1; fi
if [[ ${TICKETING_MODE:-jira} == jira && -z ${SLACK_BOT_TOKEN:-} ]]; then
  die "Cloud Run serves Slack; with TICKETING_MODE=jira also configure Slack (team mode), or run \`agent start\` on a VM"
fi
if [[ ${SANDBOX:-local} == docker ]]; then
  die "Cloud Run can't run Docker: set SANDBOX=local, or deploy to a VM or GKE for SANDBOX=docker"
fi

echo "Deploying $SERVICE to $PROJECT ($REGION) from $ENV_FILE ($([[ $team == 1 ]] && echo team mode || echo Slack mode))"

run gcloud services enable run.googleapis.com secretmanager.googleapis.com cloudbuild.googleapis.com \
  artifactregistry.googleapis.com --project "$PROJECT"

# Plain settings, as a YAML env file so values need no escaping.
envfile=$(mktemp)
trap 'rm -f "$envfile"' EXIT
secret_flags=()
for key in "${keys[@]}"; do
  contains "$key" "${SKIP_KEYS[@]}" && continue
  val=$(env_get "$key")
  [[ -z $val ]] && continue
  if [[ $SECRETS == manager ]] && contains "$key" "${SECRET_KEYS[@]}"; then
    secret_flags+=("$key")
    continue
  fi
  printf "%s: '%s'\n" "$key" "${val//\'/\'\'}" >>"$envfile"
done
grep -q '^WORKING_DIR:' "$envfile" || echo "WORKING_DIR: '/tmp/workspace'" >>"$envfile"
if [[ $DRY_RUN == 1 ]]; then
  echo "Plain settings:"
  sed -E "s/^($(IFS='|'; echo "${SECRET_KEYS[*]}")): .*/\1: (hidden)/; s/^/  /" "$envfile"
fi

secrets_arg=""
if [[ ${#secret_flags[@]} -gt 0 ]]; then
  # The account the service runs as reads the secrets: SERVICE_ACCOUNT, else
  # the existing service's, else Compute Engine's default.
  runtime_sa=${SERVICE_ACCOUNT:-$(gcloud run services describe "$SERVICE" --project "$PROJECT" --region "$REGION" \
    --format 'value(spec.template.spec.serviceAccountName)' 2>/dev/null || true)}
  if [[ -z $runtime_sa ]]; then
    number=$(gcloud projects describe "$PROJECT" --format='value(projectNumber)')
    runtime_sa=$number-compute@developer.gserviceaccount.com
  fi
  echo "Secrets will be readable by $runtime_sa"
  for key in "${secret_flags[@]}"; do
    name="intern-$(echo "$key" | tr 'A-Z_' 'a-z-')"
    val=$(env_get "$key")
    if ! gcloud secrets describe "$name" --project "$PROJECT" >/dev/null 2>&1; then
      run gcloud secrets create "$name" --project "$PROJECT" --replication-policy automatic
    fi
    current=$(gcloud secrets versions access latest --secret "$name" --project "$PROJECT" 2>/dev/null || true)
    if [[ $current != "$val" ]]; then
      echo "Storing $key in Secret Manager as $name"
      if [[ $DRY_RUN == 1 ]]; then
        echo "+ gcloud secrets versions add $name --data-file=- (value hidden)"
      else
        printf '%s' "$val" | gcloud secrets versions add "$name" --project "$PROJECT" --data-file=- >/dev/null
      fi
    fi
    run gcloud secrets add-iam-policy-binding "$name" --project "$PROJECT" \
      --member "serviceAccount:$runtime_sa" --role roles/secretmanager.secretAccessor --condition None >/dev/null
    secrets_arg+="${secrets_arg:+,}$key=$name:latest"
  done
fi

deploy=(gcloud run deploy "$SERVICE" --project "$PROJECT" --region "$REGION"
  --allow-unauthenticated --no-cpu-throttling --timeout 3600 --memory 2Gi
  --env-vars-file "$envfile")
if [[ -n $IMAGE ]]; then deploy+=(--image "$IMAGE"); else deploy+=(--source .); fi
if [[ -n $secrets_arg ]]; then deploy+=(--set-secrets "$secrets_arg"); fi
if [[ $team == 1 ]]; then
  # One instance: the poll loop and agent_state.jsonc live in the process.
  deploy+=(--min-instances 1 --max-instances 1 --concurrency 20)
else
  deploy+=(--min-instances 0 --max-instances 5 --concurrency 5)
fi
run "${deploy[@]}"

[[ $DRY_RUN == 1 ]] && exit 0
url=$(gcloud run services describe "$SERVICE" --project "$PROJECT" --region "$REGION" --format 'value(status.url)')
echo
echo "Deployed: $url"
if curl -fsS "$url/healthz" >/dev/null 2>&1; then echo "Health check: ok"; else echo "Health check: failed; see the logs with:"; echo "  gcloud run services logs read $SERVICE --project $PROJECT --region $REGION"; fi

if grep -q '^PUBLIC_URL=' "$ENV_FILE"; then
  sed -i.tmp "s|^PUBLIC_URL=.*|PUBLIC_URL=$url|" "$ENV_FILE" && rm -f "$ENV_FILE.tmp"
else
  printf 'PUBLIC_URL=%s\n' "$url" >>"$ENV_FILE"
fi
if command -v go >/dev/null && go run ./cmd/agent slack-manifest --url "$url" >slack-app-manifest.yaml 2>/dev/null; then
  echo
  echo "Last step: turn on Slack events. Open api.slack.com/apps > your app > App Manifest,"
  echo "paste slack-app-manifest.yaml (now with $url/slack/events) and save."
else
  echo
  echo "Last step: in your Slack app, Event Subscriptions > Request URL: $url/slack/events"
  echo "and subscribe to app_mention, message.channels, message.groups and message.im."
fi
