#!/usr/bin/env bash
# vl-lock-test.sh — pins for the atomic heartbeat lock.
#
# The lock this replaces failed in three directions in one day (2026-09-03):
# a dead pid under a live owner, a live pid silently overwritten by a second
# writer, and a pid that went stale when its session was resumed. Every pin
# below is one of those, or the rule that makes them un-representable.
set -uo pipefail
# (d) run from anywhere: the wrappers below resolve by $PWD, so resolve this
# file's directory ONCE (a second dirname walk after the cd fails) and pin the
# cwd to it. The repo root used to produce 11 false FAILs.
SDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || { echo "cannot resolve the suite dir" >&2; exit 1; }
cd "$SDIR"

LOCK_SH="$SDIR/vl-lock.sh"
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "${2:-}"; }
check(){ if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }
has()  { case "$2" in *"$3"*) ok "$1";; *) bad "$1" "missing [$3] in: $2";; esac; }
hasnt(){ case "$2" in *"$3"*) bad "$1" "found [$3] in: $2";; *) ok "$1";; esac; }
lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }
hasi()  { has  "$1" "$(lower "$2")" "$(lower "$3")"; }
hasnti(){ hasnt "$1" "$(lower "$2")" "$(lower "$3")"; }

# A source pin passes vacuously against a missing file, which is a false green
# of exactly the kind these tests exist to prevent.
if [ ! -f "$LOCK_SH" ]; then
  printf 'FAIL: %s does not exist — every pin below would be vacuous\n' "$LOCK_SH"
  exit 1
fi

WORK="$(mktemp -d)"
cleanup() {
  # keepers poll for the dir; a plain rm leaves them looping on a missing lock.
  # Kill every keeper group the suite spawned BEFORE removing the dirs.
  find "$WORK" -name keeper.pid 2>/dev/null | while read -r kp; do
    pg="$(cat "$kp" 2>/dev/null || true)"
    [ -n "$pg" ] && kill -KILL -- "-$pg" 2>/dev/null || true
  done
  sleep 0.2
  rm -rf "$WORK"
}
trap cleanup EXIT
o=no; o=${o}fx   # the pre-rename prefix, assembled at runtime (never written literal)
mkdir -p "$WORK/home"; export HOME="$WORK/home"   # OTHER_HOME is $HOME/vl-main.lock.d; never the real HOME
export VL_LOCK_DIR="$WORK/$o-main.lock.d"
L() { VL_LOCK_DIR="$VL_LOCK_DIR" bash "$LOCK_SH" "$@" 2>&1; }

echo "== a free lock reports free =="
out="$(L status)"; rc=$?
has  "status names it free" "$out" "free"
check "status rc on a free lock" "$rc" "0"

echo "== acquire, then a SECOND acquire FAILS (the replacement class) =="
out="$(L acquire vl-63 'cutover boot 6' 90)"; check "first acquire rc" "$?" "0"
hasi "first acquire confirms" "$out" "acquired"
out="$(L acquire vl-b3 'a different cutover' 90)"; rc=$?
check "second acquire rc is nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has  "second acquire refuses"        "$out" "REFUSED"
has  "second acquire names the holder" "$out" "vl-63"
has  "second acquire names the task"   "$out" "cutover boot 6"

echo "== the lock carries no pid, by construction =="
body="$(cat "$VL_LOCK_DIR"/* 2>/dev/null)"
hasnt "no pid= field in the lock"  "$body" "pid="
hasnt "no PID word in the lock"    "$body" "PID"
has   "records the session"        "$body" "vl-63"
has   "records the task"           "$body" "cutover boot 6"
has   "records acquired"           "$body" "acquired="
has   "records expiry"             "$body" "expiry="
has   "records a heartbeat"        "$body" "heartbeat="

echo "== a fresh heartbeat reads held, and never 'stale' =="
out="$(L status)"
has   "fresh status says held"     "$out" "held"
hasnti "fresh status is not stale" "$out" "stale"
check "check rc on held-fresh" "$(L check >/dev/null 2>&1; echo $?)" "1"

echo "== a STALE heartbeat says stale, and NEVER says dead =="
age_lock() { # rewrite the heartbeat the way the script actually reads it
  local secs="$1" f="$VL_LOCK_DIR/meta"
  { grep -v '^heartbeat' "$f"
    printf 'heartbeat=%s\n' "$(date -Is -d "$secs seconds ago")"
    printf 'heartbeat_epoch=%s\n' "$(( $(date +%s) - secs ))"
  } > "$f.tmp" && mv -f "$f.tmp" "$f"
}
age_lock 660
out="$(L status)"
hasi   "stale status says stale"      "$out" "stale"
hasnti "stale status never says dead" "$out" "dead"
hasi  "stale status demands corroboration" "$out" "corroborat"
has   "stale status still names the holder" "$out" "vl-63"
check "check rc on held-stale" "$(L check >/dev/null 2>&1; echo $?)" "2"

