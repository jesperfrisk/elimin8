.PHONY: up down logs ps build clean \
	backend-run backend-test backend-vet backend-fmt-check \
	frontend-dev frontend-build frontend-test e2e-up check

## Fast pre-push gate (same checks as CI; needs no running services)
check: backend-fmt-check backend-vet backend-test frontend-build

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

## Remove containers, networks, and the mongodb volume
clean:
	docker compose down -v

## Backend (native, requires Go 1.27+; needs mongodb running, e.g. `make up` or `docker compose up -d --wait mongodb`)
backend-run:
	cd backend && go run ./cmd/api

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
	docker compose -f compose.yaml -f compose.e2e.yaml up --build --wait -d

## End-to-end tests (requires the stack from `make e2e-up`; plain `make up` hits the 30/min login limit)
frontend-test:
	cd frontend && npm run test:e2e
