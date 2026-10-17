#!/usr/bin/env bash
# nofx-lock — the main-tree lock, atomic and heartbeat-based.
#
# WHY THIS SHAPE (owner ruling 2026-09-03). The old lock was ONE FLAT FILE
# written with `>`, carrying a PID, read with `kill -0`. It failed in THREE
# directions in a single day, and not one was caught by the file — every one was
# caught by a peer asking:
#
#  1. DEAD PID, LIVE OWNER. Agents recorded the shell's own id, but every tool
#     call is a fresh shell, so it was dead within a second while its owner
#     worked on. A peer found an unexpired lock that did not resolve and
#     correctly did NOT clear it.
#  2. LIVE PID, SILENTLY REPLACED. `>` truncates: a second acquirer clobbered an
#     active cutover's lock with no error and no trace. A write that clobbers is
#     not a lock.
#  3. STALE PID AFTER RESUME. A resumed session wrote a lock naming its own
#     former, now-dead process.
#
# The fault under all three: a process id answers "does some process exist",
# which was never the question. The question is "is the OWNER still working",
# and only the owner can answer it.
#
# So: acquisition is an ATOMIC CREATE (mkdir succeeds for exactly one caller and
# fails for every other — no read-then-write anywhere, so (2) is not detected
# but UNREPRESENTABLE), identity is the SESSION NAME rather than a process id,
# and liveness is a HEARTBEAT the holder rewrites every 2 minutes. There is no
# pid field, so (1) and (3) have nothing to record wrongly.
#
# A heartbeat older than 5 minutes is reported STALE, NEVER DEAD. Corroboration
# (HEAD moving, a build in flight, the named session answering) remains required
# before any clearing, and this script never clears a lock itself at any age.
#
# Usage:
#   nofx-lock acquire <session> <task> [minutes]   # atomic; refuses if held.
#                                                  # STARTS A KEEPER that beats
#                                                  # until the [minutes] window
#                                                  # you declared, then STOPS.
#   nofx-lock heartbeat <session>                  # holder rewrites; every 2m
#   nofx-lock status                               # free | held | STALE + age
#   nofx-lock check                                # rc 0 free · 1 held · 2 stale
#                                                  #    · 3 incomplete (acquire
#                                                  #      in flight) · 4 abandoned
#                                                  #      -incomplete (names nobody)
#   nofx-lock with-heartbeat <session> -- <cmd>    # beats for <cmd>'s lifetime
#   nofx-lock reclaim <new> <stale> "<corroboration>"  # ONLY on a stale heartbeat
#   nofx-lock release <session>                    # holder only; STOPS the keeper
#   nofx-lock clear-incomplete                     # removes a lock that names
#                                                  # NOBODY and has stood past
#                                                  # the abandon window; refuses
#                                                  # any lock that has meta
#
# THE KEEPER NEVER EXTENDS YOUR WINDOW. It beats until the expiry you asked for
# at acquire and then stops, so the lock goes STALE at the window you declared
# rather than never. A holder who needs longer RE-ACQUIRES OR EXTENDS
# EXPLICITLY — there is no path by which simply staying busy keeps the tree.
# That bound is deliberate: an unbounded keeper survives its own session and
# beats forever for a holder who is gone, turning a 5-minute false STALE
# (recoverable, and corroboration is required anyway) into a permanent false
# ALIVE that no succession path can reach.
set -uo pipefail

LOCK_DIR="${NOFX_LOCK_DIR:-$HOME/nofx-main.lock.d}"
LEGACY_LOCK="${NOFX_LEGACY_LOCK:-$HOME/nofx-main.lock}"
HEARTBEAT_STALE_SECONDS="${NOFX_LOCK_STALE_SECONDS:-300}"   # 5 min
HEARTBEAT_EVERY_SECONDS="${NOFX_LOCK_BEAT_SECONDS:-120}"     # 2 min
# How long a lock directory may exist with NO meta before it is ABANDONED rather
# than merely being born. `acquire` fills meta ~7ms after mkdir (measured, n=10:
# min 6.92ms, mean 7.35ms, max 7.71ms), so 30s is ~4000x the observed window —
# wide enough that a loaded machine cannot cross it, narrow enough that a lock
# orphaned mid-creation clears within the minute.
INCOMPLETE_ABANDON_SECONDS="${NOFX_LOCK_INCOMPLETE_SECONDS:-30}"

