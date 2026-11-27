# Elimin8 Frontend

The frontend is a React application built with Vite, with organizer and player pages and event-driven MQTT updates.

## Run with Docker

From the repository root:

```sh
make up
```

(equivalent to `docker compose up --build --wait`; see the root [Makefile](../Makefile)). Open `http://localhost:3000` (override with `FRONTEND_PORT` in a root `.env`, see [.env.example](../.env.example)). The frontend image builds static assets and serves them through Nginx as a non-root user. Nginx forwards `/api/` requests to the backend, so browser requests use the same origin.

The Docker frontend also accepts connections from other devices on the same network at `http://<host-LAN-IP>:3000`. Both API requests and MQTT use that same address. Windows Firewall must allow inbound TCP port 3000 from `LocalSubnet` on the active network profile. Browser clipboard access may be unavailable over LAN HTTP; private codes remain visible for manual copying.

## Local Development

Use Node.js 22.12 or newer. Start the backend on port 8080, then:

```sh
make frontend-dev
```

Open the address printed by Vite (normally `http://localhost:5173`). Vite forwards `/api` requests and `/api/mqtt` WebSocket connections to `http://127.0.0.1:8080`.

```sh
make frontend-build
```

`npm run preview` previews static assets only; use the development server or Docker to exercise the API proxy.

## Source Layout

`src/main.jsx` only bootstraps React. Everything else is one component per file:

| File | Responsibility |
| --- | --- |
| `App.jsx` | App shell, hash routing, saved access, shared `run`/`busy` helpers |
| `Home.jsx`, `OrganizerHome.jsx`, `PlayerHome.jsx`, `LoginForm.jsx` | Landing, create/join, and code entry pages |
| `GamePage.jsx` | Game frame shared by both roles (heading, stats, management code) and the panel slot |
| `OrganizerView.jsx` | Organizer roster, start/end, add players, private codes, lobby missions |
| `PlayerView.jsx` | Player status card, victim/mission, eliminations |
| `LobbyMissions.jsx`, `Eliminations.jsx` | Mission setup and elimination confirmation |
| `Teams.jsx`, `Timer.jsx`, `Withdrawal.jsx`, `KillBoard.jsx`, `Results.jsx` | Shipped Phase 1 team setup, countdown, confirmed withdrawal, authenticated status board and final results |
| `slots.jsx` | Extension points for new panels (below) |
| `common.jsx` | Shared helpers: `PageHeading`, `CopyButton`, `formatDate` |
| `api.js`, `useGame.js`, `gameActionError.js` | API client, game state and MQTT hook, error messages |

### Adding a panel

Game features use their own component plus an import and an entry in `src/slots.jsx` for each phase where the panel appears. Teams, withdrawals, Kill Board, timer and results already use these slots.

| Array | Rendered when | Typical use |
| --- | --- | --- |
| `lobbyPanels` | `game.status === "lobby"` | team editor, timer setup |
| `inGamePanels` | `game.status === "active"` | withdrawal, Kill Board, countdown |
| `gameOverPanels` | `game.status === "ended"` | results |

Panels render below the existing game content for every viewer and receive `game`, `access`, `action` and `busy` (see the comment in `slots.jsx`). A panel checks `access.role` itself and returns `null` when it has nothing to show. Lobby panels are Timer and Teams; active-game panels add Withdrawal and KillBoard; the ended-game slot renders Results.

## Available Experience

