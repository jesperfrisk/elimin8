# Elimin8 Frontend

The frontend is a React application built with Vite, with organizer and player pages and event-driven MQTT updates.

## Run with Docker

From the repository root:

```sh
docker compose up --build --wait
```

Open `http://localhost:3000`. The frontend image builds static assets and serves them through Nginx as a non-root user. Nginx forwards `/api/` requests to the backend, so browser requests use the same origin.

The Docker frontend also accepts connections from other devices on the same network at `http://<host-LAN-IP>:3000`. Both API requests and MQTT use that same address. Windows Firewall must allow inbound TCP port 3000 from `LocalSubnet` on the active network profile. Browser clipboard access may be unavailable over LAN HTTP; private codes remain visible for manual copying.

## Local Development

Use Node.js 22.12 or newer. Start the backend on port 8080, then:

```sh
cd frontend
npm ci
npm run dev
```

Open the address printed by Vite (normally `http://localhost:5173`). Vite forwards `/api` requests and `/api/mqtt` WebSocket connections to `http://127.0.0.1:8080`.

```sh
npm run build
```

`npm run preview` previews static assets only; use the development server or Docker to exercise the API proxy.

## Planned Experience

- Create games and configure rules, missions, teams, and optional timers.
- Join a game using an invitation link or game code.
- View private targets, missions, and teammates once a game starts.
- Report and confirm eliminations, and confirm voluntary withdrawals.
- View the public Kill Board with player statuses, active player count, and remaining time.
- Provide organizer controls and show final results after the game ends.

## API and Privacy

The backend is authoritative for game state, permissions, assignments, scores, and timers. The frontend must use responses scoped to the current user's access. Hiding information in the UI is not a substitute for server-side authorization.

The public Kill Board must not reveal targets, missions, teams, killer identities, or in-game kill counts.

Any configuration shipped to the browser is public. Database credentials and other secrets must remain on the backend.

## Current Status

Implemented routes use hash navigation:

- `/#/`: landing page and games remembered in this tab.
- `/#/organizer`: create a game or reopen it using invitation/management codes.
- `/#/organizer/{id}`: add named players, copy private invitations, see joined status, reset codes, start/end games.
- `/#/player`: join or return using invitation/private player codes.
- `/#/player/{id}`: own lobby membership, active game, and ended-game status.

Organizers can paste player names one per line. Generated codes are held only in page memory and can be copied into individual invitations; they are never written to browser storage. Temporary access tokens and basic game labels are kept in `sessionStorage` for reloads. Private codes allow access from another browser. A returning organizer must retain the management code shown once at creation.

MQTT.js subscribes to the role-scoped topic returned by the API. Subscribe acknowledgement is followed by a state fetch, preventing missed events during initial connection or reconnection. Notifications cause authorized fetches; there is no periodic polling. Disconnection is visible, failed fetches retry with backoff, and revoked/expired access prompts code re-entry. MQTT keepalives maintain the transport and do not poll game state.

## End-to-end Checks

Start the Docker stack, then run:

```sh
npm run test:e2e
```

Tests cover real organizer/player browser contexts, joined-only start, MQTT lifecycle updates, reconnect recovery, topic isolation, code reset, and API permissions. On Windows they use installed Microsoft Edge; elsewhere run `npx playwright install chromium` first. Override the browser with `PLAYWRIGHT_CHANNEL` and the server with `E2E_BASE_URL`. Test games are created in the configured development database and ended after successful checks; existing games are not deleted.

See the [project README](../README.md) for the complete game rules and roadmap, and the [backend README](../backend/README.md) for the API architecture.
