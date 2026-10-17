#!/usr/bin/env bash
# W-ONE-BUTTON M4 — stage ONLY the allow-list, then prove what was staged.
#
# ALLOW-LIST, not deny-list, for what goes IN: a deny-list decides what to
# leave out and therefore ships anything nobody thought of. The secret scan is
# the deny-list, and it runs as a second, independent opinion over the result.
#
# What a release owns (design notes R-e/R-f): the binary, the built web assets,
# the RELEASE marker, the AddOn sources and their protocol doc, the updater
# binaries once they exist, and the licence. Nothing else. In particular the
# installation keeps its own data/ and .env — they are NEVER shipped and never
# overwritten (R-d).
set -uo pipefail
SRC="${1:-}"; OUT="${2:-}"
[ -d "${SRC:-}" ] && [ -n "${OUT:-}" ] || { echo "package: usage: package.sh <repo-root> <stage-dir>" >&2; exit 2; }
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The package holds EXACTLY ONE binary: vl-bin when the repo has it, else
# nofx-bin; BOTH present is refused. R5 removes the nofx branch.
BIN_NAME="vl-bin"; [ -f "$SRC/$BIN_NAME" ] || BIN_NAME="nofx-bin"
if [ -f "$SRC/vl-bin" ] && [ -f "$SRC/nofx-bin" ]; then
  echo "package: the repo holds BOTH vl-bin and nofx-bin — refusing (a package must hold exactly one)" >&2
  exit 2
fi
ALLOW=(
  "$BIN_NAME"
  "web/dist"
  "ninjascript/vltrader_tcp_PROTOCOL.md"
  "LICENSE"
)
# deploy/RELEASE is NOT copied. The checked-in file is the marker the BOOT
# PROCEDURE wrote for the previous boot, so copying it would ship a stale sha
# that disagrees with the binary in the same archive. It is WRITTEN here from
# the source sha, and the manifest pins the two to be byte-equal.
OPTIONAL=( "updater/nofx-updater" "updater/nofx-updater-bootstrap" "updater/vl-updater" "updater/vl-updater-bootstrap" "calendar_static_t1.json" ) # R5 removes the nofx names

mkdir -p "$OUT"
staged=()
copy_one() {
  local rel="$1" required="$2"
  if [ ! -e "$SRC/$rel" ]; then
    if [ "$required" = "required" ]; then echo "package: REFUSED — required artifact missing: $rel"; return 1; fi
    return 0
  fi
  bash "$HERE/check-archive-paths.sh" "$rel" >/dev/null || { echo "package: REFUSED — unsafe path: $rel"; return 1; }
  mkdir -p "$OUT/$(dirname "$rel")"
  cp -a "$SRC/$rel" "$OUT/$rel"
  staged+=("$rel")
}

rc=0
for rel in "${ALLOW[@]}"; do copy_one "$rel" required || rc=1; done

SRC_SHA="${3:-}"
if [ -n "$SRC_SHA" ]; then
  case "$SRC_SHA" in *[!0-9a-f]*|"") echo "package: REFUSED — source sha must be 40 hex"; rc=1 ;; esac
  [ "${#SRC_SHA}" -eq 40 ] || { echo "package: REFUSED — source sha must be 40 hex (got ${#SRC_SHA})"; rc=1; }
  if [ "$rc" -eq 0 ]; then
    mkdir -p "$OUT/deploy"
    printf '%s\n' "$SRC_SHA" > "$OUT/deploy/RELEASE"
    staged+=("deploy/RELEASE")
  fi
else
  echo "package: REFUSED — the source sha is required; deploy/RELEASE is written, never copied"
  rc=1
fi
# the AddOn sources, by glob, each path checked
if compgen -G "$SRC/ninjascript/*.cs" >/dev/null; then
  for f in "$SRC"/ninjascript/*.cs; do copy_one "ninjascript/$(basename "$f")" required || rc=1; done
else
  echo "package: REFUSED — no ninjascript/*.cs found"; rc=1
fi
for rel in "${OPTIONAL[@]}"; do copy_one "$rel" optional || rc=1; done
[ "$rc" -eq 0 ] || { echo "package: REFUSED"; exit 1; }

# Every staged path re-checked as a set, so a glob cannot smuggle one through.
mapfile -t all < <(cd "$OUT" && find . -type f -printf '%P\n')
bash "$HERE/check-archive-paths.sh" "${all[@]}" >/dev/null || { echo "package: REFUSED — unsafe staged path"; exit 1; }

# A symlink in the stage would resolve on the OWNER's machine, not ours.
if find "$OUT" -type l | grep -q .; then
  echo "package: REFUSED — symlink in the staged tree:"; find "$OUT" -type l; exit 1
fi
echo "package: staged ${#all[@]} file(s) from ${#staged[@]} allow-list entr(ies)"
