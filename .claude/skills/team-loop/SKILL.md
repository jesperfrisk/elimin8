---
name: team-loop
description: One tick of the per-person agent loop - fix feedback on your PRs, review and merge PRs into dev, then pick a new ticket. Run repeatedly with `/loop 10m /team-loop`.
---

# Team loop (one tick)

Each team member runs `/loop 10m /team-loop` on their own machine. A tick is
idempotent, never blocks waiting, and ends with a short status. All
coordination goes through GitHub (assignee = ticket lock, PR comments = review
state). Do **one** step per tick, in priority order, then stop.

## Priority order

1. **Your PRs needing work**
   (`gh pr list --author @me --base dev --state open --json number,headRefName,isDraft,mergeable,reviewDecision,comments`):
   - conflicting, CI red, a `pr-reviewer: CHANGES|CONFLICT <sha>` comment for
     the current head, or unresolved human review comments -> follow
     `ticket-worker` step 8 (address feedback) on that branch. One PR per tick.
   - draft with green CI and a rebased branch -> `gh pr ready`.
2. **Review and merge** other people's non-draft PRs into `dev` with the
   `pr-reviewer` skill (spawn the `pr-reviewer` subagent, or run the skill).
3. **New work**: only if none of your PRs wait on feedback or CI and you have
   fewer than 2 open PRs: `/pick-ticket`.
4. Nothing to do -> say so in one line and end the tick.

## Rules

- Never sleep or poll inside a tick; the loop provides the cadence.
- All `CLAUDE.md` restrictions hold: nothing on `main`, no direct `dev` pushes,
  merge only through `.claude/scripts/merge-to-dev.sh`, no approvals, no
  force-push except `--force-with-lease` on your own feature branch after a
  rebase (with the user's approval).
- If a step reports `NEEDS-INPUT`/`NEEDS-SPLIT`, or the same PR has failed 3
  ticks in a row, add the `needs-human` label, comment why, and leave it alone.

## Status line

`team-loop: <fixed #n | reviewed #n (LGTM/CHANGES/MERGED) | picked #n | idle>`,
with anything needing a human first.
