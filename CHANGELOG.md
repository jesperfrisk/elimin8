# Changelog

Notable changes and the reasoning behind them. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Added

- Post-game recap UI (#42): once a game has ended, the public board (`#/board/<id>`, no login) and the organizer/player game pages show a recap with a winner hero, kill leaderboard (ties shown as ties), elimination timeline with `mm:ss` offsets, award cards and a game summary, with empty states and a data-table fallback. On the board it replaces the player grid ("Show players" toggles back). It consumes the existing recap routes and shows subjects only, never killers. Reduced motion disables the staggered reveal. Winners come from the server-calculated result, including team survival wins and timer ties with zero kills. Both recap routes expose only winner kind, ID and name after the game ends.
- Public board recap API (#85): unauthenticated `GET /api/board/{gameId}/recap` returns the same payload as the authenticated recap for ended games (409 before the end, 404 for unknown ids), reusing `RecapOf`, the board's per-IP rate limit and `Cache-Control: no-store`. Tests assert the exact keys and that no killer, mission, target, code or team-membership data appears.
- Remembered-player credential foundation (#82, part of #71): player code login
  sets a game-scoped 30-day HttpOnly, SameSite-protected cookie, Secure over HTTPS
  or an explicitly trusted TLS proxy. Only hashes and fixed-expiry metadata are
  stored in a separate MongoDB collection with TTL cleanup. Validation checks
  application expiry and current player version; remember secrets never authorize
  bearer routes or appear in JSON. Browser logins validate Origin, and Nginx now
  preserves Host/port. Exchange and browser restoration remain #83/#84.
- `make reset` (#76): destructive local reset that runs `docker compose down --volumes --remove-orphans` for this Compose project only (honors `COMPOSE_PROJECT_NAME`), deleting the MongoDB and RustFS volumes (games, access records, photos) and the local seed marker, and leaving the stack stopped and empty with no reseeding. Repeatable. `make clean` is now an alias for it, so there is one cleanup implementation.
- Persistent private elimination notice (#74): after a victim accepts an elimination, their own game view gains `me.elimination` (`killer`, `mission`, `confirmedAt`) and the player page shows an "Elimination notice" panel that survives reloads and game-ending kills. The mission is snapshotted at acceptance (before the transfer) and never serialized in the shared `eliminations` list; legacy eliminations report `mission: null`. No other role sees the killer/mission relationship, and no push, service worker or OS notification is added. Existing dynamic-target and team-repair tests still pass; the ROADMAP notification item is ticked.

### Fixed

- Reconcile Phase 1 documentation with shipped behavior (#46): correct
  usage and privacy descriptions for teams, timers, kill counts and board
  access, and add a [capability-by-capability verification checklist](ROADMAP.md#phase-1-implementation-and-verification)
  with merged implementation, regression tests and remaining runtime checks.
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

### Changed

- Login and game-creation rate limits are now keyed by a validated client
  address instead of the socket peer (#52), so players behind the Compose
  Nginx proxy no longer share one bucket. `TRUSTED_PROXIES` (comma-separated
  IPs/CIDRs; default empty = trust nothing, ignore `X-Forwarded-For`) lists
  the proxies whose forwarded header is believed; it is read right to left,
  skipping trusted hops, and a malformed value stops startup. A fixed
  aggregate cap of 600 attempts per minute per endpoint still bounds code
  hashing across many clients. 429 and `Retry-After` are unchanged.
  `compose.yaml` trusts the private Docker ranges by default.

### Added

- Built-in mission presets (#75): organizers can select laugh, high-five,
  Cheers and selfie tasks in the lobby, then explicitly add them to that
  game's mission list alongside custom entries. They use the existing
  enable/disable and assignment rules, remain hidden from players and public
  views, and do not alter another game's missions. Rejected saves retain the
  selection for retry; successful saves clear it before the game refresh.
- Public live board UI (#39): `#/board/<gameId>` is a full-screen, read-only
  wall display for a TV or phone, with no login and no app chrome. It reads
  only the public board endpoint and subscribes anonymously to the board
  topic (one refetch per `board.updated`, no polling, a visible
  "Reconnecting…" state). Each player is a poster tile showing their picture
  (initials for now) with their name and a text status; display numbers are
  not shown. Active tiles glow, eliminated tiles are dimmed with a red X,
  withdrawn tiles are grey and dashed. Tiles are sized to fit the screen from
  2 to about 100 players without scrolling on a TV. Includes lobby, active and
  ended ("Game over", survivors tagged) states, a friendly 404 screen, a
  fullscreen button, and a copy-board-link button in the organizer and player
  game views.
- Player photo UI (#41): a **Your photo** panel for joined players with
  **Take a photo** (front camera on phones) and **Choose a photo**, a preview
  in the shape of a board tile, and nothing uploaded or public until it is
  confirmed. The browser shrinks photos to at most 1200 px before upload, so
  camera photos never hit the 5 MiB or 25 megapixel limit and EXIF never
  leaves the device. Errors from the server are shown inline, the panel says
  the photo is public on the board and deleted 24 hours after the game ends,
  and uploads stop once the game has ended. Organizers get an avatar per
  roster row with Add, Replace and Remove. Photos now fill the live board
  tiles (the public board payload gained an optional opaque `photoUrl` and a
  photo change republishes the board), with initials as the fallback and the
  same dimming and X for eliminated players. Shared `PlayerAvatar` component
  for the recap to reuse.
- Board elimination moment (#40): a live elimination flashes and shakes the
  tile, sweeps a red X across it, dims it and ticks the active count down,
  with a low synthesized Web Audio gong. Withdrawals get a quieter grey
  fade and no sound; the end of the game plays a winner flourish and a
  short chord before the "Game over" banner. Several eliminations queue and
  play in order, speeding up after the first three so a burst takes about
  11 s at most. Nothing animates or sounds on first load, reconnect catch-up
  or tab wake-up. Sound sits behind a "Tap to enable sound" gate with a
  remembered choice and a mute toggle, works when audio or `localStorage` is
  blocked, and `prefers-reduced-motion` swaps the motion for a short fade.
- Optional player photos, delivered as three backend slices of #37 (the photo
  UI is #41, below):
  - Private local RustFS object storage in Compose and an S3 adapter (#53).
    Native development can leave storage disabled.
  - Authorized photo upload, replace, remove and read (#54): JPEG/PNG/WebP up
    to 5 MiB is decoded, rotated, stripped of EXIF/GPS and re-encoded as a
    JPEG of at most 400px. Players manage their own photo, organizers any
    player's; reads use opaque IDs and never expose bucket paths.
  - Restart-safe retention (#55): photos expire 24 hours after the game ends
    (`PHOTO_RETENTION_HOURS`), expired reads return 404 before the sweep
    runs, and a startup plus periodic sweep deletes every object under the
    game's prefix, including replaced and orphaned uploads.
- Post-game recap endpoint (#38): `GET /api/games/{id}/recap` (organizer and
  participants, ended games only) returns the summary, leaderboard,
  elimination timeline and awards, naming subjects but never killers or
  pairs. Withdrawals appear in the timeline and are not kills.
- Game results (#20): when a game ends, the server stores the final result
  once and serves it to everyone in the game; a results screen shows the
  winner or winners, ties and standings with confirmed kills.
- Winner detection (#19): the server ends the game when one player (or one
  team, including a team with an unbeatable majority) remains, after every
  confirmed elimination and withdrawal.
- Privacy-safe Kill Board (#18): `GET /api/games/{id}/killboard` lists names
  and active/eliminated/withdrawn status in name order, with a live panel in
  the game view.
- Confirmed player withdrawals (#13): two-step withdrawal that is not a kill;
  the withdrawing player's hunter inherits their victim and mission.
- Private kill tracking (#14): accepted eliminations increment the killer's
  confirmed kills in the same revision-checked write. Totals feed the
  results and are not exposed elsewhere.

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
