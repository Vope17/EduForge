#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p docs/evidence
node scripts/gen-types.mjs --check
node tests/frontend.test.mjs
node tests/store-schema.test.mjs
node --experimental-vm-modules scripts/check-source.mjs
docker compose config --quiet
command -v go >/dev/null 2>&1 || { printf '%s\n' 'Go is required for the remaining checks; no build has been verified.' >&2; exit 2; }
test -z "$(gofmt -l cmd internal)" || { printf '%s\n' 'Run gofmt -w cmd internal before verifying.' >&2; exit 2; }
go mod verify
go vet ./...
go test -race ./...
go build -buildvcs=false -o /tmp/eduforge-verified ./cmd/eduforge
cd frontend
npm ci
npm run build
