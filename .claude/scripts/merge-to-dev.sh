#!/usr/bin/env bash
# Guarded squash-merge of a PR into `dev`. This is the ONLY way agents may
# merge: `gh pr merge` itself is denied in .claude/settings.json, and only this
# script is allowed. It refuses anything that is not a green, non-draft,
# conflict-free PR from a feature/fix/chore branch into dev, and anything the
# reviewer has not signed off on for the *current* head commit.
#
# Usage: .claude/scripts/merge-to-dev.sh <pr-number>
set -euo pipefail

pr="${1:?usage: merge-to-dev.sh <pr-number>}"
[[ "$pr" =~ ^[0-9]+$ ]] || { echo "refuse: PR number must be numeric" >&2; exit 2; }

fail() { echo "refuse: $*" >&2; exit 1; }

json=$(gh pr view "$pr" --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,reviewDecision,labels,statusCheckRollup,comments)
q() { jq -r "$1" <<<"$json"; }

[[ "$(q .state)" == "OPEN" ]]            || fail "PR #$pr is not open"
[[ "$(q .baseRefName)" == "dev" ]]       || fail "PR #$pr targets '$(q .baseRefName)', not dev"
[[ "$(q .isDraft)" == "false" ]]         || fail "PR #$pr is a draft"
[[ "$(q .headRefName)" =~ ^(feature|fix|chore)/ ]] || fail "head branch '$(q .headRefName)' is not feature/fix/chore"
[[ "$(q .mergeable)" == "MERGEABLE" ]]   || fail "PR #$pr is not cleanly mergeable ($(q .mergeable))"
[[ "$(q .reviewDecision)" != "CHANGES_REQUESTED" ]] || fail "changes requested by a reviewer"
if jq -e '[.labels[].name] | any(. == "hold" or . == "needs-human")' <<<"$json" >/dev/null; then
  fail "PR #$pr has a hold/needs-human label"
fi

# Every check must have finished successfully (skipped/neutral are fine).
total=$(jq -r '[.statusCheckRollup[]?] | length' <<<"$json")
bad=$(jq -r '[.statusCheckRollup[]? | (.conclusion // .state) | select(. != "SUCCESS" and . != "SKIPPED" and . != "NEUTRAL")] | length' <<<"$json")
[[ "$total" -gt 0 ]] || fail "no CI checks reported yet"
[[ "$bad" -eq 0 ]]   || fail "$bad CI check(s) not green (run: gh pr checks $pr)"

# Reviewer sign-off must reference the current head SHA, so any later push
# invalidates it and forces a re-review.
head=$(q .headRefOid)
if ! jq -e --arg sha "$head" '[.comments[].body] | any(contains("pr-reviewer: LGTM " + $sha))' <<<"$json" >/dev/null; then
  fail "no 'pr-reviewer: LGTM $head' comment for the current head commit"
fi

gh pr merge "$pr" --squash --match-head-commit "$head"
echo "merged PR #$pr into dev at $head"
