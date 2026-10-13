# Image for SANDBOX=docker: one fresh container per runner job, with only
# the ticket's workspace mounted at /workspace. It carries the Claude Code
# CLI plus common toolchains so the agent can build and test the target
# project. Add the toolchains your target repos need.
#
# Build: docker build -t intern-sandbox:latest deploy/sandbox
FROM golang:1.23-bookworm

ARG CLAUDE_CODE_VERSION=latest
ARG NODE_MAJOR=22

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git make python3 python3-pip python3-venv \
 && curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
 && rm -rf /var/lib/apt/lists/* /root/.npm

# The sandbox runs as the host user's uid with HOME=/tmp/home, so tool
# caches must be writable by anyone.
ENV GOPATH=/tmp/go GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go/pkg/mod \
    npm_config_cache=/tmp/npm-cache DISABLE_AUTOUPDATER=1
RUN git config --system --add safe.directory /workspace

WORKDIR /workspace
