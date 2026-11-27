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

Open the address printed by Vite (normally `http://localhost:5173`). The dev proxy keeps the browser's Host header (`changeOrigin: false` in `vite.config.js`), which the backend's same-origin checks for player login and remembered sessions require. Vite forwards `/api` requests and `/api/mqtt` WebSocket connections to `http://127.0.0.1:8080`.

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
| `MissionPresets.jsx`, `missionPresetCatalog.js` | Optional built-in mission picker; selected tasks become ordinary game-local missions through the existing API |
| `Teams.jsx`, `Timer.jsx`, `Withdrawal.jsx`, `KillBoard.jsx`, `Results.jsx` | Shipped Phase 1 team setup, countdown, confirmed withdrawal, authenticated status board and final results |
| `slots.jsx` | Extension points for new panels (below) |
| `common.jsx` | Shared helpers: `PageHeading`, `CopyButton`, `formatDate` |
| `Board.jsx`, `board.css`, `boardLayout.js` | Public live board (`#/board/<gameId>`), its skin, and pure layout helpers (tile fitting, initials, avatar colour) |
| `Recap.jsx`, `recap.css`, `recapData.js` | Post-game recap (winner, leaderboard, timeline, awards, summary) shown on the board and game pages once a game has ended |
| `rememberedGames.js`, `PlayerGateway.jsx` | Remembered player games: non-secret saved metadata, the session exchange and revoke calls, renewal timing, and the screen that restores a game or asks for the code |
| `PlayerAvatar.jsx`, `PhotoUpload.jsx`, `photoImage.js`, `photo.css` | A player's picture (photo with initials fallback), the photo upload panel and roster controls, and on-device photo shrinking |
| `boardMotion.js`, `boardSound.js` | The elimination moment: pure timing and diff logic, and the isolated Web Audio tones (replaceable by real audio files) |
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
#36, and the `#/board/<gameId>` page is implemented in #39. Public recap access
and its UI remain pending #42.

Any configuration shipped to the browser is public. Database credentials and other secrets must remain on the backend.

## Current Status

Implemented routes use hash navigation:

- `/#/`: landing page and the games this device can open: player games remembered on this device and organizer games remembered in this tab.
- `/#/organizer`: create a game or reopen it using invitation/management codes.
- `/#/organizer/{id}`: add named players, copy private invitations, see joined status, reset codes, start/end games.
- `/#/player`: join or return using invitation/private player codes.
- `/#/player/{id}`: own lobby membership, active game, and ended-game status. A game this tab has no access to is signed back in from the remembered cookie, or asks for the private code.
- `/#/board/{id}`: the public live board (no login, no app chrome), see below.

The organizer page also includes **Lobby missions**. Paste one mission per line; blank lines are ignored and repeated tasks remain separate entries. Large lists are submitted in batches of 100. Each successful batch clears its saved lines from the input, preserving unsaved lines if a later batch fails. New missions start enabled; use **Disable** or **Enable** on each entry to change which tasks are eligible. Changes save immediately, synchronize across organizer tabs, and lock when the lobby closes.

**Mission presets** offers four optional tasks involving the assigned victim:
making them laugh, a high-five, saying Cheers, and a selfie. Select any combination
with the checkboxes, then press **Add selected presets** to add them to this
game's mission list. Nothing is added automatically. Custom entry stays available,
and presets use the same enable/disable controls and mission assignment rules.
While saving, the picker is disabled. A rejected save shows the server error and
keeps the selection for retry; a successful save clears it before refreshing game
state so a failed refresh cannot resubmit saved tasks. The catalogue is static,
but the resulting entries belong only to the current game. Only organizers see
the picker, and only in the lobby.

Starting requires at least two joined players and one enabled mission. Each participant gets a private mission and a victim. Victims form one shuffled circle: nobody targets themselves, and everyone has exactly one victim and one hunter. All enabled mission entries are used once before reuse, with each round shuffled again. Players see only their own mission and victim after the start, including after reloads or reconnects; organizers see all assignments in the roster. The lobby mission list remains organizer-only.

Active players can confirm an elimination report directly for their assigned target, without selecting or searching for a player. The backend rejects reports against any other player. MQTT triggers the victim's on-screen request naming the killer, with **Accept elimination** and **Decline elimination** actions. The request receives keyboard focus and survives reloads/reconnects. Acceptance transfers the victim's target and mission to the killer; decline leaves assignments unchanged. Pending, accepted, declined, and cancelled reports are visible only to involved players and the organizer. Eliminated players retain game access but cannot report more kills. The final survivor or winning team ends the game automatically. Shared participant information exposes IDs, names, status and optional elimination timestamps.