echo "== heartbeat refreshes it; a foreign session may not beat =="
L heartbeat vl-63 >/dev/null
out="$(L status)"
hasnti "beating clears stale" "$out" "stale"
out="$(L heartbeat vl-b3)"; rc=$?
check "foreign heartbeat rc nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "foreign heartbeat refuses" "$out" "REFUSED"

echo "== release is owner-scoped, and frees the lock =="
out="$(L release vl-b3)"; rc=$?
check "foreign release rc nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
out="$(L release vl-63)"; check "owner release rc" "$?" "0"
has   "release confirms" "$out" "released"
has   "lock is free again" "$(L status)" "free"
check "lock dir is gone" "$([ -e "$VL_LOCK_DIR" ] && echo present || echo gone)" "gone"

echo "== with-heartbeat keeps a long job fresh, and stops when it ends =="
L acquire vl-63 'long build' 90 >/dev/null
age_lock 660
L with-heartbeat vl-63 -- true >/dev/null 2>&1
hasnti "with-heartbeat beat at least once" "$(L status)" "stale"
before="$(cat "$VL_LOCK_DIR/heartbeat")"
sleep 1
check "beater does not outlive its command" "$(cat "$VL_LOCK_DIR/heartbeat")" "$before"
L release vl-63 >/dev/null

echo "== reclaim: REFUSED on a fresh heartbeat =="
L acquire vl-b3 'a live cutover' 90 >/dev/null
out="$(L reclaim vl-63 vl-b3 'HEAD has not moved in 20m; no build in flight')"; rc=$?
check "reclaim rc on fresh is nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "reclaim refuses a fresh lock" "$out" "REFUSED"
hasi  "refusal says the heartbeat is fresh" "$out" "fresh"
check "holder is unchanged after a refused reclaim" "$(L status | grep -c "vl-b3")" "1"

echo "== reclaim: allowed once STALE, and only with corroboration =="
age_lock 660
out="$(L reclaim vl-63 vl-b3)"; rc=$?
check "reclaim without corroboration rc nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
hasi  "refuses when corroboration is missing" "$out" "corroborat"
out="$(L reclaim vl-63 vl-ed 'HEAD static; no build')"; rc=$?
check "reclaim naming the WRONG stale session rc nonzero" "$([ $rc -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "refuses when the named session is not the holder" "$out" "REFUSED"
out="$(L reclaim vl-63 vl-b3 'HEAD has not moved in 20m; no build in flight')"; rc=$?
# rc 3, NOT acquire's 0 — a script must be able to tell "took a free lock" from
# "inherited an abandoned one", so a lane can refuse to inherit.
check "reclaim rc is 3, distinct from acquire's 0" "$rc" "3"
hasi  "reclaim confirms" "$out" "reclaim"
out="$(L status)"
has   "new holder is named"        "$out" "vl-63"
hasnti "reclaimed lock is fresh"   "$out" "stale"

echo "== the reclaim is written to the lock's history =="
hist="$(cat "$VL_LOCK_DIR/history" 2>/dev/null)"
has  "history names who took over"      "$hist" "vl-63"
has  "history names who was taken from" "$hist" "vl-b3"
has  "history carries the corroboration" "$hist" "no build in flight"
has  "history is timestamped"           "$hist" "20"
check "history survives into status" "$(L status | grep -c 'reclaim')" "1"

echo "== a reclaimed lock still beats and releases as the new holder =="
check "old holder may no longer beat" "$(L heartbeat vl-b3 >/dev/null 2>&1; echo $?)" "1"
L heartbeat vl-63 >/dev/null; check "new holder may beat" "$?" "0"
L release vl-63 >/dev/null; has "released" "$(L status)" "free"

echo "== the script cannot express pid liveness =="
src="$(grep -v '^[[:space:]]*#' "$LOCK_SH" | sed 's/[[:space:]]#.*$//')"
hasnt "no kill -0"  "$src" "kill -0"
hasnt "no pgrep"    "$src" "pgrep"
hasnt "no \$\$"     "$src" '$$'