_now()      { date -Is; }
_epoch()    { date +%s; }
_meta()     { cat "$LOCK_DIR/meta" 2>/dev/null; }
_field()    { _meta | grep -m1 "^$1=" | cut -d= -f2-; }

# The meta file is replaced, never edited in place: a reader must never catch a
# half-written heartbeat. mktemp rather than the shell's own id — this file does
# not name processes, and a pin enforces that.
_write_meta() {
  local tmp; tmp="$(mktemp "$LOCK_DIR/.meta.XXXXXX")" || return 1
  cat > "$tmp" && mv -f "$tmp" "$LOCK_DIR/meta"
}

_require_holder() { # _require_holder <session>
  local want="$1" have; have="$(_field session)"
  if [ "$want" != "$have" ]; then
    echo "REFUSED — '$want' is not the holder ('$have', task: $(_field task))."
    return 1
  fi
}

cmd_acquire() {
  local session="${1:?session required}" task="${2:?task required}" mins="${3:-45}"
  # THE ATOMIC STEP. mkdir either creates the directory or fails; it never
  # overwrites. Exactly one caller can win, whatever the interleaving.
  if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    echo "REFUSED — lock already held:"; cmd_status; return 1
  fi
  _write_meta <<META
session=$session
task=$task
acquired=$(_now)
expiry=$(date -Is -d "+$mins minutes")
expiry_epoch=$(date +%s -d "+$mins minutes")
heartbeat=$(_now)
heartbeat_epoch=$(_epoch)
META
  _spawn_keeper "$session"
  echo "ACQUIRED by $session — a keeper is beating every ${HEARTBEAT_EVERY_SECONDS}s until $(_field expiry), then it STOPS; stale after ${HEARTBEAT_STALE_SECONDS}s. It never extends the window: need longer, re-acquire or extend explicitly."
}

