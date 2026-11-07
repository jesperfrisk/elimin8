# Elimin8 Backend

The backend will be a Go HTTP API backed by MongoDB. It will manage games, players, teams, secret assignments, elimination confirmations, withdrawals, timers, and results.

## Planned Structure

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
    middleware/            # Authentication, authorization, and request middleware
    database/              # MongoDB connection lifecycle and indexes
  README.md
```

Only this README is created for now. The source directories and Go module will be added when implementation begins.

## Layer Responsibilities

- **Controllers** validate HTTP input, call services, and return responses containing only information the caller may access.
- **Services** enforce game rules, permission checks, team-safe assignments, elimination confirmation, and victory detection. They depend on repository interfaces defined alongside the services that use them.
- **Models** define domain data and invariants independently of HTTP handlers and MongoDB-specific types.
- **Repositories** implement persistence using the official MongoDB Go driver and map stored documents to domain models.

Request flow: HTTP request -> controller -> service -> repository -> MongoDB.

## MongoDB

MongoDB will persist games, players, teams, missions, assignments, and elimination claims. The exact collections and indexes will be designed during implementation.

Related assignment, elimination, and scoring changes must be atomic. If these operations span multiple documents, local development and deployment will need a MongoDB replica set to support transactions.

Connection settings will come from environment variables such as `MONGODB_URI` and `MONGODB_DATABASE`. Real credentials and local database files must remain untracked; an `.env.example` can document required settings when configuration is implemented.

## Privacy Requirements

The server is authoritative for assignments, permissions, timers, and scores. Public responses must never expose secret assignments, missions, team memberships, killer identities, or in-game kill counts. Private player responses expose only that player's permitted information; organizer access must be explicitly authorized.

See the [project README](../README.md) for the complete game rules and roadmap.

## Current Status

Documentation scaffold only. No Go module, dependencies, database connection, or runnable server has been created yet.
