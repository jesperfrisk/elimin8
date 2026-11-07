# Elimin8 Frontend

The frontend will be a React application that communicates with the Go backend API.

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

Documentation scaffold only. React tooling, source directories, dependencies, and run commands will be added when implementation begins.

See the [project README](../README.md) for the complete game rules and roadmap, and the [backend README](../backend/README.md) for the API architecture.
