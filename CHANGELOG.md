# Changelog

Notable changes and the reasoning behind them. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Fixed

- `MONGODB_URI` in `compose.yaml` pointed the backend at Mongo's
  host-remapped port instead of its container-internal port. Compose port
  mappings (`host:container`) only affect connections from the host; traffic
  between containers on the Compose network always uses the container's own
  port. Whenever a developer remapped Mongo's host port to resolve a local
  conflict, the backend could no longer reach it. `MONGODB_URI` now always
  targets `mongodb:27017` regardless of the host mapping.

### Added

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
- README roadmap "Phase 3": single lobby QR code with cookie-based player
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