Active players can withdraw from the game with **Withdraw from game** (`Withdrawal.jsx`, an in-game panel). The first click only requests a short-lived confirmation token and shows a confirmation step; **Confirm withdrawal** is a separate second call, and **Keep playing** (or waiting for the token to expire) changes nothing. A withdrawn player sees a withdrawn screen and no assignments, their hunter inherits their victim and mission, and the roster shows them as Withdrawn. A withdrawal is not a kill.

When a game ends by survival or timer expiry, the **Final results** panel (`Results.jsx`, a game-over panel) shows the server-calculated result: the winner or winners (with a tie notice when the top score is shared) and the final standings with each player's or team's confirmed kills. It loads `GET /api/games/{id}/result` once the game has ended, for organizers and players alike. Kill counts are never sent before then, and a game the organizer ended manually has no result to show.

**Live board.** `#/board/<gameId>` is a full-screen, read-only wall display for a TV or phone. It needs no login and is outside the app shell. It calls only `GET /api/board/{id}` and subscribes anonymously to `boards/{id}/updates` (a `board-` client id, no username); it never reads stored access tokens. A `board.updated` message triggers one refetch, there is no polling, and a subscribe or reconnect refetches so nothing is missed. The header shows the game name, the active count, the server-authoritative countdown (reusing `useCountdown` from `Timer.jsx`), a **Live** / **Reconnecting…** badge and a **Fullscreen** button. States: loading, unknown game (friendly 404 screen), lobby ("waiting for the game to start", no players listed), active, and ended (a "Game over" banner with the survivors tagged; the recap ticket replaces the banner).

Each player is a portrait poster tile: the avatar fills the tile (their photo, or initials on a colour derived from the player ID when they have none or it has expired), the player's name and a text status sit on a dark strip right under it (no display numbers). Active tiles glow lime. Eliminated tiles are dimmed and desaturated with a red X across the picture and the text "Eliminated"; withdrawn tiles are grey with a dashed border and the text "Withdrew", never a red X. Status is always text as well as colour, and each tile has an `aria-label` with name and status. The grid is sized by `fitGrid` so every player fits the screen without scrolling from 2 to about 100 players on a TV; below a readable tile size (92px) it keeps that size and scrolls vertically instead. Decorative glow and blink animations are disabled under `prefers-reduced-motion`. Organizers and joined players get an **Open the live board** link and a **Copy board link** button in their game views. **Elimination moment.** A change that arrives over a live update plays a short moment; the first load, a reconnect or subscribe catch-up, waking a sleeping tab and a game that ended before the board opened show the final state silently. An elimination flashes the tile red, shakes it, sweeps a red X across the picture in two strokes and settles it into the dimmed eliminated state, while the active count ticks down in red, and plays a low synthesized gong. Withdrawals get a quieter grey fade and no sound. At the end of the game the survivors get a light sweep and gold glow with a short rising chord, after the last elimination, and then the "Game over" banner appears. Several eliminations in one update queue and play one after another, each in order: the first three get the full 1.8 s moment, the 4th to 7th about half, the rest a quick snap, so a burst takes about 11 s at most (`boardMotion.js`); a tile keeps looking active until its own moment starts. Under `prefers-reduced-motion` the flash, shake and sweep become a short fade and the sound toggle stays.

Sound is generated with the Web Audio API (no audio file), so it is silent until someone taps **Tap to enable sound** (browsers block audio before a gesture; **Keep silent** hides the gate). The choice and the mute toggle (**Sound on** / **Sound off**) are remembered per device in `localStorage`; after a reload the first tap or key press re-arms audio. Blocked audio, a failing `AudioContext` and unavailable or throwing storage are all ignored without an error. Every cue also fires a `board:sound` window event (`{ kind, audible, id }`) that tests use to observe sound.

**Photos.** A joined player gets a **Your photo** panel in the lobby and during the game (not after it ends). **Take a photo** opens the front camera on phones and **Choose a photo** opens the gallery or files, so nobody is stuck with camera-only. The picked photo is shown in the shape of a board tile with **Use this photo**, **Choose another** and **Cancel**; nothing is uploaded, or shown publicly, until it is confirmed. Before upload the browser shrinks the photo to at most 1200 px on its longest side and re-encodes it as a JPEG (`photoImage.js`), so camera photos never hit the server's 5 MiB or 25 megapixel limit, uploads are quick on mobile data and EXIF never leaves the device; the server still re-processes everything. Server errors (too large, unsupported, game ended) are shown next to the buttons. The panel states that the photo is shown on the public board and deleted 24 hours after the game ends, and **Remove photo** takes it off again. Organizers see a small avatar for every player in the roster, with **Add photo** / **Replace photo** (uploaded straight away) and **Remove photo** (after a confirmation) next to **Reset code**, until the game ends. `PlayerAvatar` shows the photo with a fixed aspect, so loading never shifts the layout, and falls back to initials if there is no photo or it has expired. Photos of players in the game are read without credentials (that is how the public board shows them); in the organizer and player views they are fetched with the signed-in credentials, so a photo for an invitee who has not joined still shows.

