#!/usr/bin/env bash
# One step from a fresh clone to a checked configuration: builds intern,
# asks for the settings your chosen mode needs, writes .env and checks every
# credential with `agent doctor`. Needs Go 1.23+ or Docker.
#
# Usage: scripts/setup.sh [--env-file path]
set -euo pipefail
cd "$(dirname "$0")/.."

if command -v go >/dev/null; then
  echo "Building intern..."
  go build -o build/agent ./cmd/agent
  exec build/agent setup "$@"
fi

if command -v docker >/dev/null; then
  echo "Go isn't installed; building intern's Docker image instead (takes a few minutes the first time)..."
  docker build -q -t intern-agent . >/dev/null
  exec docker run --rm -it --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD:/work" -w /work --entrypoint /app/agent intern-agent setup "$@"
fi

echo "error: install Go 1.23+ (https://go.dev/dl) or Docker, then re-run scripts/setup.sh" >&2
exit 1
