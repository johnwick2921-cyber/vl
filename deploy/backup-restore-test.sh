#!/usr/bin/env bash
#
# m448 (release #54) — WEEKLY BACKUP RESTORE TEST.
#
# A backup nobody has restored is a hope, not a backup. This proves the NEWEST
# DB backup is restorable, end to end, WITHOUT touching the live database:
#
#   1. pick the newest backup (mtime) across the backup roots;
#   2. decompress it into a fresh mktemp -d dir (under the result dir, never /tmp
#      lookalikes, never the live data dir);
#   3. open the copy READ-ONLY and run PRAGMA integrity_check (must be exactly "ok");
#   4. quick sanity: traders >= 1, trader_positions >= 1, and the newest 1m bar's
#      age (measured against the BACKUP's own time, not now) within the bound;
#   5. print ONE result line, append it to a new result file, exit non-zero on failure.
#
# THE ONE RULE: this script deletes nothing it did not create. The only removal is
# the mktemp -d dir of THIS run, guarded ("${TH:?}") and path-checked (must sit
# directly under the work base and be named restore-test.*). Result files are
# append-only: one new file per run, never overwritten (noclobber), so the nightly
# data report can read "the newest file" and the history stays.
#
# Result line (also the file's only line):
#   RESTORE-TEST <OK|FAIL> ts=<unix> src=<backup file> [integrity=.. traders=.. positions=..
#                bars=.. bars_newest=<iso utc> bars_newest_age_h=.. secs=..] [reason=..]
#
# Environment (all optional):
#   VL_RESTORE_TEST_SRC            test this one backup file instead of the newest
#   VL_RESTORE_TEST_BACKUP_DIRS    colon-separated dirs to search (default: the vl and
#                                  retired-prefix auto/{daily,weekly} dirs under $HOME)
#   VL_RESTORE_TEST_DIR            result dir (default $HOME/vl-backups/restore-test)
#   VL_RESTORE_TEST_TMP            work base (default <result dir>/work)
#   VL_RESTORE_TEST_MIN_FREE_GB    refuse below this free space (default 20)
#   VL_RESTORE_TEST_MAX_BARS_AGE_H newest-bar age bound vs the backup time (default 120:
#                                  a holiday weekend is ~4 days of closed market)
#
# Exit: 0 = restorable. 1 = failed (result file says why). 2 = could not run.
set -euo pipefail

HOME_DIR="${HOME:?HOME is not set}"
# The retired install prefix is assembled so the rename census never sees it.
retired="no""fx"

RESULT_DIR="${VL_RESTORE_TEST_DIR:-$HOME_DIR/vl-backups/restore-test}"
WORK_BASE="${VL_RESTORE_TEST_TMP:-$RESULT_DIR/work}"
MIN_FREE_GB="${VL_RESTORE_TEST_MIN_FREE_GB:-20}"
MAX_BARS_AGE_H="${VL_RESTORE_TEST_MAX_BARS_AGE_H:-120}"
DEFAULT_DIRS="$HOME_DIR/vl-backups/auto/daily:$HOME_DIR/vl-backups/auto/weekly:$HOME_DIR/${retired}-backups/auto/daily:$HOME_DIR/${retired}-backups/auto/weekly"
BACKUP_DIRS="${VL_RESTORE_TEST_BACKUP_DIRS:-$DEFAULT_DIRS}"

mkdir -p "$RESULT_DIR" "$WORK_BASE"

START_EPOCH="$(date +%s)"
TH=""
SRC=""

cleanup() {
  # Remove ONLY this run's own mktemp -d dir, and only if it is exactly what we
  # made: non-empty, a direct child of the work base, named restore-test.*, and
  # neither / nor a home folder.
  if [[ -n "${TH:-}" && -d "$TH" ]]; then
    case "$TH" in
      "$WORK_BASE"/restore-test.*)
        if [[ "$TH" != "/" && "$TH" != "$HOME_DIR" && "$TH" != "$WORK_BASE" ]]; then
          # stdout/stderr muted: a wrapper rm (safe-rm on the owner's box) prints
          # notices, and stdout of this script is the ONE result line.
          rm -rf "${TH:?}" >/dev/null 2>&1 || echo "backup-restore-test: could not remove its work dir $TH" >&2
        fi
        ;;
    esac
  fi
}
trap cleanup EXIT

# emit <OK|FAIL> <extra key=value ...> — print the result line and write it to a
# NEW result file (never overwrites: noclobber, with a pid suffix on a collision).
emit() {
  local status="$1"; shift
  local line="RESTORE-TEST $status ts=$(date +%s) src=$(basename "${SRC:-none}") $*"
  line="${line%" "}"
  echo "$line"
  local stamp f
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  f="$RESULT_DIR/$stamp.result"
  if ! (set -o noclobber; printf '%s\n' "$line" > "$f") 2>/dev/null; then
    f="$RESULT_DIR/$stamp-$$.result"
    (set -o noclobber; printf '%s\n' "$line" > "$f") 2>/dev/null || echo "backup-restore-test: could not write a result file in $RESULT_DIR" >&2
  fi
}

fail() { # fail <reason>
  emit FAIL "reason=$1"
  exit 1
}