# THE KEEPER (2026-09-09). Until today acquire PRINTED "heartbeat every 120s" and
# started nothing: a holder who simply WAITED — for a position to close, for an
# owner to run the kill — went stale at 300s with no writer in existence, and
# status printed the reclaim recipe over a live cutover. The verb was never
# broken; nothing called it for a holder who was not running commands. Class 88:
# a liveness signal that is a side effect of activity, dying exactly when the
# work pauses — when the whole point of a lock is to be held while you wait.
#
# BOUNDED BY THE DECLARED EXPIRY, AND THAT BOUND IS THE DESIGN. An unbounded
# keeper survives its session and beats forever for a holder who is gone, turning
# a 5-minute false STALE — recoverable, and the canon already says corroborate —
# into a permanent false ALIVE no succession path can reach. Bounded, an
# abandoned lock still goes stale: at the window its holder asked for, not never.
#
# THE PID IS A STOP HANDLE, NEVER LIVENESS (class 70). It lives in its own file,
# never in meta, and nothing reads it to decide whether the holder is working —
# that is still the heartbeat, and only the heartbeat. release uses it to stop
# the process it started, so no writer outlives its lock.
_spawn_keeper() {
  local session="$1" self="${BASH_SOURCE[0]}" exp kpid pgid
  exp="$(_field expiry_epoch)"
  [ -n "$exp" ] || return 0
  # setsid makes the keeper a SESSION AND GROUP LEADER, so the group contains
  # exactly the keeper loop and whatever it spawns — which is what release has
  # to be able to end as one thing. See _stop_keeper for why.
  setsid nohup bash -c '
    lock="$1"; sess="$2"; exp="$3"; every="$4"; self="$5"
    why=""
    while [ -d "$lock" ]; do
      if [ "$(date +%s)" -ge "$exp" ]; then why="ended at the declared expiry"; break; fi
      NOFX_LOCK_DIR="$lock" bash "$self" heartbeat "$sess" >/dev/null 2>&1 || { why="stopped: this session is no longer the holder"; break; }
      sleep "$every"
    done
    # WHY IT STOPPED, RECORDED. A keeper that exits ON PURPOSE at the window its
    # holder declared leaves a lock that goes stale — which reads identically to
    # the defect this keeper was built to fix unless the tool says otherwise.
    [ -n "$why" ] && [ -d "$lock" ] && printf %s "$why" > "$lock/keeper.ended" 2>/dev/null
    rm -f "$lock/keeper.pid" 2>/dev/null
  ' _ "$LOCK_DIR" "$session" "$exp" "$HEARTBEAT_EVERY_SECONDS" "$self" >/dev/null 2>&1 &
  kpid=$!
  # The GROUP id, read from /proc rather than assumed: setsid forks in some
  # shells and execs in others, so $! is not reliably the leader.
  #
  # THE FIELD IS COUNTED AFTER comm, NOT FROM THE START OF THE LINE. /proc/PID/stat
  # is "pid (comm) state ppid pgrp ...", and comm is the only field that can hold
  # spaces or parens. Splitting the raw line puts pgrp at $5 only while comm is a
  # single bare word; strip through the LAST ')' first and pgrp is unambiguously
  # the third field of what remains.
  #
  # This line previously carried '"'"' quoting — the form for embedding a quote
  # inside an ALREADY single-quoted string. Here it is at top level, so bash read
  # {print $5} in a DOUBLE-quoted region, expanded $5 against the function's own
  # (empty) arguments, and `set -u` aborted the substitution: every acquire printed
  # "line 143: $5: unbound variable" to stderr and the /proc read never once ran.
  # The fallback below silently covered it, which is why 75 green tests missed it.
  #
  # AND THE READ MUST WAIT FOR setsid, OR IT RECORDS **OUR OWN** GROUP.
  # Job control is off in a non-interactive shell, so a background job does NOT
  # get its own process group — it starts in THIS SHELL'S. `setsid` moves it only
  # once it execs. `kpid=$!` returns before that, so a /proc read that wins the
  # race returns the PARENT's pgrp — and _stop_keeper would then send SIGTERM to
  # the process group of whoever invoked this script. That is not theoretical:
  # it killed the test run that found it, exit 143.
  #
  # So the value is accepted ONLY once it is its own group LEADER (pgrp == pid),
  # which is true exactly when setsid has completed and is impossible for a
  # value borrowed from our parent. If that never happens the keeper is still in
  # our group and must be stopped as a lone PID, never as a group; we record it
  # and let _stop_keeper make that call from the same evidence.
  pgid=""
  _kw=0
  while [ "$_kw" -lt 50 ]; do
    _cand="$(sed -e 's/^.*) //' "/proc/$kpid/stat" 2>/dev/null | awk '{print $3}')"
    if [ -n "$_cand" ] && [ "$_cand" = "$kpid" ]; then pgid="$_cand"; break; fi
    [ -d "/proc/$kpid" ] || break
    sleep 0.1; _kw=$((_kw+1))
  done
  [ -n "$pgid" ] || pgid="$kpid"
  echo "$pgid" > "$LOCK_DIR/keeper.pid"
  disown 2>/dev/null || true
}

# _keeper_owns — is $1 a live process belonging to THIS lock's keeper?
# Identification is POSITIVE: the cmdline must name this lock directory. A pid
# that is gone, recycled, or someone else's fails, and its caller signals nothing.
_keeper_owns() {
  local p="$1"
  [ -n "$p" ] && [ -r "/proc/$p/cmdline" ] || return 1
  tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null | grep -qF -- "$LOCK_DIR"
}

# _is_group_leader — does $1 name a process GROUP, or merely a process?
# pgrp == pid is true only for a leader. Every other number, signalled with
# `kill -- -N`, hits a group we did not create.
_is_group_leader() {
  local p="$1" pg
  [ -r "/proc/$p/stat" ] || return 1
  pg="$(sed -e 's/^.*) //' "/proc/$p/stat" 2>/dev/null | awk '{print $3}')"
  [ -n "$pg" ] && [ "$pg" = "$p" ]
}

