#!/usr/bin/env bash
#
# W-BARS-CONTRACT-KEY rollback (2026-09-18) — RESTORE.md Option A, scripted.
#
# Puts the PRE-MIGRATION bars table (bars_pre_contract_key_<date>, keyed
# (symbol, tf, open_time_ms), its legacy indexes still attached) back under the
# name `bars`, and parks the migrated table as bars_contract_key_<date>. Run
# BEFORE starting a binary older than the wave on a migrated database: that
# binary's upsert names ON CONFLICT(symbol, tf, open_time_ms), which the new key
# does not satisfy — SQLite refuses every write and persistence dies silently.
#
# Idempotent: with no bars_pre_contract_key_* table there is nothing to do.
#
#   deploy/bars-key-rollback.sh [--force] [--db PATH]
#
# rc 0 = rolled back · rc 4 = nothing to do (already on the old key) ·
# rc 2 = refused: bot running (use --force) · rc 3 = refused: unsafe state ·
# rc 1 = a step failed (the transaction rolled back; nothing renamed).
set -euo pipefail

# Shell twin VL_ → NOFX_ → default; DB uses the install-root rule. R5 removes
# the NOFX twins.
DEFAULT_DB="$HOME/vl/data/data.db"; [ -d "$HOME/vl" ] || DEFAULT_DB="$HOME/nofx/data/data.db"
DB="${VL_DB:-${NOFX_DB:-$DEFAULT_DB}}"
FORCE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) FORCE=1 ;;
    --db) DB="$2"; shift ;;
    *) echo "bars-key-rollback: unknown arg $1" >&2; exit 1 ;;
  esac
  shift
done
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL="$HERE/bars-key-rollback.sql"
[[ -f "$DB" ]] || { echo "bars-key-rollback: DB not found at $DB" >&2; exit 1; }
[[ -f "$SQL" ]] || { echo "bars-key-rollback: $SQL missing" >&2; exit 1; }
command -v sqlite3 >/dev/null || { echo "bars-key-rollback: sqlite3 CLI required" >&2; exit 1; }

q() { sqlite3 "$DB" "$1"; }

OLD="$(q "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'bars_pre_contract_key_%' ORDER BY name DESC LIMIT 1;")"
PK="$(q "SELECT group_concat(name, ',') FROM (SELECT name FROM pragma_table_info('bars') WHERE pk>0 ORDER BY pk);")"
if [[ -z "$OLD" ]]; then
  echo "bars-key-rollback: nothing to do — no bars_pre_contract_key_* table in $DB (bars key = $PK)"
  exit 4
fi
if [[ "$PK" != "symbol,tf,contract,open_time_ms" ]]; then
  echo "bars-key-rollback: REFUSED — $OLD exists but bars is keyed ($PK), not the contract key; resolve by hand (deploy/RESTORE.md)" >&2
  exit 3
fi
DATE="${OLD#bars_pre_contract_key_}"
NEW="bars_contract_key_${DATE}"
if [[ -n "$(q "SELECT name FROM sqlite_master WHERE type='table' AND name='$NEW';")" ]]; then
  echo "bars-key-rollback: REFUSED — $NEW already exists; rename or drop it first, deliberately" >&2
  exit 3
fi
if { pgrep -f nofx-bin >/dev/null 2>&1 || pgrep -f vl-bin >/dev/null 2>&1; } && [[ $FORCE -ne 1 ]]; then # R5 removes the nofx pattern
  echo "bars-key-rollback: REFUSED — a bot binary (nofx-bin/vl-bin) is running (pid $(pgrep -f 'nofx-bin|vl-bin' | head -1)); stop it, or pass --force if that process is not using $DB" >&2
  exit 2
fi

# Backup first (guarded write): VACUUM INTO is SQLite's online consistent copy.
DEFAULT_BKDIR="$HOME/vl-backups"; [ -d "$HOME/vl" ] || DEFAULT_BKDIR="$HOME/nofx-backups"
BKDIR="${VL_BARS_KEY_BACKUP_DIR:-${BARS_KEY_BACKUP_DIR:-$DEFAULT_BKDIR}}"
mkdir -p "$BKDIR"
BK="$BKDIR/pre-bars-key-rollback-$(date +%Y%m%d-%H%M%S).db"
q "VACUUM INTO '$BK';"
BKROWS="$(sqlite3 "$BK" "SELECT COUNT(*) FROM bars;")"
NEWROWS="$(q "SELECT COUNT(*) FROM bars;")"
OLDROWS="$(q "SELECT COUNT(*) FROM \"$OLD\";")"
[[ "$BKROWS" == "$NEWROWS" ]] || { echo "bars-key-rollback: backup verification FAILED ($BKROWS vs $NEWROWS rows)" >&2; rm -f "$BK"; exit 1; }
echo "bars-key-rollback: backup $BK (bars rows=$BKROWS)"

# The rename pair, one transaction.
sed -e "s/@OLD@/$OLD/g" -e "s/@NEW@/$NEW/g" "$SQL" | sqlite3 "$DB"

PK2="$(q "SELECT group_concat(name, ',') FROM (SELECT name FROM pragma_table_info('bars') WHERE pk>0 ORDER BY pk);")"
UNIQ="$(q "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='bars' AND name='idx_bars_sym_tf_time_unique';")"
echo "bars-key-rollback: DONE — bars key=($PK2) rows=$(q "SELECT COUNT(*) FROM bars;") (was $OLD, $OLDROWS rows) · migrated table parked as $NEW rows=$(q "SELECT COUNT(*) FROM \"$NEW\";") · legacy unique index on bars=$UNIQ"
if [[ "$PK2" != "symbol,tf,open_time_ms" || "$UNIQ" != "1" ]]; then
  echo "bars-key-rollback: WARNING — post-state is not the expected legacy shape; do not boot an old binary until this is understood" >&2
  exit 1
fi
echo "bars-key-rollback: bars written after the migration are in $NEW; to carry them back (overlap minutes collapse to one row):"
echo "  sqlite3 $DB \"INSERT OR IGNORE INTO bars(symbol,tf,open_time_ms,o,h,l,c,v,convention,contract,source) SELECT symbol,tf,open_time_ms,o,h,l,c,v,convention,contract,source FROM $NEW;\""
