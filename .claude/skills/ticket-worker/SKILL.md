---
name: ticket-worker
description: Playbook for implementing one GitHub issue in Elimin8 (branch from dev, tests, draft PR into dev). Used by ticket-worker subagents; invoke manually as /ticket-worker <issue#> to work a single ticket yourself.
---

# Ticket worker

You implement **one issue** and deliver a **draft PR into `dev`**. Nothing else.

## Hard rules

- **Never** commit or push to `main` or `dev`. **Never** merge any PR
  (`gh pr merge` is denied). Never force-push, never rewrite someone else's
  branch. Only a human merges `dev` -> `main`.
- Base your branch on the **latest `origin/dev`**, not main.
- Branch name: `<type>/<issue#>-<short-slug>` where type is `feature`, `fix`
  or `chore` (e.g. `feature/12-kill-feed`). One issue = one branch = one PR.
- Stay in scope. Anything discovered but out of scope goes in the PR body /
  final report so the orchestrator can file a new issue. Don't fix it.
- Follow `CLAUDE.md`: server is authoritative, no leaking
  missions/targets/team/kill counts in API responses, no multi-document
  writes, never hardcode ports in `compose.yaml`.

## Token budget (~150k)

Target **<= ~150k tokens** for the whole ticket. To stay inside it:

1. Read the issue and only the files it implies; use grep/targeted reads, not
   directory dumps. Don't re-read files you just edited.
2. Run tests narrowly while iterating
   (`cd backend && go test ./internal/services -run X`), the full suite once
   before pushing.
3. Checkpoint: once you understand the task, if you estimate it won't fit
   (touches many unrelated areas, needs design decisions, needs more than ~8
   files of real changes), **stop** and report `NEEDS-SPLIT` with a proposed
   split. An oversized ticket is for the orchestrator to re-slice, not for
   you to grind through.
4. If you are blocked on a human decision, report `NEEDS-INPUT` with the
   exact question rather than guessing.

## Procedure

0. **Claim first.** Several people's agents pick tickets concurrently; the
   assignee is the lock.
   - `gh issue view <n> --json assignees,labels,state`. If it has an assignee,
     is closed, or lacks `ready-for-agent`, stop and report `TAKEN`.
   - Otherwise `gh issue edit <n> --add-assignee @me --add-label in-progress`
     and re-read the assignees: if anyone other than you is there, you lost the
     race: remove yourself and report `TAKEN`.
   - If you end without a PR (`NEEDS-SPLIT`, `NEEDS-INPUT`, `BLOCKED`), release:
     `--remove-assignee @me --remove-label in-progress` and comment why.
1. `gh issue view <n>`: read body + comments. Confirm acceptance criteria are
   testable. Check "Blocked by" tickets are merged into `dev`; if not, release
   the claim and report `BLOCKED`. Only edit files listed under "Owns files";
   if you must touch a shared file beyond what's listed, keep the change
   minimal and mention it in the PR.
2. `git fetch origin && git switch -c <branch> origin/dev`, then
   `git branch --unset-upstream` so a bare `git push` can't hit dev.
3. Implement the thinnest vertical slice satisfying the acceptance criteria.
   Add/adjust tests next to existing ones (Go: `*_test.go` beside the code;
   e2e: `frontend/tests/*.spec.js`). Update README/CHANGELOG only where
   behaviour or setup actually changed.
4. Verify with `make check` (gofmt, vet, backend tests, frontend build). Run
   `make frontend-test` only if you changed frontend behaviour and the Docker
   stack is available; otherwise say it was not run.
5. Commit in small logical commits, imperative messages, ending with the
   attribution line from the session's system reminder.
6. **You own your conflicts.** There is no central orchestrator: before
   marking ready, and again whenever another PR lands in `dev`,
   `git fetch origin && git rebase origin/dev` (resolve conflicts yourself,
   keep both sides' intent, re-run `make check` after), then
   `git push -u origin HEAD:<branch>` (explicit branch name). If the branch
   was already pushed and the rebase rewrote it, `git push --force-with-lease
   origin HEAD:<branch>` is allowed for your own feature branch only, with
   the user's approval.
7. Open a **draft** PR: `gh pr create --draft --base dev --head <branch>`
   using `.github/pull_request_template.md`; include `Closes #<n>`.
8. **Address feedback until merged.** Merging is done by the `pr-reviewer`
   loop (`.claude/scripts/merge-to-dev.sh`), never by you. When CI is green and
   the branch is rebased, mark the PR ready: `gh pr ready <pr>`. Then, on every
   later run (`/worker-loop` re-enters here for your one open PR), check
   `gh pr view <pr> --json comments,reviews,mergeable,statusCheckRollup`:
   - `pr-reviewer: CHANGES <sha>` comment, human review comments, red CI, or
     conflicts -> fix on the same branch in small commits (rebase per step 6),
     `make check`, push with an explicit `HEAD:<branch>`, then
     `gh pr comment <pr>` summarising what you changed. A new head SHA
     triggers a fresh review. Disagree with a finding? Reply with reasoning,
     don't silently ignore it.
   - `pr-reviewer: LGTM <current sha>` + green CI -> nothing to do; the
     reviewer merges. Don't push again (it invalidates the LGTM).
   - After 3 fix rounds without progress, label `needs-human` and stop.
   - Once merged, the issue closes via `Closes #<n>`; delete nothing yourself.
9. Final report (this is all the orchestrator sees; keep it short):
   - `DONE` | `NEEDS-SPLIT` | `NEEDS-INPUT` | `BLOCKED` | `TAKEN`
   - PR URL and branch
   - What `make check` / tests showed (say honestly what was not run)
   - Areas/files touched (for conflict prediction)
   - Follow-ups discovered