# _stop_keeper ends the process GROUP this script started, and WAITS for it.
#
# THE DEFECT THIS REPLACES, found by an adversarial pass before this ever
# shipped: it killed only the keeper LOOP. The loop runs `bash "$self" heartbeat`
# as a foreground CHILD, so killing the parent ORPHANED that child — and
# _write_meta mv's into "$LOCK_DIR/meta" by absolute path with no identity check.
# An orphan that had already passed _require_holder while A held the lock landed
# A's meta into the lock B created at the same path seconds later. B then held
# the tree while the lock said A: B could not release its own lock, and A —
# holding nothing — could, freeing the tree under a live cutover. The atomic mv
# is what made it silent; the wrong content landed whole, never torn.
#
# That is failure (2) from this file's own header — a write that clobbers —
# rebuilt one layer down by the keeper wave. The group kill CLOSES the window
# rather than narrowing it: loop and child die together, and release never
# removes the directory while a writer could still be inside it.
#
# ON /proc: this is NOT class-70 pid liveness. It never answers "is the holder
# working" — that is the heartbeat, and only the heartbeat. It answers "has the
# process I just signalled finished dying", about a child this script started,
# bounded, and reachable from nowhere but here. status and check cannot see it.
_stop_keeper() {
  local pg i=0
  pg="$(cat "$LOCK_DIR/keeper.pid" 2>/dev/null || true)"
  rm -f "$LOCK_DIR/keeper.pid" "$LOCK_DIR/keeper.ended" 2>/dev/null
  [ -n "$pg" ] || return 0
  # A24 — UNKNOWN TAKES NO DESTRUCTIVE BRANCH. Everything below signals; nothing
  # below signals a target it has not positively identified as OUR keeper.
  case "$pg" in ''|*[!0-9]*) return 0 ;; esac
  _keeper_owns "$pg" || return 0
  # Group-kill ONLY a genuine group leader. A number that is not one names some
  # OTHER group — possibly this shell's — and `kill -- -N` would signal it whole.
  if _is_group_leader "$pg"; then _sig() { kill "-$1" -- "-$pg" 2>/dev/null; }
  else                           _sig() { kill "-$1" "$pg" 2>/dev/null; }; fi
  _sig TERM
  # BOUNDED WAIT, ~5s. Never rm under a live writer.
  while [ "$i" -lt 50 ]; do
    [ -d "/proc/$pg" ] || break
    sleep 0.1; i=$((i+1))
  done
  # Whatever is left in the group cannot execute another statement after this.
  [ -d "/proc/$pg" ] || return 0
  _sig KILL
  return 0
}

cmd_heartbeat() {
  local session="${1:?session required}"
  [ -d "$LOCK_DIR" ] || { echo "REFUSED — no lock to beat."; return 1; }
  _require_holder "$session" || return 1
  # THE WINDOW IS A BOUND, NOT A DISPLAY — enforced HERE, at the only place a
  # heartbeat can be written.
  #
  # Until now `expiry` was written at acquire and only ever PRINTED; nothing
  # compared it, so any writer could extend a lock forever. Bounding the keeper's
  # own loop was not enough: it constrains the keeper this script starts and
  # nothing else, and every lane on this machine has been running a hand-rolled
  # beater for exactly as long as the tool failed to start one. Refusing at the
  # source makes it an invariant — NO writer extends a window, hand-rolled or
  # not — and it terminates the keeper for free, because the loop breaks on the
  # same non-zero rc.
  #
  # A lock created before this change carries no expiry_epoch; it is left
  # unbounded rather than retroactively expired.
  local exp; exp="$(_field expiry_epoch)"
  if [ -n "$exp" ] && [ "$(date +%s)" -ge "$exp" ]; then
    echo "REFUSED — past the declared expiry ($(_field expiry)). No writer extends a window: re-acquire, or extend explicitly."
    return 1
  fi
  { _meta | grep -v '^heartbeat'; echo "heartbeat=$(_now)"; echo "heartbeat_epoch=$(_epoch)"; } | _write_meta
  echo "heartbeat $(_now)"
}

# The history lives in the lock dir and dies with the lock, so it is printed
# wherever the lock is inspected and once more at release — a succession chain
# that vanishes silently would defeat the point of recording it.
_show_history() {
  [ -s "$LOCK_DIR/history" ] || return 0
  echo "  history:"; sed 's/^/    /' "$LOCK_DIR/history"
}

_age() { local hb; hb="$(_field heartbeat_epoch)"; echo $(( $(_epoch) - ${hb:-0} )); }

