---
name: review-loop
description: One tick of the reviewer role - sweep every open PR into dev, comment or LGTM, and merge the green ones. Run with `/loop 5m /review-loop`.
---

# Review loop (one tick)

The reviewer is a dedicated role: one session (or one person's machine) runs
this on a timer. It looks at **every** open PR into `dev` (drafts included), whoever
authored it, and either leaves comments or signs off and merges. It never
implements tickets.

## Tick

1. Spawn the `pr-reviewer` subagent (or follow the `pr-reviewer` skill
   yourself) with no PR number: it sweeps all candidates itself.
2. Relay its short report (`#n -> MERGED | LGTM (waiting on CI) | CHANGES | CONFLICT | NEEDS-HUMAN | SKIPPED`).
3. End the tick. Never sleep or poll; pending CI is next tick's job.

## How "approve" works

GitHub blocks approving your own PR and `gh pr review --approve` is denied for
agents, so approval = a `pr-reviewer: LGTM <head-sha>` comment. The merge
script (`.claude/scripts/merge-to-dev.sh`) only merges when that comment
matches the current head commit, CI is green, and the base is `dev`. Any new
push by the author invalidates the LGTM and triggers a fresh review.

Several reviewers running at once is safe: verdicts are keyed by head SHA and
the merge uses `--match-head-commit`.

## Status line

`review-loop: <merged #n,... | commented #n | waiting on CI #n | idle>`, with
anything needing a human first.
