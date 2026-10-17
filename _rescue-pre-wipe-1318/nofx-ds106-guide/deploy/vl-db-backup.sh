#!/usr/bin/env bash
#
# C1 — vl SQLite online backup + retention prune.
#
# Uses the SQLite *online backup API* (via python3 stdlib — no sqlite3 CLI needed,
# and consistent even while the bot is writing) to snapshot data/data.db, verifies
# the copy's integrity, gzips it, and prunes to a daily+weekly retention window.
#
# Layout under $VL_BACKUP_DIR (default follows the BackupRoot rule below):
#   daily/vl-YYYY-MM-DD_HHMMSS.db.gz    every run   → keep newest KEEP_DAILY
#   weekly/vl-YYYY-MM-DD_HHMMSS.db.gz   1 per ISO wk → keep newest KEEP_WEEKLY
#
# Research ledger (data/data.db.research.db, 200+ GB on the live box) is OPT-IN:
#   VL_BACKUP_RESEARCH=1   back up research daily too (KEEP_RESEARCH_* retention)
#   unset / anything else    research is NOT touched — the default run is
#                            byte-identical in effect to the pre-P2-5 script
#                            (main DB only). A missing research DB is not a failure.
#
# Disk-space precheck runs for EVERY source that would be written (main DB too):
# the run REFUSES (loud stderr + exit 1, nothing written) when free space on the
# backup volume is < 2.5 × source size, or when free-after-backup would drop
# below VL_BACKUP_MIN_FREE_GB (default 50). A full disk breaks the live bot —
# a refused backup is the correct outcome.
#
# Driven by the user systemd timer vl-backup.timer (05:00 + 17:30 CT). Safe to
# run by hand any time. Exits non-zero (and keeps nothing partial) on any failure.
set -euo pipefail

# Every var is the shell twin VL_ → NOFX_ → default; the DB default uses
# the install-root rule ($HOME/vl when present, else the old install dir — never
# a hardcoded /home/hoang); the backup-dir default follows BackupRoot (vl if it
# exists, else the old backups dir while the old install exists, else vl). R5
# removes the NOFX twins.
INSTALL_ROOT="$HOME/vl"; [ -d "$INSTALL_ROOT" ] || INSTALL_ROOT="$HOME/nofx"
DB="${VL_DB:-${NOFX_DB:-$INSTALL_ROOT/data/data.db}}"
DB_RESEARCH="${VL_DB_RESEARCH:-${NOFX_DB_RESEARCH:-$INSTALL_ROOT/data/data.db.research.db}}"
BACKUP_ROOT="$HOME/vl-backups"
[ -d "$BACKUP_ROOT" ] || { [ -d "$HOME/nofx" ] && BACKUP_ROOT="$HOME/nofx-backups"; }
ROOT="${VL_BACKUP_DIR:-${NOFX_BACKUP_DIR:-$BACKUP_ROOT/auto}}"
KEEP_DAILY="${VL_KEEP_DAILY:-${NOFX_KEEP_DAILY:-14}}"          # R5 removes the NOFX twin
KEEP_WEEKLY="${VL_KEEP_WEEKLY:-${NOFX_KEEP_WEEKLY:-8}}"        # R5 removes
# Research is OPT-IN and its retention is deliberately SHORT when opted in
# (CTO ruling 2026-09-26): a 213 GB research snapshot is a disk, not a record.
BACKUP_RESEARCH="${VL_BACKUP_RESEARCH:-${NOFX_BACKUP_RESEARCH:-0}}"                # R5 removes
KEEP_RESEARCH_DAILY="${VL_KEEP_RESEARCH_DAILY:-${NOFX_KEEP_RESEARCH_DAILY:-1}}"   # R5 removes
KEEP_RESEARCH_WEEKLY="${VL_KEEP_RESEARCH_WEEKLY:-${NOFX_KEEP_RESEARCH_WEEKLY:-1}}" # R5 removes
MIN_FREE_GB="${VL_BACKUP_MIN_FREE_GB:-${NOFX_BACKUP_MIN_FREE_GB:-50}}"             # R5 removes

DAILY_DIR="$ROOT/daily"
WEEKLY_DIR="$ROOT/weekly"
mkdir -p "$DAILY_DIR" "$WEEKLY_DIR"

ts="$(date +%Y-%m-%d_%H%M%S)"

