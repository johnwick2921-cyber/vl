#!/usr/bin/env bash
# VL auto-start installer (WSL2/Linux with systemd). Run ONCE from YOUR clone:
#   sudo bash deploy/install-autostart.sh
#
# Universal by design — NOTHING machine-specific lives in the committed unit
# templates. Everything is detected at install time:
#   - repo dir     = this script's own location (any clone path works)
#   - target user  = the user who invoked sudo (SUDO_USER)
#   - node/npm     = ABSOLUTE paths from that user's own shell, nvm-aware
#                    (systemd services never read ~/.bashrc, so an nvm node
#                    is invisible to services unless its path is baked in)
#
# Idempotent — safe to re-run any time (e.g. after `git pull` brings new unit
# templates); it re-renders and reinstalls over whatever is there, including
# older broken installs.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: must run as root:  sudo bash deploy/install-autostart.sh" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
VL_DIR="$(cd "$SCRIPT_DIR/.." && pwd -P)"

TARGET_USER="${SUDO_USER:-}"
if [ -z "$TARGET_USER" ] || [ "$TARGET_USER" = "root" ]; then
  TARGET_USER="$(stat -c '%U' "$VL_DIR")"   # fallback: owner of the checkout
fi
if [ -z "$TARGET_USER" ] || [ "$TARGET_USER" = "root" ]; then
  echo "ERROR: could not determine the target user — run via sudo from your normal account." >&2
  exit 1
fi
TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"

if [ ! -x "$VL_DIR/vl-bin" ]; then
  echo "⚠ WARNING: $VL_DIR/vl-bin not found or not executable."
  echo "  Build it first:  cd $VL_DIR && go build -o vl-bin ."
  echo "  Installing anyway — the backend unit will retry every 5s until the binary exists."
fi

# --- node/npm detection (nvm-aware) ------------------------------------------
# 1. login shell (-l): loads ~/.profile, which loads nvm on most setups
# 2. interactive shell (-i): covers nvm installed only in ~/.bashrc
# 3. newest node under ~/.nvm directly
# 4. system PATH as last resort
detect_node() {
  local n
  n="$(sudo -u "$TARGET_USER" bash -lc 'command -v node' 2>/dev/null | tail -1)" || true
  if [ -z "${n:-}" ]; then
    n="$(sudo -u "$TARGET_USER" bash -ic 'command -v node' 2>/dev/null | tail -1)" || true
  fi
  if [ -z "${n:-}" ] && [ -n "$TARGET_HOME" ]; then
    n="$(ls -1d "$TARGET_HOME"/.nvm/versions/node/*/bin/node 2>/dev/null | sort -V | tail -1)" || true
  fi
  if [ -z "${n:-}" ]; then
    n="$(command -v node 2>/dev/null)" || true
  fi
  echo "${n:-}"
}
NODE_BIN="$(detect_node)"
if [ -z "$NODE_BIN" ]; then
  echo "ERROR: node not found for user $TARGET_USER. Install Node.js (or set an nvm default alias) and re-run." >&2
  exit 1
fi
NODE_DIR="$(dirname "$NODE_BIN")"
if [ ! -x "$NODE_DIR/npm" ]; then
  echo "ERROR: npm not found next to node ($NODE_DIR) — broken Node install?" >&2
  exit 1
fi

echo "→ detected: user=$TARGET_USER  repo=$VL_DIR  node=$NODE_DIR"

# --- .env guard ---------------------------------------------------------------
# Services read env ONLY via .env (godotenv) — never ~/.bashrc. A missing
# NT_TRANSPORT silently falls back to the deprecated CSV bridge.
if [ -f "$VL_DIR/.env" ]; then
  if ! grep -q '^NT_TRANSPORT=' "$VL_DIR/.env"; then
    printf '\n# Added by deploy/install-autostart.sh — services never read ~/.bashrc\nNT_TRANSPORT=tcp\n' >> "$NOFX_DIR/.env"
    echo "⚠ NOTICE: NT_TRANSPORT was missing from .env — appended NT_TRANSPORT=tcp."
    echo "  (Without it the bot silently uses the deprecated CSV path instead of the TCP bridge.)"
  fi
else
  echo "⚠ WARNING: $VL_DIR/.env not found. Copy .env.example to .env and configure it,"
  echo "  or the service will run with defaults (including an INSECURE default JWT secret)."
fi

# --- stop previous instances ----------------------------------------------------
echo "→ stopping any previous instances (units, nohup launches, stray vite)..."
systemctl stop vl vl-web 2>/dev/null || true
pkill -x vl-bin 2>/dev/null || true
pkill -f "node $VL_DIR/web/node_modules/.bin/vite" 2>/dev/null || true
sleep 2

# --- render templates + install -------------------------------------------------
echo "→ rendering unit templates and installing to /etc/systemd/system..."
for u in vl.service vl-web.service; do
  sed -e "s|__VL_USER__|$TARGET_USER|g" \
      -e "s|__VL_DIR__|$VL_DIR|g" \
      -e "s|__NODE_DIR__|$NODE_DIR|g" \
      "$SCRIPT_DIR/$u" > "/etc/systemd/system/$u"
done
systemctl daemon-reload
systemctl reset-failed vl vl-web 2>/dev/null || true
systemctl enable --now vl vl-web

sleep 4
echo
echo "→ status:"
systemctl --no-pager --lines=0 status vl 2>/dev/null | sed -n '1,4p' || true
systemctl --no-pager --lines=0 status vl-web 2>/dev/null | sed -n '1,4p' || true
echo
echo "→ listeners (8080=API, 3000=UI; 36974 binds once a ninjatrader trader loads):"
ss -tlnp | grep -E ':(8080|3000|36974)' || echo "  (none yet — check journalctl -u vl -u vl-web)"
echo
echo "→ logs are in the JOURNAL (unit-level file logging 209s on this WSL2 systemd):"
echo "    journalctl -u vl -f        # backend"
echo "    journalctl -u vl-web -f    # frontend"
echo
echo "✓ done. Crash-restart proof:"
echo "    sudo kill -9 \$(pgrep -x vl-bin); sleep 6; pgrep -x vl-bin && echo RESTARTED"
echo "  Windows-side steps (Task Scheduler / NT8): see docs/AUTOSTART.md"
