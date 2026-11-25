# Elimin8 🎯

**Trust no one. Complete your mission. Be the last one standing.**

Elimin8 is a real-life social elimination game where every player is both a hunter and a target.

Each player receives a secret victim and a mission. To eliminate their victim, they must trick them into completing a specific action, such as shaking hands, saying a particular phrase, or accepting an object.

When a player is eliminated, their killer inherits their victim's target and mission. In team games, teammates can never be assigned to eliminate each other.

Elimin8 manages secret assignments, teams, eliminations, withdrawals, timers, and game progression while keeping sensitive game information hidden.

## Run Locally

Install Docker with Docker Compose v2 and start Docker Desktop (using Linux containers on Windows). From the repository root:

```sh
make up
```

(equivalent to `docker compose up --build --wait`; see the [Makefile](Makefile) for all shortcuts, or run `docker compose` directly.)

- Frontend: http://localhost:3000
- Backend readiness: http://localhost:8080/api/health/ready
- MongoDB: `mongodb://localhost:27017`, database `elimin8`

The app includes organizer and player pages, code-based game access, MongoDB persistence, and live lobby/game updates over MQTT. Docker handles the Go and Node build tools; no local installation is required.

Compose waits for MongoDB and the API to become healthy before starting dependent services. MongoDB data is stored in a named volume and survives container replacement. The frontend listens on all host interfaces on port 3000; the backend and MongoDB host ports remain bound to localhost.

If any of those host ports already have something else running locally, copy [.env.example](.env.example) to `.env` and override `MONGODB_PORT`, `BACKEND_PORT`, and/or `FRONTEND_PORT`; Compose reads `.env` automatically. This only changes host-side bindings, not the ports services use to talk to each other inside Docker.

Devices on the same network can open `http://<your-computer-LAN-IP>:3000`. On Windows, use `ipconfig` to find the IPv4 address of your Wi-Fi or Ethernet adapter. API requests and MQTT connections go through the frontend automatically. If Windows Firewall blocks access, allow inbound TCP port 3000 from `LocalSubnet` on the active network profile. This configuration is for development on a trusted network.

```sh
make logs
make down
```

`make down` (`docker compose down`) preserves database data. To deliberately delete all local database data, use `make clean` (`docker compose down --volumes`).

See [backend setup](backend/README.md) and [frontend setup](frontend/README.md) for architecture, configuration, and development commands.

## Available Now: Games, Missions, and Confirmed Eliminations

1. Open **Organize** and create a game with a name and optional description. Save the private organizer management code shown once after creation.
2. Add player names to the lobby, one per line. Each roster entry receives its own eight-character private code. Copy each invitation and share it with that player only.
3. Players open **Play** and enter the shared game invitation code plus their private player code. The organizer sees each player change from **Invited** to **Joined** immediately.
4. Add missions to the lobby, one per line. Paste as many as you need; each entry starts enabled. Disable missions to leave them out without deleting them, and re-enable them whenever you like before starting. The mission list belongs only to this game and remains hidden from players. Repeated lines create separate entries.
5. Start the game when ready. Only players who joined before the start are included, and mission configuration locks. Closing a browser does not undo a join. Included players can return using their codes; unjoined invitees cannot join after the start.
6. During an active game, confirm an elimination report for your assigned target directly. Their screen receives a live MQTT confirmation request naming you. They can accept or decline; assignments change only on acceptance. Pending requests survive reconnects. Only the two involved players and the organizer can see a report.
7. Acceptance marks the victim eliminated and transfers their target and mission to the killer. Only your currently assigned target can be reported or eliminated; accepting the report preserves the target circle. Eliminated players can still return to the game but cannot report kills. Declined reports leave assignments unchanged, and the killer can submit a new report. One outgoing report per killer and one incoming report per victim can be pending at a time. Reports involving a newly eliminated player are cancelled.
8. The game ends as soon as there is a survival winner: the last active player, or, in team games, the last team with active players or a team that holds an unbeatable majority (no team-safe circle can exist any more). Eliminations and withdrawals both trigger the check. The winner's name is announced; nothing else is revealed. The organizer can also end a lobby or active game, cancelling pending reports. Ended games remain available to their organizer and previously admitted players.

