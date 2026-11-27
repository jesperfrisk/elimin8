# Changelog

Notable changes and the reasoning behind them. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Fixed

- After a team game starts, a player's own-team list (`me.team.members`) no
  longer includes invitees who never joined (#51). Lobby setup can assign
  unjoined invitees to a team, but starting freezes the roster to the joined
  participants. Included teammates stay listed after elimination or
  withdrawal, and the lobby view and the organizer's configuration are
  unchanged.
- The browser suite no longer fails with HTTP 429 from its own fixture logins (#45). The anonymous login limit (30 per minute per address) stays the default; `LOGIN_RATE_LIMIT_PER_MINUTE` can override it, bounded to 1-600 (invalid values keep 30), and `compose.e2e.yaml` / `make e2e-up` set 600 for the isolated test stack only. Added tests for rejection, Retry-After and the bounds.
- Pending elimination reports are now cancelled when team-circle repair gives
  the killer a different target (#49). Previously a report whose killer and
  victim were both still alive stayed pending, could not be accepted, and
  blocked the killer from reporting the new target. The check runs in the same
  revision-checked mutation as the transfer, so reconnects see the persisted
  cancellation.
- In team mode, a lone surviving player is now recorded as a win for their
  team (#44). `detectWinner` handled "one player left" before checking the
  game mode, so a team game ended with a player winner while the result still
  listed team standings. Individual mode is unchanged. Games that already
  ended this way keep their stored winner.
- Clearing the game timer and deleting all teams did not persist in MongoDB
  (#43): the repository used only `$set` with BSON `omitempty`, so a cleared
  field stayed stored and reappeared on the next read. `Save` now `$unset`s
  every absent top-level game field in the same revision-filtered atomic
  update, leaving `publishedRevision` (the outbox) untouched.
- Player mutations now revalidate the caller inside the revision-checked game write (#50). A report, response, withdrawal request or confirmation authorized before an organizer reset the player code, or whose access expired while it waited, is rejected as unauthorized and changes nothing, including after a revision-conflict retry. Actions committed before the reset stay valid, and new credentials work. Fresh requests, game scoping, organizer permissions and MQTT revocation are unchanged.

- `MONGODB_URI` in `compose.yaml` pointed the backend at Mongo's
  host-remapped port instead of its container-internal port. Compose port
  mappings (`host:container`) only affect connections from the host; traffic
  between containers on the Compose network always uses the container's own
  port. Whenever a developer remapped Mongo's host port to resolve a local
  conflict, the backend could no longer reach it. `MONGODB_URI` now always
  targets `mongodb:27017` regardless of the host mapping.

### Added

- Public live board API (#36): `GET /api/board/{gameId}` and an anonymous,
  read-only MQTT topic `boards/{gameId}/updates` expose only names, status, a
  stable display number, the active count and the timer, so a TV can watch a
  game from a link. New game ids are 26 characters (130 bits) because the id
  is now the board link; older 120-bit ids keep working. The topic only fires
  when the visible board changes, so private revisions (such as a pending
  elimination report) do not leak timing.
- `dev` integration branch and a documented `feature/* -> dev -> main` flow
  (see `CLAUDE.md`). `main` is only changed by human-approved PRs from `dev`.
- Multi-agent ticket workflow: `/orchestrate-tickets` skill (slices work into
  ~150k-token issues, dispatches workers, tracks PRs, resolves conflicts),
  `ticket-worker` subagent pinned to Sonnet 5.5 in its own worktree plus its
  playbook skill, and a shared `.claude/settings.json` that denies pushes to
  `main`/`dev`, force-pushes, `gh pr merge` and PR approvals.
- GitHub CI (gofmt, vet, backend tests, frontend build) on PRs into `dev` and
  `main`, a check that only `dev` may target `main`, plus PR and ticket
  templates. `make check` runs the same gate locally.
- Roadmap moved from `README.md` to its own `ROADMAP.md`, with a new
  "Phase 3": single lobby QR code with cookie-based player
  identity and code recovery, mission presets, elimination notifications
  (who/how), dynamic targets, selfie uploads with S3-compatible storage
  (RustFS) deleted at game end, and `make dev` / `make reset`. The
  accept/decline elimination flow and target-only kills already existed.
- Host ports for `mongodb`, `backend`, and `frontend` in `compose.yaml` are
  now overridable via `MONGODB_PORT`, `BACKEND_PORT`, and `FRONTEND_PORT` in
  a root `.env` file (see `.env.example`). Previously the only way to work
  around a local port conflict was editing `compose.yaml` directly, which is
  how the `MONGODB_URI` bug above was introduced. `docker compose` loads a
  root `.env` automatically, so no extra tooling is needed.
- Root `Makefile` wrapping the Docker Compose and native dev commands
  already documented in the project, backend, and frontend READMEs
  (`make up`, `make down`, `make backend-test`, `make frontend-dev`, etc.),
  so contributors don't need to remember or look up the underlying commands.
- `CLAUDE.md` with project context for AI coding assistants.
- `.claude/` is now tracked so Claude Code skills (e.g. `.claude/skills/grill-me/`)
  are shared across the team, instead of living only in each developer's
  home directory. `.claude/worktrees/` (per-session worktrees) and
  `.claude/settings.local.json` (per-user overrides) stay gitignored.

### Why two `.env.example` files?

`.env.example` (repo root) configures `docker compose` itself — only the
three host port overrides above. `backend/.env.example` documents
environment variables for running the Go binary directly on the host
(`go run ./cmd/api`), bypassing Docker. The two are not redundant: the
root file is consumed by Compose before any container starts, while the
backend file's values (e.g. `MONGODB_URI=mongodb://localhost:27017`) only
make sense for a process running outside the Compose network, and differ
from the values Compose injects into the `backend` container
(`mongodb://mongodb:27017`).
