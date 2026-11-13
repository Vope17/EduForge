#!/bin/sh
# Verify the legacy volume identity and SQLite writes using only disposable data.
set -eu
docker compose run --rm --no-deps \
    -e EDUFORGE_DB=/tmp/eduforge-permissions/test.sqlite \
    --entrypoint sh backend -c '
    set -e
    id -u | grep -qx 100
    id -g | grep -qx 101
    mkdir -m 700 /tmp/eduforge-permissions
    trap "rm -rf /tmp/eduforge-permissions" EXIT
    /app/server migrate
    test -s /tmp/eduforge-permissions/test.sqlite
    test -w /tmp/eduforge-permissions/test.sqlite
    test -w "$CODEX_HOME"
    codex --version >/dev/null
    echo "Backend identity, SQLite and Codex home write permissions: OK"
'
