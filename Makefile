.PHONY: up down logs ps build clean \
	backend-run backend-test backend-vet \
	frontend-dev frontend-build frontend-test

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

## Frontend (native, requires Node 22.12+; needs backend running on :8080)
frontend-dev:
	cd frontend && npm ci && npm run dev

frontend-build:
	cd frontend && npm ci && npm run build

## End-to-end tests (requires the Docker stack running, e.g. `make up`)
frontend-test:
	cd frontend && npm run test:e2e
