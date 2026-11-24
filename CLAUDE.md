# CLAUDE.md

Guidance for Claude Code (and other AI assistants) working in this repository.

## Project

Elimin8 is a real-life social elimination game: every player gets a secret
victim and mission, and a killer inherits their victim's target and mission
when that victim is eliminated. See [README.md](README.md) for game rules,
[ROADMAP.md](ROADMAP.md) for planned work (keep its checkboxes current when
shipping a feature), and [backend/README.md](backend/README.md) /
[frontend/README.md](frontend/README.md) for architecture.

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

## Git workflow (branches, PRs, agents)

```
feature/* | fix/* | chore/*  --PR-->  dev  --PR (human approves)-->  main
```

- **`main` is never touched directly** — no commits, pushes, or merges by
  Claude or any agent. Every PR into `main` comes from `dev` and must be
  approved and merged by a human.
- **`dev` is the integration branch.** Branch every feature from the latest
  `origin/dev` and open the PR back into `dev` (squash merge). Don't push to
  `dev` directly either.
- Branch names: `<feature|fix|chore>/<issue#>-<short-slug>`. One issue = one
  branch = one PR. Never leave a bare `git push` pointing at `dev`/`main`:
  push with an explicit `HEAD:<branch>`.
- Run `make check` (gofmt, vet, backend tests, frontend build) before
  pushing; CI runs the same on PRs into `dev` and `main`.
- Agents never run `gh pr merge`, approve PRs, or force-push (denied in
  `.claude/settings.json`). A force-with-lease on a single owned feature
  branch after a rebase is the only exception, and needs the user's approval.
- **The one sanctioned merge path** is `.claude/scripts/merge-to-dev.sh <pr#>`
  (used by the `pr-reviewer` agent). It refuses unless the PR targets `dev`,
  is non-draft, mergeable, CI-green, not labelled `hold`/`needs-human`, and has
  a `pr-reviewer: LGTM <head-sha>` comment for the current head commit. Never
  edit it to loosen those checks without a human's approval. Also enable
  GitHub branch protection on `main` (and required CI on `dev`) as the real
  backstop; agent permissions alone are not a security boundary.

### Loops (one role per session)

- **Reviewer:** `/loop 5m /review-loop` (`.claude/skills/review-loop/`).
  Sweeps every open PR into `dev` (drafts too), comments or signs off, and merges
  green ones via the `pr-reviewer` agent.
- **Worker:** `/loop 10m /worker-loop` (`.claude/skills/worker-loop/`). Holds
  **one ticket at a time**, fixes review feedback on its PR
  (`ticket-worker` step 8), and claims the next free ticket
  (`/pick-ticket`) only after that PR is merged.

Review state lives in GitHub (`pr-reviewer: LGTM|CHANGES|CONFLICT <sha>`
comments, `needs-human` / `hold` labels); the issue assignee is the ticket
lock. Any number of each role may run concurrently.

### Ticket workers and the orchestrator

- `/orchestrate-tickets <goal|issues>` (`.claude/skills/orchestrate-tickets/`)
  slices work into issues, dispatches workers, tracks PRs, resolves
  conflicts, and opens the `dev` -> `main` release PR for human review.
- Workers are the `ticket-worker` subagent (`.claude/agents/ticket-worker.md`,
  **Sonnet 5.5**, own worktree) following `.claude/skills/ticket-worker/`.
  Each ticket is sized for **~150k tokens**; a worker that can't fit reports
  `NEEDS-SPLIT` instead of grinding on.
- **The human decides how, the agent recommends.** Before writing code a
  worker settles the open design decisions with the person it works with
  (`ticket-worker` step 2a: `AskUserQuestion` with a recommended option first,
  or the `grill-me` skill for tangled tickets) and records them in the PR's
  Decisions section. Unattended runs take the recommended option and mark it
  `default, unconfirmed` so review can overrule it.
- Don't run parallel tickets that touch the same files (e.g. `models/game.go`,
  `main.jsx`, READMEs); the orchestrator serializes those.

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
