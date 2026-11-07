# Elimin8 Backend

The backend will be a Go HTTP API backed by MongoDB. It will manage games, players, teams, secret assignments, elimination confirmations, withdrawals, timers, and results.

## Structure

```text
backend/
  cmd/
    api/
      main.go              # Application entry point
  internal/
    config/                # Environment configuration
    controllers/           # HTTP request handling and response mapping
    services/              # Game rules and application logic
    models/                # Domain types: games, players, teams, missions, claims
    repositories/
      mongodb/             # MongoDB persistence implementations
    database/              # MongoDB connection lifecycle and indexes
  README.md
```

The API uses Go's standard `net/http` package and the official MongoDB Go driver v2. Handlers are registered in `cmd/api/main.go`; there is no separate routes package. Authentication and game features will be added later.

## Run with Docker

From the repository root:

```sh
docker compose up --build --wait
```

The API is available at `http://localhost:8080`. Compose starts MongoDB first, waits for its health check, then starts the API. MongoDB data persists in a named Docker volume.

## Run Locally

Install Go 1.27 or newer (or enable Go's automatic toolchain download) and start MongoDB:

```sh
docker compose up -d --wait mongodb
cd backend
go run ./cmd/api
```

Defaults are `HTTP_ADDR=:8080`, `MONGODB_URI=mongodb://localhost:27017`, and `MONGODB_DATABASE=elimin8`. See [.env.example](.env.example) for reference. Export overrides into your shell before running Go; `.env` files are not automatically loaded. For local-only access, set `HTTP_ADDR=127.0.0.1:8080`.

```sh
go test ./...
go vet ./...
```

## Health Endpoints

- `GET /api/health/live`: returns HTTP 200 while the API is serving, independently of MongoDB.
- `GET /api/health/ready`: pings MongoDB with a two-second timeout and returns HTTP 200 when connected, or HTTP 503 when unavailable. Connection details are never returned.

Startup verifies the connection before listening. Shutdown drains HTTP requests and closes the MongoDB client. The Docker image runs the API as a non-root user.

## Layer Responsibilities

- **Controllers** validate HTTP input, call services, and return responses containing only information the caller may access.
- **Services** enforce game rules, permission checks, team-safe assignments, elimination confirmation, and victory detection. They depend on repository interfaces defined alongside the services that use them.
- **Models** define domain data and invariants independently of HTTP handlers and MongoDB-specific types.
- **Repositories** implement persistence using the official MongoDB Go driver and map stored documents to domain models.

Request flow: HTTP request -> controller -> service -> repository -> MongoDB.

## MongoDB

MongoDB will persist games, players, teams, missions, assignments, and elimination claims. No game collections or indexes are created yet; only connectivity is implemented.

Related assignment, elimination, and scoring changes must be atomic. If these operations span multiple documents, local development and deployment will need a MongoDB replica set to support transactions.

Compose currently runs a standalone MongoDB instance for local development, with authentication disabled and its host port bound to localhost. Configure a replica set before implementing multi-document transactions. This Compose configuration is not a production deployment.

Connection settings come from `MONGODB_URI` and `MONGODB_DATABASE`. Real credentials and local database files must remain untracked.

## Privacy Requirements

The server is authoritative for assignments, permissions, timers, and scores. Public responses must never expose secret assignments, missions, team memberships, killer identities, or in-game kill counts. Private player responses expose only that player's permitted information; organizer access must be explicitly authorized.

See the [project README](../README.md) for the complete game rules and roadmap.

## Current Status

Runnable infrastructure scaffold with MongoDB connectivity and health checks. Game APIs, authentication, and authorization are not implemented yet.
