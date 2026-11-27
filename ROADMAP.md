# Roadmap 🗺️

Planned and completed work for Elimin8. See the [README](README.md) for game
rules and the [CHANGELOG](CHANGELOG.md) for what has shipped.

## Phase 1: MVP

- [x] Create games
- [x] Join games using invitation codes
- [x] Configure teams manually or randomly
- [x] Configure missions
- [x] Randomly assign private missions, exhausting enabled entries before reuse
- [x] Assign private victims in one circle without self-targets
- [x] Generate team-safe victim assignments
- [x] Reveal private assignments when games start
- [x] Implement mutual elimination confirmation
- [x] Transfer victims and missions automatically
- [x] Support confirmed player withdrawals
- [x] Create privacy-safe Kill Board
- [x] Track kills privately
- [x] Support optional game timers
- [x] Detect individual and team winners
- [x] Calculate game results

## Phase 2: Improved Experience

- [ ] Live game dashboard
- [ ] Advanced post-game statistics

## Phase 3: Frictionless Joining and Richer Play

Joining and recovery:

- [ ] One shared QR code per lobby (replaces per-player QR invitations): players scan it, enter their name, and press **Join**
- [ ] Persistent cookie holding the player's unique ID, so they stay signed in as the same player
- [ ] Cookie-loss recovery: players can re-enter their unique code to reclaim their player and have the cookie set again

Eliminations and missions:

- [ ] Notify an eliminated player that they are out, who eliminated them, and how (the mission used); the accept/decline step before elimination already exists
- [ ] Mission presets (e.g. make someone laugh, high five, cheers, selfie) selectable when configuring a lobby
- [ ] Dynamic targets and missions: only the *current* target can be eliminated, and a player's target and mission may change during play

Media:

- [ ] Upload a photo/selfie per game and per temporary player account (backend shipped in #54; UI pending in #41)
- [x] Object storage (RustFS or another S3-compatible store) in the Docker Compose infrastructure
- [x] Delete all uploaded images 24 hours after the game ends (configurable retention, `PHOTO_RETENTION_HOURS`; #37 / #55)

Developer experience:

- [ ] `make dev`: seed realistic test data (lobbies with believable players)
- [ ] `make reset`: restore everything to a clean state
