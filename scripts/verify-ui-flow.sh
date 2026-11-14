#!/bin/sh
# Runs scripts/verify-ui-flow.mjs against a throwaway Docker Compose project
# (own name, port and volumes), so the flow never touches real project data.
# The flow creates a project and changes provider settings; the stack and its
# volumes are removed afterwards.
# Requires: docker compose, and EDUFORGE_PLAYWRIGHT_MODULE pointing at
# Playwright's index.mjs on the host.
set -eu
cd "$(dirname "$0")/.."
export EDUFORGE_PORT="${EDUFORGE_UI_FLOW_PORT:-18090}"
project=eduforge-uiflow
compose() { docker compose -p "$project" "$@"; }
trap 'compose down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up --build -d --wait --wait-timeout 120
EDUFORGE_UI_FLOW_URL="http://127.0.0.1:$EDUFORGE_PORT" node scripts/verify-ui-flow.mjs
