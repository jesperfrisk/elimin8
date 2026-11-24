---
name: ticket-worker
description: Playbook for implementing one GitHub issue in Elimin8 (branch from dev, tests, PR into dev). Used by ticket-worker subagents; invoke manually as /ticket-worker <issue#> to work a single ticket yourself.
---

# Ticket worker

You implement **one issue** and deliver a **ready-for-review PR into `dev`**. Nothing else.

## Hard rules

- **Never** commit or push to `main` or `dev`. **Never** merge any PR
  (`gh pr merge` is denied). Never force-push, never rewrite someone else's
  branch. Only a human merges `dev` -> `main`.
- Base your branch on the **latest `origin/dev`**, not main.
- Branch name: `<type>/<issue#>-<short-slug>` where type is `feature`, `fix`
  or `chore` (e.g. `feature/12-kill-feed`). One issue = one branch = one PR.
- **The human decides how; you recommend.** Don't settle alone the choices that
  are theirs to make (step 2a). Bring them in before writing code, with your
  recommendation, and build what they choose.
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
   (touches many unrelated areas, needs more than ~8 files of real changes),
   **stop** and report `NEEDS-SPLIT` with a proposed
   split. An oversized ticket is for the orchestrator to re-slice, not for
   you to grind through.
4. Open design decisions are not a reason to split: settle them with the
   human (step 2a). Only when no human is available *and* the choice is hard
   to undo, report `NEEDS-INPUT` with the exact question rather than guessing.

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
2a. **Decision checkpoint: decide together, before writing code.** The people
   you work with want a say in *how* a ticket is solved. Having read the issue
   and the code it touches, list the decisions that are genuinely theirs and
   settle them with the human first.

   *Ask about* (typical examples):
   - more than one reasonable reading of an acceptance criterion or game rule;
   - choices the issue leaves open: data/API shape beyond the contract, status
     codes and error behaviour, UX flow, wording users will read;
   - changing existing behaviour, or the expectation of an existing test;
   - trade-offs: editing a shared file beyond "Owns files", adding a
     dependency or installing a tool, adding an endpoint the issue doesn't list;
   - scope: what goes in this PR and what becomes a follow-up.

   *Don't ask about*: anything already fixed by the issue, `CLAUDE.md` or an
   existing convention in the code; naming, formatting, or other choices that
   are cheap to change in review. If nothing is open, say "no open decisions"
   in one line and carry on. This is a short conversation, not a questionnaire.

   *How:*
   - Use `AskUserQuestion`: up to 4 questions per call, 2-4 options each. Put
     your **recommendation first**, labelled "(Recommended)", and say why in
     its description; give the trade-off of every other option. Make each
     question stand on its own (name the ticket and what is being decided; the
     person may only see the dialog). Use `preview` to compare API shapes,
     snippets or UI sketches. They can always answer "Other".
   - If the choices are tangled (one answer opens new questions), the ticket is
     vague, or it is large, invoke the `grill-me` skill instead: walk the
     decision tree one question at a time, each with your recommended answer,
     until you share an understanding.
   - **Their answer wins**, even over your recommendation. Don't re-argue it.
     If it would break a hard rule (privacy and authority rules in
     `CLAUDE.md`, never merging, ...), say so once and ask again; don't
     silently deviate. If it grows the ticket past the ~150k budget, report
     `NEEDS-SPLIT` with what they decided.
   - A new decision turns up mid-work? Pause and ask the same way (batch them);
     don't guess and don't leave it for the PR.
   - Record every decision (what, options, chosen, who decided) in the PR's
     **Decisions** section (see the template).

   *No human available* (you are a subagent, a `/worker-loop` tick, or
   `AskUserQuestion` is unavailable or unanswered): never block. A subagent
   uses the decisions its spawning session collected and passed in its prompt
   (`pick-ticket` runs this checkpoint before spawning). For anything still
   open, take your recommended option, mark it `default, unconfirmed` in the
   PR's Decisions section so a human can overrule it in review, and list it in
   the final report. If the choice is hard to undo or contradicts the issue,
   release the claim and report `NEEDS-INPUT` with the exact question instead.
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
7. Open a **ready-for-review (non-draft)** PR so the reviewer sees it:
   `gh pr create --base dev --head <branch>` (no `--draft`) using `.github/pull_request_template.md`; include `Closes #<n>`.
8. **Address feedback until merged.** Merging is done by the `pr-reviewer`
   loop (`.claude/scripts/merge-to-dev.sh`), never by you. If the PR is somehow still a draft, run `gh pr ready <pr>` once
   `make check` passes and the branch is rebased. On every
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
   - Decisions made together with the human, and any taken as
     `default, unconfirmed` that still need a human's eye
   - Follow-ups discovered
