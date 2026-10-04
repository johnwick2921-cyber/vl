#!/usr/bin/env bash
# postboot-check.sh — BOOT-DAY OBSERVATION KIT (DS-101, 2026-09-26)
#
# READ-ONLY. This script never writes a file, never opens the DB for writing,
# never calls the bot's API, never kills or restarts anything. Every DB touch
# goes through `sqlite3 -readonly "file:...?mode=ro"`.
#
# Usage:
#   deploy/postboot-check.sh [logfile]      # run every check against one log
#   deploy/postboot-check.sh --selfcheck    # verify every expected string this
#                                           # kit greps for still exists in the
#                                           # repo sources (anti-rot)
#
# Env overrides (defaults are the production layout):
#   VL_REPO=/home/hoang/vl   VL_DATA=$VL_REPO/data
#   VL_BIN=$VL_REPO/vl-bin VL_ENV=$VL_REPO/.env
#
# Exit code = number of FAILs (0 = all green). Run right after the boot line
# sweep and again at Sunday 17:00 CT open (the SUBSET: planner-max, fast-market
# max, salvage counters, no-ERROR — see deploy/POSTBOOT-CHECK.md).
set -u

# The repo this script ships with is the selfcheck/baseline source of truth
# (the deployed tree may still be the OLD head before the weekend boot).
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
KIT_REPO=$(cd "$SCRIPT_DIR/.." && pwd)

# R5: env reads are the VL_ names only (the old-name twins are gone).
# The install-side binary is vl-bin, end of story.
VL_REPO="${VL_REPO:-$KIT_REPO}"
VL_DATA="${VL_DATA:-$VL_REPO/data}"
VL_BIN="${VL_BIN:-}"
if [ -z "$VL_BIN" ]; then
  VL_BIN="$VL_REPO/vl-bin"
fi
VL_ENV="${VL_ENV:-$VL_REPO/.env}"
DB=$VL_DATA/data.db

# The documented `_ =` baseline at dev 35a53d29 (regex below, non-test Go
# files). Bump deliberately in deploy/POSTBOOT-CHECK.md when a reviewed wave
# adds a justified hit.
UNDERSCORE_EQ_BASELINE=432

fail=0
pass=0
ok()  { pass=$((pass+1)); echo "PASS $1"; }
bad() { fail=$((fail+1)); echo "FAIL $1"; }

sql() { sqlite3 -readonly "file:$DB?mode=ro" "$1" 2>/dev/null; }

# ---------------------------------------------------------------------------
# 1. stop-entry (attempted-entry) guard — the boot line reads the SAME
#    predicate the placement branch calls; guard=MISROUTED is a refused boot.
# ---------------------------------------------------------------------------
check_stop_entry() {
  local line
  line=$(grep -m1 '🎯 stop-entry:' "$LOG" 2>/dev/null)
  [ -n "$line" ] || { bad "stop-entry boot line absent"; return; }
  echo "$line" | grep -q 'guard=MISROUTED' && { bad "stop-entry guard MISROUTED: $line"; return; }
  echo "$line" | grep -q 'seam=' || { bad "stop-entry seam not stated: $line"; return; }
  ok "stop-entry guard line present and routed: $line"
}

# ---------------------------------------------------------------------------
# 2. retention off — every table =off(keep all, rows=N); the boot line's N is
#    cross-checked against the LIVE row counts (counters are read, not faked).
# ---------------------------------------------------------------------------
check_retention() {
  local line table want got
  line=$(grep -m1 '🧹 retention:' "$LOG" 2>/dev/null)
  [ -n "$line" ] || { bad "retention boot line absent"; return; }
  for pair in "decision_records:decision_records" "equity_snapshots:equity_snapshots" "nt8_order_snapshots:nt8_order_snapshots" "level_stats:level_stats"; do
    table=${pair%%:*}
    want=$(echo "$line" | grep -oE "$table=off\(keep all, rows=[0-9]+\)" | grep -oE '[0-9]+' | head -1)
    if [ -z "$want" ]; then bad "retention: $table not off(keep all) in boot line"; continue; fi
    got=$(sql "SELECT COUNT(*) FROM $table;")
    [ "$got" = "$want" ] && ok "retention $table off(keep all), live rows $got == boot $want" \
                          || bad "retention $table: boot says rows=$want, live COUNT=$got"
  done
}