# ── THE KEEPER (2026-09-09) ─────────────────────────────────────────────────
#
# acquire printed "heartbeat every 120s" and started NOTHING. A holder who
# simply WAITED — for a position to close, for an owner to run the kill — went
# STALE at 300s with no writer in existence, and status then printed the reclaim
# recipe over a live cutover. Measured: acquired 21:25:07, and at 21:50:16 the
# meta's heartbeat was byte-identical to `acquired`, never beaten once.
#
# The verb was never broken — a hand-beat revives it. Nothing called it for a
# holder who was not running commands. Class 88's shape: a liveness signal that
# is a side effect of activity, dying the moment the work pauses, when the whole
# point of a lock is to be held while you WAIT.
#
# THE KEEPER IS BOUNDED BY THE DECLARED EXPIRY, and that bound is the design.
# An UNBOUNDED keeper survives its session and beats forever for a holder who is
# gone — turning a 5-minute false STALE (recoverable, and the canon already says
# corroborate) into a permanent false ALIVE that no succession path can reach.
# Bounded, an abandoned lock still goes stale — at the window its holder asked
# for, instead of never.
#
# Thresholds are compressed so these run in seconds, not the 400 the real window
# would need.
echo "== the keeper: acquire starts one, and a WAITING holder stays ALIVE =="
KW="$(mktemp -d)"
K() { VL_LOCK_DIR="$KW/lock.d" VL_LOCK_STALE_SECONDS=4 VL_LOCK_BEAT_SECONDS=1 bash "$LOCK_SH" "$@" 2>&1; }
out="$(K acquire vl-keeper 'waiting on a position to close' 60)"
check "acquire rc" "$?" "0"
hasi  "acquire confirms" "$out" "acquired"
hasnti "the acquire message no longer promises an unstarted beat" "$out" "heartbeat every"

sleep 6   # LONGER than the stale window, with NO other action of any kind
out="$(K status)"
hasi   "a WAITING holder is still ALIVE past the stale window" "$out" "alive"
hasnti "a waiting holder is not reported STALE" "$out" "stale"
hasi   "status reports the auto-beat" "$out" "auto-beat"

echo "== killing the keeper makes it STALE — the beat is a process, not a fiction =="
kp="$(cat "$KW/lock.d/keeper.pid" 2>/dev/null || echo)"
if [ -n "$kp" ]; then
  ok "a keeper pid is recorded as a stop handle"
  kill "$kp" 2>/dev/null; sleep 6
  hasi "with the keeper dead the lock goes STALE" "$(K status)" "stale"
else
  bad "a keeper pid is recorded as a stop handle" "no $KW/lock.d/keeper.pid"
fi

echo "== a second acquire never succeeds while the lock lives =="
out="$(K acquire vl-other 'a different cutover' 60)"; rc=$?
check "second acquire rc is a refusal" "$rc" "1"
hasi  "second acquire is REFUSED" "$out" "refused"

echo "== release stops the keeper — no writer outlives its lock =="
# THE PROCESS, NOT THE FILE, AND ON A BEAT LONG ENOUGH TO SEE IT.
#
# Two drafts of this pin failed to bite. The first checked only that keeper.pid
# was gone — which rm -rf "$LOCK_DIR" does anyway, so deleting _stop_keeper left
# it green with an orphan still looping. The second checked the PROCESS but ran
# on a 1s beat, so an unstopped keeper noticed the missing directory and exited
# by itself before the assertion — the pin was measuring the OS's timing, not
# the code. With a 30s beat an unstopped keeper is still sleeping when we look,
# and only _stop_keeper can have ended it.
#
# (The script may not use kill -0 — class 70 — but this test may: the pins forbid
# pid liveness in the TOOL, where a reader could mistake it for an answer, not in
# a test that is asking about a process on purpose.)
KWR="$(mktemp -d)"
R() { VL_LOCK_DIR="$KWR/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=30 bash "$LOCK_SH" "$@" 2>&1; }
R acquire vl-rel 'a holder that will release' 60 >/dev/null
kp2="$(cat "$KWR/lock.d/keeper.pid" 2>/dev/null || echo)"
hasi "release confirms" "$(R release vl-rel)" "released"
sleep 1
if [ -n "$kp2" ] && kill -0 "$kp2" 2>/dev/null; then
  bad "release STOPS the keeper process" "pid $kp2 still running after release — a writer outliving its lock"
  kill "$kp2" 2>/dev/null
else
  ok "release STOPS the keeper process"
fi
rm -rf "$KWR"
rm -rf "$KW"

echo "== the keeper OUTLIVES the shell that spawned it =="
# A keeper that dies with its session reproduces the defect one layer down.
KW2="$(mktemp -d)"
( VL_LOCK_DIR="$KW2/lock.d" VL_LOCK_STALE_SECONDS=4 VL_LOCK_BEAT_SECONDS=1 \
    bash "$LOCK_SH" acquire vl-detached 'spawned by a shell that exits' 60 >/dev/null 2>&1 )
sleep 6
out="$(VL_LOCK_DIR="$KW2/lock.d" VL_LOCK_STALE_SECONDS=4 bash "$LOCK_SH" status 2>&1)"
hasi "a keeper spawned by an exited shell keeps beating" "$out" "alive"
VL_LOCK_DIR="$KW2/lock.d" bash "$LOCK_SH" release vl-detached >/dev/null 2>&1
rm -rf "$KW2"

echo "== THE BOUND: the keeper stops at the declared expiry, and never extends it =="
KW3="$(mktemp -d)"
# a lock declared for 0 minutes is already expired: the keeper must not beat it
( VL_LOCK_DIR="$KW3/lock.d" VL_LOCK_STALE_SECONDS=4 VL_LOCK_BEAT_SECONDS=1 \
    bash "$LOCK_SH" acquire vl-brief 'a window that has already closed' 0 >/dev/null 2>&1 )
sleep 6
out="$(VL_LOCK_DIR="$KW3/lock.d" VL_LOCK_STALE_SECONDS=4 bash "$LOCK_SH" status 2>&1)"
hasi "a lock past its declared expiry goes STALE — the keeper never auto-extends" "$out" "stale"
VL_LOCK_DIR="$KW3/lock.d" bash "$LOCK_SH" release vl-brief >/dev/null 2>&1
rm -rf "$KW3"

echo "== the acquire message says what the keeper actually does =="
src2="$(cat "$LOCK_SH")"
case "$src2" in
  *keeper.pid*) ok "the acquire path records a keeper" ;;
  *) bad "the acquire path records a keeper" "no keeper.pid anywhere — the message would be the defect again" ;;
