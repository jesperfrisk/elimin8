# Elimin8 Frontend

The frontend is a minimal React application built with Vite. Its starter screen checks the Go API's readiness endpoint.

## Run with Docker

From the repository root:

```sh
docker compose up --build --wait
```

Open `http://localhost:3000`. The frontend image builds static assets and serves them through Nginx as a non-root user. Nginx forwards `/api/` requests to the backend, so browser requests use the same origin.

## Local Development

Use Node.js 22.12 or newer. Start the backend on port 8080, then:

```sh
cd frontend
npm ci
npm run dev
```

Open the address printed by Vite (normally `http://localhost:5173`). Vite forwards `/api` requests to `http://127.0.0.1:8080`.

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

React/Vite starter with a connection indicator. Game screens and interactions are not implemented yet.

See the [project README](../README.md) for the complete game rules and roadmap, and the [backend README](../backend/README.md) for the API architecture.
