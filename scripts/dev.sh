#!/bin/sh
# Runs a command inside the dev image with the checkout mounted (Go and the
# office tools are not installed on the host). Usage: scripts/dev.sh make test
set -eu
cd "$(dirname "$0")/.."
docker build -q --target dev -t eduforge-dev . >/dev/null
exec docker run --rm -v "$PWD:/src:z" \
  -v eduforge-gocache:/go/pkg -v eduforge-gobuild:/root/.cache \
  -v eduforge-web-modules:/src/frontend/node_modules -v eduforge-math-modules:/src/math/node_modules \
  -e GOFLAGS=-buildvcs=false \
  eduforge-dev "$@"
