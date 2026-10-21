#!/usr/bin/env bash
# remove.sh — take down one friend bot (W-FRIEND-HOSTED, segment 1).
#
# It stops the unit, verifies it is actually down, then MOVES the friend folder
# to ~/trash/<date>/ (THE ONE RULE: never delete), and marks the registry entry
# state=revoked. The connect key dies with the moved .env.
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

# R6: the bot runs as a system unit with no XDG_RUNTIME_DIR; `systemctl --user`
# fails with "No medium found". Set it explicitly (provision.sh does the same).
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=/run/user/$(id -u)/bus}"

DEST="$ROOT/$NAME"
REGISTRY="$ROOT/friends.json"

# Defense in depth: the folder must RESOLVE inside $ROOT (a symlinked name or a
# traversal must never point the move at a path outside the friend root).
REAL_ROOT="$(realpath -m -- "$ROOT")"
REAL_DEST="$(realpath -m -- "$DEST")"
case "$REAL_DEST" in
  "$REAL_ROOT"/*) : ;;
  *) echo "remove: '$NAME' resolves outside $ROOT - refusing" >&2; exit 1 ;;
esac
[ "$REAL_DEST" != "$REAL_ROOT" ] || { echo "remove: refusing to move the friend root itself" >&2; exit 1; }

IS_TEST=0
[ "$ROOT" = "/home/hoang/vl-friends" ] || IS_TEST=1

# mark_revoked: under the friends.json flock (shared with the Go writer), set the
# row's state=revoked atomically (temp file + os.replace, never a bare truncate).
# Prints "revoked" if the state changed, else "no-change".
mark_revoked() {
  python3 - "$REGISTRY" "$NAME" <<'PY'
import fcntl, json, os, sys, tempfile
reg, name = sys.argv[1:3]
lock_path = reg + ".lock"
with open(lock_path, "w") as lf:
    fcntl.flock(lf, fcntl.LOCK_EX)
    entries = json.load(open(reg))
    changed = False
    for e in entries:
        if e.get("name") == name and e.get("state") != "revoked":
            e["state"] = "revoked"
            changed = True
    if changed:
        fd, tmp = tempfile.mkstemp(dir=os.path.dirname(reg) or ".", prefix=".friends-", suffix=".json.tmp")
        with os.fdopen(fd, "w") as f:
            json.dump(entries, f, indent=2)
        os.replace(tmp, reg)
    print("revoked" if changed else "no-change")
PY
}

# R12: the folder is already gone (a previous run moved it). If the registry row
# is still active, mark it revoked — never leave a stuck active row behind.
if [ ! -d "$DEST" ]; then
  if [ -f "$REGISTRY" ]; then
    RESULT="$(mark_revoked)"
    if [ "$RESULT" = "revoked" ]; then
      echo "remove: friend '$NAME' had no folder but its active row was marked revoked" >&2
      exit 0
    fi
  fi
  echo "remove: friend '$NAME' has no folder at $DEST - nothing to do" >&2
  exit 1
fi

# Stop and disable the unit (production; tests set REMOVE_CHECK_ACTIVE=1 and stub
# systemctl). R5: `stop` itself must succeed, and after it the unit MUST read
# inactive|failed (captured string); active or an empty/unknown status refuses.
if [ "$IS_TEST" -eq 0 ] || [ -n "${REMOVE_CHECK_ACTIVE:-}" ]; then
  if ! systemctl --user stop "vl-friend@$NAME.service" >/dev/null 2>&1; then
    echo "remove: failed to stop '$NAME' - refusing" >&2
    exit 1
  fi
  systemctl --user disable "vl-friend@$NAME.service" >/dev/null 2>&1 || true
  STATE="$(systemctl --user is-active "vl-friend@$NAME.service" 2>/dev/null || true)"
  case "$STATE" in
    inactive|failed) : ;;
    active) echo "remove: friend '$NAME' is still active after stop - refusing to revoke a running bot" >&2; exit 1 ;;
    *) echo "remove: cannot confirm '$NAME' is down (is-active='${STATE:-<unknown>}') - refusing" >&2; exit 1 ;;
  esac
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
if [ -f "$REGISTRY" ]; then
  mark_revoked >/dev/null
fi

echo "remove: friend '$NAME' moved to $TARGET"
