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

DEST="$ROOT/$NAME"
REGISTRY="$ROOT/friends.json"
IS_TEST=0
[ "$ROOT" = "/home/hoang/vl-friends" ] || IS_TEST=1

if [ ! -d "$DEST" ]; then
  echo "remove: friend '$NAME' has no folder at $DEST — nothing to do" >&2
  exit 1
fi

# Stop and disable the unit (production only).
if [ "$IS_TEST" -eq 0 ]; then
  systemctl --user stop "vl-friend@$NAME.service" 2>/dev/null || true
  systemctl --user disable "vl-friend@$NAME.service" 2>/dev/null || true
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
  python3 - "$REGISTRY" "$NAME" <<'PY'
import json, sys
reg, name = sys.argv[1:3]
entries = json.load(open(reg))
changed = False
for e in entries:
    if e.get("name") == name:
        e["state"] = "revoked"
        changed = True
if changed:
    json.dump(entries, open(reg, "w"), indent=2)
PY
fi

echo "remove: friend '$NAME' moved to $TARGET"