- Create games and configure rules, missions, teams, and optional timers.
- Join a game using an invitation link or game code.
- View private targets and missions once a game starts; view own teammates as soon as lobby teams are configured. Starting freezes the teammate list to included participants, excluding unjoined invitees (#51).
- Report and confirm eliminations, and confirm voluntary withdrawals.
- View the authenticated Kill Board with player statuses and a count for each status; a separate timer panel shows remaining time when enabled.
- Provide organizer controls and show final results after the game ends.

## API and Privacy

The backend is authoritative for game state, permissions, assignments, scores, and timers. The frontend must use responses scoped to the current user's access. Hiding information in the UI is not a substitute for server-side authorization.

The public Kill Board must not reveal targets, missions, teams, killer identities, or in-game kill counts.

The current in-game Kill Board requires organizer/player access and returns only
IDs, names and status. Authenticated game views also expose optional elimination
timestamps on shared participant entries. Privacy verification must check both
payloads. Organizers receive no in-game kill totals either; scores appear only
in post-game result/recap responses. The anonymous board API is implemented in
#36, but the `#/board/<gameId>` page is pending #39 and public recap access is
pending #42; neither is an existing frontend route.

Any configuration shipped to the browser is public. Database credentials and other secrets must remain on the backend.

## Current Status

Implemented routes use hash navigation:

- `/#/`: landing page and games remembered in this tab.
- `/#/organizer`: create a game or reopen it using invitation/management codes.
- `/#/organizer/{id}`: add named players, copy private invitations, see joined status, reset codes, start/end games.
- `/#/player`: join or return using invitation/private player codes.
- `/#/player/{id}`: own lobby membership, active game, and ended-game status.

The organizer page also includes **Lobby missions**. Paste one mission per line; blank lines are ignored and repeated tasks remain separate entries. Large lists are submitted in batches of 100. Each successful batch clears its saved lines from the input, preserving unsaved lines if a later batch fails. New missions start enabled; use **Disable** or **Enable** on each entry to change which tasks are eligible. Changes save immediately, synchronize across organizer tabs, and lock when the lobby closes.

Starting requires at least two joined players and one enabled mission. Each participant gets a private mission and a victim. Victims form one shuffled circle: nobody targets themselves, and everyone has exactly one victim and one hunter. All enabled mission entries are used once before reuse, with each round shuffled again. Players see only their own mission and victim after the start, including after reloads or reconnects; organizers see all assignments in the roster. The lobby mission list remains organizer-only.

Active players can confirm an elimination report directly for their assigned target, without selecting or searching for a player. The backend rejects reports against any other player. MQTT triggers the victim's on-screen request naming the killer, with **Accept elimination** and **Decline elimination** actions. The request receives keyboard focus and survives reloads/reconnects. Acceptance transfers the victim's target and mission to the killer; decline leaves assignments unchanged. Pending, accepted, declined, and cancelled reports are visible only to involved players and the organizer. Eliminated players retain game access but cannot report more kills. The final survivor or winning team ends the game automatically. Shared participant information exposes IDs, names, status and optional elimination timestamps.

Active players can withdraw from the game with **Withdraw from game** (`Withdrawal.jsx`, an in-game panel). The first click only requests a short-lived confirmation token and shows a confirmation step; **Confirm withdrawal** is a separate second call, and **Keep playing** (or waiting for the token to expire) changes nothing. A withdrawn player sees a withdrawn screen and no assignments, their hunter inherits their victim and mission, and the roster shows them as Withdrawn. A withdrawal is not a kill.

When a game ends by survival or timer expiry, the **Final results** panel (`Results.jsx`, a game-over panel) shows the server-calculated result: the winner or winners (with a tie notice when the top score is shared) and the final standings with each player's or team's confirmed kills. It loads `GET /api/games/{id}/result` once the game has ended, for organizers and players alike. Kill counts are never sent before then, and a game the organizer ended manually has no result to show.

Start/end errors appear beside the game controls, receive keyboard focus, and scroll into view. Messages distinguish missing enabled missions, expired access, missing permissions or games, lifecycle conflicts, invalid data, rate limits, and server/connection failures. **Refresh game** resynchronizes uncertain outcomes; access errors also link to the organizer desk. A successful action followed by a failed state refresh is reported separately from a rejected action.

Organizers can paste player names one per line. Generated codes are held only in page memory and can be copied into individual invitations; they are never written to browser storage. Temporary access tokens and basic game labels are kept in `sessionStorage` for reloads. Private codes allow access from another browser. A returning organizer must retain the management code shown once at creation.

MQTT.js subscribes to the role-scoped topic returned by the API. Subscribe acknowledgement is followed by a state fetch, preventing missed events during initial connection or reconnection. Notifications cause authorized fetches; there is no periodic polling. Disconnection is visible, failed fetches retry with backoff, and revoked/expired access prompts code re-entry. MQTT keepalives maintain the transport and do not poll game state.

## End-to-end Checks

Start the isolated test stack, then run the suite. `make e2e-up` applies `compose.e2e.yaml`, which raises the backend per-client login limit to 600 attempts per minute for this stack only; fixture logins from one client can exceed the normal default of 30. Runtime proxy isolation (#52 / PR #66) now identifies separate clients behind configured trusted proxies; it does not exempt repeated browser-fixture logins from one client, and an aggregate endpoint cap still applies. Fixture logins assert a successful response, so a rate-limit failure is reported at the login rather than later. See the [backend README](../backend/README.md) for native/Vite and Compose proxy settings and the outstanding deployed-stack check.

```sh
make e2e-up
make frontend-test
```

The test stack uses the separate `elimin8-e2e` Compose project and its own database and storage volumes, so it does not raise the login limit on the normal `elimin8` stack. Stop the normal stack first to free its host ports, or override `MONGODB_PORT`, `BACKEND_PORT`, `FRONTEND_PORT`, and `PHOTO_STORAGE_PORT` and set `E2E_BASE_URL` to the test frontend. Stop the test stack with `docker compose -p elimin8-e2e -f compose.yaml -f compose.e2e.yaml down`.

Tests cover real organizer/player browser contexts, mission batches and partial-failure recovery, mission toggles and persistence, joined-only start, MQTT lifecycle updates, assigned-target enforcement, elimination confirmation/decline/transfers, failed-response recovery, final victory, reconnect recovery, topic isolation, code reset, and API permissions. On Windows they use installed Microsoft Edge; elsewhere run `npx playwright install chromium` first. Override the browser with `PLAYWRIGHT_CHANNEL` and the server with `E2E_BASE_URL`. Test games are created in the configured development database and ended after successful checks; existing games are not deleted.

See the [project README](../README.md) for the complete game rules, the [roadmap](../ROADMAP.md), and the [backend README](../backend/README.md) for the API architecture.