# space_precheck <src> — REFUSE loudly (exit 1) unless the backup volume has
# ≥ 2.5 × the source size free AND keeps ≥ MIN_FREE_GB free after the write.
space_precheck() {
  local src="$1" size avail need floor after
  size="$(stat -c %s "$src" 2>/dev/null || echo 0)"
  if [[ "$size" -le 0 ]]; then
    echo "vl-backup: REFUSED — cannot stat source $src" >&2
    exit 1
  fi
  avail="$(df -P -B1 "$ROOT" 2>/dev/null | awk 'NR==2 {print $4}')"
  if [[ -z "${avail:-}" || ! "$avail" =~ ^[0-9]+$ ]]; then
    echo "vl-backup: REFUSED — free space on $ROOT unreadable (df gave: ${avail:-<nothing>})" >&2
    exit 1
  fi
  need=$(( size * 5 / 2 ))                 # 2.5 × source size, integer math
  floor=$(( MIN_FREE_GB * 1024 * 1024 * 1024 ))
  after=$(( avail - size ))
  if (( avail < need )); then
    echo "vl-backup: REFUSED — free space on $ROOT is $avail bytes, need ≥ 2.5 × $size bytes = $need bytes for $src — NOTHING written" >&2
    exit 1
  fi
  if (( after < floor )); then
    echo "vl-backup: REFUSED — backing up $src would leave $after bytes free, below the NOFX_BACKUP_MIN_FREE_GB floor of ${MIN_FREE_GB} GB — NOTHING written" >&2
    exit 1
  fi
}

# backup_one <src> <prefix> — precheck + online backup + quick_check on the COPY
# + gzip. Writes to a .partial file first so a crash never leaves a truncated
# backup that looks complete. Runs niced: the research DB is 200+ GB and a
# full-copy stall must not starve the bot on the same box.
backup_one() {
  local src="$1" prefix="$2" tmp final
  tmp="$DAILY_DIR/.${prefix}-${ts}.db.partial"
  final="$DAILY_DIR/${prefix}-${ts}.db.gz"

  space_precheck "$src"

  nice -n 19 ionice -c 3 python3 - "$src" "$tmp" <<'PY'
import sqlite3, sys
src, dst = sys.argv[1], sys.argv[2]
s = sqlite3.connect(src)
d = sqlite3.connect(dst)
ok = "fail"
try:
    with d:
        s.backup(d)            # atomic, consistent snapshot via the backup API
    ok = d.execute("PRAGMA quick_check").fetchone()[0]
finally:
    d.close(); s.close()
if ok != "ok":
    sys.exit("backup integrity check FAILED: %s" % ok)
PY

  gzip -f "$tmp"                  # -> $tmp.gz
  mv -f "$tmp.gz" "$final"
  echo "vl-backup: wrote $final ($(du -h "$final" | cut -f1))"
}

# prune <dir> <keep> <prefix> — keep the newest N (names sort lexically).
prune() {
  local dir="$1" keep="$2" prefix="$3" f n=0
  # shellcheck disable=SC2012
  for f in $(ls -1 "$dir"/${prefix}-*.db.gz 2>/dev/null | sort -r); do
    n=$((n + 1))
    if [[ $n -gt $keep ]]; then
      rm -f "$f"
      echo "vl-backup: pruned $(basename "$f")"
    fi
  done
}

# promote_weekly <prefix> <src> — one snapshot per ISO week (first run wins).
promote_weekly() {
  local prefix="$1" src="$2" week_tag weekly
  week_tag="$(date +%G-W%V)"     # e.g. 2026-W33
  if ! ls "$WEEKLY_DIR"/${prefix}-*."${week_tag}".db.gz >/dev/null 2>&1; then
    weekly="$WEEKLY_DIR/${prefix}-${ts}.${week_tag}.db.gz"
    cp -f "$src" "$weekly"
    echo "vl-backup: promoted weekly $weekly"
  fi
}

if [[ ! -f "$DB" ]]; then
  echo "vl-backup: DB not found at $DB" >&2
  exit 1
fi

backup_one "$DB" "vl"
promote_weekly "vl" "$DAILY_DIR/vl-${ts}.db.gz"
prune "$DAILY_DIR" "$KEEP_DAILY" "nofx"
prune "$DAILY_DIR" "$KEEP_DAILY" "vl"     # R5 removes the old-prefix prune above
prune "$WEEKLY_DIR" "$KEEP_WEEKLY" "nofx"
prune "$WEEKLY_DIR" "$KEEP_WEEKLY" "vl"   # R5 removes the old-prefix prune above

if [[ "$BACKUP_RESEARCH" == "1" ]]; then
  if [[ -f "$DB_RESEARCH" ]]; then
    backup_one "$DB_RESEARCH" "vl-research"
    promote_weekly "vl-research" "$DAILY_DIR/vl-research-${ts}.db.gz"
    prune "$DAILY_DIR" "$KEEP_RESEARCH_DAILY" "vl-research"
    prune "$WEEKLY_DIR" "$KEEP_RESEARCH_WEEKLY" "vl-research"
  else
    echo "vl-backup: research DB absent at $DB_RESEARCH — skipped (a machine without the research ledger is not a failure)"
  fi
elif [[ -f "$DB_RESEARCH" ]]; then
  echo "vl-backup: research backup OFF (VL_BACKUP_RESEARCH unset) — main DB only; opt in with VL_BACKUP_RESEARCH=1"
fi

echo "vl-backup: done ($(ls -1 "$DAILY_DIR"/vl-*.db.gz 2>/dev/null | wc -l) daily, $(ls -1 "$WEEKLY_DIR"/vl-*.db.gz 2>/dev/null | wc -l) weekly)"
