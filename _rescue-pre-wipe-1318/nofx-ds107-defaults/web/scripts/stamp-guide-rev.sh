#!/usr/bin/env bash
# Stamp GUIDE_BUILT_REV from a RUNNING binary's vcs.revision — never hand-typed.
#
# The Guide's drift banner compares this constant against what /api/health
# reports. That report is kernel.RunningRevision(), i.e. shortRev() — 12 chars.
# A hand-typed 40-char sha can therefore never match, even for the same commit,
# which is exactly how the banner ended up permanently on.
#
#   usage: web/scripts/stamp-guide-rev.sh [health-url]
#
# Run it BEFORE `npm run build`, against the binary that will serve the dist.
set -euo pipefail

URL="${1:-http://127.0.0.1:8080/api/health}"
TYPES="$(cd "$(dirname "$0")/.." && pwd)/src/guide/types.ts"

rev="$(curl -s --max-time 5 "$URL" | sed -n 's/.*"revision"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p')"

if [ -z "$rev" ]; then
  echo "stamp-guide-rev: no revision from $URL — is the bot up?" >&2
  echo "  REFUSING to stamp: a guessed rev is worse than a stale one." >&2
  exit 1
fi

# "" means the boot assertion has not run yet. Not knowing is not a revision.
if [ "${#rev}" -lt 7 ]; then
  echo "stamp-guide-rev: revision %s is too short to be a commit (${rev})" >&2
  exit 1
fi

current="$(sed -n "s/^export const GUIDE_BUILT_REV = '\(.*\)'$/\1/p" "$TYPES")"
sed -i "s|^export const GUIDE_BUILT_REV = '.*'$|export const GUIDE_BUILT_REV = '${rev}'|" "$TYPES"

echo "stamp-guide-rev: ${current:-<none>} → ${rev}  (read from ${URL})"
