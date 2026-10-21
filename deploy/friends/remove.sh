#!/usr/bin/env bash
# remove.sh — take down one friend bot (W-FRIEND-HOSTED, segment 1).
#
# It stops the unit, then MOVES the friend folder to ~/trash/<date>/ (THE ONE
# RULE: never delete), and marks the registry entry state=revoked. The connect
# key dies with the moved .env.
#
# Usage:
#   remove.sh <name> [--root DIR]
set -euo pipefail

NAME=""
ROOT="/home/hoang/vl-friends"
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="${2:?remove: --root needs a DIR}"; shift 2 ;;
    -*) echo "remove: unknown arg $1" >&2; exit 2 ;;
    *) NAME="$1"; shift ;;
  esac
done
[ -n "$NAME" ] || { echo "usage: remove.sh <name> [--root DIR]" >&2; exit 2; }

# B2: name is lowercase a-z0-9, 2-20 chars — validated BEFORE any path is built
# (a traversal name like ".." must never reach `mv`).
if [[ ! "$NAME" =~ ^[a-z0-9]{2,20}$ ]]; then
  echo "remove: invalid friend name '$NAME' (must be 2-20 lowercase a-z0-9)" >&2
  exit 2
fi

DEST="$ROOT/$NAME"
REGISTRY="$ROOT/friends.json"

# Defense in depth: the folder must RESOLVE inside $ROOT (a symlinked name or a
# traversal must never point the move at a path outside the friend root).
REAL_ROOT="$(realpath -m -- "$ROOT")"
REAL_DEST="$(realpath -m -- "$DEST")"
case "$REAL_DEST" in
  "$REAL_ROOT"/*) : ;;
  *) echo "remove: '$NAME' resolves outside $ROOT — refusing" >&2; exit 1 ;;
esac
[ "$REAL_DEST" != "$REAL_ROOT" ] || { echo "remove: refusing to move the friend root itself" >&2; exit 1; }

IS_TEST=0
[ "$ROOT" = "/home/hoang/vl-friends" ] || IS_TEST=1

if [ ! -d "$DEST" ]; then
  echo "remove: friend '$NAME' has no folder at $DEST — nothing to do" >&2
  exit 1
fi

# Stop and disable the unit (production; tests set REMOVE_CHECK_ACTIVE=1 and
# stub systemctl). U9: after stop, the unit MUST be inactive before anything is
# moved — never report a still-running bot as revoked.
if [ "$IS_TEST" -eq 0 ] || [ -n "${REMOVE_CHECK_ACTIVE:-}" ]; then
  systemctl --user stop "vl-friend@$NAME.service" 2>/dev/null || true
  systemctl --user disable "vl-friend@$NAME.service" 2>/dev/null || true
  if systemctl --user is-active "vl-friend@$NAME.service" >/dev/null 2>&1; then
    echo "remove: friend '$NAME' is still active after stop — refusing to revoke a running bot" >&2
    exit 1
  fi
fi

# MOVE to trash — never delete (THE ONE RULE). Collision-safe name.
TRASH="${TRASH_DIR:-$HOME/trash/$(date +%F)}"
mkdir -p "$TRASH"
TARGET="$TRASH/friend-$NAME"
if [ -e "$TARGET" ]; then
  TARGET="$TRASH/friend-$NAME-$(date +%H%M%S)"
fi
mv "$DEST" "$TARGET"

# Mark the registry entry revoked (the folder is gone; the key died with it).
# Atomic temp+rename + flock shared with the Go writer (U17).
if [ -f "$REGISTRY" ]; then
  python3 - "$REGISTRY" "$NAME" <<'PY'
import fcntl, json, os, sys, tempfile
reg, name = sys.argv[1:3]
lock_path = reg + ".lock"
with open(lock_path, "w") as lf:
    fcntl.flock(lf, fcntl.LOCK_EX)
    entries = json.load(open(reg))
    changed = False
    for e in entries:
        if e.get("name") == name:
            e["state"] = "revoked"
            changed = True
    if changed:
        fd, tmp = tempfile.mkstemp(dir=os.path.dirname(reg) or ".", prefix=".friends-", suffix=".json.tmp")
        with os.fdopen(fd, "w") as f:
            json.dump(entries, f, indent=2)
        os.replace(tmp, reg)
PY
fi

echo "remove: friend '$NAME' moved to $TARGET"
