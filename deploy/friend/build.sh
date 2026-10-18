#!/usr/bin/env bash
# deploy/friend/build.sh — build the friend app exe + the obfuscated add-on DLL,
# sign the manifest with the friend-distribution key, and drop everything into
# /home/hoang/vl-friends/_dist/<build>/ (B10.3). Old builds persist; only the
# LATEST pointer file is rewritten (no symlink swap, no deletion).
#
# Usage:
#   BUILD=37 APP_VERSION=1.1.0 bash deploy/friend/build.sh
#   (BUILD defaults to a numeric timestamp; APP_VERSION defaults to 1.0.0)
#
# Requires on the box: go, powershell.exe (Windows), NinjaTrader 8, Obfuscar.
set -euo pipefail

BUILD="${BUILD:-$(date +%Y%m%d%H%M%S)}"
APP_VERSION="${APP_VERSION:-1.0.0}"
ROOT="${VL_FRIENDS_ROOT:-/home/hoang/vl-friends}"
DIST_ROOT="$ROOT/_dist"
OUT="$DIST_ROOT/$BUILD"

KEY=/home/hoang/.vl-friend-dist/key
if [ ! -f "$KEY" ]; then
  echo "missing friend-distribution key: $KEY (B10.2 — created once by DS-108)" >&2
  exit 1
fi

# The shared dist dir is OUTSIDE this worktree; only create it (never remove).
mkdir -p "$OUT"

# 1. Cross-compile the friend app (trimmed + stripped; the version is stamped in).
GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X main.appVersion=$APP_VERSION" \
  -o "$OUT/vl-friend.exe" ./cmd/vl-friend

# 2. Obfuscated add-on DLL on Windows (NT8's compiler + Obfuscar), into a fresh
#    temp dir. The PowerShell script prints the obfuscated DLL path + sha.
TMP=$(mktemp -d) || exit 1
trap 'rm -rf "${TMP:?}"' EXIT   # our own mktemp under /tmp — allowed cleanup

WIN_SRC=$(wslpath -w "$PWD/ninjascript")
WIN_OUT=$(wslpath -w "$TMP/addon")
WIN_CUSTOM='C:\Users\hoang\Documents\NinjaTrader 8\bin\Custom'

PS_OUT=$(powershell.exe -NoProfile -ExecutionPolicy Bypass -File \
  "$PWD/deploy/friend/build-windows.ps1" \
  -Src "$WIN_SRC" -Out "$WIN_OUT" -CustomDir "$WIN_CUSTOM" 2>&1) || {
    echo "$PS_OUT" >&2
    echo "build-windows.ps1 failed" >&2
    exit 1
  }
echo "$PS_OUT"

ADDON_WIN=$(printf '%s\n' "$PS_OUT" | sed -n 's/^VL_ADDON_OBFUSCATED_DLL=//p')
ADDON_SHA=$(printf '%s\n' "$PS_OUT" | sed -n 's/^VL_ADDON_OBFUSCATED_SHA256=//p')
[ -n "$ADDON_WIN" ] || { echo "build-windows.ps1 printed no DLL path" >&2; exit 1; }
[ -n "$ADDON_SHA" ] || { echo "build-windows.ps1 printed no DLL sha" >&2; exit 1; }

# Copy the obfuscated DLL from the Windows temp dir into the dist.
ADDON_LOCAL=$(wslpath -u "$ADDON_WIN")
cp "$ADDON_LOCAL" "$OUT/VL.AddOn.dll"

# The friend double-clicks a friendly name on first run: VL-Setup.exe is the
# same self-installing app (first run = install wizard). The owner hands it to
# the friend alongside the install code.
cp "$OUT/vl-friend.exe" "$OUT/VL-Setup.exe"

# 3. SHA-256 of the app exe.
APP_SHA=$(sha256sum "$OUT/vl-friend.exe" | cut -d' ' -f1)

# 4. Manifest (B2 contract). The URLs are RELATIVE so the same dist serves every
#    friend; the app resolves them against its own bot's base URL.
cat > "$TMP/manifest.json" <<EOF
{"addon_build":"$BUILD","addon_sha256":"$ADDON_SHA","addon_url":"/api/friend/artifact/addon","app_version":"$APP_VERSION","app_sha256":"$APP_SHA","app_url":"/api/friend/artifact/app"}
EOF

# 5. Sign the manifest with the friend-distribution key (namespace "release").
ssh-keygen -Y sign -f "$KEY" -n release "$TMP/manifest.json" >/dev/null
cp "$TMP/manifest.json" "$OUT/manifest.json"
cp "$TMP/manifest.json.sig" "$OUT/manifest.json.sig"

# 6. Point LATEST at this build (a plain file rewrite — no deletion).
printf '%s\n' "$BUILD" > "$DIST_ROOT/LATEST"

echo "friend dist ready: $OUT"
echo "  addon sha256: $ADDON_SHA"
echo "  app   sha256: $APP_SHA"
