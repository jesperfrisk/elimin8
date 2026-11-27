.PHONY: up down logs ps build reset clean \
	backend-run backend-test backend-vet backend-fmt-check \
	frontend-dev frontend-build frontend-test e2e-up check

## Fast pre-push gate (same checks as CI; needs no running services)
check: backend-fmt-check backend-vet backend-test frontend-build

## Source revision embedded in the backend (see GET /api/version). Override with
## BUILD_REVISION=<sha>; "unknown" if git metadata is unavailable.
BUILD_REVISION ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
export BUILD_REVISION

## Docker Compose stack (backend + frontend + mongodb)
up:
	docker compose up --build --wait

down:
	docker compose down

logs:
	docker compose logs -f

ps:
	docker compose ps

build:
	docker compose build

## DESTRUCTIVE: remove this project's containers, networks and data volumes
## (mongodb_data games/access records, rustfs_data photos) and leave it stopped
## and empty. Scoped to the Compose project ("elimin8" from compose.yaml, or
## COMPOSE_PROJECT_NAME / `make reset COMPOSE_PROJECT_NAME=elimin8-e2e`); never
## prunes globally or touches other projects, .env or source. Does not start or
## seed anything. Safe to repeat. SEED_MANIFEST is the local seed marker `make dev`
## will write (path to agree with the seeding ticket); removing it lets the next
## `make dev` seed again.
SEED_MANIFEST ?= .dev-seed.json
reset:
	docker compose down --volumes --remove-orphans
	rm -f -- $(SEED_MANIFEST)

## Alias of reset (kept for existing habits)
clean: reset

## Backend (native, requires Go 1.27+; needs mongodb running, e.g. `make up` or `docker compose up -d --wait mongodb`)
backend-run:
	cd backend && go run -ldflags "-X github.com/jesperfrisk/elimin8/backend/internal/buildinfo.revision=$(BUILD_REVISION)" ./cmd/api

backend-test:
	cd backend && go test ./...

backend-vet:
	cd backend && go vet ./...

backend-fmt-check:
	@cd backend && test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

## Frontend (native, requires Node 22.12+; needs backend running on :8080)
frontend-dev:
	cd frontend && npm ci && npm run dev

frontend-build:
	cd frontend && npm ci && npm run build

## Isolated stack for the browser suite: raises the login limit to 600/min (test-only)
e2e-up:
	docker compose -p elimin8-e2e -f compose.yaml -f compose.e2e.yaml up --build --wait -d

## End-to-end tests (requires the stack from `make e2e-up`; plain `make up` hits the 30/min login limit)
frontend-test:
	cd frontend && npm run test:e2e