Starting requires at least two joined players and one enabled mission. Each participant receives a private, randomly assigned mission and victim. Victims form one shuffled circle: everyone has one victim and one hunter, and nobody targets themselves. Every enabled mission entry is used once before any entry is reused; each new round is shuffled again. Disabled entries and unjoined invitees are excluded. Assignments persist when players reconnect. Shared participant information exposes only names, IDs, and status (active, eliminated, or withdrawn); other players' assignments stay private. An active player can withdraw in two steps: request, then confirm. A withdrawal is not a kill, and the withdrawn player's hunter inherits their victim and mission. When a game ends, the server calculates a final result once: the winner or winners (shared top scores are a tie), and each player's or team's confirmed kills. It is visible to everyone in the game only after the game has ended; mid-game responses never contain kill counts. Teams, timers, and organizer dispute resolution remain future work. The rules below describe the planned complete game.

Player and organizer codes authenticate access across devices. Private codes are shown only when generated; an organizer can reset a lost player code, invalidating that player's previous code and access credentials. Browser tabs remember temporary access for up to 12 hours, without saving private codes. Keep the organizer management code: account recovery is not implemented.

Lobby and lifecycle updates use MQTT over same-origin WebSockets, with no frontend polling. The Go API embeds the broker; no extra broker port or service is required. A mutex serializes game mutations inside the API, and atomic MongoDB revision checks protect concurrent joins and starts across instances. See the backend README for realtime deployment constraints.

## 🎮 How It Works

### 1. Create a Game

An organizer creates a game and invites participants using a link or game code.

The organizer can configure:

- Game name and description
- Individual or team mode
- Manual or randomized teams
- Manual or randomized victim assignments
- Available missions
- Optional game duration
- Game rules and settings

### 2. Join the Game

Players join the lobby using an invitation link or game code.

Secret assignments are not revealed until the game starts.

### 3. Start the Game

When the organizer starts the game, each player privately receives:

- **Their victim:** The player they must eliminate.
- **Their mission:** The task they must complete.
- **Their teammates:** Other members of their team, if teams are enabled.

Players cannot see who is hunting them, who other players are hunting, or which players belong to opposing teams.

If a timer is enabled, the countdown begins when the game starts.

### 4. Complete Your Mission

Players attempt to complete their assigned missions through real-life interactions.

Example missions:

- Get your victim to shake your hand.
- Make your victim say a specific word.
- Convince your victim to give you a high-five.
- Get your victim to take a selfie with you.
- Make your victim hand you an object.

All missions must be safe, voluntary, and appropriate for the participants.

### 5. Confirm an Elimination

Both the killer and victim must confirm an elimination in the app.

1. The killer reports a successful elimination.
2. The victim receives a confirmation request.
3. The victim confirms the elimination.
4. Once both have confirmed, the victim is marked as eliminated.
5. The killer inherits the victim's target and mission.
6. Assignments are updated automatically.
7. The public Kill Board updates without revealing the killer's identity.

Disputed eliminations can be reviewed by the organizer.

### 6. Inherit a Victim

When Alice eliminates Bob, Alice inherits Bob's victim and mission.

Example:

Before:

- Alice → Bob
- Bob → Charlie
- Charlie → Diana
- Diana → Alice

After Alice eliminates Bob:

- Alice → Charlie
- Charlie → Diana
- Diana → Alice

In team games, the system must ensure that no player inherits a teammate as their victim.

If necessary, assignments are rearranged automatically.

### 7. Leave a Game

Players can voluntarily withdraw from an active game.

1. The player selects "Leave Game".
2. The app asks for confirmation.
3. The player confirms their withdrawal.
4. The player is marked as withdrawn.
5. Their hunter inherits their victim and mission.
6. The Kill Board updates.

Withdrawals do not count as kills.

### 8. Kill Board

Every game includes a public **Kill Board** displaying the status of participants.

The Kill Board only displays:

- Player names
- Player status (Active, Eliminated, or Withdrawn)
- Total number of active players
- Remaining game time, if enabled

Example:

| Player  | Status     |
| ------- | ---------- |
| Alice   | Active     |
| Bob     | Eliminated |
| Charlie | Active     |
| Diana   | Withdrawn  |
| Eve     | Active     |

**The Kill Board must never display:**

- Kill counts
- Team names or team memberships
- Who eliminated whom
- Current victim assignments
- Current missions
- Assignment history
- Individual elimination statistics

The purpose of the Kill Board is to show who remains in the game without exposing relationships between players.

### 9. Win the Game

Elimin8 supports both timed and untimed games.

#### Untimed Games

- **Individual mode:** The last active player wins.
- **Team mode:** The last remaining team wins.

