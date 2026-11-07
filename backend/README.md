# Elimin8 Backend

The backend is a Go HTTP API backed by MongoDB, with an embedded Mochi MQTT broker for realtime updates. It currently manages games, organizer-owned rosters, private-code access, joining, starting, and ending. Teams, secret assignments, eliminations, timers, and results remain future work.

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
    realtime/              # Authenticated MQTT-over-WebSocket broker and outbox
  README.md
```

The API uses Go's standard `net/http` package and the official MongoDB Go driver v2. Controllers register game handlers on the application's mux; there is no separate routes package. Code-based credentials authorize all game access.

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

MongoDB currently stores two collections:

- `games`: game details, unique invitation code, organizer-code hash, lifecycle timestamps, embedded player roster, revision, and last published revision. Each player stores their name, private-code hash, credential version, joined timestamp, and inclusion flag.
- `game_access`: hashed temporary bearer tokens, scoped game/player identity, credential version, and expiry. A TTL index removes expired entries; authorization also checks expiry directly.

Startup creates a unique invitation-code index and the access-token TTL index. Game documents are created by API requests; no seed data or manual database migration is necessary.

All game mutations take a per-game mutex from a bounded lock pool. Persistence uses an atomic `updateOne` filtered by game ID and previous revision. If another instance changes the game, the service reloads and rechecks the rules before retrying. Starting selects joined players and sets the active status in the same write, so a concurrent join either commits first and is included or is rejected after the start. MongoDB is the final concurrency authority.

Roster size has no small application-level total-player cap. MongoDB documents have a 16 MiB limit; the repository reserves space by rejecting writes above 12 MiB, and request bodies are limited to 1 MiB. Adding players accepts up to 50 names per request; the frontend automatically submits larger rosters in batches of 25. Concurrent scrypt calculations are bounded to control memory use.

Related assignment, elimination, and scoring changes must be atomic. If these operations span multiple documents, local development and deployment will need a MongoDB replica set to support transactions.

Compose currently runs a standalone MongoDB instance for local development, with authentication disabled and its host port bound to localhost. Configure a replica set before implementing multi-document transactions. This Compose configuration is not a production deployment.

Connection settings come from `MONGODB_URI` and `MONGODB_DATABASE`. Real credentials and local database files must remain untracked.

## Privacy Requirements

The server is authoritative for assignments, permissions, timers, and scores. Public responses must never expose secret assignments, missions, team memberships, killer identities, or in-game kill counts. Private player responses expose only that player's permitted information; organizer access must be explicitly authorized.

See the [project README](../README.md) for the complete game rules and roadmap.

## Current Status

Game lifecycle, organizer rosters, private-code authentication, authorized MQTT updates, and MongoDB persistence are implemented. Missions and victim assignments are deliberately absent.

## Game API

| Method | Endpoint | Access / behavior |
| --- | --- | --- |
| POST | `/api/games` | Create a lobby; returns temporary organizer access and a one-time management code |
| POST | `/api/access` | Log in with `invitationCode`, private `code`, and `role` (`organizer` or `player`); first player login joins the lobby |
| GET | `/api/games/{id}` | Read the caller's authorized view |
| POST | `/api/games/{id}/players` | Organizer: add `{ "names": ["Alice", "Bob"] }`; returns generated private codes once |
| POST | `/api/games/{id}/players/{player}/code` | Organizer: reset a player's code and revoke prior credentials |
| POST | `/api/games/{id}/start` | Organizer: atomically freeze joined participants and activate the lobby |
| POST | `/api/games/{id}/end` | Organizer: end a lobby or active game permanently |
| GET (upgrade) | `/api/mqtt` | Authenticated MQTT over WebSockets |

Protected HTTP requests use `Authorization: Bearer <token>`. Tokens expire after 12 hours. Private codes are normalized to uppercase with spaces/hyphens ignored, and hashed with scrypt using the random game ID as salt. Player codes have eight unambiguous random characters; organizer management codes have 24. Code reset preserves joined status and the frozen participant roster while incrementing the credential version. Players see their own identity and aggregate counts; only organizers receive the named roster. No response exposes stored hashes.

Anonymous creation/login attempts are limited per remote address and endpoint. Forwarding headers are not trusted, so clients behind Nginx share its limit of 30 attempts per minute for each endpoint. For larger deployments, configure a trusted-proxy-aware limiter before exposing the service publicly. Use HTTPS/WSS outside localhost and keep private codes out of logs and URLs.

## MQTT and Delivery

The broker is embedded in the Go process and uses the API's authentication checks. Nginx and Vite proxy WebSocket upgrades on `/api/mqtt`; no public MQTT TCP listener exists. MQTT clients use their temporary bearer token as username, a client ID beginning with the returned `clientPrefix`, clean sessions, and no will message.

- Organizers subscribe only to `games/{id}/organizer/updates`.
- Players subscribe only to `games/{id}/player/updates`.
- Client publishing, wildcard subscriptions, and access to other roles/games are denied.
- Access is rechecked for subscriptions, message delivery, and keepalives, including expiry and code-reset revocation.
- The backend publishes QoS 1 invalidations containing only `gameId`, `type: "game.updated"`, and `revision`.

Browsers fetch their authorized state after subscribing, after reconnecting, and when notified. They ignore stale HTTP revisions and serialize refreshes to avoid missing updates during an in-flight request. No periodic frontend state requests run; failed synchronization requests retry with backoff.

The game revision and last published revision form a coalescing outbox stored atomically with state changes. A worker wakes after mutations and retries pending delivery every three seconds. Duplicate notifications and coalesced intermediate revisions are expected. MongoDB remains authoritative, and reconnecting clients always resynchronize. Broker subscriptions are in memory; database state/outbox survive restarts.

Run one API/broker instance with this configuration. Revision checks protect database writes across instances, but horizontal realtime fan-out requires a shared broker or shared event bus before scaling out.
