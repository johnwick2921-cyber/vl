#!/usr/bin/env bash
# m468 Q5 — the release QUALITY GATE.
#
# Usage: quality-gate.sh <commit-sha>
#   env: GITHUB_REPOSITORY  owner/name whose checks are read (required; never guessed)
#        GH_TOKEN           read by gh itself (checks:read is enough)
#        GATE_MAX_WAIT_MIN  how long to wait for a suite still running (default 300)
#        GATE_TAG           the tag being released, only used in the retry recipe
#        GH_BIN / GIT_BIN / SLEEP_BIN  overridable for tests
#
# Why it exists: on #623 the release was published at 08:31 UTC and the full
# test suite finished at 11:26 UTC. Nothing stopped a release whose tests had
# not passed. This script refuses one.
#
# Rules:
#   1. Checks are read by NAME. test.yml uses job-level continue-on-error, so a
#      RUN reports success while its "Backend Tests" job failed. Run
#      conclusions are never read; only check-run conclusions are.
#   2. For each required name, only its LATEST check-run counts (highest id),
#      so a flake that was re-run green is green, and a re-run still running is
#      still pending.
#   3. The FAST suites ("Backend Tests", "Frontend Tests", ~7-20 min) must be
#      green on the tagged commit ITSELF. Nothing stands in for them. A tag is
#      pushed seconds after its merge, so at the first poll the commit's own
#      suite is still running; if a green PR head answered then, the gate
#      would pass before the commit's own result existed. That is exactly
#      b81b03152 (v2026.10.28.1): release run 13:16:12Z, its own Backend Tests
#      started 13:16:13Z and failed 13:23:19Z, PR head green since 10-26. The
#      commit's own run is also the only one that sees TODAY's calendar (DST,
#      the roll, a clock time bomb): a pass days earlier on the same tree
#      says nothing about those.
#   4. The SLOW race suite ("Go Unit Tests & Coverage", ~2-3 h) may be answered
#      by the commit's second parent (the PR head the PR's checks ran on) when
#      the commit is a 2-parent merge AND both have the identical tree —
#      identical tree means identical code. That is the only stand-in, so the
#      usual release waits for ~15 min of its own fast suites, not ~3 hours.
#      The commit's own Backend Tests runs the same `go test ./...`, so the
#      merged code's Go tests still run on the merged commit before release.
#   5. The commit's OWN result wins whenever it has one. A required check that
#      finished non-success on the commit itself fails the gate at once, even if
#      the PR head was green: that exact code was tested and went red.
#   6. Missing or still running: poll every 120 s, up to GATE_MAX_WAIT_MIN.
#   7. HTTP 401/403/404 (or gh "not logged in") fails at once — a check we
#      cannot read is never "still pending".
#
# Exit: 0 PASS · 1 FAIL (failed, unreadable, or timed out) · 2 bad usage.
set -euo pipefail

# The checks a release requires. Names must match the check-run names exactly
# (note "Frontend Tests" is not "Frontend Tests (React/TypeScript)").
REQUIRED_CHECKS=(
  "Go Unit Tests & Coverage"
  "Backend Tests"
  "Frontend Tests"
)
# The one required check an identical-tree PR head may answer for (rule 4).
# Every other required check must be green on the tagged commit itself.
P2_MAY_ANSWER="Go Unit Tests & Coverage"

GH="${GH_BIN:-gh}"
GIT="${GIT_BIN:-git}"
SLEEP="${SLEEP_BIN:-sleep}"
POLL_SEC=120
MAX_WAIT_MIN="${GATE_MAX_WAIT_MIN:-300}"
REPO="${GITHUB_REPOSITORY:-}"
RECIPE_TAG="${GATE_TAG:-<the same tag>}"
NL=$'\n'

usage() { echo "quality-gate: $*" >&2; exit 2; }

