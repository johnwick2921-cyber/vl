#!/usr/bin/env bash
# planner-ab-report.sh — PLANNER A/B report (HIGH vs MAX), READ-ONLY.
#
# Reads the planner log (🧠 planner call / 🧭 planner read / 💀 born-dead drops)
# and data.db (plans + trader_positions) — sqlite3 -readonly only. Never writes
# except the two report files under --out-dir.
#
# Usage:
#   deploy/planner-ab-report.sh --boot YYYY-MM-DD [--end YYYY-MM-DD] \
#       [--log file] [--db file] [--out-dir dir]
#
# Windows: baseline = [boot-7d, boot) · test = [boot, end] (default end=boot+1d).
# Per (window × reasoning level, read from each "🧠 planner call (reasoning=…"
# line): reads, call time avg/median/p90, first-attempt pass %, final publish %,
# born-dead drops (💀 lines), fail-closed reads, and for PUBLISHED plans the
# trade outcomes (trader_positions joined on plan_id/plan_version: count, win %,
# pnl_corrected Σ with the UNRESOLVED (NULL) count shown — never coerced).
# Output: <out-dir>/planner_ab_report.md + .csv. Exit 0 on success, 2 on usage.
set -u

BOOT=""; END=""; LOG=""; DB=""; OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --boot) BOOT=$2; shift 2;;
    --end) END=$2; shift 2;;
    --log) LOG=$2; shift 2;;
    --db) DB=$2; shift 2;;
    --out-dir) OUT=$2; shift 2;;
    *) echo "unknown arg $1" >&2; exit 2;;
  esac
done

# install root is $HOME/vl — never a hardcoded /home/hoang. R5: env reads
# are the VL_ names only.
DEFAULT_REPO="$HOME/vl"
VL_REPO="${VL_REPO:-$DEFAULT_REPO}"
VL_DATA="${VL_DATA:-$VL_REPO/data}"
[ -n "$LOG" ] || LOG=$(ls -t "$VL_DATA"/vl_*.log 2>/dev/null | head -1)
[ -n "$DB" ] || DB=$VL_DATA/data.db
[ -n "$OUT" ] || OUT=$VL_DATA
[ -n "$BOOT" ] || { echo "--boot YYYY-MM-DD required" >&2; exit 2; }
[ -r "$LOG" ] || { echo "log not readable: $LOG" >&2; exit 2; }
[ -r "$DB" ] || { echo "db not readable: $DB" >&2; exit 2; }
END=${END:-$(date -d "$BOOT +1 day" +%F)}
mkdir -p "$OUT"

# Log timestamps are CT ("MM-DD HH:MM:SS"); plans.created_at is stored with
# +00:00, so the SQL window uses the UTC dates (the log day boundary == the CT
# date the session names; the report documents this seam).
BASELINE_CT_START=$(date -d "$BOOT -7 days" +%m-%d)
BASELINE_CT_END=$(date -d "$BOOT" +%m-%d)
TEST_CT_START=$(date -d "$BOOT" +%m-%d)
TEST_CT_END=$(date -d "$END" +%m-%d)
SQL_BASELINE_START=$(date -d "$BOOT -7 days" +%F)
SQL_BASELINE_END=$(date -d "$BOOT" +%F)
SQL_TEST_START=$(date -d "$BOOT" +%F)
SQL_TEST_END=$(date -d "$END" +%F)

sql() { sqlite3 -readonly "file:$DB?mode=ro" "$1" 2>/dev/null; }

