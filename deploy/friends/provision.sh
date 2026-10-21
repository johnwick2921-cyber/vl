#!/usr/bin/env bash
# provision.sh — create a ready, empty VL bot for one friend (W-FRIEND-HOSTED,
# segment 1). The friend's bot is the SAME program as the owner's, running from
# its own folder so it reads its own .env and database — never the owner's.
#
# Usage:
#   provision.sh <name> [--root DIR]
#     name      friend name; lowercase a-z0-9, 2-20 chars (B2 contract)
#     --root    build under DIR instead of /home/hoang/vl-friends (tests);
#               with --root no systemd unit is installed or started.
#
# Environment:
#   TUNNEL_IP   the box's Tailscale (tunnel) address the friend binds to.
#               Default 127.0.0.1 (local only) with a warning.
#   TS_AUTHKEY  optional Tailscale auth key folded into the one-time install code
#               (the Friends page supplies it; empty here).
#
# Output: the one-time install code, on stdout ONLY (never stored in the clear).
# The connect key lives only in the friend's .env (0600); friends.json stores
# only its SHA-256 (B2).
set -euo pipefail

NAME=""
ROOT="/home/hoang/vl-friends"
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="${2:?provision: --root needs a DIR}"; shift 2 ;;
    -*) echo "provision: unknown arg $1" >&2; exit 2 ;;
    *) NAME="$1"; shift ;;
  esac
done
[ -n "$NAME" ] || { echo "usage: provision.sh <name> [--root DIR]" >&2; exit 2; }

# B2: name is lowercase a-z0-9, 2-20 chars.
if [[ ! "$NAME" =~ ^[a-z0-9]{2,20}$ ]]; then
  echo "provision: invalid friend name '$NAME' (must be 2-20 lowercase a-z0-9)" >&2
  exit 2
fi

TUNNEL_IP="${TUNNEL_IP:-127.0.0.1}"
IS_TEST=0
[ "$ROOT" = "/home/hoang/vl-friends" ] || IS_TEST=1
if [ "$TUNNEL_IP" = "127.0.0.1" ] && [ "$IS_TEST" -eq 0 ]; then
  echo "provision: WARNING TUNNEL_IP not set — friend bot binds 127.0.0.1 (local only)" >&2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REGISTRY="$ROOT/friends.json"
DEST="$ROOT/$NAME"

# R6: the bot runs as a system unit with no XDG_RUNTIME_DIR, so `systemctl --user`
# fails with "No medium found"; set it explicitly (remove.sh does the same).
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=/run/user/$(id -u)/bus}"
# R11: umask 077 from the very start, so $ROOT and friends.json are 0700/0600
# from the moment they are created (not only after the friend folder).
umask 077

mkdir -p "$ROOT"
[ -f "$REGISTRY" ] || printf '[]\n' > "$REGISTRY"

