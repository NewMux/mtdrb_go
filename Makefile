# CoachPulse developer entrypoints.
SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml
GO ?= go

# Migrations run as the owner; the API runs as the RLS-bound app role. Keeping
# the two URLs distinct is what makes the isolation tests meaningful.
OWNER_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/coachpulse?sslmode=disable
APP_DATABASE_URL   ?= postgres://coachpulse_app:coachpulse_app@localhost:5432/coachpulse?sslmode=disable

.PHONY: help up down logs reset migrate migrate-down migrate-status generate \
        build run worker test test-integration lint fmt vet tidy verify app-install app-start

help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

up: ## Start Postgres and MinIO, then apply migrations
	$(COMPOSE) up -d --wait
	$(MAKE) migrate

down: ## Stop the local stack
	$(COMPOSE) down

reset: ## Destroy and rebuild the local stack from scratch
	$(COMPOSE) down -v
	$(MAKE) up

logs: ## Tail local stack logs
	$(COMPOSE) logs -f

migrate: ## Apply all pending migrations as the database owner
	DATABASE_URL="$(OWNER_DATABASE_URL)" $(GO) run ./cmd/migrate up

migrate-down: ## Roll back the most recent migration
	DATABASE_URL="$(OWNER_DATABASE_URL)" $(GO) run ./cmd/migrate down

migrate-status: ## Show migration state
	DATABASE_URL="$(OWNER_DATABASE_URL)" $(GO) run ./cmd/migrate status

generate: ## Regenerate the client's API types from api/openapi.yaml
	cd app && npm run api-types

build: ## Compile all binaries
	$(GO) build ./...

run: ## Start the API against the local stack
	DATABASE_URL="$(APP_DATABASE_URL)" $(GO) run ./cmd/api

worker: ## Start the recurring-jobs worker
	DATABASE_URL="$(APP_DATABASE_URL)" $(GO) run ./cmd/worker

test: ## Run unit tests
	$(GO) test -race ./...

# -p 1 is required, not incidental: integration packages share one database
# and reset it between tests, so running them concurrently lets one package's
# TRUNCATE land in the middle of another's assertions.
test-integration: ## Run tests that require Postgres (RLS, ledger, journeys)
	TEST_DATABASE_URL="$(OWNER_DATABASE_URL)" \
	TEST_APP_DATABASE_URL="$(APP_DATABASE_URL)" \
	$(GO) test -race -p 1 -tags=integration ./...

fmt: ## Format all Go sources
	gofmt -w ./cmd ./internal

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint
	golangci-lint run

tidy: ## Tidy module requirements
	$(GO) mod tidy

verify: fmt vet lint test ## Everything CI runs, locally

app-install: ## Install Expo client dependencies
	cd app && npm install

app-start: ## Start the Expo client
	cd app && npx expo start

app-typecheck: ## Typecheck the Expo client
	cd app && npx tsc --noEmit

app-test: ## Run the Expo client's tests
	cd app && npx jest

app-bundle: ## Bundle every route — catches imports typecheck cannot
	cd app && npx expo export --platform web --output-dir dist

app-verify: app-typecheck app-test app-bundle ## Everything CI runs for the client
