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

Phase 1 fields are reserved on `Game` (`Mode`, `Teams`, `Timer`, `EndReason`) and `Player` (`TeamID`, `ConfirmedKills`). They are `omitempty`, carry no JSON tags, and are not exposed in any view until their feature adds them.

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

MongoDB currently stores two collections:

- `games`: game details, unique invitation code, organizer-code hash, lifecycle timestamps, embedded player roster, revision, and last published revision. Each player stores their name, private-code hash, credential version, joined timestamp, and inclusion flag.
- `game_access`: hashed temporary bearer tokens, scoped game/player identity, credential version, and expiry. A TTL index removes expired entries; authorization also checks expiry directly.

Game documents also embed lobby mission candidates (`id`, `text`, `enabled`). They have no shared pool, separate collection, or lifecycle outside their game. IDs support lobby controls only. Starting copies one randomly selected mission text into each joined player's `mission` field. Every enabled entry is used once before reuse; each round uses a fresh FisherÃ¢â‚¬â€œYates shuffle with unbiased cryptographic random indices. Repeated text represents separate entries. Disabled entries and unjoined invitees are excluded. Starting requires at least two joined players and one enabled mission; failure returns HTTP 409 with an actionable error.

Victim assignment shuffles the joined roster into a single circle, storing the next participant's ID in each player's `victimId`. Each participant has exactly one victim and one hunter, with no self-targets or smaller disconnected circles. Player IDs distinguish participants even when names match. Teams are not implemented yet.

Missions, victims, participant inclusion, and active status commit in the same revision-checked game write. Concurrent configuration changes or joins trigger a retry against current state. Failed starts save no partial assignments, and subsequent start requests cannot reroll a started game. Assignments survive reconnects, code resets, and ending the game. Each player view includes only their own `me.mission` and `me.victim` after the start; `victim` contains only `id` and `name`, never that victim's assignments. Organizer roster entries include all pairings. Lobby responses omit assignments. MQTT notifications contain no assignments and trigger authorized view refreshes.

Organizers add up to 100 missions per request, each with up to 1,000 Unicode characters and no internal line breaks. There is no small total-mission cap; the existing game-document size limit applies. New entries are enabled. Disabling retains the entry so it can be re-enabled. All mission changes require organizer access and lobby status, and use the existing atomic revision checks and MQTT invalidations. Only organizer views include mission configuration. Existing game documents without missions are treated as empty.

Elimination reports are embedded in the game with killer/victim IDs, creation/resolution timestamps, and `pending`, `accepted`, `declined`, or `cancelled` status. Only living participants in an active game can report their currently assigned target. The server checks the assignment again when accepting a report. Each killer can have one outgoing pending report, and each victim one incoming pending report. Only the named victim may respond. Accepting atomically marks the victim eliminated, transfers their mission and target to the killer, repairs the target circle, and cancels pending reports involving eliminated players. Other players retain their missions. The final survivor ends the game with `winnerId` and no remaining assignment. Declines leave assignments unchanged. Ending a game cancels pending reports. Duplicate or stale responses return HTTP 409.

In team mode, starting requires every joined player on a team and at least two teams with players (409 otherwise). The organizer's game view gains `mode` and `teams`; a player's `me.team` holds only their own team name and teammates. Participant and other-player views carry no team data.

Survival winners (`services/winners.go`): after every confirmed elimination and every confirmed withdrawal, `detectWinner` ends an active game once. Individual mode: the last active player wins. Team mode: the last team with active players wins, and so does a team with an unbeatable majority, that is when no team-safe circle can exist for the remaining players (more than half of them on one team). Withdrawn and eliminated players are not active. The game gets `endedAt`, `endReason: "survival"`, `winnerKind` (`player` or `team`) and `winnerId`, and the remaining players' victims and missions are cleared. Detection runs inside the per-game mutex and revision-checked write, so two concurrent final events end the game once with one winner. The winner stays hidden while the game is active. Timer-expiry winners are left to the results ticket.

Final results (`services/results.go`): when a game ends, the result is calculated once and stored on the game as `result`; it is never recalculated, so it is immutable. A survival ending (see above) records the winner chosen by the winner logic. When the timer expires first (`OnTimerExpired`), the individual with the most confirmed kills wins, or in team mode the team with the highest combined confirmed kills; players or teams that share the top score are all listed as winners and `tie` is true, so a tie is never broken silently (a game with no kills is a tie of everyone). Kills earned by players who were later eliminated or withdrew still count, withdrawals never earn kills, and eliminated or withdrawn participants remain eligible and appear in the standings. Standings list every participant (individual games) or team (team games), most kills first, ties ordered by name. A game the organizer ended manually has no result (404), and `GET /api/games/{id}/result` returns 409 until the game has ended. Kill counts exist only in this payload, after the end: game views, mid-game and afterwards, never carry them. Games that ended by survival before results existed get theirs derived from the stored winner when read.

Team-safe assignment (`services/victims.go`): in team mode the start circle has no victim on the hunter's own team. A safe circle exists only when at least two players remain and no team holds more than half of them. Otherwise start fails with `ErrNoValidAssignment` (HTTP 409) and writes no assignments. After every elimination transfer, `repairTeamCircle` reorders the living circle (a two-player swap, else a full re-deal) if a hunter would otherwise target a teammate; missions stay with their holders. If no safe circle remains, the team holding the majority has already won (see survival winners below); `ValidAssignmentExists(game) bool` still reports it, counting joined players before the start and active players afterwards. Eliminations and withdrawals both run the same post-transfer step. Individual mode is unchanged. Randomness is injectable through the `victimIntn` package variable in tests.

