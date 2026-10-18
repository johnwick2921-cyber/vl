#!/usr/bin/env bash
# check-exposure_test.sh — pins for deploy/friends/check-exposure.sh
# (W-FRIEND-HOSTED, S2 tooling, DS-106). Drives the PRODUCTION script against
# synthetic listeners bound by this test; it never touches a real service.
#
# Fixtures:
#   E1 a wildcard (0.0.0.0) bind is a FAIL
#   E2 a loopback bind is ok
#   E3 a missing listener is a FAIL
#   E4 probe mode reports reachable for a live port
#   E5 probe mode reports unreachable for a closed port
set -uo pipefail

CHECK="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-exposure.sh"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "${2:-}"; }
has() { case "$2" in *"$3"*) ok "$1";; *) bad "$1" "missing [$3] in: $2";; esac; }

[ -f "$CHECK" ] || { printf 'FAIL: %s missing\n' "$CHECK"; exit 1; }

WORK="$(mktemp -d -p /tmp)"
trap 'rm -rf "${WORK:?}"' EXIT

# pick ports nobody is listening on
WILD=24981
LOOP=24982
CLOSED=24983

pids=""
cleanup() {
  for p in $pids; do kill "$p" 2>/dev/null || true; done
}
trap 'cleanup' EXIT

# start_listener ADDR PORT — a background python TCP listener; returns its pid
# via the global $pids, and waits until it is actually listening.
start_listener() {
  local addr="$1" port="$2"
  python3 -c 'import socket,sys,time
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
s.bind((sys.argv[1],int(sys.argv[2]))); s.listen(1); time.sleep(60)' "$addr" "$port" &
  local pid=$!
  pids="$pids $pid"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if ss -ltnH "sport = :$port" 2>/dev/null | grep -q .; then return 0; fi
    sleep 0.1
  done
  return 0
}

# E1: a wildcard bind must FAIL and name the all-interfaces address.
start_listener 0.0.0.0 "$WILD"
out="$(bash "$CHECK" --ports "$WILD" 2>&1)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then ok "E1 wildcard bind exits nonzero"; else bad "E1 wildcard bind exits nonzero" "rc=$rc"; fi
has "E1 wildcard bind names 0.0.0.0" "$out" "0.0.0.0"

# E2: a loopback bind is ok.
start_listener 127.0.0.1 "$LOOP"
out="$(bash "$CHECK" --ports "$LOOP" 2>&1)" && rc=0 || rc=$?
if [ "$rc" -eq 0 ]; then ok "E2 loopback bind exits 0"; else bad "E2 loopback bind exits 0" "rc=$rc out=$out"; fi
has "E2 loopback bind named ok" "$out" "ok:"

# E3: a port nobody listens on is a FAIL ("not listening").
out="$(bash "$CHECK" --ports "$CLOSED" 2>&1)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then ok "E3 missing listener exits nonzero"; else bad "E3 missing listener exits nonzero" "rc=$rc"; fi
has "E3 missing listener says not listening" "$out" "not listening"

# E4/E5: probe mode prints reachable / unreachable.
out="$(bash "$CHECK" --probe 127.0.0.1 --ports "$LOOP,$CLOSED" 2>&1)" || true
has "E4 probe reachable"   "$out" "reachable:   tcp 127.0.0.1:$LOOP"
has "E5 probe unreachable" "$out" "unreachable: tcp 127.0.0.1:$CLOSED"

printf '%s\n' "check-exposure_test.sh: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
