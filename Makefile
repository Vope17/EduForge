.PHONY: test build web run check compose types typecheck

test:
	go test -race ./...
	node scripts/gen-types.mjs --check
	node tests/frontend.test.mjs
	node tests/store-schema.test.mjs

# Regenerates frontend/src/api/domain.ts after changing the Go structs it mirrors.
types:
	node scripts/gen-types.mjs

typecheck:
	node scripts/gen-types.mjs --check
	cd frontend && npm run typecheck

web:
	cd frontend && npm ci && npm run build

build: web
	mkdir -p bin
	go build -buildvcs=false -trimpath -o bin/eduforge ./cmd/eduforge

run:
	go run ./cmd/eduforge serve

check:
	go vet ./...
	go test -race ./...
	node scripts/gen-types.mjs --check
	cd frontend && npm run build
	node tests/frontend.test.mjs
	node tests/store-schema.test.mjs

compose:
	docker compose up --build -d
