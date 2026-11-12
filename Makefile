COMPOSE ?= docker compose

.PHONY: dev up down build test lint migrate config

dev:
	$(COMPOSE) -f compose.yaml -f compose.dev.yaml up --build --watch

up:
	$(COMPOSE) up --build --detach --wait --wait-timeout 120

down:
	$(COMPOSE) down --remove-orphans

build:
	$(COMPOSE) build backend frontend

test:
	$(COMPOSE) --profile tools run --rm --build --no-deps backend-tools go test ./...
	$(COMPOSE) --profile tools run --rm --build --no-deps frontend-tools npm test

lint:
	$(COMPOSE) --profile tools run --rm --build --no-deps backend-tools sh -ec 'files="$$(gofmt -l .)"; if [ -n "$$files" ]; then printf "%s\n" "$$files"; exit 1; fi; go vet ./...'
	$(COMPOSE) --profile tools run --rm --build --no-deps frontend-tools npm run lint

migrate:
	$(COMPOSE) run --rm --build --no-deps backend migrate

config:
	$(COMPOSE) config --quiet
	$(COMPOSE) -f compose.yaml -f compose.dev.yaml config --quiet