# ── AN INCOMPLETE LOCK IS ITS OWN STATE ──────────────────────────────────────
# `mkdir` is the atomic step and meta arrives ~7ms later, so between the two the
# directory exists and describes nobody. Every reader used to treat that as a
# COMPLETE lock whose fields happened to be empty: _age fell back to
# ${hb:-0} and returned ~1.79 BILLION seconds, so status printed
# "STALE — held by '' (task: )" and check returned 2. Two different situations —
# "being created right now" and "held by someone who stopped beating" — arrived
# at the same reader as the same answer, and the second is the one that invites a
# takeover.
#
# Worse, it was TERMINAL. _require_holder compares against an empty session, so
# every verb refused: release ("'x' is not the holder ('')"), reclaim (it will
# not let you name an empty holder), acquire (the directory exists). A lock
# nobody can clear with the tool is a tree nobody can unlock without rm -rf,
# which is the one thing this tool exists to stop people doing by hand.
_has_meta() { [ -f "$LOCK_DIR/meta" ]; }

# Seconds since the DIRECTORY was created — the only clock available when there
# is no meta to read.
_dir_age() {
  local t; t="$(stat -c %Y "$LOCK_DIR" 2>/dev/null)" || { echo 0; return; }
  echo $(( $(_epoch) - ${t:-0} ))
}

cmd_status() {
  if [ -f "$LEGACY_LOCK" ]; then
    echo "LEGACY lock file present at $LEGACY_LOCK — a lane is still on the old shape:"
    sed 's/^/    /' "$LEGACY_LOCK"
  fi
  if [ ! -d "$LOCK_DIR" ]; then
    [ -f "$LEGACY_LOCK" ] && return 0
    echo "free"; return 0
  fi
  # A directory with no meta is BEING BORN or was ORPHANED mid-creation. It is
  # never "stale", because staleness is a statement about a holder and there is
  # no holder yet. Reported before the age branch, which cannot describe it.
  if ! _has_meta; then
    local dage; dage="$(_dir_age)"
    if [ "$dage" -ge "$INCOMPLETE_ABANDON_SECONDS" ]; then
      echo "ABANDONED-INCOMPLETE — '$LOCK_DIR' has existed ${dage}s with no meta (> ${INCOMPLETE_ABANDON_SECONDS}s)."
      echo "  An acquire created the directory and died before writing its identity, so this lock"
      echo "  names nobody and no verb can address it. Clear it with: nofx-lock clear-incomplete"
      return 4
    fi
    echo "INCOMPLETE — '$LOCK_DIR' exists but has no meta yet (${dage}s old); an acquire is in flight."
    echo "  This is NOT stale and NOT abandoned. Look again in a moment."
    return 3
  fi
  local age session task; age="$(_age)"; session="$(_field session)"; task="$(_field task)"
  if [ "$age" -gt "$HEARTBEAT_STALE_SECONDS" ]; then
    # STALE, NOT DEAD. The distinction is the whole point: a stale heartbeat
    # says the holder has not checked in, not that it has stopped. Corroborate
    # before clearing — HEAD moving, a build in flight, the session answering.
    echo "STALE — held by '$session' (task: $task), heartbeat ${age}s old (> ${HEARTBEAT_STALE_SECONDS}s), expiry $(_field expiry) · $(_auto_beat)."
    echo "  DO NOT CLEAR ON THIS ALONE. Corroborate first: is HEAD moving? is a build running?"
    echo "  does '$session' answer? Clear only with a note naming what you checked."
    echo "  To take it over on the record: nofx-lock reclaim <you> '$session' \"<what you checked>\""
    _show_history
    return 2
  fi
  echo "held by '$session' (task: $task), heartbeat ${age}s old — ALIVE, expiry $(_field expiry) · $(_auto_beat)"
  _show_history
}

# _auto_beat says whether acquire started a keeper, READ FROM THE FILE. It never
# probes a process: asking "is that pid alive" would make the pid the liveness
# answer, which is class 70 rebuilt. This distinguishes the two readings STALE
# could not tell apart — "the holder is gone" from "the tool never beat for a
# holder who was waiting" — which is the whole reason the keeper exists.
_auto_beat() {
  if [ -f "$LOCK_DIR/keeper.pid" ]; then
    echo "auto-beat: on (until $(_field expiry); it never extends the window)"
  elif [ -s "$LOCK_DIR/keeper.ended" ]; then
    # THE EXPIRY FIELD IS LOAD-BEARING NOW. It was written at acquire and only
    # ever printed; with a keeper bounded by it, a holder who UNDER-DECLARES
    # their window goes stale mid-cutover with the keeper having exited on
    # purpose. Silence there is indistinguishable from the defect this wave
    # fixed, so the reason is carried out loud.
    echo "auto-beat: ENDED — $(cat "$LOCK_DIR/keeper.ended" 2>/dev/null); re-acquire or extend explicitly"
  else
    echo "auto-beat: off — this holder must beat by hand"
  fi
}