# One awk pass: emit one raw row per (window, level):
#   window,level,reads,calls,avg_sec,attempt1,publish,failclosed,drops,times_csv
raw=$(A="$BASELINE_CT_START" B="$BASELINE_CT_END" T="$TEST_CT_START" U="$TEST_CT_END" awk '
function level_of(line,   l) {
  l = substr(line, index(line, "reasoning=") + 10)
  return substr(l, 1, index(l, " ") - 1)
}
{
  md = substr($0, 1, 5)
  if (md >= ENVIRON["A"] && md < ENVIRON["B"]) w = "baseline"
  else if (md >= ENVIRON["T"] && md < ENVIRON["U"]) w = "test"
  else next
  if ($0 ~ /🧠 planner call \(reasoning=/) {
    lvl = level_of($0)
    t = $0; sub(/.*completed in /, "", t); sub(/s.*/, "", t)
    if (lvl != "" && t ~ /^[0-9]+(\.[0-9]+)?$/) {
      k = w SUBSEP lvl
      calls[k]++
      csec[k] += t
      tlist[k] = tlist[k] (tlist[k] == "" ? "" : ",") t
      LAST[w] = lvl
    }
  } else if ($0 ~ /🧭 planner read:/) {
    lvl = LAST[w]
    if (lvl == "") next
    k = w SUBSEP lvl
    reads[k]++
    a = $0; sub(/.*attempts=/, "", a); sub(/ .*/, "", a)
    if (a == "1") attempt1[k]++
    if ($0 ~ /lifecycle=active/) publish[k]++
    if ($0 ~ /lifecycle=no_trade/) failed[k]++
  } else if ($0 ~ /💀 born-dead /) {
    lvl = LAST[w]
    if (lvl != "") drops[w SUBSEP lvl]++
  }
}
END {
  for (k in calls) {
    split(k, kv, SUBSEP)
    printf "%s,%s,%d,%d,%.3f,%d,%d,%d,%d,%s\n",
      kv[1], kv[2], reads[k], calls[k], (calls[k] ? csec[k] / calls[k] : 0),
      attempt1[k], publish[k], failed[k], drops[k], tlist[k]
  }
}' "$LOG")

md=$OUT/planner_ab_report.md
csv=$OUT/planner_ab_report.csv
{
  echo "# Planner A/B report — boot $BOOT · baseline [$BASELINE_CT_START, $BASELINE_CT_END) · test [$TEST_CT_START, $TEST_CT_END)"
  echo
  echo "Reads are grouped by the reasoning level of the planner call that produced them. Call time in seconds. pnl_corrected: NULL rows are UNRESOLVED — excluded from the sum, the count is shown. Trade columns are per WINDOW (trader_positions do not record the reasoning level): every level row of one window shows the same window totals."
  echo
  echo "| window | level | reads | avg s | median s | p90 s | first-attempt pass % | final publish % | born-dead drops | fail-closed | trades | win % | pnl_corrected Σ | UNRESOLVED |"
  echo "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|"
} > "$md"
{
  echo "window,level,reads,avg_s,median_s,p90_s,first_attempt_pass_pct,final_publish_pct,born_dead_drops,fail_closed,trades,win_pct,pnl_corrected_sum,unresolved"
} > "$csv"

echo "$raw" | sort | while IFS=, read -r w lvl readcnt calls cavg a1 pub failcnt drops tlist; do
  [ -n "$w" ] || continue
  [ "$readcnt" = "" ] && readcnt=0
  med=$(echo "$tlist" | tr ',' '\n' | sort -n | awk '{a[NR]=$1} END{if(NR==0) print 0; else print (NR%2? a[(NR+1)/2] : (a[NR/2]+a[NR/2+1])/2)}')
  p90=$(echo "$tlist" | tr ',' '\n' | sort -n | awk '{a[NR]=$1; n=NR} END{if(n==0) print 0; else {idx=int(n*0.9)+1; if(idx>n) idx=n; print a[idx]}}')
  a1p=$(awk -v x="$a1" -v n="$readcnt" 'BEGIN{printf "%.1f", (n>0? x*100/n : 0)}')
  pubp=$(awk -v x="$pub" -v n="$readcnt" 'BEGIN{printf "%.1f", (n>0? x*100/n : 0)}')
  if [ "$w" = "baseline" ]; then S=$SQL_BASELINE_START; E=$SQL_BASELINE_END; else S=$SQL_TEST_START; E=$SQL_TEST_END; fi
  trades=$(sql "SELECT COUNT(*),
       SUM(CASE WHEN pnl_corrected IS NULL THEN 1 ELSE 0 END),
       COALESCE(SUM(pnl_corrected), 0),
       SUM(CASE WHEN pnl_corrected IS NOT NULL AND pnl_corrected > 0 THEN 1 ELSE 0 END),
       SUM(CASE WHEN pnl_corrected IS NOT NULL THEN 1 ELSE 0 END)
    FROM trader_positions WHERE plan_id IN
      (SELECT plan_id FROM plans WHERE lifecycle='active'
        AND created_at >= '$S 00:00:00' AND created_at < '$E 00:00:00');")
  tc=$(echo "$trades" | awk -F'|' '{print $1}'); [ "$tc" = "" ] && tc=0
  unres=$(echo "$trades" | awk -F'|' '{print $2}'); [ "$unres" = "" ] && unres=0
  pnl=$(echo "$trades" | awk -F'|' '{print $3}'); [ "$pnl" = "" ] && pnl=0
  wins=$(echo "$trades" | awk -F'|' '{print $4}'); [ "$wins" = "" ] && wins=0
  res=$(echo "$trades" | awk -F'|' '{print $5}'); [ "$res" = "" ] && res=0
  winp=$(awk -v a="$wins" -v b="$res" 'BEGIN{printf "%.1f", (b>0? a*100/b : 0)}')
  printf '| %s | %s | %d | %.1f | %.1f | %.1f | %s%% | %s%% | %d | %d | %d | %s%% | %.2f | %d |\n' \
    "$w" "$lvl" "$readcnt" "$cavg" "$med" "$p90" "$a1p" "$pubp" "$drops" "$failcnt" "$tc" "$winp" "$pnl" "$unres" >> "$md"
  printf '%s,%s,%d,%.3f,%.3f,%.3f,%s,%s,%d,%d,%d,%s,%.2f,%d\n' \
    "$w" "$lvl" "$readcnt" "$cavg" "$med" "$p90" "$a1p" "$pubp" "$drops" "$failcnt" "$tc" "$winp" "$pnl" "$unres" >> "$csv"
done

echo "wrote $md and $csv"