# ---------------------------------------------------------------------------
# 3. JWT — the boot line fires UNCONDITIONALLY, so the real proof is .env:
#    a secret ≥24 chars that is NOT the shipped default.
# ---------------------------------------------------------------------------
check_jwt() {
  grep -q '🔑 JWT secret configured' "$LOG" 2>/dev/null || { bad "JWT boot line absent"; return; }
  local v
  v=$(grep -E '^JWT_SECRET=' "$VL_ENV" 2>/dev/null | cut -d= -f2-)
  if [ -z "$v" ]; then bad "JWT_SECRET unset in .env (default-jwt-secret in force)"; return; fi
  if [ "$v" = "default-jwt-secret-change-in-production" ]; then bad "JWT_SECRET is the shipped default"; return; fi
  [ ${#v} -ge 24 ] && ok "JWT secret set, length ${#v}" || bad "JWT_SECRET shorter than 24 chars"
}

# ---------------------------------------------------------------------------
# 4. transport — the per-trader line names the resolved transport; the CSV
#    fallback WARN must not exist on the live boot (NT_TRANSPORT=tcp).
# ---------------------------------------------------------------------------
check_transport() {
  grep -q 'Using NinjaTrader (transport via NT_TRANSPORT env' "$LOG" 2>/dev/null \
    || { bad "transport boot line absent"; return; }
  if grep -q '⚠️ transport:' "$LOG" 2>/dev/null; then bad "transport WARN present (CSV fallback): $(grep -m1 '⚠️ transport:' "$LOG")"; else ok "transport line present, no CSV-fallback WARN"; fi
  grep -qE '^NT_TRANSPORT=tcp' "$VL_ENV" 2>/dev/null && ok "NT_TRANSPORT=tcp in .env" \
    || bad "NT_TRANSPORT=tcp not found in .env"
}

# ---------------------------------------------------------------------------
# 5. fast-market reasoning — the wire must match the .env-declared level
#    (default max when unset, per the owner's MAX rule). A read at any other
#    level is a mismatch FAIL.
# ---------------------------------------------------------------------------
check_fast_market_reasoning() {
  local expected lines bad
  expected=$(grep -m1 -E '^FAST_MARKET_REASONING=' "$VL_ENV" 2>/dev/null | tail -1 | cut -d= -f2-)
  [ -z "$expected" ] && expected=max
  lines=$(grep 'planner mode: fast-market' "$LOG" 2>/dev/null)
  if [ -z "$lines" ]; then
    ok "no fast-market read yet — nothing to mismatch (env FAST_MARKET_REASONING=$expected)"
    return
  fi
  bad=0
  while IFS= read -r l; do
    if echo "$l" | grep -q "downgraded to $expected"; then
      :
    else
      bad=1
      echo "  mismatch: ${l:0:160}"
    fi
  done <<<"$lines"
  [ "$bad" = "0" ] && ok "every fast-market read at the .env level ($expected)" \
    || bad "a fast-market read ran at a level other than FAST_MARKET_REASONING=$expected"
}

# ---------------------------------------------------------------------------
# 6. death-reread retry — mechanism line on (never off) + the per-strategy
#    death_reread_retry_min knob as stored (nil = wake_min_interval_min).
# ---------------------------------------------------------------------------
check_death_reread() {
  grep -q '🧬 death→reread=on' "$LOG" 2>/dev/null || { bad "death→reread not on in boot"; return; }
  ok "death→reread=on"
  local rows
  rows=$(sql "SELECT id, config FROM strategies WHERE config IS NOT NULL AND config != '';")
  if [ -z "$rows" ]; then bad "no strategy rows to read death_reread_retry_min from"; return; fi
  echo "$rows" | while IFS='|' read -r id cfg; do
    v=$(echo "$cfg" | python3 -c 'import json,sys
try:
  d=json.load(sys.stdin).get("day_plan") or {}
  print(d.get("death_reread_retry_min","<nil>"))
except Exception:
  print("<unparsable>")' 2>/dev/null)
    echo "INFO strategy $id day_plan.death_reread_retry_min=$v (nil = wake_min_interval_min)"
  done
}

# ---------------------------------------------------------------------------
# 7. busy_timeout — the shipped binary must embed the per-connection DSN
#    pragma (#246 P1-B). A PRAGMA run on one connection only does not prove
#    the pool; the binary carrying the DSN suffix does.
# ---------------------------------------------------------------------------
check_busy_timeout() {
  if strings -n 8 "$VL_BIN" 2>/dev/null | grep -qE '_pragma=busy_timeout\(5000\)|_busy_timeout=5000'; then
    ok "binary carries the per-connection busy_timeout DSN"
  else
    bad "binary does not embed the busy_timeout DSN (or strings unavailable)"
  fi
}

# ---------------------------------------------------------------------------
# 8. first planner read at the .env-declared level — the expected level is
#    READ from AI_PLAN_REASONING in .env (default max when unset, per the
#    owner's MAX rule). PASS = the first post-boot planner wire matches it;
#    FAIL only on a mismatch (the owner's HIGH A/B from this weekend boot
#    passes when .env says high).
# ---------------------------------------------------------------------------
check_planner_reasoning() {
  local expected first
  expected=$(grep -m1 -E '^AI_PLAN_REASONING=' "$VL_ENV" 2>/dev/null | tail -1 | cut -d= -f2-)
  [ -z "$expected" ] && expected=max
  first=$(grep -m1 '🧠 planner call (reasoning=' "$LOG" 2>/dev/null)
  [ -n "$first" ] || { bad "no planner call line found"; return; }
  echo "$first" | grep -q "reasoning=$expected" \
    && ok "first planner call matches AI_PLAN_REASONING=$expected" \
    || bad "first planner call does NOT match AI_PLAN_REASONING=$expected: ${first:0:140}"
}

# ---------------------------------------------------------------------------
# 8b. duplicate reasoning keys in .env — WARN only (the bot reads the LAST
#     occurrence; multiple keys are a footgun, not a boot defect).
# ---------------------------------------------------------------------------
check_reasoning_env_dupes() {
  local k n
  for k in AI_PLAN_REASONING FAST_MARKET_REASONING AI_EXEC_REASONING; do
    n=$(grep -cE "^$k=" "$VL_ENV" 2>/dev/null || true)
    if [ -n "$n" ] && [ "$n" -gt 1 ]; then
      echo "WARN $k appears ${n}× in .env — the LAST line wins for the bot; remove the duplicates"
    fi
  done
}

# ---------------------------------------------------------------------------
# 9. born-dead salvage counters — boot counter == live event count
#    (counters record; never infer).
# ---------------------------------------------------------------------------
check_salvage_counters() {
  local line want got
  line=$(grep -m1 'plan liveness:' "$LOG" 2>/dev/null)
  [ -n "$line" ] || { bad "plan liveness boot line absent"; return; }
  want=$(echo "$line" | grep -oE 'born-dead dropped=[0-9]+' | grep -oE '[0-9]+' | head -1)
  [ -n "$want" ] || { bad "born-dead dropped counter absent from boot line"; return; }
  got=$(sql "SELECT COUNT(*) FROM system_config WHERE key LIKE 'plan_liveness_event:born_dead_dropped:%';")
  [ "$got" = "$want" ] && ok "born-dead dropped counter boot $want == live $got" \
    || bad "born-dead dropped: boot says $want, live COUNT=$got"
}

# ---------------------------------------------------------------------------
# 10. no ERROR lines — the boot and the trading day must be clean; any [ERRO]
#     is printed (a human must look).
# ---------------------------------------------------------------------------
check_no_errors() {
  local n
  n=$(grep -c '\[ERRO\]' "$LOG" 2>/dev/null || true)
  if [ "$n" = "0" ]; then ok "zero [ERRO] lines"; else
    bad "$n [ERRO] line(s):"
    grep '\[ERRO\]' "$LOG" | head -5 | sed 's/^/    /'
  fi
}

# ---------------------------------------------------------------------------
# 11. no `_ =` regressions — the regex-counted baseline is documented in
#     deploy/POSTBOOT-CHECK.md (432 at dev 35a53d29). An increase fails.
# ---------------------------------------------------------------------------
check_underscore_eq() {
  local n
  n=$(git -C "$VL_REPO" grep -E '_ = [a-zA-Z_]' -- '*.go' ':!*_test.go' 2>/dev/null | wc -l | tr -d ' ')
  if [ -n "$n" ] && [ "$n" -le "$UNDERSCORE_EQ_BASELINE" ]; then ok "_ = hits $n ≤ baseline $UNDERSCORE_EQ_BASELINE"; else
    bad "_ = hits $n > baseline $UNDERSCORE_EQ_BASELINE"
  fi
}

# ---------------------------------------------------------------------------
# --selfcheck: every needle the kit greps for must exist in the repo sources,
# so a renamed/removed boot line fails the kit, not the boot.
# ---------------------------------------------------------------------------
selfcheck() {
  local f=0
  while IFS='|' read -r needle where; do
    [ -z "$needle" ] && continue
    read -ra paths <<<"$where"
    if git -C "$KIT_REPO" grep -qF -- "$needle" -- "${paths[@]}" 2>/dev/null; then
      echo "SELFCHECK OK   $needle"
    else
      echo "SELFCHECK FAIL $needle (expected in $where)"
      f=$((f+1))
    fi
  done <<'NEEDLES'
🎯 stop-entry:|trader/*.go
🧹 retention:|main.go internal/retention/*.go
🔑 JWT secret configured|main.go
transport via NT_TRANSPORT env|trader/*.go
⚠️ transport:|trader/ninjatrader/*.go
🧠 planner call (reasoning=|trader/*.go
planner mode: fast-market|trader/*.go
🧬 death→reread=on|trader/*.go
🧬 plan lifecycle:|trader/*.go
born-dead dropped=|trader/*.go
_pragma=busy_timeout(5000)|store/sqlitedriver/*.go
NEEDLES
  exit $f
}

[ "${1:-}" = "--selfcheck" ] && selfcheck

LOG=${1:-}
if [ -z "$LOG" ]; then
  LOG=$(ls -t "$VL_DATA"/vl_*.log 2>/dev/null | head -1)
fi
if [ -z "$LOG" ] || [ ! -r "$LOG" ]; then
  echo "no readable log (pass the path; looked in $VL_DATA)" >&2
  exit 2
fi
echo "== postboot-check against $LOG"
for t in sqlite3 strings python3 git; do command -v "$t" >/dev/null || echo "WARN $t missing — related checks will FAIL"; done

check_stop_entry
check_retention
check_jwt
check_transport
check_fast_market_reasoning
check_death_reread
check_busy_timeout
check_planner_reasoning
check_reasoning_env_dupes
check_salvage_counters
check_no_errors
check_underscore_eq

echo "== $pass PASS, $fail FAIL"
exit $fail