# For scripts (the tree guard): 0 free · 1 held-fresh · 2 held-stale.
cmd_check() {
  [ -d "$LOCK_DIR" ] || { echo free; return 0; }
  # rc 3 and 4 are ADDITIVE — 0/1/2 keep the meanings the tree-guard spec was
  # written against. A caller that has not been taught the new codes still sees a
  # non-zero "not free", which is the safe reading; one that has can tell "an
  # acquire is in flight" from "held by someone who stopped beating", and only
  # the latter is ever grounds for a takeover.
  if ! _has_meta; then
    if [ "$(_dir_age)" -ge "$INCOMPLETE_ABANDON_SECONDS" ]; then echo abandoned-incomplete; return 4; fi
    echo incomplete; return 3
  fi
  if [ "$(_age)" -gt "$HEARTBEAT_STALE_SECONDS" ]; then echo stale; return 2; fi
  echo held; return 1
}

# clear-incomplete — the ONLY way to remove a lock that names nobody.
#
# It refuses a lock with meta (that one has a holder; use release or reclaim) and
# refuses one younger than the abandon threshold (that one is an acquire in
# flight, and taking it would be the class-70 replacement bug wearing a new hat).
# Both refusals matter: this verb needs no session and so is the one verb an
# impatient reader could point at a live lock.
cmd_clear_incomplete() {
  [ -d "$LOCK_DIR" ] || { echo "no lock"; return 0; }
  if _has_meta; then
    echo "REFUSED — this lock HAS meta and names a holder ('$(_field session)', task: $(_field task))."
    echo "  clear-incomplete only removes a lock that names nobody. Use release, or reclaim on the record."
    return 1
  fi
  local dage; dage="$(_dir_age)"
  if [ "$dage" -lt "$INCOMPLETE_ABANDON_SECONDS" ]; then
    echo "REFUSED — '$LOCK_DIR' is only ${dage}s old; an acquire completes in milliseconds, so this one is probably in flight."
    echo "  Wait until it is ${INCOMPLETE_ABANDON_SECONDS}s old and look again: if meta has appeared, it is a real lock."
    return 1
  fi
  if ! rm -rf "$LOCK_DIR" 2>/dev/null || [ -d "$LOCK_DIR" ]; then
    echo "CLEAR FAILED — '$LOCK_DIR' still exists after rm. Fix the cause and try again."
    return 1
  fi
  echo "cleared an incomplete lock that had stood ${dage}s with no meta"
}

# Beats while <cmd> runs and STOPS when it ends. The beater's lifetime is
# exactly the work's lifetime; a beater that outlived its job would report a
# gone owner as live, which is failure (1) in a new costume.
cmd_with_heartbeat() {
  local session="${1:?session required}"; shift
  [ "${1:-}" = "--" ] && shift
  [ -d "$LOCK_DIR" ] || { echo "REFUSED — no lock to beat."; return 1; }
  _require_holder "$session" || return 1
  cmd_heartbeat "$session" >/dev/null
  ( while sleep "$HEARTBEAT_EVERY_SECONDS"; do
      [ -d "$LOCK_DIR" ] || exit 0
      [ "$(_field session)" = "$session" ] || exit 0
      cmd_heartbeat "$session" >/dev/null
    done ) &
  local beater=$!
  "$@"; local rc=$?
  kill "$beater" 2>/dev/null; wait "$beater" 2>/dev/null
  return $rc
}