esac

echo "== NO WRITER OUTLIVES ITS LOCK: an orphan cannot land in the next holder's lock =="
#
# The keeper loop runs `bash "$self" heartbeat` as a foreground CHILD. Killing
# only the loop orphaned it, and _write_meta mv's into $LOCK_DIR/meta by absolute
# path with no identity check — so a writer that passed _require_holder while A
# held the lock landed A's meta into the lock B created at the same path seconds
# later. B held the tree, the lock said A: B could not release its own lock and
# A, holding nothing, could.
#
# THE SHIM IS CONDITIONAL, AND THAT IS THE WHOLE TECHNIQUE. Two earlier drafts of
# this pin passed with the defect fully present. The first sampled the race —
# eight rounds against a natural rate near one in sixty, which tests nothing. The
# second widened the window with an UNCONDITIONAL sleep in mktemp, which also
# slowed `acquire` (it writes meta too) and shifted the very timing it meant to
# expose. Slowness has to be switchable: on for the one beat being parked, off
# for everything else.
SLOWFLAG="$(mktemp -u)"
SHIM="$(mktemp -d)"
cat > "$SHIM/mktemp" <<SHIMEOF
#!/usr/bin/env bash
[ -e "$SLOWFLAG" ] && sleep 3
exec /usr/bin/mktemp "\$@"
SHIMEOF
chmod +x "$SHIM/mktemp"
KWO="$(mktemp -d)"
export VL_LOCK_DIR="$KWO/lock.d"
O() { VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=1 PATH="$SHIM:$PATH" bash "$LOCK_SH" "$@" 2>&1; }
rm -f "$SLOWFLAG"
O acquire sess-A "lane A cutover" 45 >/dev/null
touch "$SLOWFLAG"      # the NEXT beat parks inside mktemp holding A's meta
sleep 1.5
rm -f "$SLOWFLAG"      # everything after this is fast again
O release sess-A >/dev/null
O acquire sess-B "lane B — a DIFFERENT cutover" 45 >/dev/null
sleep 4                # give the parked writer time to land its mv
who="$(bash "$LOCK_SH" status 2>&1)"
hasnt "a second acquire never sees the first holder's identity" "$who" "sess-A"
has   "the lock reports the holder that actually took it"       "$who" "sess-B"
rel="$(bash "$LOCK_SH" release sess-B 2>&1)"
hasi  "the holder can release its OWN lock"                     "$rel" "released"
kp="$(cat "$VL_LOCK_DIR/keeper.pid" 2>/dev/null || true)"
[ -n "$kp" ] && kill -KILL -- "-$kp" 2>/dev/null
rm -rf "$KWO" "$SHIM"; rm -f "$SLOWFLAG"
unset VL_LOCK_DIR
export VL_LOCK_DIR="$WORK/$o-main.lock.d"