# Defense in depth: the folder must RESOLVE inside $ROOT (a symlinked name or a
# traversal must never point the mkdir at a path outside the friend root).
REAL_ROOT="$(realpath -m -- "$ROOT")"
REAL_DEST="$(realpath -m -- "$DEST")"
case "$REAL_DEST" in
  "$REAL_ROOT"/*) : ;;
  *) echo "provision: '$NAME' resolves outside $ROOT - refusing" >&2; exit 1 ;;
esac
[ "$REAL_DEST" != "$REAL_ROOT" ] || { echo "provision: refusing to create the friend root itself" >&2; exit 1; }

# Refuse a second provision of the same name (folder exists).
if [ -e "$DEST" ]; then
  echo "provision: '$DEST' already exists - refusing (remove.sh first)" >&2
  exit 1
fi

# Secrets: a fresh random login secret and connect key per friend.
JWT_SECRET="$(openssl rand -base64 64 | tr -d '\n')"
CONNECT_KEY="$(openssl rand -hex 32)"
# Encryption keys, same generator/format as the owner's start.sh. Both are
# demanded before store init (crypto/crypto.go:77-94): DATA_ENCRYPTION_KEY is
# AES-256 base64; RSA_PRIVATE_KEY is PEM on ONE line with literal \n.
DATA_ENCRYPTION_KEY="$(openssl rand -base64 32 | tr -d '\n')"
RSA_PRIVATE_KEY="$(openssl genrsa 2048 2>/dev/null | awk '{printf "%s\\n", $0}')"
KEY_SHA="$(printf '%s' "$CONNECT_KEY" | sha256sum | awk '{print $1}')"

# R11: the registry read (duplicate-name check + next-free-N), the port-in-use
# check, the folder creation and the append are ONE critical section under a
# single flock (shared with the Go writer). A corrupt registry ABORTS - it is
# never silently replaced with a one-row list.
exec 9>>"$REGISTRY.lock"
flock -x 9
N="$(python3 - "$REGISTRY" "$NAME" <<'PY'
import json, os, sys
reg, name = sys.argv[1:3]
entries = []
if os.path.exists(reg) and os.path.getsize(reg) > 0:
    try:
        entries = json.load(open(reg))
    except Exception as e:
        print("provision: %s is corrupt - refusing to overwrite it: %s" % (reg, e), file=sys.stderr)
        sys.exit(1)
for e in entries:
    if e.get("name") == name:
        print("provision: friend '%s' is already in the registry - refusing" % name, file=sys.stderr)
        sys.exit(1)
used = {int(e.get("web_port")) for e in entries if isinstance(e.get("web_port"), int)}
n = 1
while (18080 + n) in used:
    n += 1
print(n)
PY
)"
WEB_PORT=$((18080 + N))
NT_PORT=$((36980 + N))

# Refuse if either port is already listening (any mode - the check reads ss via
# PATH so a test can stub it).
for p in "$WEB_PORT" "$NT_PORT"; do
  if ss -ltn 2>/dev/null | grep -qE ":$p[[:space:]]"; then
    echo "provision: port $p is already in use - refusing" >&2
    exit 1
  fi
done

# The folder (B2): .env + data/ + symlinks to the owner's web/dist and deploy/RELEASE
# so the friend always serves the same build as the owner.
mkdir -p "$DEST/data" "$DEST/web" "$DEST/deploy"
ln -sfn /home/hoang/vl/web/dist "$DEST/web/dist"
ln -sfn /home/hoang/vl/deploy/RELEASE "$DEST/deploy/RELEASE"

# The .env (0600): the friend's own secret, ports bound to the tunnel IP only.
cat > "$DEST/.env" <<EOF
JWT_SECRET=$JWT_SECRET
NT_CONNECT_KEY=$CONNECT_KEY
DATA_ENCRYPTION_KEY=$DATA_ENCRYPTION_KEY
RSA_PRIVATE_KEY="$RSA_PRIVATE_KEY"
API_SERVER_PORT=$WEB_PORT
API_SERVER_HOST=$TUNNEL_IP
NT_TCP_LISTEN_ADDR=$TUNNEL_IP:$NT_PORT
VL_FRIEND_INSTANCE=1
VL_FRIEND_NAME=$NAME
EOF
chmod 0600 "$DEST/.env"

# Registry: SHA-256 of the key only, never the key itself. Still under the same
# flock; the bytes land via a temp file + os.replace, never a bare truncate (U17).
python3 - "$REGISTRY" "$NAME" "$WEB_PORT" "$NT_PORT" "$KEY_SHA" <<'PY'
import datetime, json, os, sys, tempfile
reg, name, web, nt, sha = sys.argv[1:6]
entries = []
if os.path.exists(reg) and os.path.getsize(reg) > 0:
    try:
        entries = json.load(open(reg))
    except Exception as e:
        print("provision: %s is corrupt - refusing: %s" % (reg, e), file=sys.stderr)
        sys.exit(1)
entries.append({
    "name": name,
    "web_port": int(web),
    "nt_port": int(nt),
    "key_sha256": sha,
    "state": "active",
    "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "tailscale_device_id": "",
})
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(reg) or ".", prefix=".friends-", suffix=".json.tmp")
with os.fdopen(fd, "w") as f:
    json.dump(entries, f, indent=2)
os.replace(tmp, reg)
PY
flock -u 9
exec 9>&-

# One-time install code (B2), stdout only. The clear key is never written to disk
# outside the friend's 0600 .env. CONNECT_KEY / TS_AUTHKEY are handed to the
# python3 helper via the ENVIRONMENT, never argv (argv is visible in `ps` and
# /proc/<pid>/cmdline to any local user, S10b).
TS_AUTHKEY="${TS_AUTHKEY:-}"
CONNECT_KEY="$CONNECT_KEY" TS_AUTHKEY="$TS_AUTHKEY" python3 - "$NAME" "$TUNNEL_IP" "$NT_PORT" "$WEB_PORT" <<'PY'
import base64, json, os, sys
name, host, nt, web = sys.argv[1:5]
key = os.environ["CONNECT_KEY"]
tsk = os.environ.get("TS_AUTHKEY", "")
obj = {"v": 1, "name": name, "host": host, "nt_port": int(nt),
       "web_port": int(web), "connect_key": key, "ts_authkey": tsk}
raw = json.dumps(obj, separators=(",", ":")).encode()
print(base64.urlsafe_b64encode(raw).decode().rstrip("="))
PY

# Production: install + enable + start the user unit.
if [ "$IS_TEST" -eq 0 ]; then
  mkdir -p ~/.config/systemd/user
  ln -sfn "$SCRIPT_DIR/vl-friend@.service" ~/.config/systemd/user/vl-friend@.service
  systemctl --user daemon-reload
  systemctl --user enable "vl-friend@$NAME.service" >/dev/null
  systemctl --user start "vl-friend@$NAME.service"
fi
