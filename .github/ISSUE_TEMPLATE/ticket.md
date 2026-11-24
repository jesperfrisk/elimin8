---
name: Ticket
about: A single, independently shippable slice sized for one worker (~150k tokens)
labels: ready-for-agent
---

## Parent

<!-- Parent issue/PRD reference, or remove this section. -->

## What to build

<!-- One behaviour, end to end. No code snippets. -->

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2

## API contract

<!-- Backend tickets: endpoints, request/response JSON, who may call them
(organizer/player/public) and what must NOT be present for each role.
UI tickets that depend on a backend ticket build against this contract.
Remove this section if no API changes. -->

## Owns files

<!-- New files this ticket creates/owns, e.g. backend/internal/services/withdrawals.go.
Also list the few shared lines it may touch (e.g. one route line in a
controller, one route/import line in frontend/src/main.jsx). Anything else
shared belongs to another ticket: don't edit it. -->

## Touches (for conflict planning)

<!-- Existing files modified, e.g. models/game.go, README.md. -->

## Verification

- [ ] `make check` passes
- [ ] Specific tests that must exist: <!-- e.g. services/withdrawals_test.go covers X -->

## Blocked by

None - can start immediately.
<!-- Otherwise: "Blocked by #12, #15" (must be merged into dev). -->
