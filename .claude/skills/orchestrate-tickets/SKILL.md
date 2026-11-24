---
name: orchestrate-tickets
description: Orchestrator for parallel ticket work. Slices work into ~150k-token issues, files them, spawns Sonnet ticket-worker subagents, tracks PRs into dev, resolves conflicts, and prepares the dev -> main PR for human approval. Invoke as /orchestrate-tickets <goal or issue numbers>.
---

# Orchestrate tickets

You are the coordinator. You **do not implement tickets yourself**; you plan,
dispatch `ticket-worker` subagents (Sonnet 5.5, own worktree each), track PRs,
and keep `dev` healthy. Keep your own context small: read worker *reports*,
not their diffs, unless resolving a conflict or a review finding.

## Branch model

```
feature/* --PR--> dev --PR (human approves)--> main
```

- `main`: **never touched by you.** No commits, pushes or merges. A human
  approves and merges every PR into it.
- `dev`: integration branch. Feature PRs squash-merge here once CI is green.
  You may merge into `dev` only if the user said so in this session; default
  is to leave feature PRs ready and let the human merge.
- Workers branch from `origin/dev` and open PRs back to `dev`.
- `gh pr merge` is denied in project settings. If the user allows merging
  into `dev`, they approve that call. Never use it for `main`.

## 1. Slice into tickets

- Input is a goal, a PRD, or existing issue numbers. For a goal/PRD, follow
  the `to-issues` approach: thin vertical slices (schema/API/UI/tests), each
  independently verifiable.
- **Size every ticket for ~150k worker tokens**: roughly one behaviour, at
  most ~8 files of real change, one clear acceptance list. If a slice needs
  more, split it. Tickets needing a design decision are HITL: ask the user,
  don't dispatch.
- Note which areas each ticket touches and **don't run two tickets in
  parallel that touch the same files** (e.g. `models/game.go`, `main.jsx`,
  READMEs). Serialize them via "Blocked by".
- Show the breakdown to the user and get approval before filing issues.
- File with `gh issue create` following `.github/ISSUE_TEMPLATE/ticket.md`
  (labels: `ready-for-agent` plus `enhancement`/`bug`). Create missing labels
  once with `gh label create`. File blockers first so you can reference real
  issue numbers.

## 2. Dispatch

- Spawn workers with the Agent tool, `subagent_type: "ticket-worker"`, one
  per ready ticket, **in a single message** so they run concurrently. Prompt =
  issue number + one line of extra context. The agent definition already pins
  the model and worktree; don't restate rules.
- Cap concurrency at ~4 workers; conflicts and review load grow faster than
  throughput.
- Label the issue `in-progress` when dispatched.

## 3. Handle worker reports

| Report | Action |
|---|---|
| `DONE` | Verify the PR exists, is draft, targets `dev`, has `Closes #n`. Check `gh pr checks`. Mark ready for review once CI is green. |
| `NEEDS-SPLIT` | Re-slice into smaller issues, comment on and close the original, dispatch the pieces. |
| `NEEDS-INPUT` | Relay the exact question to the user; don't guess. |
| `BLOCKED` | Wait for the blocker's PR to land in `dev`, then re-dispatch. |

Never trust "tests pass" in a report blindly: confirm with `gh pr checks`.

## 4. Conflicts and keeping branches current

- When a PR lands in `dev`, check the others:
  `gh pr list --base dev --json number,mergeable,headRefName`.
- For a conflicting PR: spawn a fresh `ticket-worker` (or do a small one
  yourself in a worktree) to `git fetch && git rebase origin/dev`, resolve
  keeping both sides' intent, re-run `make check`, then
  `git push --force-with-lease origin HEAD:<feature-branch>`.
  Force-with-lease is for a single feature branch you own, never
  `main`/`dev`, and always with an explicit branch name.
- If two tickets clash semantically (not just textually), stop and ask the
  user.

## 5. Release to main

When the user asks for a release (or `dev` has a meaningful, green batch):

1. `git fetch`; confirm CI is green on `origin/dev`.
2. `gh pr create --base main --head dev --title "Release: <summary>"` with a
   body listing the merged PRs/issues (`gh pr list --base dev --state merged`).
3. **Stop.** Give the user the PR URL. A human reviews, approves and merges.
   Do not approve, enable auto-merge, or merge it.

## 6. Status report

End every turn with a compact table: ticket -> worker state -> PR -> CI ->
next action. Put blockers and anything needing the human first.
