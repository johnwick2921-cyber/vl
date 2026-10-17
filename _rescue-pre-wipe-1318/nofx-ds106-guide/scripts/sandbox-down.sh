#!/usr/bin/env bash
# Stop ONLY the sandbox (matches the sandbox binary path + the sandbox vite config).
# The live bot and the normal dev server are never matched by these patterns.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
pkill -f "$PWD/.sandbox/nofx-sandbox" 2>/dev/null
pkill -f "vite.sandbox.config.ts" 2>/dev/null
sleep 1
echo "sandbox stopped."
ss -lntp 2>/dev/null | grep -E '127.0.0.1:(8081|3001|36985)' && echo "(still winding down…)" || echo "ports 8081/3001/36985 free"
echo "live bot still up:"; ss -lntp 2>/dev/null | grep -E '127.0.0.1:(8080|36974)' | awk '{print "  " $4}'