#### Timed Games

The organizer can configure a game duration, such as 30 minutes, 1 hour, 2 hours, or a custom duration.

If a survival winner is determined before the timer expires, the game ends immediately using the normal rules.

If the timer expires first:

- **Individual mode:** The player with the most confirmed kills wins.
- **Team mode:** The team with the highest combined number of confirmed kills wins.

Kill counts remain secret until the game has ended.

Kills earned by eliminated or withdrawn players still count toward the final results.

If multiple players or teams share the highest score, the result is a tie by default.

Pending elimination claims reported before the deadline may be resolved during a configurable confirmation grace period.

## 👥 Teams

Elimin8 supports both individual and team-based games.

Teams may be assigned manually by the organizer or generated randomly.

Players can privately see their own teammates.

Other team memberships are never publicly displayed during an active game.

### Team Rules

1. Every active player has one victim and one hunter.
2. No player can target themselves.
3. No player can target a teammate.
4. Victim assignments form a single closed loop.
5. Team restrictions apply after eliminations and withdrawals.
6. Assignments must be rearranged when necessary to preserve these restrictions.

If a team gains an unbeatable majority of active players and valid assignments become impossible, that team wins immediately under the default team-game rules.

## ⏱️ Game Timer

The game timer is optional.

When enabled:

1. The timer starts when the game begins.
2. All players can see the remaining time.
3. The timer pauses if the game is paused.
4. A survival victory ends the game immediately.
5. If time expires first, the game enters a finalization phase.
6. Eligible pending eliminations are resolved.
7. The system calculates the winner using secret kill counts.
8. The final result is announced.

The server is responsible for maintaining the authoritative timer.

## 🔒 Privacy and Information Visibility

Secrecy is a core mechanic of Elimin8.

The application must enforce different visibility levels for game information.

| Information               | Player | Organizer | Board link viewer |
| ------------------------- | ------ | --------- | ----------------- |
| Own victim                | Yes    | Yes       | No                |
| Own mission               | Yes    | Yes       | No                |
| Own teammates             | Yes    | Yes       | No                |
| Other players' victims    | No     | Yes       | No                |
| Other players' missions   | No     | Yes       | No                |
| Opposing team memberships | No     | Yes       | No                |
| Public player statuses    | Yes    | Yes       | Yes               |
| Kill counts during game   | No     | Yes       | No                |
| Killer identities         | No     | Yes       | No                |
| Remaining time            | Yes    | Yes       | Yes               |

A *board link viewer* is anyone who opens the public board link (`#/board/<gameId>`). They need no account and can only read names, status, the active count and the timer.

All participants can see the public Kill Board, but it must not reveal any secret relationships.

### Privacy Requirements

- Kill counts must be stored privately on the server.
- Team memberships must not be exposed through public APIs.
- Active victim assignments must never be included in public game responses.
- Elimination notifications must not reveal the killer's identity to uninvolved players.
- Public activity feeds must not expose elimination relationships.
- Players must only be able to access their own assignments and team information.
- Organizers may access private game information for administration and dispute resolution.
- Final results may reveal winners and scores after the game ends.

## ✨ Features

### Game Management

- Create and manage games
- Invite players with links or game codes
- Configure individual or team games
- Assign teams manually or randomly
- Configure missions and victim assignments
- Enable optional game timers
- Start, pause, and end games
- Manage disputes

### Secret Assignments

- Generate valid circular target assignments
- Prevent teammates from targeting one another
- Reveal assignments privately when the game starts
- Automatically transfer victims and missions
- Preserve assignment secrecy

### Eliminations and Withdrawals

- Require confirmation from both killer and victim
- Handle disputes
- Automatically update assignments
- Support confirmed voluntary withdrawals
- Maintain private elimination history
- Track confirmed kills internally

### Public Kill Board

- Display active players
- Display eliminated players
- Display withdrawn players
- Show remaining active player count
- Show remaining game time
- Never expose teams, kill counts, or killer identities

### Victory Detection

- Detect individual survival winners
- Detect team survival winners
- Support timed games
- Calculate private individual and team scores
- Handle tied results

## 🗺️ Roadmap

Planned and completed work is tracked in [ROADMAP.md](ROADMAP.md).

## 🤝 Contributing

Contributions, bug reports, and feature suggestions are welcome.

Contribution guidelines will be added as the project develops.

## 📄 License

License to be determined.

---

**Elimin8**: Everyone is a target. Nobody is safe. Only one remains.