# reclaim — succession, on the record.
#
# The failure this exists for is INVISIBLE SUCCESSION: a lock whose owner is
# gone, taken over by someone else with nothing written down. So every reclaim
# APPENDS to the lock's history — who took it, from whom, when, and what they
# checked — and an unauditable reclaim is worse than none.
#
# It is refused while the heartbeat is FRESH, without exception. A reclaim that
# can take a live lock is replacement with better manners, which is the very
# failure the atomic create removed.
#
# rc 3 is deliberately distinct from acquire's rc 0: a script can tell "took a
# free lock" from "inherited an abandoned one", and a lane that would rather not
# inherit can refuse.
cmd_reclaim() { # reclaim <new_session> <stale_session> <corroboration...>
  local new="${1:?new session required}" stale="${2:-}" ; shift 2 2>/dev/null || true
  local why="${*:-}"
  [ -d "$LOCK_DIR" ] || { echo "REFUSED — no lock to reclaim (use acquire)."; return 1; }
  local holder age; holder="$(_field session)"; age="$(_age)"
  if [ -z "$stale" ] || [ "$stale" != "$holder" ]; then
    echo "REFUSED — name the session you are taking over. The holder is '$holder', you named '${stale:-<nothing>}'."
    return 1
  fi
  if [ "$age" -le "$HEARTBEAT_STALE_SECONDS" ]; then
    echo "REFUSED — '$holder' has a FRESH heartbeat (${age}s old, stale is > ${HEARTBEAT_STALE_SECONDS}s)."
    echo "  A live holder is never reclaimable. Wait, or ask '$holder' to release."
    return 1
  fi
  if [ -z "$why" ]; then
    echo "REFUSED — state the corroboration you checked (HEAD not moving, no build in flight, session not answering)."
    echo "  usage: nofx-lock reclaim <new> <stale> \"<what you checked>\""
    return 1
  fi
  printf '%s reclaim: %s took over from %s (heartbeat was %ss old) — corroboration: %s\n' \
    "$(_now)" "$new" "$stale" "$age" "$why" >> "$LOCK_DIR/history"
  { _meta | grep -vE '^(session|heartbeat)'
    echo "session=$new"
    echo "heartbeat=$(_now)"
    echo "heartbeat_epoch=$(_epoch)"
  } | _write_meta
  echo "RECLAIMED by $new from $stale — logged to the lock history."
  return 3
}

cmd_release() {
  local session="${1:?session required}"
  [ -d "$LOCK_DIR" ] || { echo "no lock"; return 0; }
  _require_holder "$session" || return 1
  _show_history
  _stop_keeper
  # THE REMOVAL IS THE RELEASE. This was `rm -rf "$LOCK_DIR"; echo "released"`,
  # and the `;` threw rm's exit status away: the function returned echo's 0. A
  # read-only parent, a stale NFS handle or a permissions change printed
  # "released by <session>", returned SUCCESS, and left the directory standing —
  # while _stop_keeper above had ALREADY stopped the heartbeat. The lock then
  # went STALE on a holder who was alive and believed it had finished, and the
  # next lane to look saw an abandoned lock that was nothing of the kind.
  #
  # So: rm's status is checked, AND the directory is confirmed gone, because a
  # zero exit is a claim and the absent directory is the fact.
  if ! rm -rf "$LOCK_DIR" 2>/dev/null || [ -d "$LOCK_DIR" ]; then
    echo "RELEASE FAILED — '$LOCK_DIR' still exists after rm."
    echo "  The lock is STILL HELD by $session and its keeper has been STOPPED,"
    echo "  so it will read STALE within ${HEARTBEAT_STALE_SECONDS}s though you are still here."
    echo "  Fix the cause (permissions on the parent? read-only mount?) and release again."
    return 1
  fi
  echo "released by $session"
}

case "${1:-status}" in
  acquire)        shift; cmd_acquire "$@" ;;
  heartbeat)      shift; cmd_heartbeat "$@" ;;
  status)         cmd_status ;;
  check)          cmd_check ;;
  with-heartbeat) shift; cmd_with_heartbeat "$@" ;;
  reclaim)        shift; cmd_reclaim "$@" ;;
  release)        shift; cmd_release "$@" ;;
  clear-incomplete) cmd_clear_incomplete ;;
  *) echo "usage: nofx-lock {acquire <session> <task> [mins]|heartbeat <session>|status|check|with-heartbeat <session> -- <cmd>|reclaim <new> <stale> \"<corroboration>\"|release <session>|clear-incomplete}"; exit 64 ;;
esac
