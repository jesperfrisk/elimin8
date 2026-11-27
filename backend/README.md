# Elimin8 Backend

The backend is a Go HTTP API backed by MongoDB, with an embedded Mochi MQTT broker for realtime updates. It manages games, organizer-owned rosters, private-code access, lobby missions, private assignments, mutually confirmed eliminations, confirmed player withdrawals, assignment transfers, and last-player victory. Final results (winners, ties, and confirmed-kill standings) are calculated when a game ends by survival or timer.

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

### Layout (per-domain files)

Models, services and controllers are split by domain so feature work mostly owns its own files:

| Domain | `models/` | `services/` | `controllers/` |
| --- | --- | --- | --- |
| Core game, access | `game.go` | `games.go` (errors, repository, `mutate`) | `games.go` (wiring, rate limiting, auth) |
| Lifecycle (create, login, start/end) | | `lifecycle.go` | `lifecycle.go` |
| Players, credentials | `player.go` | `players.go` | `players.go` |
| Missions | `mission.go` | `missions.go` | `missions.go` |
| Eliminations | `elimination.go` | `eliminations.go`, `victims.go` | `eliminations.go` |
| API views | `views.go` | `views.go` | |
| Withdrawals | `withdrawals.go` | `withdrawals.go` | `withdrawals.go` |
| Kill Board | `killboard.go` | `killboard.go` | `killboard.go` |
| Teams, timer | `teams.go`, `timer.go` | same names | same names |
| Results | `results.go` | `results.go` | `results.go` |

Phase 1 state is persisted on `Game` (`Mode`, `Teams`, `Timer`, `EndReason`) and `Player` (`TeamID`, `ConfirmedKills`). Domain structs carry no JSON tags; role-scoped API views explicitly select fields. Team setup is organizer-only, a player sees only their own team, and kill totals appear only in post-game result/recap responses, never in game views for any role.

**Adding a domain or route:** put handlers in `controllers/<domain>.go` with a `register<Domain>(mux)` method that calls `mux.HandleFunc(...)`, then add one `c.register<Domain>(mux)` line to `GameController.Register` in `controllers/games.go` (the placeholder domains are already wired). Put rules in `services/<domain>.go` and types in `models/<domain>.go`.

The API uses Go's standard `net/http` package and the official MongoDB Go driver v2. Controllers register game handlers on the application's mux; there is no separate routes package. Code-based credentials authorize all game access.

## Run with Docker

From the repository root:

```sh
make up
```

(equivalent to `docker compose up --build --wait`; see the root [Makefile](../Makefile)). The API is available at `http://localhost:8080` (override with `BACKEND_PORT` in a root `.env`, see [.env.example](../.env.example)). Before starting, copy the root `.env.example` to `.env` and set unique `PHOTO_STORAGE_ACCESS_KEY` and `PHOTO_STORAGE_SECRET_KEY` values. Compose waits for MongoDB and the private RustFS object store to become healthy before starting the API. MongoDB and RustFS data persist in separate named Docker volumes.

## Run Locally