**Remembered games (#84).** A player code login also sets a game-scoped, HttpOnly, 30-day cookie (the backend does this; JavaScript never reads it). The browser keeps only non-secret lookup metadata in `localStorage` (`elimin8.games.v1`: game id, name, invitation code, saved time), never private codes, the remember secret or any hash, and the per-tab bearer access stays in `sessionStorage` as before. After a restart the games are listed on `#/player` as "remembered on this device". Opening one asks the server to exchange the cookie for a fresh admission (`POST /api/games/{id}/session`), so the same player is back without a code; nothing is restored until a game is opened. Remembering two games never overwrites either. While a game is open the page renews its admission 15 minutes before it ends, and again if the server rejects it, which swaps the token and reconnects live updates without losing the page; renewal never logs in, rejoins or extends the 30 days, and stops if a renewed admission is barely longer than the last (the 30-day limit is near). If the remembered sign-in is gone (cookies cleared, 30 days passed, signed out elsewhere, or the organizer reset the code) a recovery form shows the game with its invitation code pre-filled and asks only for the private code; the right code returns the same player and sets the cookie again, a wrong code or another game's code is refused, and **Remove this game from this device** forgets it. **Sign out of this game** revokes the credential on the server first and only then clears that game's saved data, access and cookie, leaving other games alone; if the server can't be reached the player stays signed in and is told so. A temporary failure when opening a game (offline, rate limited) offers **Try again**. Organizer access is unchanged and is never remembered.

Start/end errors appear beside the game controls, receive keyboard focus, and scroll into view. Messages distinguish missing enabled missions, expired access, missing permissions or games, lifecycle conflicts, invalid data, rate limits, and server/connection failures. **Refresh game** resynchronizes uncertain outcomes; access errors also link to the organizer desk. A successful action followed by a failed state refresh is reported separately from a rejected action.

Organizers can paste player names one per line. Generated codes are held only in page memory and can be copied into individual invitations; they are never written to browser storage. Temporary access tokens and basic game labels are kept in `sessionStorage` for reloads. Private codes allow access from another browser. A returning organizer must retain the management code shown once at creation.

MQTT.js subscribes to the role-scoped topic returned by the API. Subscribe acknowledgement is followed by a state fetch, preventing missed events during initial connection or reconnection. Notifications cause authorized fetches; there is no periodic polling. Disconnection is visible, failed fetches retry with backoff, and revoked/expired access prompts code re-entry. MQTT keepalives maintain the transport and do not poll game state.

## End-to-end Checks

Start the isolated test stack, then run the suite. `make e2e-up` applies `compose.e2e.yaml`, which raises the backend per-client login limit to 600 attempts per minute for this stack only; fixture logins from one client can exceed the normal default of 30. Runtime proxy isolation (#52 / PR #66) now identifies separate clients behind configured trusted proxies; it does not exempt repeated browser-fixture logins from one client, and an aggregate endpoint cap still applies. Fixture logins assert a successful response, so a rate-limit failure is reported at the login rather than later. See the [backend README](../backend/README.md) for native/Vite and Compose proxy settings and the outstanding deployed-stack check.

```sh
make e2e-up
make frontend-test
```

The test stack uses the separate `elimin8-e2e` Compose project and its own database and storage volumes, so it does not raise the login limit on the normal `elimin8` stack. Stop the normal stack first to free its host ports, or override `MONGODB_PORT`, `BACKEND_PORT`, `FRONTEND_PORT`, `MONGO_UI_PORT`, `PHOTO_STORAGE_PORT` and `PHOTO_STORAGE_CONSOLE_PORT` (`make e2e-up` runs the host-port preflight first) and set `E2E_BASE_URL` to the test frontend. Stop the test stack with `docker compose -p elimin8-e2e -f compose.yaml -f compose.e2e.yaml down`.

Tests cover real organizer/player browser contexts, mission batches and partial-failure recovery, mission toggles and persistence, joined-only start, MQTT lifecycle updates, assigned-target enforcement, elimination confirmation/decline/transfers, failed-response recovery, final victory, reconnect recovery, topic isolation, code reset, and API permissions. On Windows they use installed Microsoft Edge; elsewhere run `npx playwright install chromium` first. Override the browser with `PLAYWRIGHT_CHANNEL` and the server with `E2E_BASE_URL`. Test games are created in the configured development database and ended after successful checks; existing games are not deleted.

See the [project README](../README.md) for the complete game rules, the [roadmap](../ROADMAP.md), and the [backend README](../backend/README.md) for the API architecture.
