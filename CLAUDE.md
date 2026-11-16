# CLAUDE.md

Guidance for Claude Code (and other AI assistants) working in this repository.

## Project

Elimin8 is a real-life social elimination game: every player gets a secret
victim and mission, and a killer inherits their victim's target and mission
when that victim is eliminated. See [README.md](README.md) for game rules
and [backend/README.md](backend/README.md) / [frontend/README.md](frontend/README.md)
for architecture.

- **Backend**: Go (`net/http`, official MongoDB driver v2), MongoDB, an
  embedded Mochi MQTT broker for realtime updates over WebSockets.
- **Frontend**: React + Vite, hash-based routing, MQTT.js for live updates.
- **Infra**: Docker Compose (`compose.yaml`) for local dev; Nginx serves the
  built frontend and proxies `/api/` to the backend in the Docker image.

## Common commands

Prefer the root `Makefile` targets; see it for the full list.

```sh
make up              # docker compose up --build --wait
make down             # docker compose down (keeps DB data)
make clean            # docker compose down -v (deletes DB data)
make logs

make backend-test     # go test ./... (needs no running services)
make backend-vet      # go vet ./...
make backend-run       # go run ./cmd/api (needs mongodb running separately)

make frontend-dev      # npm run dev (needs backend running on :8080)
make frontend-build
make frontend-test      # Playwright e2e; needs the Docker stack running
```

## Local environment

- Host ports for `mongodb`/`backend`/`frontend` are overridable via
  `MONGODB_PORT`, `BACKEND_PORT`, `FRONTEND_PORT` in a root `.env` (copy
  from `.env.example`). Never hardcode a different port directly in
  `compose.yaml` — that's how a prior bug slipped in (see CHANGELOG.md).
- `MONGODB_URI` inside `compose.yaml` must stay `mongodb://mongodb:27017`
  (the container-internal port). Only host-side port *mappings* are
  configurable; container-to-container traffic on the Compose network is
  unaffected by host port remaps.
- `backend/.env.example` is for running the Go binary natively, outside
  Docker — different values, different purpose, not a duplicate of the
  root `.env.example`.

## Conventions worth knowing

- The backend has no routes package; controllers register handlers
  directly on the mux. Request flow: HTTP request → controller → service →
  repository → MongoDB.
- The server is authoritative for all game state, permissions, assignments,
  scores, and timers. Public/player responses must never leak missions,
  targets, team membership, or kill counts — hiding something in the UI is
  not sufficient; it must not be present in the API response for that role.
- MongoDB writes use per-game mutexes plus atomic `updateOne` filtered by
  game ID and revision; a concurrent writer reloads and rechecks rules
  before retrying. Don't introduce multi-document writes for related state
  (assignments, eliminations, scores) without a replica set — see
  backend/README.md.
- `.claude/` is tracked and shared across the team (skills, project
  settings). `.claude/worktrees/` (session worktrees) and
  `.claude/settings.local.json` (per-user overrides) are gitignored and
  must never be committed.