[ "$#" -eq 1 ] || usage "usage: quality-gate.sh <commit-sha>"
[[ "$MAX_WAIT_MIN" =~ ^[0-9]+$ ]] || usage "GATE_MAX_WAIT_MIN must be a whole number of minutes, got '$MAX_WAIT_MIN'"
[[ "$REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]] || usage "GITHUB_REPOSITORY must be owner/name, got '$REPO' — the gate will not guess whose checks to read"

SHA="$("$GIT" rev-parse --verify --quiet "$1^{commit}" || true)"
[[ "$SHA" =~ ^[0-9a-f]{40}$ ]] || usage "'$1' is not a commit in this checkout"

# --- candidates ---------------------------------------------------------------
P2=""
read -r -a PARENTS <<<"$("$GIT" rev-list --parents -n 1 "$SHA")"
if [ "${#PARENTS[@]}" -eq 3 ]; then
  if [ "$("$GIT" rev-parse "$SHA^{tree}")" = "$("$GIT" rev-parse "${PARENTS[2]}^{tree}")" ]; then
    P2="${PARENTS[2]}"
  else
    echo "quality-gate: the PR head ${PARENTS[2]:0:12} (^2) has a different tree from ${SHA:0:12} — its checks tested other code and do not count; only the commit's own suite can pass this gate"
  fi
fi

NAMES_JSON="$(printf '%s\n' "${REQUIRED_CHECKS[@]}" | jq -R . | jq -cs .)"

# For each required name, the check-run with the highest id. A name with no
# check-run at all is reported as "missing". Placeholders are "-" because read
# with a tab IFS would collapse empty fields.
JQ_PICK='[.[] | .check_runs[]?] as $all
  | $names[] as $n
  | ([$all[] | select(.name == $n)] | max_by(.id)) as $r
  | if $r == null then [$n, "missing", "-", "-"]
    else [$n, ($r.status // "unknown"), ($r.conclusion // "-"), ($r.details_url // "-")] end
  | @tsv'

# read_checks <sha> sets, per required check (same order as REQUIRED_CHECKS):
#   R_STATE[i]  success | fail | pending
#   R_NOTE[i]   the conclusion (fail) or the status (pending: queued,
#               in_progress, missing, unreadable)
#   R_URL[i]    the check-run's details url, or "-"
# and R_ERR, a reason when the whole answer could not be read this round.
# An unreadable answer (401/403/404, not logged in) exits the script.
read_checks() {
  local c="$1" resp rows rc=0 name status concl url i
  R_STATE=() R_NOTE=() R_URL=() R_ERR=""
  for i in "${!REQUIRED_CHECKS[@]}"; do
    R_STATE[i]=pending R_NOTE[i]=unreadable R_URL[i]=-
  done
  resp="$("$GH" api --paginate "repos/$REPO/commits/$c/check-runs?per_page=100&filter=all" 2>&1)" || rc=$?
  if [ "$rc" -ne 0 ]; then
    if [ "$rc" -eq 4 ] || [[ "$resp" =~ HTTP\ 40[134] ]]; then
      echo "QUALITY GATE: FAIL ${SHA:0:12} — cannot read checks for ${c:0:12} in $REPO (${resp%%"$NL"*}). A permission or not-found error is never 'still pending': give the job checks:read and check the repo name."
      exit 1
    fi
    R_ERR="could not read checks this round (gh exit $rc: ${resp%%"$NL"*})"
    return 0
  fi
  if ! rows="$(printf '%s' "$resp" | jq -rs --argjson names "$NAMES_JSON" "$JQ_PICK" 2>/dev/null)"; then
    R_ERR="the checks answer was not readable JSON this round"
    return 0
  fi
  while IFS=$'\t' read -r name status concl url; do
    [ -n "$name" ] || continue
    for i in "${!REQUIRED_CHECKS[@]}"; do
      [ "${REQUIRED_CHECKS[i]}" = "$name" ] || continue
      R_URL[i]="$url"
      if [ "$status" = completed ] && [ "$concl" = success ]; then
        R_STATE[i]=success R_NOTE[i]=success
      elif [ "$status" = completed ]; then
        R_STATE[i]=fail R_NOTE[i]="$concl"
      else
        R_STATE[i]=pending R_NOTE[i]="$status"
      fi
    done
  done <<<"$rows"
}

retry_recipe() {
  local runs="$1" ids="" id
  while read -r id; do
    [ -n "$id" ] && ids+="${ids:+ }$id"
  done < <(grep -oE '/actions/runs/[0-9]+' <<<"$runs" | sed 's#.*/##' | sort -u)
  echo "  Failed test job(s):"
  printf '%s' "$runs"
  echo "  If it is a one-off flake: re-run only the failed jobs, wait for green, then re-dispatch the release for the SAME tag:"
  for id in $ids; do
    echo "    gh run rerun $id --failed --repo $REPO"
  done
  [ -n "$ids" ] || echo "    gh run rerun <run-id> --failed --repo $REPO   (run id not readable from the check-run)"
  echo "    gh workflow run release.yml --repo $REPO -f tag=$RECIPE_TAG"
  echo "  A real failure needs a fix and a new tag."
}

# --- poll ---------------------------------------------------------------------
REQ_LIST="$(IFS=,; echo "${REQUIRED_CHECKS[*]}")"
echo "quality-gate: ${SHA:0:12} in $REPO — required by name: ${REQ_LIST//,/, }; every one on the tagged commit itself${P2:+, except that the PR head ${P2:0:12} (^2, identical tree) may answer for $P2_MAY_ANSWER while the own run of it on the tagged commit is still going}"

max_sec=$((MAX_WAIT_MIN * 60))
waited=0
while :; do
  read_checks "$SHA"
  OWN_STATE=("${R_STATE[@]}") OWN_NOTE=("${R_NOTE[@]}") OWN_URL=("${R_URL[@]}") own_err="$R_ERR"

  # Rule 5: a required check that finished non-success on the tagged commit
  # fails the gate at once, whatever the PR head says.
  fails="" failed_runs=""
  for i in "${!REQUIRED_CHECKS[@]}"; do
    if [ "${OWN_STATE[i]}" = fail ]; then
      fails+="${fails:+, }${REQUIRED_CHECKS[i]} = ${OWN_NOTE[i]}"
      failed_runs+="    ${REQUIRED_CHECKS[i]}: ${OWN_URL[i]}"$'\n'
    fi
  done
  if [ -n "$fails" ]; then
    echo "QUALITY GATE: FAIL ${SHA:0:12} — required check failed on the tagged commit: $fails"
    retry_recipe "$failed_runs"
    exit 1
  fi

  # Who answered for each required check: "own", "p2", or "" (not yet).
  ANSWER=()
  p2_need=0
  for i in "${!REQUIRED_CHECKS[@]}"; do
    ANSWER[i]=""
    if [ "${OWN_STATE[i]}" = success ]; then
      ANSWER[i]=own
    elif [ -n "$P2" ] && [ "${REQUIRED_CHECKS[i]}" = "$P2_MAY_ANSWER" ]; then
      p2_need=1
    fi
  done
  p2_note=""
  if [ "$p2_need" -eq 1 ]; then
    read_checks "$P2"
    for i in "${!REQUIRED_CHECKS[@]}"; do
      [ "${REQUIRED_CHECKS[i]}" = "$P2_MAY_ANSWER" ] || continue
      if [ -z "${ANSWER[i]}" ] && [ "${R_STATE[i]}" = success ]; then
        ANSWER[i]=p2
      else
        p2_note="; PR head ${P2:0:12}: $P2_MAY_ANSWER ${R_NOTE[i]}${R_ERR:+ ($R_ERR)}"
      fi
    done
  fi

  ok=0 on_own="" on_p2="" pend=""
  for i in "${!REQUIRED_CHECKS[@]}"; do
    case "${ANSWER[i]}" in
      own) ok=$((ok + 1)); on_own+="${on_own:+, }${REQUIRED_CHECKS[i]}" ;;
      p2)  ok=$((ok + 1)); on_p2+="${on_p2:+, }${REQUIRED_CHECKS[i]}" ;;
      *)   pend+="${pend:+, }${REQUIRED_CHECKS[i]} (${OWN_NOTE[i]})" ;;
    esac
  done
  # The ONLY way to pass: every required check counted green, each by a
  # source allowed to answer for it. A check that is missing or still running
  # is unknown, and unknown is not clear.
  if [ "$ok" -eq "${#REQUIRED_CHECKS[@]}" ]; then
    if [ -z "$on_p2" ]; then
      echo "QUALITY GATE: PASS ${SHA:0:12} — all ${#REQUIRED_CHECKS[@]} required checks success on the tagged commit"
    else
      echo "QUALITY GATE: PASS ${SHA:0:12} — $on_own success on the tagged commit; $on_p2 success on the PR head ${P2:0:12} (^2, tree identical to the tagged commit)"
    fi
    exit 0
  fi
  own_detail="${own_err:-waiting on $pend}"

  if [ "$waited" -ge "$max_sec" ]; then
    echo "QUALITY GATE: FAIL ${SHA:0:12} — timed out after $((waited / 60)) min; tagged commit: $own_detail$p2_note"
    echo "  When the suite has finished green, re-dispatch the release for the SAME tag:"
    echo "    gh workflow run release.yml --repo $REPO -f tag=$RECIPE_TAG"
    exit 1
  fi
  echo "quality-gate: waiting ($((waited / 60)) of $MAX_WAIT_MIN min) — tagged commit: $own_detail$p2_note"
  "$SLEEP" "$POLL_SEC"
  waited=$((waited + POLL_SEC))
done