After starting, `participants` contains only IDs, names, a `status` (`active`, `eliminated`, or `withdrawn`), and elimination timestamps as shared participant information. Private `eliminations` contains only reports involving the caller; organizers can read all reports. Eliminated players retain login access but no assignment or ability to report. MQTT invalidations carry no report details; authorized state fetches deliver the confirmation prompt, including after reconnects. Existing documents without elimination fields work without a migration.

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

Game lifecycle, organizer rosters, lobby missions, private assignments, mutual elimination confirmation, atomic transfers, last-player victory, final results, private-code authentication, authorized MQTT updates, and MongoDB persistence are implemented.

## Game API

| Method | Endpoint | Access / behavior |
| --- | --- | --- |
| POST | `/api/games` | Create a lobby; returns temporary organizer access and a one-time management code |
| POST | `/api/access` | Log in with `invitationCode`, private `code`, and `role` (`organizer` or `player`); first player login joins the lobby |
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
| GET (upgrade) | `/api/mqtt` | Authenticated MQTT over WebSockets |

Protected HTTP requests use `Authorization: Bearer <token>`. Tokens expire after 12 hours. Private codes are normalized to uppercase with spaces/hyphens ignored, and hashed with scrypt using the random game ID as salt. Player codes have eight unambiguous random characters; organizer management codes have 24. Code reset preserves joined status and the frozen participant roster while incrementing the credential version. Players receive their own assignments and a safe participant list after starting; only organizers receive other players' assignments. No response exposes stored hashes.

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

## Game timer

`PUT /api/games/{id}/timer` with `{"durationMinutes": 1..10080 | null}` sets or clears the limit (organizer, lobby only). Starting the game stores `timer.endsAt = startedAt + limit`. Expiry is checked lazily inside every game read/write (`mutate`, `Authorize`) and by a 5 second background sweep (`RunTimerSweep`), so it survives restarts; the revision-checked save makes it happen exactly once. Expired games become `ended` with `endReason: "timer"` and `endedAt = endsAt`, pending eliminations are cancelled, and `GameService.OnTimerExpired` stores the final result (see Final results). `GameView` exposes `timerMinutes`, `endsAt`, `remainingSeconds` and `endReason` to all roles.

## Private photo storage foundation

[RustFS](https://docs.rustfs.com/en/installation/container) is open source under Apache 2.0 and runs entirely locally in Docker; no cloud account is required. Compose pins `rustfs/rustfs:1.0.0-beta.9`, disables its console, and binds its S3 port to loopback. `PHOTO_STORAGE_PORT` changes the host port only (default 9000); the backend always connects to `http://rustfs:9000` inside Compose. Copy the root `.env.example` and set unique `PHOTO_STORAGE_ACCESS_KEY` and `PHOTO_STORAGE_SECRET_KEY` before `make up`; empty credentials fail configuration instead of enabling known default credentials. `PHOTO_STORAGE_REGION` defaults to `us-east-1`, and `PHOTO_STORAGE_BUCKET` to `elimin8-photos`.

For native Go development, leave `PHOTO_STORAGE_ENDPOINT` and both credential variables unset to disable storage. To enable it, start Compose's `rustfs` service, export `PHOTO_STORAGE_ENDPOINT=http://127.0.0.1:9000` (adjust for the host port), set `PHOTO_STORAGE_PROVIDER=rustfs`, and export the same region, bucket, and credentials as Compose. The binary does not load `.env` files automatically. Partial configuration is rejected. Use HTTPS for object storage beyond a trusted local network.

Startup creates a missing bucket with the provider's default private ACL, tolerates another API instance creating that same owned bucket first, and checks both bucket ACL and bucket policy. The bucket must be dedicated to photos, with no policy and no grants to anyone except its owner; startup refuses an existing policy or broader grant and never rewrites external policies or ACLs. Once the bucket is confirmed private, startup enables and verifies all four S3 PublicAccessBlock settings, preventing public ACLs and policies. Native configuration defaults to strict `PHOTO_STORAGE_PROVIDER=s3`. Compose explicitly selects `rustfs`: RustFS does not implement ACL authorization and returns a synthetic owner grant without an ID; only its exact single CanonicalUser/FULL_CONTROL grant is accepted in this mode, still requiring no policy and verified public-access blocking. The storage credential therefore needs HeadBucket/CreateBucket, GetBucketAcl/GetBucketPolicy, PutPublicAccessBlock/GetPublicAccessBlock, and object Put/Get/Delete/List permissions. Keep the bucket private: subsequent photo endpoints serve images through the backend rather than exposing S3 URLs or credentials.

The adapter uses the official AWS SDK for Go v2 with explicit credentials and path-style addressing for S3-compatible stores. Each object is bounded to 5 MiB for both writes and reads; listings follow continuation tokens and cap accumulated results at 100,000. Objects use `games/<gameID>/photos/<photoID>` keys generated by application code. Error messages and SDK logging do not expose credentials, bucket paths, or remote diagnostics. This foundation adds no photo HTTP routes; API/processing and 24-hour post-game cleanup are tracked separately in #54 and #55, under #37.

The opt-in storage integration test runs against a disposable RustFS service when `ELIMIN8_TEST_S3_ENDPOINT` and both `PHOTO_STORAGE_ACCESS_KEY`/`PHOTO_STORAGE_SECRET_KEY` are exported: `go test ./internal/storage -run TestS3LivePrivateOperations -count=1` from `backend/`. It verifies private Put/Get/List/Delete and denies anonymous object reads. Never point it at a shared production bucket.
