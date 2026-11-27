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

### Phase 1 implementation and verification

All 16 MVP capabilities are implemented on `dev`. The evidence below maps each
checkbox to the merged implementation and regression coverage; completion does
not mean every deployment has been refreshed or every follow-up is resolved.

| Capability | Implementation | Verification |
| --- | --- | --- |
| Create games | [Lifecycle service](backend/internal/services/lifecycle.go) | `TestLifecycleAndPrivacy` in [games tests](backend/internal/services/games_test.go) |
| Join with invitation codes | [Lifecycle service](backend/internal/services/lifecycle.go) | Lifecycle, private-code reset and concurrent join/start tests in [games tests](backend/internal/services/games_test.go) |
| Manual/random teams | [Team service](backend/internal/services/teams.go) | `TestTeamsManualRandomAndStartValidation` in [team tests](backend/internal/services/teams_test.go) |
| Configure missions | [Mission service](backend/internal/services/missions.go) | Persistence, privacy, validation and retry tests in [mission tests](backend/internal/services/missions_test.go) |
| Exhaust enabled missions before reuse | [Mission assignment](backend/internal/services/missions.go) | [Assignment tests](backend/internal/services/mission_assignment_test.go) |
| Single private victim circle | [Victim assignment](backend/internal/services/victims.go) | `TestVictimsFormOneCircleOfJoinedPlayers` in [victim tests](backend/internal/services/victims_test.go) |
| Team-safe assignment | [Victim assignment](backend/internal/services/victims.go) | [Team-circle tests](backend/internal/services/victims_team_test.go) and [transfer audit](backend/internal/services/team_victim_audit_test.go) |
| Reveal assignments at start | [Role-scoped views](backend/internal/services/views.go) | [Mission lifecycle](backend/internal/services/mission_lifecycle_test.go) and [victim lifecycle](backend/internal/services/victim_lifecycle_test.go) tests |
| Mutual elimination confirmation | [Elimination service](backend/internal/services/eliminations.go) | [Confirmation, decline, concurrency and privacy tests](backend/internal/services/eliminations_test.go) |
| Automatic victim/mission transfer | [Elimination service](backend/internal/services/eliminations.go) | Circle/transfer tests in [elimination tests](backend/internal/services/eliminations_test.go) and [team audit](backend/internal/services/team_victim_audit_test.go) |
| Confirmed withdrawals | [PR #28](https://github.com/jesperfrisk/elimin8/pull/28) | [Withdrawal tests](backend/internal/services/withdrawals_test.go), including transfers without a kill and concurrent confirmations |
| Privacy-safe Kill Board | [PR #30](https://github.com/jesperfrisk/elimin8/pull/30) | [Exact-field, ordering and inclusion tests](backend/internal/services/killboard_test.go) |
| Private confirmed kills | [PR #24](https://github.com/jesperfrisk/elimin8/pull/24) | [Kill tests](backend/internal/services/kills_test.go), including `TestKillCountsNeverInAnyAPIResponse` |
| Optional timers | [Timer service](backend/internal/services/timer.go) | [Timer tests](backend/internal/services/timer_test.go) and [browser timer test](frontend/tests/timer.spec.js) |
| Individual/team winners | [PR #31](https://github.com/jesperfrisk/elimin8/pull/31), corrected by [PR #61](https://github.com/jesperfrisk/elimin8/pull/61) | [Winner tests](backend/internal/services/winners_test.go), including singleton-team elimination and withdrawal |
| Final results | [PR #33](https://github.com/jesperfrisk/elimin8/pull/33) | [Results tests](backend/internal/services/results_test.go), including ties, immutability and no mid-game kill counts |

The acceptance follow-ups are resolved: [#43](https://github.com/jesperfrisk/elimin8/issues/43)
fixed persisted timer/team clearing ([PR #60](https://github.com/jesperfrisk/elimin8/pull/60));
[#44](https://github.com/jesperfrisk/elimin8/issues/44) fixed singleton-team winner identity
([PR #61](https://github.com/jesperfrisk/elimin8/pull/61)); and
[#45](https://github.com/jesperfrisk/elimin8/issues/45) isolated browser-fixture login limits
([PR #64](https://github.com/jesperfrisk/elimin8/pull/64)). The
[#45 completion report](https://github.com/jesperfrisk/elimin8/issues/45#issuecomment-7215488952)
records two consecutive full Playwright runs passing 18/18 after the fixes.
That is recorded validation evidence, not a new test run by this documentation audit.

Remaining follow-ups are [#35](https://github.com/jesperfrisk/elimin8/issues/35)
(refresh and identify the affected stale local runtime) and
[#52](https://github.com/jesperfrisk/elimin8/issues/52) (runtime login limits behind
trusted proxies). Neither is a missing Phase 1 feature. Phase 2/3 work below
retains its existing scope.

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

- [ ] Upload a photo/selfie per game and per temporary player account
- [ ] Object storage (RustFS or another S3-compatible store) in the Docker Compose infrastructure
- [ ] Delete all uploaded images 24 hours after the game ends (configurable retention; #37 / #55)

Developer experience:

- [ ] `make dev`: seed realistic test data (lobbies with believable players)
- [ ] `make reset`: restore everything to a clean state
