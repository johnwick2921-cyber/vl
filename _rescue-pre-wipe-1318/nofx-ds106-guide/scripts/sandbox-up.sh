#!/usr/bin/env bash
# VL SANDBOX — an isolated copy of the app for hands-on testing.
#   API   127.0.0.1:8081   (own binary, own DB, SANDBOX_MODE=1)
#   UI    127.0.0.1:3001   (same React app, proxied at the sandbox API)
#   DB    data/sandbox.db  (a SCRUBBED copy — the live data/data.db is never opened)
#   NT8   127.0.0.1:36985  (a dead listener; NT8 never connects, no orders possible)
# The live bot (:8080 / :36974 / data/data.db) is NEVER touched by this script.
# Idempotent: safe to re-run; it stops only its own processes first.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
ROOT="$PWD"
SB_DB="$ROOT/data/sandbox.db"
SB_BIN="$ROOT/.sandbox/nofx-sandbox"
LOG_DIR="$ROOT/.sandbox"
mkdir -p "$LOG_DIR"

echo "▸ stopping any previous sandbox (live bot untouched)…"
"$ROOT/scripts/sandbox-down.sh" >/dev/null 2>&1

echo "▸ building the sandbox binary…"
go build -o "$SB_BIN" . || { echo "build failed"; exit 1; }

if [ ! -f "$SB_DB" ]; then
  echo "▸ creating data/sandbox.db (scrubbed copy of the live schema + accounts)…"
  sqlite3 "file:$ROOT/data/data.db?mode=ro" ".backup '$SB_DB'" || { echo "copy failed"; exit 1; }
  # SCRUB: nothing in the sandbox may reach the outside world or auto-trade.
  sqlite3 "$SB_DB" "
    UPDATE traders SET is_running = 0;
    DELETE FROM telegram_configs;            -- never fight the live Telegram bot
    DELETE FROM day_plan_alerts;             -- reseeded below
    DELETE FROM plans; DELETE FROM plan_overlays; DELETE FROM plan_qa;
    DELETE FROM trader_positions;            -- seeded trades only
  " 2>/dev/null
  TRADER=$(sqlite3 "$SB_DB" "SELECT id FROM traders ORDER BY rowid LIMIT 1;")
  echo "▸ seeding realistic day-plan data for trader ${TRADER:0:24}…"
  go run ./cmd/sandbox-seed -db "$SB_DB" -trader "$TRADER" -symbol MNQ || true
else
  echo "▸ reusing existing data/sandbox.db (use scripts/sandbox-reset.sh for a clean slate)"
fi

echo "▸ starting the sandbox API on 127.0.0.1:8081…"
cd "$ROOT" || exit 1
API_SERVER_PORT=8081 \
API_SERVER_HOST=127.0.0.1 \
DB_PATH="$SB_DB" \
SANDBOX_MODE=1 \
SANDBOX_LLM="${SANDBOX_LLM:-mock}" \
NT_TCP_LISTEN_ADDR=127.0.0.1:36985 \
TELEGRAM_BOT_TOKEN= \
  setsid nohup "$SB_BIN" > "$LOG_DIR/api.log" 2>&1 < /dev/null &

echo "▸ starting the sandbox UI on 127.0.0.1:3001…"
cd "$ROOT/web" || exit 1
setsid nohup npx vite --config "$ROOT/web/vite.sandbox.config.ts" > "$LOG_DIR/ui.log" 2>&1 < /dev/null &

for i in $(seq 1 30); do
  sleep 1
  API_UP=$(ss -lnt 2>/dev/null | grep -c '127.0.0.1:8081')
  UI_UP=$(ss -lnt 2>/dev/null | grep -c '127.0.0.1:3001')
  [ "$API_UP" -ge 1 ] && [ "$UI_UP" -ge 1 ] && break
done

echo
echo "════════════════════════════════════════════════"
echo "  SANDBOX READY →  http://127.0.0.1:3001/"
echo "════════════════════════════════════════════════"
ss -lntp 2>/dev/null | grep -E '127.0.0.1:(8081|3001|36985)' | awk '{print "   listening " $4}'
echo "   live bot (untouched):"
ss -lntp 2>/dev/null | grep -E '127.0.0.1:(8080|36974)' | awk '{print "   " $4 "  " $6}'
echo "   logs: .sandbox/api.log · .sandbox/ui.log"
