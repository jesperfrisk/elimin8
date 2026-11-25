---
name: worker-loop
description: One tick of the ticket-worker role - hold at most ONE ticket; fix review feedback on it until its PR is merged, then automatically claim the next free ticket. Run with `/loop 10m /worker-loop`.
---

# Worker loop (one tick)

A worker owns **at most one ticket at a time** (WIP limit = 1). It only picks
a new ticket after the previous PR has been merged into `dev` (or the ticket
was released). All state lives in GitHub; the issue assignee is the lock.

## Tick

1. **Find my current ticket.**
   `gh issue list --assignee @me --label in-progress --state open --json number,title`
   and `gh pr list --author @me --base dev --state open --json number,headRefName,isDraft,mergeable,reviewDecision,comments,statusCheckRollup`.
   If more than one in-progress ticket is found, stop and report it (WIP
   violation); don't pick anything new.
2. **I hold a ticket** -> work only on it, then end the tick:
   - No PR yet -> spawn one `ticket-worker` subagent for that issue (it
     resumes from its branch if one exists).
   - PR is still a draft (legacy) -> `gh pr ready` once `make check`/CI is green.
   - PR has `pr-reviewer: CHANGES|CONFLICT <sha>` for the current head, human
     review comments, red CI, or conflicts -> spawn `ticket-worker` in
     address-feedback mode (`ticket-worker` step 8) on that PR's branch.
   - PR has `pr-reviewer: LGTM <sha>` or is waiting on CI/review -> do
     nothing. Don't push (it would invalidate the LGTM). Say "waiting".
   - Same PR failed 3 ticks in a row -> label `needs-human`, comment why,
     and release the ticket (`--remove-assignee @me --remove-label in-progress`)
     so the worker isn't stuck forever.
3. **I hold nothing** (my last PR is merged and its issue closed, or I never
   had one) -> run `/pick-ticket`: claim the lowest-numbered free ticket
   whose blockers are merged into `origin/dev`, and start it. `git fetch`
   first so "merged" is judged against the latest `origin/dev`.
4. No free ticket -> say which are blocked/taken and end the tick.

## Rules

- One ticket, one branch, one PR, at a time. Never claim a second ticket
  "while waiting" on review.
- Never merge, never approve, never push to `dev`/`main`; the reviewer loop
  merges. Force-push only `--force-with-lease` on your own branch after a
  rebase, with the user's approval.
- Decisions (`ticket-worker` step 2a): when step 3 picks a *new* ticket and a
  human is attached to this session, run the decision checkpoint with them
  first. An unattended tick never blocks on a question: it takes the
  recommended option, marks it `default, unconfirmed` in the PR's Decisions
  section, and lets review overrule it.
- Never sleep or poll inside a tick; the loop provides the cadence.

## Status line

`worker-loop: <working #n | fixed feedback on #n | waiting on review #n | picked #n | idle>`,
with anything needing a human first.