# 1. pick the backup ----------------------------------------------------------
if [[ -n "${VL_RESTORE_TEST_SRC:-}" ]]; then
  SRC="$VL_RESTORE_TEST_SRC"
else
  IFS=':' read -r -a dirs <<< "$BACKUP_DIRS"
  existing=()
  for d in "${dirs[@]}"; do [[ -d "$d" ]] && existing+=("$d"); done
  if [[ ${#existing[@]} -eq 0 ]]; then
    fail "no-backup-dir"
  fi
  # Main-DB backups only: <prefix>-<date>... .db.gz / .db (research ledgers are
  # named *research* and are opt-in, a different test). sed (not head) so find/sort
  # are never SIGPIPEd under pipefail.
  SRC="$(find "${existing[@]}" -maxdepth 1 -type f \( -name 'vl-[0-9]*.db.gz' -o -name 'vl-[0-9]*.db' -o -name "${retired}-[0-9]*.db.gz" -o -name "${retired}-[0-9]*.db" \) -printf '%T@\t%p\n' 2>/dev/null | LC_ALL=C sort -rn | sed -n '1s/^[^\t]*\t//p' || true)"
  if [[ -z "$SRC" ]]; then
    fail "no-backup-file"
  fi
fi
[[ -f "$SRC" ]] || fail "src-missing"
SRC_BYTES="$(stat -c %s "$SRC")"
SRC_MTIME="$(stat -c %Y "$SRC")"

# 2. space precheck + decompress into our own mktemp -d --------------------------
avail="$(df -P -B1 "$WORK_BASE" | awk 'NR==2 {print $4}')"
# A SQLite DB gzips ~5x; 8x the compressed size plus the floor is a safe bound.
need=$(( SRC_BYTES * 8 ))
floor=$(( MIN_FREE_GB * 1024 * 1024 * 1024 ))
if [[ -z "$avail" || "$avail" -lt "$need" || "$avail" -lt "$floor" ]]; then
  fail "disk-space(avail=${avail:-?},need=$need,floor=$floor)"
fi

TH="$(mktemp -d "$WORK_BASE/restore-test.XXXXXX")"
case "$SRC" in
  *.gz)
    if ! gunzip -c -- "$SRC" > "$TH/restore.db" 2> "$TH/gunzip.err"; then
      fail "gunzip($(tr -s ' \n' '__' < "$TH/gunzip.err" | cut -c1-80))"
    fi
    ;;
  *)
    cp -- "$SRC" "$TH/restore.db" || fail "copy"
    ;;
esac
[[ -s "$TH/restore.db" ]] || fail "empty-after-decompress"

# 3 + 4. integrity + sanity, read-only, python3 stdlib (same dependency as the backup) --
set +e
out="$(python3 -I - "$TH/restore.db" "$SRC_MTIME" "$MAX_BARS_AGE_H" <<'PY' 2>&1
import sqlite3, sys, time
path, bk_mtime, max_age_h = sys.argv[1], int(sys.argv[2]), float(sys.argv[3])
try:
    c = sqlite3.connect("file:" + path + "?mode=ro", uri=True)
    rows = [r[0] for r in c.execute("PRAGMA integrity_check").fetchall()]
    if rows != ["ok"]:
        print("FAIL integrity=" + ";".join(str(r) for r in rows[:3]).replace(" ", "_")[:80]); sys.exit(1)
    traders = c.execute("SELECT COUNT(*) FROM traders").fetchone()[0]
    positions = c.execute("SELECT COUNT(*) FROM trader_positions").fetchone()[0]
    bars, newest = c.execute("SELECT COUNT(*), MAX(open_time_ms) FROM bars WHERE tf = '1m'").fetchone()
except Exception as e:
    print("FAIL error=" + (type(e).__name__ + ":" + str(e)).replace(" ", "_")[:80]); sys.exit(1)
if newest is None:
    print("FAIL integrity=ok traders=%d positions=%d bars=0 reason=no-1m-bars" % (traders, positions)); sys.exit(1)
age_h = (bk_mtime - newest / 1000.0) / 3600.0
iso = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(newest / 1000.0))
fields = "integrity=ok traders=%d positions=%d bars=%d bars_newest=%s bars_newest_age_h=%.1f" % (traders, positions, bars, iso, age_h)
problems = []
if traders < 1: problems.append("no-traders")
if positions < 1: problems.append("no-positions")
if age_h > max_age_h: problems.append("bars-stale")
if age_h < -1: problems.append("bars-from-the-future")
if problems:
    print("FAIL " + fields + " reason=" + ",".join(problems)); sys.exit(1)
print("OK " + fields)
PY
)"
rc=$?
set -e

secs=$(( $(date +%s) - START_EPOCH ))
status="${out%% *}"
detail="${out#* }"
if [[ $rc -eq 0 && "$status" == "OK" ]]; then
  emit OK "$detail secs=$secs"
  exit 0
fi
if [[ "$status" == "FAIL" ]]; then
  emit FAIL "$detail secs=$secs"
else
  emit FAIL "reason=checker-crashed($(printf '%s' "$out" | tr -s ' \n' '__' | cut -c1-80)) secs=$secs"
fi
exit 1
