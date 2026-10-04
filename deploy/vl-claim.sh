#!/usr/bin/env bash
# vl-claim — PART 3 step 0's claim commit, made checkable.
#
# WHY THIS EXISTS. Step 0 (push-empty-at-accept, born class 70) stops two lanes
# building the same wave. On 2026-09-04 it worked exactly as designed: it caught
# a genuine collision on fix/reaper-reads-snapshot within 70 seconds. And it
# still failed at the only thing that mattered next — the claim read
#
#   claim: reaper reads the snapshot, not order_update silence (PART 3 step 0)
#
# which proves a collision and cannot resolve it. No session name, so the lane
# that found it could prove someone else held the wave and had no idea whom to
# ask. In this repo every commit carries the identical git author, so the author
# field answers nothing (PROVENANCE, CLAUDE.md). The claim message is the ONLY
# place a reachable identity can live.
#
# The step-0 template already showed <session>. A template is a suggestion; this
# is the check that makes it a rule.
#
#   vl-claim new <branch> "<wave>"        # create + push a well-formed claim
#   vl-claim check [branch]               # validate one branch's claim  (rc 1 = bad)
#   vl-claim audit                        # every origin claim commit    (rc 1 = any bad)
#
# VL_SESSION names the lane; it is required for `new`.
set -uo pipefail

# The contract. All three parts are mandatory:
#   claim: <wave> — <wave>-<uuid-prefix>/<ListAgents-name>[<ref>], <ISO-8601 with offset>
#
# THE SESSION FIELD MUST BE ROUTABLE, NOT MERELY ATTRIBUTABLE (owner ruling
# 2026-09-07, AUDIT-CHECKLIST PART 3 step 0). The 2026-09-04 rule fixed
# attribution — a claim names WHO. It did not fix ADDRESSING: the uuid prefix
# lives in the claim/lock namespace, ListAgents addresses sessions by a short ref
# in another, and nothing joins them. On 2026-09-07 a lane hit a collision on
# fix/session-calendar, read the holder's name, and could not send it a message:
# ListAgents offered four sessions and none matched. One relay went to ALL FOUR
# because there was no way to send it to one.
#
# The ref may be the literal [unlisted] when a lane cannot read its own — never
# omitted, never guessed (A24).
CLAIM_RE='^claim: .+ — .+-[0-9a-f]{6,40}/[^,]+\[[A-Za-z0-9_-]+\], [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([+-][0-9]{2}:?[0-9]{2}|Z)'

die() { echo "$*" >&2; exit 1; }

claim_msg_of() { # first-parent commit on <branch> that is not on dev
  local br="$1"
  git log --format='%s' "origin/dev..$br" 2>/dev/null | tail -1
}

cmd_new() {
  local br="${1:-}" wave="${2:-}"
  [ -n "$br" ] && [ -n "$wave" ] || die "usage: vl-claim new <branch> \"<wave>\""
  local sess="${VL_SESSION:-}"
  [ -n "$sess" ] || die "REFUSED — VL_SESSION is unset. A claim without a reachable identity proves a collision and cannot resolve it (2026-09-04)."
  printf '%s' "$sess" | grep -qE -- '-[0-9a-f]{6,40}/[^,]+\[[A-Za-z0-9_-]+\]$' || die "REFUSED — VL_SESSION must be routable: <wave>-<uuid-prefix>/<ListAgents-name>[<ref>], e.g. claimid-ee7f9468/vl-db[ca9c60]. Use [unlisted] if you cannot read your own ref (owner ruling 2026-09-07). Got: $sess"
  git ls-remote --heads origin "$br" | grep -q . && die "REFUSED — $br already exists on origin: ANOTHER LANE HAS THIS WAVE. Stop and coordinate."
  git checkout -q -b "$br" origin/dev || die "cannot branch from origin/dev"
  git commit -q --allow-empty -m "claim: $wave — $sess, $(date -Is)"
  git push -q -u origin "$br" || die "push failed"
  echo "CLAIMED $br by $sess"
}

cmd_check() {
  local br="${1:-HEAD}" msg rc=0
  msg="$(claim_msg_of "$br")"
  if [ -z "$msg" ]; then
    echo "no claim commit found on $br (nothing ahead of origin/dev)"; return 1
  fi
  if printf '%s' "$msg" | grep -qE "$CLAIM_RE"; then
    echo "OK   $br"
    echo "     $msg"
  else
    rc=1
    echo "BAD  $br"
    echo "     $msg"
    printf '     ^ missing a session and/or an ISO timestamp.\n'
    printf '       required: claim: <wave> — <session>, <ISO-8601>\n'
    printf '       a claim without a reachable identity proves a collision and cannot resolve it.\n'
  fi
  return $rc
}

cmd_audit() {
  local rc=0 n=0 bad=0
  while read -r _ ref; do
    local br="${ref#refs/heads/}"
    case "$br" in dev|main|stable|release/*) continue ;; esac
    local msg; msg="$(claim_msg_of "origin/$br")"
    printf '%s' "$msg" | grep -q '^claim:' || continue   # only branches that DID claim
    n=$((n+1))
    if ! printf '%s' "$msg" | grep -qE "$CLAIM_RE"; then
      bad=$((bad+1)); rc=1
      echo "BAD  $br"
      echo "     $msg"
    fi
  done < <(git ls-remote --heads origin)
  echo "audited $n claim commit(s); $bad malformed"
  return $rc
}

case "${1:-}" in
  new)   shift; cmd_new "$@" ;;
  check) shift; cmd_check "$@" ;;
  audit) shift; cmd_audit "$@" ;;
  *) die "usage: vl-claim {new <branch> \"<wave>\"|check [branch]|audit}" ;;
esac