Install Go 1.27 or newer (or enable Go's automatic toolchain download) and start MongoDB:

```sh
docker compose up -d --wait mongodb
make backend-run
```

Defaults are `HTTP_ADDR=:8080`, `MONGODB_URI=mongodb://localhost:27017`, and `MONGODB_DATABASE=elimin8`. See [.env.example](.env.example) for reference. Export overrides into your shell before running Go; `.env` files are not automatically loaded. For local-only access, set `HTTP_ADDR=127.0.0.1:8080`.

```sh
make backend-test
make backend-vet
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

MongoDB currently stores three collections:

- `games`: game details, unique invitation code, organizer-code hash, lifecycle timestamps, embedded player roster, revision, and last published revision. Each player stores their name, private-code hash, credential version, joined timestamp, and inclusion flag.
- `game_access`: hashed temporary bearer tokens, scoped game/player identity, credential version, and expiry. A TTL index removes expired entries; authorization also checks expiry directly.
- `player_sessions`: separate hashed remembered-player credentials with game/player identity, credential version, creation time and fixed expiry. An absolute-expiry TTL index cleans them up; application validation enforces expiry even if TTL deletion is delayed. These credentials cannot authenticate bearer or MQTT requests.

**Saving and optional fields.** `GameRepository.Save` is one revision-filtered `updateOne`. Optional top-level `Game` fields use BSON `omitempty`, so a value the service clears (the timer, all teams) is simply absent from the marshaled document, and a bare `$set` would leave the old value stored. `Save` therefore adds a `$unset` for every top-level `Game` field that is absent from the document, in the same atomic update. The list of fields comes from the `Game` struct by reflection, so a new optional field is covered automatically. `_id` and `publishedRevision` are never written or unset by `Save`: the outbox field is only advanced by `Published`. Audit of the optional top-level fields: `startedAt`, `endedAt`, `winnerId`, `winnerKind`, `mode`, `teams`, `timer`, `endReason`, `result` and `withdrawals` are all handled this way. Nested optional fields (for example `Player.TeamID` or `WithdrawnAt`) need nothing extra, because `players` is always written as a whole array and replaces the stored one. No remaining cases.

Run the real-MongoDB persistence test with `MONGODB_TEST_URI=mongodb://localhost:27017 go test ./internal/repositories/...` (it uses a throwaway database and skips when the variable is unset, so `make backend-test` still needs no services).

Game documents also embed lobby mission candidates (`id`, `text`, `enabled`). They have no shared pool, separate collection, or lifecycle outside their game. IDs support lobby controls only. Starting copies one randomly selected mission text into each joined player's `mission` field. Every enabled entry is used once before reuse; each round uses a fresh Fisher–Yates shuffle with unbiased cryptographic random indices. Repeated text represents separate entries. Disabled entries and unjoined invitees are excluded. Starting requires at least two joined players and one enabled mission; failure returns HTTP 409 with an actionable error.

Victim assignment shuffles the joined roster into a single circle, storing the next participant's ID in each player's `victimId`. Each participant has exactly one victim and one hunter, with no self-targets or smaller disconnected circles. Player IDs distinguish participants even when names match. Team mode also forbids teammate targets and repairs the circle after transfers (see Team-safe assignment below).

Missions, victims, participant inclusion, and active status commit in the same revision-checked game write. Concurrent configuration changes or joins trigger a retry against current state. Failed starts save no partial assignments, and subsequent start requests cannot reroll a started game. Assignments survive reconnects, code resets, and ending the game. Each player view includes only their own `me.mission` and `me.victim` after the start; `victim` contains only `id` and `name`, never that victim's assignments. Organizer roster entries include all pairings. Lobby responses omit assignments. MQTT notifications contain no assignments and trigger authorized view refreshes.

Organizers add up to 100 missions per request, each with up to 1,000 Unicode characters and no internal line breaks. There is no small total-mission cap; the existing game-document size limit applies. New entries are enabled. Disabling retains the entry so it can be re-enabled. All mission changes require organizer access and lobby status, and use the existing atomic revision checks and MQTT invalidations. Only organizer views include mission configuration. Existing game documents without missions are treated as empty.

Elimination reports are embedded in the game with killer/victim IDs, creation/resolution timestamps, and `pending`, `accepted`, `declined`, or `cancelled` status. Only living participants in an active game can report their currently assigned target. The server checks the assignment again when accepting a report. Each killer can have one outgoing pending report, and each victim one incoming pending report. Only the named victim may respond. Accepting atomically marks the victim eliminated, transfers their mission and target to the killer, repairs the target circle, and cancels pending reports involving eliminated players. Other players retain their missions. The final survivor ends the game with `winnerId` and no remaining assignment. Declines leave assignments unchanged. Ending a game cancels pending reports. Duplicate or stale responses return HTTP 409.

In team mode, starting requires every joined player on a team and at least two teams with players (409 otherwise). The organizer's game view gains `mode` and `teams`; a player's `me.team` holds only their own team name and teammates. In the lobby it lists everyone assigned to the team; once the game has started it lists only the joined participants (invitees who never joined are excluded when the roster is frozen), and included teammates stay listed after they are eliminated or withdraw. Participant and other-player views carry no team data.

Survival winners (`services/winners.go`): after every confirmed elimination and every confirmed withdrawal, `detectWinner` ends an active game once. Individual mode: the last active player wins. Team mode: the last team with active players wins, even when only one player of that team is left (the game is recorded as a team win, never a player win, so the game view and the final result agree), and so does a team with an unbeatable majority, that is when no team-safe circle can exist for the remaining players (more than half of them on one team). Withdrawn and eliminated players are not active. The game gets `endedAt`, `endReason: "survival"`, `winnerKind` (`player` or `team`) and `winnerId`, and the remaining players' victims and missions are cleared. Detection runs inside the per-game mutex and revision-checked write, so two concurrent final events end the game once with one winner. The winner stays hidden while the game is active. Timer-expiry winners are calculated by the implemented results service below.

Final results (`services/results.go`): when a game ends, the result is calculated once and stored on the game as `result`; it is never recalculated, so it is immutable. A survival ending (see above) records the winner chosen by the winner logic. When the timer expires first (`OnTimerExpired`), the individual with the most confirmed kills wins, or in team mode the team with the highest combined confirmed kills; players or teams that share the top score are all listed as winners and `tie` is true, so a tie is never broken silently (a game with no kills is a tie of everyone). Kills earned by players who were later eliminated or withdrew still count, withdrawals never earn kills, and eliminated or withdrawn participants remain eligible and appear in the standings. Standings list every participant (individual games) or team (team games), most kills first, ties ordered by name. A game the organizer ended manually has no result (404), and `GET /api/games/{id}/result` returns 409 until the game has ended. Kill counts exist only in this payload, after the end: game views, mid-game and afterwards, never carry them. Games that ended by survival before results existed get theirs derived from the stored winner when read.

Team-safe assignment (`services/victims.go`): in team mode the start circle has no victim on the hunter's own team. A safe circle exists only when at least two players remain and no team holds more than half of them. Otherwise start fails with `ErrNoValidAssignment` (HTTP 409) and writes no assignments. After every elimination transfer, `repairTeamCircle` reorders the living circle (a two-player swap, else a full re-deal) if a hunter would otherwise target a teammate; missions stay with their holders. If no safe circle remains, the team holding the majority has already won (see survival winners below); `ValidAssignmentExists(game) bool` still reports it, counting joined players before the start and active players afterwards. Eliminations and withdrawals both run the same post-transfer step. Individual mode is unchanged. Randomness is injectable through the `victimIntn` package variable in tests.

After starting, `participants` contains only IDs, names, a `status` (`active`, `eliminated`, or `withdrawn`), and elimination timestamps as shared participant information. Private `eliminations` contains only reports involving the caller; organizers can read all reports. Eliminated players retain login access but no assignment or ability to report. MQTT invalidations carry no report details; authorized state fetches deliver the confirmation prompt, including after reconnects. Existing documents without elimination fields work without a migration. When a victim accepts, the killer's mission is snapshotted onto the accepted elimination in the same revision-checked mutation, before the assignment transfer (stored in BSON, excluded from JSON, so the `eliminations` list never carries it). Only the victim's own view gains `me.elimination` = `{killer: {id, name}, mission: string|null, confirmedAt}` for a confirmed elimination (also after a game-ending one); eliminations stored before snapshots existed return `mission: null`. Pending, declined and cancelled claims, withdrawals, other players, organizers, participants, the Kill Board and the recap never carry the killer/mission relationship.

Startup creates a unique invitation-code index and the access-token TTL index. Game documents are created by API requests; no seed data or manual database migration is necessary.

All game mutations take a per-game mutex from a bounded lock pool. Persistence uses an atomic `updateOne` filtered by game ID and previous revision. If another instance changes the game, the service reloads and rechecks the rules before retrying. Starting selects joined players and sets the active status in the same write, so a concurrent join either commits first and is included or is rejected after the start. MongoDB is the final concurrency authority.

Roster size has no small application-level total-player cap. MongoDB documents have a 16 MiB limit; the repository reserves space by rejecting writes above 12 MiB, and request bodies are limited to 1 MiB. Adding players accepts up to 50 names per request; the frontend automatically submits larger rosters in batches of 25. Concurrent scrypt calculations are bounded to control memory use.

Related assignment, elimination, and scoring changes must be atomic. If these operations span multiple documents, local development and deployment will need a MongoDB replica set to support transactions.

Compose currently runs a standalone MongoDB instance for local development, with authentication disabled and its host port bound to localhost. Configure a replica set before implementing multi-document transactions. This Compose configuration is not a production deployment.

Connection settings come from `MONGODB_URI` and `MONGODB_DATABASE`. Real credentials and local database files must remain untracked.

## Privacy Requirements

The server is authoritative for assignments, permissions, timers, and scores. Public responses must never expose secret assignments, missions, team memberships, killer identities, or in-game kill counts. Private player responses expose only that player's permitted information; organizer access must be explicitly authorized.

See the [project README](../README.md) for the complete game rules and the [roadmap](../ROADMAP.md).

## Current Status

Game lifecycle, organizer rosters, lobby missions, manual/random teams, team-safe private assignments, mutual elimination confirmation, confirmed withdrawals, private kill tracking, individual/team survival winners, optional timers, final results, private-code authentication, authorized MQTT updates, and MongoDB persistence are implemented. See the [Phase 1 audit](../ROADMAP.md#phase-1-implementation-and-verification) for code/test evidence and the resolved acceptance follow-ups #43, #44 and #45. Normal proxy login isolation (#52 / PR #66) is also implemented; deployed-stack verification is still noted below. Runtime freshness (#35) remains an open follow-up.

## Game API

| Method | Endpoint | Access / behavior |
| --- | --- | --- |
| POST | `/api/games` | Create a lobby; returns temporary organizer access and a one-time management code |
| POST | `/api/access` | Log in with `invitationCode`, private `code`, and `role` (`organizer` or `player`); first player login joins the lobby |
| POST | `/api/games/{id}/session` | Matching Origin and valid game remember cookie: exchange for player Admission; 401 for invalid/expired/revoked credentials; no cookie/expiry renewal |
| DELETE | `/api/games/{id}/session` | Matching Origin: revoke the current game's remembered credential and its linked admissions, clear its cookie, return 204 (also when absent/unknown) |
| GET | `/api/games/{id}` | Read the caller's authorized view. After a survival ending it also carries `endReason: "survival"` and `winner: { "kind": "player"\|"team", "id", "name" }` (`null` before the end; `winnerId` is set for player winners only) |
| POST | `/api/games/{id}/players` | Organizer: add `{ "names": ["Alice", "Bob"] }`; returns generated private codes once |
| POST | `/api/games/{id}/players/{player}/code` | Organizer: reset a player's code and revoke prior credentials |
| POST | `/api/games/{id}/missions` | Organizer, lobby only: add `{ "texts": ["Get a high-five.", "Say pineapple."] }`; returns enabled mission entries |
| PATCH | `/api/games/{id}/missions/{mission}` | Organizer, lobby only: set `{ "enabled": false }` or `{ "enabled": true }` |
| POST | `/api/games/{id}/start` | Organizer: require two joined players and an enabled mission, atomically assign missions and a victim circle, freeze participants, and activate the lobby |
| POST | `/api/games/{id}/end` | Organizer: end a lobby or active game permanently |
| PATCH | `/api/games/{id}/settings` | Organizer, lobby only: `{ "mode": "individual" \| "team" }` |
| PUT | `/api/games/{id}/teams` | Organizer, lobby, team mode: full replace with `{ "teams": [ { "id"?, "name", "playerIds": [] } ] }`; returns `{ "teams" }` |
| POST | `/api/games/{id}/teams/randomize` | Organizer, lobby, team mode: `{ "teamCount": n }` (2 to joined players) deals joined players into balanced teams and persists them |
| POST | `/api/games/{id}/eliminations` | Living player, active game: report the assigned target with `{ "victimId": "..." }`; returns a pending claim |
| GET | `/api/games/{id}/result` | Organizer or any participant, only after the game has ended (409 before; 404 if the organizer ended it): `{ "reason": "survival"\|"timer", "tie": bool, "winners": [ { "kind", "id", "name" } ], "standings": [ { "kind", "id", "name", "kills" } ] }` |
| POST | `/api/games/{id}/eliminations/{claim}/response` | Named victim: respond with `{ "accept": true }` or `{ "accept": false }` |
| POST | `/api/games/{id}/withdrawals` | Living player, active game: ask to withdraw; returns `{ "withdrawalToken", "expiresAt" }`. Changes nothing until confirmed; the token expires after two minutes |
| POST | `/api/games/{id}/withdrawals/confirm` | Same player: confirm with `{ "withdrawalToken": "..." }`; returns 204. Marks the player withdrawn (not a kill), their hunter inherits the victim and mission, and the last survivor wins as after an elimination |
| GET | `/api/board/{gameId}` | **No token.** Public read-only live board: `{ "gameId", "name", "status": "lobby"\|"active"\|"ended", "activeCount", "timer": { "minutes", "endsAt", "remainingSeconds", "endReason" } \| null, "players": [ { "id", "number", "name", "status": "active"\|"eliminated"\|"withdrawn", "photoUrl"? } ] }`. 404 for unknown ids; same shape in every game state; lobby lists no players. `photoUrl` is the opaque relative photo URL, present only when the player has a photo; it is readable without credentials for included participants, and adding, replacing or removing a photo republishes the board |
| GET | `/api/board/{gameId}/recap` | **No token.** Same payload as `GET /api/games/{id}/recap` (`summary`, `leaderboard`, `timeline`, `awards`, `winners`), ended games only: 409 before the game ends, 404 for unknown ids. Shares the board's per-IP rate limit and sends `Cache-Control: no-store`. Never carries killers, missions, targets, codes or team membership (team names appear only as a team game's leaderboard rows) |
| GET (upgrade) | `/api/mqtt` | MQTT over WebSockets: authenticated, or anonymous read-only for the public board topic |

Protected HTTP requests use `Authorization: Bearer <token>`. Tokens expire after 12 hours. Private codes are normalized to uppercase with spaces/hyphens ignored, and hashed with scrypt using the random game ID as salt. Player codes have eight unambiguous random characters; organizer management codes have 24. Code reset preserves joined status and the frozen participant roster while incrementing the credential version. Players receive their own assignments and a safe participant list after starting; only organizers receive other players' assignments. No response exposes stored hashes.

Anonymous creation/login attempts are limited per client address and endpoint (30 per minute by default; `LOGIN_RATE_LIMIT_PER_MINUTE` overrides it within 1–600, and an unset, malformed or out-of-range value keeps 30). On top of that, each endpoint has a fixed aggregate cap of 600 attempts per minute across all clients, which bounds expensive code hashing even from many addresses. Exceeding either returns 429 with `Retry-After: 60`; nothing about codes, tokens or credentials is logged or returned.

The client address is the TCP peer unless that peer is listed in `TRUSTED_PROXIES` (comma-separated IPv4/IPv6 addresses or CIDRs). Unset, the secure default, trusts no proxy and ignores `X-Forwarded-For` entirely, so a spoofed header can never choose or rotate a bucket. For a trusted peer, `X-Forwarded-For` is read right to left (all header lines joined): trusted hops are skipped and the first untrusted address is the client, so entries a client forged on the left are never believed past an untrusted hop. A malformed entry stops the walk and the last valid hop is used. Malformed `TRUSTED_PROXIES` fails startup. Only list proxies that append the real peer address (Nginx's `$proxy_add_x_forwarded_for`, as in `frontend/nginx.conf`); never list a range that untrusted clients can connect from directly.

Deployments: Compose sets `TRUSTED_PROXIES` to `172.16.0.0/12,192.168.0.0/16` (Docker's default bridge ranges, overridable in the root `.env`), so each browser behind the Nginx container has its own bucket. The backend's loopback port (`127.0.0.1:${BACKEND_PORT}`) is reachable only from the host. Native development leaves it empty, and Vite's dev proxy then makes all requests share `127.0.0.1`; this is fine for one developer. If you run Nginx outside Compose, set `TRUSTED_PROXIES` to its address. The browser suite logs in from one address, so run it against `make e2e-up`, which applies `compose.e2e.yaml` (limit 600); that override is test-only and not for a shared deployment. The Compose proxy setup was covered by unit/controller tests but not verified in a deployed Docker stack. Use HTTPS/WSS outside localhost and keep private codes out of logs and URLs.

### Remembered-player sessions (#82/#83; parent #71)

Successful player code login/recovery at `POST /api/access` additionally sets
`elimin8_remember_<gameId>`, a host-only HttpOnly `SameSite=Lax` cookie scoped to
`/api/games/<gameId>/session`. It contains a new random 240-bit opaque secret;
only its SHA-256 hash is stored, in `player_sessions`. The existing Admission
JSON is unchanged. Organizer logins/creation and failed logins issue no remember
cookie. A storage failure returns a generic error instead of claiming successful
cookie issuance. Login retains the existing rate limits and body bounds; each
successful player login issues one credential, without a device cap.

Max-Age (2,592,000 seconds) and Expires are set to 30 days from code login/recovery.
Validation does not slide that deadline. Re-entering a code issues a new
credential; overwritten records remain bounded by fixed expiry and TTL cleanup.
Validation checks current player membership and credential version, so code
regeneration invalidates old remembered credentials. Admitted eliminated/withdrawn
players and ended games remain eligible. Issuance rechecks after persistence to
reject a reset race. No multi-document transaction is needed: stale credentials
are rejected on use against the authoritative current game.

Direct HTTPS sets Secure. For TLS termination, configure `TRUSTED_PROXIES` with
the immediate proxy and have it overwrite `X-Forwarded-Proto` with exactly
`https` and preserve the browser's Host, including its port. Untrusted/malformed
forwarding headers do not select HTTPS. Plain HTTP localhost/LAN development uses
a non-Secure cookie; use HTTPS for shared deployments. The supplied Nginx API
proxy preserves Host/port and overwrites forwarding headers; Vite preserves Host.
Player browser code-login requests must have a matching Origin (scheme, host
and port). Cross-site/opaque/malformed origins are rejected before login;
non-browser clients without Origin remain supported.

`POST /api/games/{id}/session` exchanges that game's cookie for a fresh player
Admission (bearer, expiry, MQTT fields and role-scoped game). It does not join or
create a player or issue organizer access. Admission lasts at most 12 hours and
is capped by the original remembered deadline. POST returns no Set-Cookie and
never extends the 30-day period. Invalid, missing, expired, revoked or wrong-game
credentials return 401, including when TTL deletion is delayed.

Both POST and DELETE require exactly one matching Origin (scheme, host and port)
and reject cross-site fetch metadata, opaque/malformed origins and missing Origin
with 403. Non-browser callers must supply Origin for these cookie-backed routes;
code login retains its no-Origin CLI contract above. Bodies are limited to 1 KiB
and may be empty or `{}`. Duplicate game cookies are rejected with 401. Rate
limits use the configured per-client allowance plus the aggregate safeguard,
shared across game IDs for each method, returning 429 and Retry-After: 60.
All responses are no-store. Other private HTTP/MQTT routes still require bearer
access and never authenticate directly from cookies.

DELETE returns 204 and clears the matching cookie name/path/flags, also when the
cookie is absent, unknown or expired. It revokes only the current credential and
admissions linked to it, preserving other devices/games and organizer access.
New code-login admissions and resumed admissions carry a private source hash;
HTTP/MQTT authorization and player/photo mutation retries recheck revocation,
fixed expiry and current player version. Pre-existing unlinked admissions retain
their original 12-hour expiry and code-reset rules.

Revocation keeps its first timestamp until the original TTL deadline. It marks
the credential first, then advances the game's revision before returning 204.
A concurrent mutation must commit before that revision fence or retry and see
revocation. Failure returns an error while the durable marker remains revoked;
a retry finishes the fence without moving expiry or the first revocation time.
This fail-closed ordering needs no multi-document transaction; game-domain
changes remain single-document atomic writes.

Browser restoration, renewal, recovery UI and sign-out remain #84. Until then the
frontend uses its existing short-lived access lifecycle. Keep #71 open until all
three slices are complete. The previously documented external HTTPS-terminator
topology is outside the current local HTTP deployment scope.

Run the isolated persistence/index test from `backend/` with `MONGODB_TEST_URI`
set and `go test ./internal/repositories/mongodb -run TestPlayerSessionMongo -count=1`.
It uses and drops a uniquely named test database and otherwise skips.
Service/controller tests need no running database.

## Public board

`GET /api/board/{gameId}` and the MQTT topic `boards/{gameId}/updates` implement anonymous read-only board access. The link is the game id only: anyone holding it can read the public payload, and it grants no ability to join or act. The `#/board/<gameId>` frontend is implemented (#39); the unauthenticated recap route `GET /api/board/{gameId}/recap` ships with #85 (recap UI is #42). The authenticated in-game Kill Board remains a separate, narrower API, and the authenticated game view includes elimination timestamps in its shared participant list.

- **Payload:** names, ids, a stable display `number`, status, `activeCount`, game status and the timer. Never missions, targets, hunters, teams, kill counts, killers or codes (tests assert the exact JSON keys in lobby, active and ended states). Players are ordered by display number, so order never reveals who was eliminated or withdrew first. The recap is a separate later field, available only after the game ends.
- **Display number:** assigned when the organizer adds a player (`Player.Number`), persisted and never reused (players are never removed). Games created before this existed fall back to the player's roster position, which is equally stable.
- **Game id entropy:** ids come from `crypto/rand` through `RandomCode`, which uses 5 bits per character. New games use 26 characters (130 bits, `services.GameIDLength`). Earlier games used 24 characters (120 bits) and stay valid with no migration.
- **Anonymous MQTT:** a client may connect without a username using a client id beginning `board-` (clean session, no will). It may subscribe only to one concrete `boards/{id}/updates` topic: no wildcards, no `games/...` topics. Publishing is always denied. At most 1,024 anonymous clients are connected at once.
- **Board messages:** `{ "gameId", "type": "board.updated" }`, published (QoS 1) only when the visible board changes (a status, the game status or the timer setup), not on private revisions such as a pending elimination report, so the topic does not leak report timing. Viewers refetch the HTTP endpoint on each message.
- **Abuse control:** the unauthenticated endpoint has its own per-address budget of 240 requests per minute, shared across game ids (so scanning many ids is bounded too) and larger than the 30-per-minute auth limit so several screens behind one address work. Responses are `Cache-Control: no-store`; each request is a single indexed read, so no server-side cache was added.

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

## Game timer

`PUT /api/games/{id}/timer` with `{"durationMinutes": 1..10080 | null}` sets or clears the limit (organizer, lobby only). Starting the game stores `timer.endsAt = startedAt + limit`. Expiry is checked lazily inside every game read/write (`mutate`, `Authorize`) and by a 5 second background sweep (`RunTimerSweep`), so it survives restarts; the revision-checked save makes it happen exactly once. Expired games become `ended` with `endReason: "timer"` and `endedAt = endsAt`, pending eliminations are cancelled, and `GameService.OnTimerExpired` stores the final result (see Final results). `GameView` exposes `timerMinutes`, `endsAt`, `remainingSeconds` and `endReason` to all roles.

## Private photo storage foundation

[RustFS](https://docs.rustfs.com/en/installation/container) is open source under Apache 2.0 and runs entirely locally in Docker; no cloud account is required. Compose pins `rustfs/rustfs:1.0.0-beta.9`, disables its console, and binds its S3 port to loopback. `PHOTO_STORAGE_PORT` changes the host port only (default 9000); the backend always connects to `http://rustfs:9000` inside Compose. Copy the root `.env.example` and set unique `PHOTO_STORAGE_ACCESS_KEY` and `PHOTO_STORAGE_SECRET_KEY` before `make up`; empty credentials fail configuration instead of enabling known default credentials. Storage is deliberately required by the Compose stack: the backend waits for RustFS health and refuses configured storage initialization failures rather than silently disabling photos. Native development can still leave storage unset as described below. `PHOTO_STORAGE_REGION` defaults to `us-east-1`, and `PHOTO_STORAGE_BUCKET` to `elimin8-photos`.

For native Go development, leave `PHOTO_STORAGE_ENDPOINT` and both credential variables unset to disable storage. To enable it, start Compose's `rustfs` service, export `PHOTO_STORAGE_ENDPOINT=http://127.0.0.1:9000` (adjust for the host port), set `PHOTO_STORAGE_PROVIDER=rustfs`, and export the same region, bucket, and credentials as Compose. The binary does not load `.env` files automatically. Partial configuration is rejected. Use HTTPS for object storage beyond a trusted local network.

Startup creates a missing bucket with the provider's default private ACL, tolerates another API instance creating that same owned bucket first, and checks both bucket ACL and bucket policy. The bucket must be dedicated to photos, with no policy and no grants to anyone except its owner; startup refuses an existing policy or broader grant and never rewrites external policies or ACLs. Once the bucket is confirmed private, startup enables and verifies all four S3 PublicAccessBlock settings, preventing public ACLs and policies. Native configuration defaults to strict `PHOTO_STORAGE_PROVIDER=s3`. Compose explicitly selects `rustfs`: RustFS does not implement ACL authorization and returns a synthetic owner grant without an ID; only its exact single CanonicalUser/FULL_CONTROL grant is accepted in this mode, still requiring no policy and verified public-access blocking. The storage credential therefore needs HeadBucket/CreateBucket, GetBucketAcl/GetBucketPolicy, PutPublicAccessBlock/GetPublicAccessBlock, and object Put/Get/Delete/List permissions. Keep the bucket private: subsequent photo endpoints serve images through the backend rather than exposing S3 URLs or credentials.

The adapter uses the official AWS SDK for Go v2 with explicit credentials and path-style addressing for S3-compatible stores. Each object is bounded to 5 MiB for both writes and reads; listings follow continuation tokens and return a bounded batch of up to 100,000 objects or 1,000 pages. A successful batch may not exhaust the prefix: deleting callers revisit it on later sweeps so arbitrarily large games make cleanup progress. Foreign game prefixes or malformed continuation tokens still fail safely. Cleanup also accepts literal externally created filenames (including periods, spaces, Unicode and dot segments) under the exact game photo prefix, and does not require object modification timestamps. Odd keys are deleted using the S3 XML bulk-delete body, keeping them out of URL paths; unrepresentable UTF-8/XML names return a per-key error instead of silently changing which key is deleted. Upload and image-read validation stays strict. Objects use `games/<gameID>/photos/<photoID>` keys generated by application code. Error messages and SDK logging do not expose credentials, bucket paths, or remote diagnostics. Storage initialization errors provide a safe operation category through `storage.InitializationStage` (configuration, bucket lookup/creation, ACL/policy checks or public-access-block write/verification). The API includes this static category in its sanitized runtime startup diagnostics; configured storage failures remain fatal. Photo API/processing and 24-hour post-game cleanup are tracked in #54 and #55, under #37; the API contract is described below.

### Player photo API

When private photo storage is configured, the backend registers:

- `PUT /api/games/{id}/players/{playerId}/photo`: authenticated multipart `photo` upload; a player may edit their own photo and the organizer may edit any player's photo. Returns only `{ "photoUrl": "..." }`.
- `DELETE /api/games/{id}/players/{playerId}/photo`: same authorization, returns 204. Both writes are permitted in the lobby and active game, and return 409 after the game ends.
- `GET /api/games/{id}/photos/{photoId}`: serves a current opaque photo reference as JPEG. Joined lobby players' photos and included participants' photos are readable by board-link viewers. Unjoined invitees' photos require organizer access; excluded invitees are never publicly readable after start. An authenticated player can also read their own photo. Invalid supplied credentials are rejected, even for otherwise public photos.

The image file limit is **5 MiB**, with at most 64 KiB of multipart overhead. The API sniffs JPEG/PNG/WebP bytes independently of the filename or declared MIME type, validates dimensions before decoding (maximum 25 megapixels and 16,384 pixels on either side), and limits concurrent decodes. Before resizing, it applies all eight EXIF orientation rotations/mirrors from JPEG APP1, PNG eXIf, or WebP EXIF metadata. Missing orientation means normal orientation; malformed, duplicate or unsupported orientation metadata returns 400. The bounded reader accepts at most 256 KiB of TIFF metadata and 512 primary-directory entries and never traverses GPS or thumbnail directories. It then creates a new JPEG with its longest side at most 400px; all source metadata, including EXIF/GPS, is discarded and transparency is flattened against white. Invalid images return 400, unsupported types 415, and oversized files/dimensions 413. Nginx permits the same bounded multipart request size; other JSON controllers retain their existing 1 MiB parsing limit.

Player `me` and organizer player views gain an optional relative `photoUrl`; shared participant views are unchanged. The public board payload carries the same optional `photoUrl` for included players (#41), and the board digest includes it, so a photo change publishes a board update. URLs contain an opaque random photo ID, never an object-store endpoint or bucket credential. Reads check the current game reference before and after storage I/O and use `Cache-Control: no-store` plus `X-Content-Type-Options: nosniff`. References change through the existing revision-checked game write and realtime invalidation. A failed object upload leaves the current photo intact. A database timeout may have committed its reference, so staged objects are retained on ambiguous write failures; immediate deletion of known unreferenced objects is best effort, with durable post-game cleanup supplied by #55 / [PR #58](https://github.com/jesperfrisk/elimin8/pull/58). Ended-game reads stop at the retention boundary (24 hours by default), even before a cleanup sweep runs. With no storage configured, photo routes are not registered; native gameplay remains available.

The photo API enforces read expiry and attempts immediate deletion of replaced, removed or definitively rejected objects. The integrated retention service from #55 / [PR #58](https://github.com/jesperfrisk/elimin8/pull/58) runs at startup and periodically to delete objects under expired game prefixes, including orphan uploads and failed immediate deletions. A per-key cleanup failure does not stop valid neighbors from being deleted or falsely mark the remaining prefix as clean; see the post-game retention policy below.

The opt-in storage integration test runs against a disposable RustFS service when `ELIMIN8_TEST_S3_ENDPOINT` and both `PHOTO_STORAGE_ACCESS_KEY`/`PHOTO_STORAGE_SECRET_KEY` are exported: `go test ./internal/storage -run TestS3LivePrivateOperations -count=1` from `backend/`. It verifies private Put/Get/List/Delete, denies anonymous object reads, and checks exact cleanup of an external filename containing spaces, Unicode, a literal plus and percent sequence. Never point it at a shared production bucket.

### Post-game photo retention

`PHOTO_RETENTION_HOURS` defaults to **24** and accepts positive whole hours. It is validated only when photo storage is enabled; invalid or overflowing values then stop startup without logging the supplied value. Native runs with photo storage disabled ignore this setting. Compose passes the root `.env` setting to the backend. Native runs must export it into the process environment. The same duration controls both photo reads and deletion: at `endedAt + retention`, GET photo requests return 404 immediately, even while cleanup is delayed. This applies to every ended game, including one ended manually by the organizer.

Configuring photo storage deliberately makes successful private-store initialization a prerequisite for the entire API startup. An unavailable store or failed privacy check stops startup; the API does not silently disable photo routes or the retention job, because doing so would undermine the configured photo privacy and deletion policy. Leave storage unconfigured for native gameplay that does not need photos. Startup failures name a static initialization stage (for example, bucket lookup, ACL/policy checks or public-access-block verification) while staying sanitized: endpoint URLs, credentials, object keys, request IDs, and remote diagnostics are not logged.

With photo storage enabled, the backend runs cleanup on startup and every minute. It discovers expired games from persisted MongoDB state using an index and pages of at most 100 game IDs. It deletes every object under the expired game's private photo prefix, including old replacements and failed-upload leftovers, then clears player photo references through the normal revision-checked game write and update notification. Results, winner metadata and end time are preserved. Partial failures retry on the next sweep, and pagination continues past failed games so they cannot block later games. Repeated or concurrent sweeps are safe. Each game has a 30-second context budget so a slow store cannot monopolize cleanup; partial progress is retained for the next sweep. Each game deletes at most one storage listing batch per sweep (up to 100,000 objects); remaining batches continue on later sweeps without blocking other games. Player references are cleared only after a fresh listing confirms that the whole prefix is empty.

Expired prefixes are checked again on later sweeps rather than permanently marked complete. This also removes a late orphan left by an upload that was already in flight during an earlier sweep. Lobby, active and unexpired games are untouched. The job logs only a generic retry message, never credentials, object keys or game relationships. Restart recovery requires no local job state or MongoDB replica set.

For the optional MongoDB pagination regression, set `MONGODB_TEST_URI` to an isolated local instance and run `go test ./internal/repositories/mongodb -run TestPhotoRetentionMongoDiscovery -count=1` from `backend/`. It creates and drops only a unique test database; normal `go test ./...` skips this integration test when the variable is unset.

Both recap routes include `winners: [{ kind, id, name }]` from the authoritative final result (or the recorded legacy winner). Organizer-ended games return an empty array. Awards do not determine winners; zero-kill timer ties and team survival winners follow the same rules as final results. This field is available only after the game ends and contains no team membership or elimination relationships.
