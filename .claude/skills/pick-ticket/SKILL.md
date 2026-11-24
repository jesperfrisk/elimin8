---
name: pick-ticket
description: Pick the next free ticket from the shared GitHub backlog and work it with the ticket-worker playbook. Several people run this concurrently; the issue assignee is the lock. Invoke as /pick-ticket [issue#].
---

# Pick ticket

Several people, each with their own agents, pull from the same backlog. No
central dispatcher: **assignee = lock**.

## Find a free ticket

If an issue number was given, use it. Otherwise:

```sh
gh issue list --label ready-for-agent --state open --assignee "" \
  --json number,title,body,labels --limit 50
```

A ticket is **free** when all hold:
- open, label `ready-for-agent`, **no assignee**, not `in-progress`
- every issue under "Blocked by" is closed **and** its PR is merged into
  `origin/dev` (`gh issue view <blocker> --json state`; `git log origin/dev`)

Prefer the lowest-numbered free ticket (blockers are filed first). If none is
free, say which tickets are blocked/taken and by whom, and stop.

## Work it

Follow the `ticket-worker` skill from step 0 (claim) onward, either yourself
or by spawning one `ticket-worker` subagent. Two tickets that list the same
file under "Owns files" must not run in parallel: if one of those is already
`in-progress`, skip to the next free ticket.

## Conflicts and merging

- You own conflicts on your PR: rebase on `origin/dev` before ready-for-review
  and whenever another PR lands first (see `ticket-worker` step 6).
- Workers never merge. The `pr-reviewer` skill reviews, comments and
  squash-merges into `dev` through `.claude/scripts/merge-to-dev.sh` once CI is
  green; you fix its comments (`ticket-worker` step 8). `dev` -> `main` is a
  human-only release PR (`/orchestrate-tickets` section 5).
- Running this in a loop? Use `/loop 10m /worker-loop`: it holds one ticket
  at a time, fixes review feedback on it, and calls this skill again only
  after its PR is merged. Never claim a second ticket while you hold one.