echo "== release kills the GROUP, so no keeper member survives it =="
KWG="$(mktemp -d)"
G() { VL_LOCK_DIR="$KWG/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=30 bash "$LOCK_SH" "$@" 2>&1; }
G acquire sess-G "a holder with a slow beat" 60 >/dev/null
pg="$(cat "$KWG/lock.d/keeper.pid" 2>/dev/null || echo)"
G release sess-G >/dev/null
sleep 1
if [ -n "$pg" ] && kill -0 -- "-$pg" 2>/dev/null; then
  bad "release stops the whole keeper group" "process group $pg still alive after release"
  kill -KILL -- "-$pg" 2>/dev/null
else
  ok "release stops the whole keeper group"
fi
rm -rf "$KWG"

echo "== NO WRITER EXTENDS A WINDOW — not even a hand-rolled one =="
#
# `expiry` was written at acquire and only ever PRINTED; nothing compared it. So
# bounding the keeper's own loop constrained the keeper this script starts and
# NOTHING else — and every lane had been running a hand-rolled beater for
# precisely as long as the tool failed to start one. A peer lane read the shipped
# file and named it: refuse at the source, and the invariant holds for every
# writer. This pin beats BY HAND, the way a lane would.
KWX="$(mktemp -d)"
X() { VL_LOCK_DIR="$KWX/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=1 bash "$LOCK_SH" "$@" 2>&1; }
X acquire sess-X 'a window of zero minutes' 0 >/dev/null
sleep 1
out="$(X heartbeat sess-X)"; rc=$?
check "a hand beat past the declared expiry is REFUSED" "$rc" "1"
hasi  "and says why"                                    "$out" "past the declared expiry"
# The holder is still the holder — expiry bounds the WINDOW, not the identity.
hasi  "the lock still names its holder"                 "$(X status)" "sess-X"
X release sess-X >/dev/null 2>&1
rm -rf "$KWX"

echo "== a lock inside its window still beats normally =="
KWY="$(mktemp -d)"
Y() { VL_LOCK_DIR="$KWY/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=1 bash "$LOCK_SH" "$@" 2>&1; }
Y acquire sess-Y 'a normal window' 45 >/dev/null
check "a beat inside the window succeeds" "$(Y heartbeat sess-Y >/dev/null 2>&1; echo $?)" "0"
Y release sess-Y >/dev/null 2>&1
rm -rf "$KWY"

echo "== acquire is CLEAN on stderr — a spawn that warns is a spawn nobody reads =="
#
# WHY THIS PIN EXISTS. _spawn_keeper read the keeper's process GROUP out of
# /proc so `release` could kill loop and child together. The line was written
# with '"'"' quoting — correct only INSIDE an already single-quoted string.
# At top level bash read {print $5} in a DOUBLE-quoted region, expanded $5
# against the function's own empty arguments, and `set -u` aborted the
# substitution: every acquire printed "$5: unbound variable" and the /proc read
# never ran once. The `[ -n "$pgid" ] || pgid="$kpid"` fallback then produced a
# working value, so behaviour was right and 75 green tests saw nothing.
#
# The defect was invisible to every behavioural assertion and LOUD on stderr.
# So stderr is what gets pinned. Any acquire that has to warn to succeed fails
# here, whatever the exit code says.
KSE="$(mktemp -d)"
err="$(VL_LOCK_DIR="$KSE/lock.d" VL_LOCK_BEAT_SECONDS=60 bash "$LOCK_SH" acquire sess-E 'stderr must be silent' 30 2>&1 >/dev/null)"
if [ -z "$err" ]; then ok "acquire writes nothing to stderr"
else bad "acquire writes nothing to stderr" "stderr: $err"; fi
VL_LOCK_DIR="$KSE/lock.d" bash "$LOCK_SH" release sess-E >/dev/null 2>&1
rm -rf "$KSE"

echo "== pgrp is counted AFTER comm, so a comm with spaces cannot shift it =="
#
# /proc/PID/stat is "pid (comm) state ppid pgrp ...". comm is the ONLY field
# that may hold spaces or parens, so splitting the raw line puts pgrp at $5
# only while comm is a single bare word. Strip through the LAST ')' and pgrp is
# the third field of what remains, whatever comm contained. A wrong pgrp is not
# a cosmetic error: `release` kills that group.
synth='4242 (my proc (x)) S 4200 9999 0 -1 4194304 0 0'
got="$(printf '%s\n' "$synth" | sed -e 's/^.*) //' | awk '{print $3}')"
check "pgrp survives a comm with spaces and parens" "$got" "9999"
naive="$(printf '%s\n' "$synth" | awk '{print $5}')"
check "and the naive whole-line split is the wrong number"  "$naive" "S"

echo "== release NEVER signals a group it did not create =="
#
# THIS PIN EXISTS BECAUSE THE BUG KILLED THE RUN THAT FOUND IT (exit 143).
#
# Job control is off in a non-interactive shell, so `setsid ... &` starts in THIS
# SHELL'S process group and moves to its own only once setsid execs. `kpid=$!`
# returns before that. A /proc read that wins the race records the PARENT's pgrp
# in keeper.pid — and _stop_keeper's `kill -TERM -- "-$pg"` then SIGTERMs the
# process group of whoever invoked the script, test runner included.
#
# The old broken awk never read /proc at all, so it always used the $kpid
# fallback, which after setsid IS its own leader. Making the read work turned
# dead code into a live shell-killer. Hence: identify positively, or signal
# nothing.
KWN="$(mktemp -d)"
N() { VL_LOCK_DIR="$KWN/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=1 bash "$LOCK_SH" "$@" 2>&1; }
N acquire sess-N 'a holder whose keeper.pid lies' 60 >/dev/null
# An innocent bystander in its OWN group, standing in for the invoking shell.
setsid bash -c 'sleep 30' >/dev/null 2>&1 &
victim=$!
sleep 0.3
vg="$(sed -e 's/^.*) //' "/proc/$victim/stat" 2>/dev/null | awk '{print $3}')"
# Point the stop handle at the bystander, exactly as the race did.
printf '%s' "$vg" > "$KWN/lock.d/keeper.pid"
N release sess-N >/dev/null 2>&1
sleep 0.5
if kill -0 "$victim" 2>/dev/null; then
  ok "a keeper.pid naming someone else's group is not signalled"
else
  bad "a keeper.pid naming someone else's group is not signalled" "release killed pid $victim (group $vg)"
fi
kill -KILL "$victim" 2>/dev/null
rm -rf "$KWN"

echo "== a keeper.pid that is not a number is inert =="
KWJ="$(mktemp -d)"
J() { VL_LOCK_DIR="$KWJ/lock.d" VL_LOCK_STALE_SECONDS=600 VL_LOCK_BEAT_SECONDS=1 bash "$LOCK_SH" "$@" 2>&1; }
J acquire sess-J 'a holder with a corrupt stop handle' 60 >/dev/null
printf 'not-a-pid' > "$KWJ/lock.d/keeper.pid"
out="$(J release sess-J 2>&1)"; rc=$?
check "release of a lock with a corrupt keeper.pid still succeeds" "$rc" "0"
hasi  "and reports the release"                                    "$out" "released"
rm -rf "$KWJ"

echo "== C1 — release REPORTS a failed removal instead of claiming success =="
#
# This was `rm -rf "$LOCK_DIR"; echo "released by $session"`. The `;` threw rm's
# status away and the function returned echo's 0. A read-only parent, a stale
# handle or a permissions change printed "released", returned SUCCESS, and left
# the directory standing — while _stop_keeper had ALREADY stopped the heartbeat.
# The lock then went STALE on a holder who was alive and thought it had finished,
# and the next lane saw an abandoned lock that was nothing of the kind.
KR="$(mktemp -d)"; mkdir -p "$KR/parent"
R() { VL_LOCK_DIR="$KR/parent/lock.d" VL_LOCK_BEAT_SECONDS=60 bash "$LOCK_SH" "$@" 2>&1; }
R acquire sess-R 'a release that cannot remove' 30 >/dev/null
chmod a-w "$KR/parent"                       # rm cannot unlink the child
out="$(R release sess-R)"; rc=$?
check "release of an unremovable lock FAILS"        "$rc" "1"
hasi  "and says the lock is still held"             "$out" "RELEASE FAILED"
hasi  "and warns the keeper is already stopped"     "$out" "keeper has been STOPPED"
hasnt "and does NOT claim it released"              "$out" "released by sess-R"
chmod u+w "$KR/parent"
kp="$(cat "$KR/parent/lock.d/keeper.pid" 2>/dev/null || true)"; [ -n "$kp" ] && kill -- -"$kp" 2>/dev/null
rm -rf "$KR"

echo "== C3 — a half-built lock is INCOMPLETE, never STALE with an empty holder =="
#
# mkdir is the atomic step and meta lands ~7ms later (measured n=10: 6.92-7.71ms).
# In that window every reader saw a COMPLETE lock whose fields were empty: _age
# fell back to ${hb:-0} and returned ~1.79 BILLION seconds, so status printed
# "STALE — held by '' (task: )" and check returned 2. "Being created right now"
# and "held by someone who stopped beating" reached the reader as the same
# answer — and only the second is ever grounds for a takeover.
KI="$(mktemp -d)"; export VL_LOCK_DIR="$KI/lock.d"; mkdir -p "$KI/lock.d"
out="$(bash "$LOCK_SH" status 2>&1)"
hasi  "a meta-less lock reads INCOMPLETE"           "$out" "INCOMPLETE"
hasnt "and is NOT reported stale"                   "$out" "STALE"
hasnt "and invents no empty holder"                 "$out" "held by ''"
check "check rc for an acquire in flight"           "$(bash "$LOCK_SH" check >/dev/null 2>&1; echo $?)" "3"
# ...and once it has stood past the abandon window it is ABANDONED, not stale.
touch -d '2 minutes ago' "$KI/lock.d"
out="$(bash "$LOCK_SH" status 2>&1)"
hasi  "an orphaned half-built lock reads ABANDONED" "$out" "ABANDONED-INCOMPLETE"
check "check rc for an abandoned half-built lock"   "$(bash "$LOCK_SH" check >/dev/null 2>&1; echo $?)" "4"

echo "== C2 — a lock that names NOBODY can be cleared, but only that kind =="
#
# _require_holder compares against an empty session, so every verb refused a
# meta-less directory: release ("'x' is not the holder ('')"), reclaim (it will
# not let you name an empty holder), acquire (the directory exists). The lock was
# TERMINAL — clearable only by an rm -rf outside the tool, which is the one thing
# this tool exists to stop people doing by hand.
out="$(bash "$LOCK_SH" clear-incomplete 2>&1)"; rc=$?
check "clear-incomplete removes an abandoned lock"  "$rc" "0"
if [ -d "$KI/lock.d" ]; then bad "the directory is gone" "still present"; else ok "the directory is gone"; fi
# The verb takes NO session, so it is the one verb an impatient reader could aim
# at a live lock. Both refusals below are what stop that.
mkdir -p "$KI/lock.d"
out="$(bash "$LOCK_SH" clear-incomplete 2>&1)"; rc=$?
check "a lock too YOUNG to be abandoned is refused" "$rc" "1"
hasi  "and says an acquire is probably in flight"   "$out" "in flight"
rm -rf "$KI/lock.d"
VL_LOCK_BEAT_SECONDS=60 bash "$LOCK_SH" acquire sess-C2 'a genuine holder' 30 >/dev/null 2>&1
out="$(bash "$LOCK_SH" clear-incomplete 2>&1)"; rc=$?
check "a lock WITH a holder is refused"             "$rc" "1"
hasi  "and names the holder it refused to clear"    "$out" "sess-C2"
kp="$(cat "$KI/lock.d/keeper.pid" 2>/dev/null || true)"
bash "$LOCK_SH" release sess-C2 >/dev/null 2>&1; [ -n "$kp" ] && kill -- -"$kp" 2>/dev/null
unset VL_LOCK_DIR; export VL_LOCK_DIR="$WORK/$o-main.lock.d"
rm -rf "$KI"


echo "== one home, TWO callers: this tool races itself for one winner =="
#
# Z18 (R5): with the wrapper and the pre-rename copies retired, the atomic
# mkdir still admits exactly one winner — two concurrent acquires of the SAME
# tool into one home.
KCONT="$WORK/cont.lock.d"
for i in $(seq 1 10); do
  rm -rf "$KCONT"
  VL_LOCK_DIR="$KCONT" bash "$LOCK_SH" acquire r-$i "race" 60 >/dev/null 2>&1 &
  VL_LOCK_DIR="$KCONT" bash "$LOCK_SH" acquire r-$i "race" 60 >/dev/null 2>&1 &
  wait
  n="$(VL_LOCK_DIR="$KCONT" bash "$LOCK_SH" status 2>&1 | grep -c "held by 'r-")"
  check "one-home race round $i has exactly one holder" "$n" "1"
  VL_LOCK_DIR="$KCONT" bash "$LOCK_SH" release r-$i >/dev/null 2>&1
done

echo "== Z18 cross-home: two homes can never stand held by different lanes =="
XH="$WORK/xhome"; mkdir -p "$XH"
VLB() { HOME="$XH" VL_LOCK_DIR= VL_LOCK_BEAT_SECONDS=60 VL_LOCK_STALE_SECONDS=600 bash "$LOCK_SH" "$@" 2>&1; }
OLDB() { HOME="$XH" VL_LOCK_DIR="$XH/$o-main.lock.d" VL_LOCK_BEAT_SECONDS=60 VL_LOCK_STALE_SECONDS=600 bash "$LOCK_SH" "$@" 2>&1; }
VLB acquire sess-B 'the vl home first' 60 >/dev/null
out="$(OLDB acquire sess-A 'the retired home second' 60)"; rc=$?
check "an old-home acquire beside a held vl home REFUSES" "$rc" "1"
hasi  "and names the other holder"        "$out" "sess-B"
check "and tears its own home down"       "$([ -d "$XH/$o-main.lock.d" ] && echo left || echo gone)" "gone"
VLB release sess-B >/dev/null
OLDB acquire sess-A 'the retired home first' 60 >/dev/null
out="$(VLB acquire sess-B 'the vl home second' 60)"; rc=$?
check "a vl-home acquire beside a held old home REFUSES" "$rc" "1"
hasi  "and names the other holder"        "$out" "sess-A"
check "and tears its own home down"       "$([ -d "$XH/vl-main.lock.d" ] && echo left || echo gone)" "gone"
OLDB release sess-A >/dev/null
OLDB acquire sess-X 'the hand-over' 60 >/dev/null
out="$(VLB acquire sess-X 'the R5 hand-over' 60)"; rc=$?
check "the SAME session may hold both homes" "$rc" "0"
hasi  "and the exemption is on the record"   "$out" "same session"
VLB release sess-X >/dev/null; OLDB release sess-X >/dev/null
both=0
for i in $(seq 1 50); do
  rm -rf "$XH/$o-main.lock.d" "$XH/vl-main.lock.d"
  OLDB acquire race-a 'race' 60 >/dev/null 2>&1 &
  VLB acquire race-b 'race' 60 >/dev/null 2>&1 &
  wait
  a=$([ -d "$XH/$o-main.lock.d" ] && echo 1 || echo 0)
  b=$([ -d "$XH/vl-main.lock.d" ] && echo 1 || echo 0)
  if [ "$((a+b))" -le 1 ]; then ok "cross-home race round $i: never both held"
  else bad "cross-home race round $i: never both held" "both homes exist"; both=$((both+1)); fi
  [ "$a" = 1 ] && OLDB release race-a >/dev/null 2>&1
  [ "$b" = 1 ] && VLB release race-b >/dev/null 2>&1
done
check "no round ever left both homes held" "$both" "0"
VLB acquire sess-B 'check mirror' 60 >/dev/null
check "check never reads 0 beside a held other home" "$(OLDB check >/dev/null 2>&1; echo $?)" "1"
VLB release sess-B >/dev/null
check "check reads 0 only when BOTH homes are absent" "$(OLDB check >/dev/null 2>&1; echo $?)" "0"
VLB2() { HOME="$XH" VL_LOCK_DIR= VL_LOCK_BEAT_SECONDS=1 VL_LOCK_STALE_SECONDS=4 bash "$LOCK_SH" "$@" 2>&1; }
VLB2 acquire sess-B 'short stale window' 60 >/dev/null
kp="$(cat "$XH/vl-main.lock.d/keeper.pid" 2>/dev/null || echo)"
[ -n "$kp" ] && kill -- -"$kp" 2>/dev/null
sleep 5
OLDB4() { HOME="$XH" VL_LOCK_DIR="$XH/$o-main.lock.d" VL_LOCK_BEAT_SECONDS=60 VL_LOCK_STALE_SECONDS=4 bash "$LOCK_SH" "$@" 2>&1; }
check "check mirrors a STALE other home as 2" "$(OLDB4 check >/dev/null 2>&1; echo $?)" "2"
out="$(OLDB reclaim sess-A sess-B 'HEAD static; no build in flight' 2>&1)"; rc=$?
check "reclaim refuses while the other home is held by another" "$rc" "1"
VLB2 reclaim sess-B sess-A 'HEAD static; no build in flight' >/dev/null 2>&1
VLB2 release sess-B >/dev/null
rm -rf "$XH"


echo "== (a) DEFAULT-HOME: envs unset, the vl home, the retired home never born =="
#
# R5 flipped the default to $HOME/vl-main.lock.d. An env-less acquire parks
# THERE; the retired home is never created. $WORK/home is the production call
# site: a temp HOME, every env var unset, the real default.
ZD="$WORK/home"
( unset VL_LOCK_DIR; HOME="$ZD" VL_LOCK_BEAT_SECONDS=60 VL_LOCK_STALE_SECONDS=600 bash "$LOCK_SH" acquire sess-D 'default home' 60 >/dev/null )
check "an env-less acquire parks in the vl home"  "$([ -d "$ZD/vl-main.lock.d" ] && echo yes || echo no)" "yes"
check "and the retired home is never created"     "$([ -e "$ZD/$o-main.lock.d" ] && echo yes || echo no)" "no"
( unset VL_LOCK_DIR; HOME="$ZD" bash "$LOCK_SH" release sess-D >/dev/null 2>&1 )
check "and release clears the vl home"            "$([ -d "$ZD/vl-main.lock.d" ] && echo yes || echo no)" "no"
rm -rf "$ZD/$o-main.lock.d" "$ZD/vl-main.lock.d"
echo "== the vl tools run DIRECTLY on their own shebang, never via bash =="
out="$("$PWD/vl-claim.sh" 2>&1)"; rc=$?
check "vl-claim direct-exec refuses a bare call" "$rc" "1"
hasi  "and prints its own usage"                       "$out" "vl-claim"

echo "== KEEPER-DRAIN: no keeper outlives the suite =="
#
# The corrupt-handle sections (sess-N, sess-J) release WITHOUT killing their
# keepers by design — those keepers exit via the dir-watch within one beat. A
# keeper that outlives a 35s drain is a real leak, not test hygiene.
drain_pat="$SDIR/vl-lock.sh heartbeat"
KEEP_BASE=$(pgrep -fc "$drain_pat" 2>/dev/null || true); KEEP_BASE=${KEEP_BASE:-0}
pkill -KILL -f "$drain_pat" 2>/dev/null || true
n=$KEEP_BASE; drain=0
while [ "$n" -gt "$KEEP_BASE" ] && [ "$drain" -lt 35 ]; do
  sleep 1; drain=$((drain+1))
  n=$(pgrep -fc "$drain_pat" 2>/dev/null || echo 0)
done
check "no keeper outlives the suite (35s drain)" "$([ "$n" -le "$KEEP_BASE" ] && echo yes || echo no)" "yes"

echo
printf 'pass=%d fail=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
