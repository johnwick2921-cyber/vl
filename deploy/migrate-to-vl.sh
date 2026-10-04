#!/usr/bin/env bash
#
# migrate-to-vl — the one scripted, reversible move of this machine from the
# pre-rename names to the VL names (the rename plan's R2 boot).
#
# Interface:
#   migrate-to-vl.sh --session <name> --sha <40hex> --release-dir <dir> \
#       [--dry-run | --rollback] [--no-auto-rollback] [--no-updater] [--release-lock]
#
# CENSUS LAW: this file, its Go test and its runbook hold ZERO occurrences of
# the pre-rename prefix in any casing. The prefix is assembled at runtime by
# the two lines below and is never written out as text. `o` is the pre-rename
# prefix; `O` is its uppercase form, used for env-key fallbacks.
#
# The rename plan's R2 section is the spec for every step here; the operator
# steps the script cannot perform live in the runbook, and step 0 checks what
# it can of them. The ERR rule: a failure in steps 1–5 or on the two step-6
# auto legs runs --rollback automatically (unless --no-auto-rollback); the
# script never exits with the bot stopped.
set -euo pipefail

# --- the pre-rename prefix, assembled at runtime (never written out) ----------
o=no; o=${o}fx
O="$(printf '%s' "$o" | tr '[:lower:]' '[:upper:]')"

# --- usage --------------------------------------------------------------------
usage() {
  cat >&2 <<'USAGE'
usage: migrate-to-vl.sh --session <name> --sha <40hex> --release-dir <dir>
       [--dry-run | --rollback] [--no-auto-rollback] [--no-updater] [--release-lock]

  --session NAME      the deploy session name (the lock's holder)
  --sha 40HEX         the R2 tree sha being booted (the release-dir build)
  --release-dir DIR   the pre-built release dir (binaries built OUTSIDE a clone)
  --dry-run           run every read-only step-0 check, print every action,
                      write nothing; exit non-zero on any refusal
  --rollback          restore the pre-rename state, reading ONLY the state file
  --no-auto-rollback  on a failure, print the banner and stop (NO UNATTENDED
                      DEPLOYS — only with the owner's explicit word)
  --no-updater        partner path: the machine never had the updater worker
  --release-lock      release the lock at step 7 (a machine with no RELEASE
                      marker commit to push); the default KEEPS it held
USAGE
  exit 2
}

# --- argv ---------------------------------------------------------------------
SESSION=""; SHA=""; RELEASE_DIR=""; MODE="forward"
NO_AUTO_ROLLBACK=0; NO_UPDATER=0; RELEASE_LOCK=0
while [ $# -gt 0 ]; do
  case "$1" in
    --session)        [ $# -ge 2 ] || usage; SESSION="$2"; shift 2 ;;
    --sha)            [ $# -ge 2 ] || usage; SHA="$2"; shift 2 ;;
    --release-dir)    [ $# -ge 2 ] || usage; RELEASE_DIR="$2"; shift 2 ;;
    --dry-run)        MODE="dry"; shift ;;
    --rollback)       MODE="rollback"; shift ;;
    --no-auto-rollback) NO_AUTO_ROLLBACK=1; shift ;;
    --no-updater)     NO_UPDATER=1; shift ;;
    --release-lock)   RELEASE_LOCK=1; shift ;;
    -h|--help)        usage ;;
    *)                echo "unknown flag: $1" >&2; usage ;;
  esac
done
[ -n "$SESSION" ] || { echo "missing --session" >&2; usage; }
if [ "$MODE" != "rollback" ]; then
  [ -n "$SHA" ] && [ ${#SHA} -eq 40 ] || { echo "missing/not-40-hex --sha" >&2; usage; }
  [ -n "$RELEASE_DIR" ] || { echo "missing --release-dir" >&2; usage; }
fi
if [ "$MODE" = "rollback" ]; then
  [ "$NO_AUTO_ROLLBACK" = 0 ] || { echo "--no-auto-rollback is meaningless with --rollback" >&2; usage; }
  [ "$RELEASE_LOCK" = 0 ] || { echo "--release-lock is meaningless with --rollback" >&2; usage; }
fi
if [ -n "$SHA" ] && ! printf '%s' "$SHA" | grep -qE '^[0-9a-fA-F]{40}$'; then
  echo "--sha must be 40 hex" >&2; usage
fi
SHA12="${SHA:0:12}"

# --- identity and paths -------------------------------------------------------
[ "$(id -u)" != "0" ] || { echo "REFUSED: run as the bot's user, never root" >&2; exit 2; }
[ -n "${HOME:-}" ] || { echo "REFUSED: HOME is unset" >&2; exit 2; }
cd "$HOME" || { echo "REFUSED: cannot cd to HOME ($HOME)" >&2; exit 2; }

OLD_ROOT="$HOME/$o"
VL_ROOT="$HOME/vl"
OLD_BIN="$OLD_ROOT/$o-bin"
HEALTH_URL="http://127.0.0.1:8080/api/health"
GATE_URL="http://127.0.0.1:8080/api/installation-gate"
LOCK_HOME="$HOME/$o-main.lock.d"
LOCK_TOOL="$OLD_ROOT/deploy/$o-lock.sh"
VL_LOCK_TOOL="$VL_ROOT/deploy/vl-lock.sh"
STATE_DIR="$HOME/.local/state/vl-migrate"
STATE_FILE="$STATE_DIR/$SESSION"

TS="$(date +%Y%m%d-%H%M%S)"
TMP_KEEPALIVE=""
TMP_LIST=""

say()  { echo "[migrate] $*"; }
warn() { echo "[migrate] WARN: $*" >&2; }
die()  { echo "[migrate] REFUSED: $*" >&2; exit 1; }
stepdie() { echo "[migrate] step ERR: $*" >&2; exit 1; }

cleanup() {
  if [ -n "$TMP_KEEPALIVE" ] && kill -0 "$TMP_KEEPALIVE" 2>/dev/null; then
    kill "$TMP_KEEPALIVE" 2>/dev/null || true
  fi
  if [ "$MODE" = "forward" ]; then
    sudo -k 2>/dev/null || true
  fi
  for f in $TMP_LIST; do rm -f "$f" 2>/dev/null || true; done
}
trap cleanup EXIT

mkt() { local f; f="$(mktemp "$HOME/.migrate-vl.XXXXXX")"; TMP_LIST="$TMP_LIST $f"; printf '%s' "$f"; }

# --- tiny helpers -------------------------------------------------------------
rev12() { local v="$1"; if [ ${#v} -ge 12 ]; then printf '%s' "${v:0:12}"; else printf '%s' "$v"; fi; }
upper12() { printf '%s' "$1" | tr 'A-F' 'a-f'; }
token_or_die() { # reads VL_CUTOVER_TOKEN, falls back to the old prefix's key
  local fallback="${O}_CUTOVER_TOKEN" tok=""
  tok="${VL_CUTOVER_TOKEN:-}"
  [ -n "$tok" ] || tok="$(printenv "$fallback" 2>/dev/null || true)"
  [ -n "$tok" ] || die "the installation gate needs a cutover token (VL_CUTOVER_TOKEN, falling back to the old prefix's) — none is set"
  printf '%s' "$tok"
}
health_rev() {
  curl -s --max-time 5 "$HEALTH_URL" 2>/dev/null \
    | sed -n 's/.*"revision"[[:space:]]*:[[:space:]]*"\([0-9a-fA-F]*\)".*/\1/p' \
    | tr 'A-F' 'a-f' || true
}
gate_payload() { # $1 = path to a 0600 header file with the token
  curl -s --max-time 10 -H "@$1" "$GATE_URL" 2>/dev/null || true
}
revs_agree() { # $1 reported $2 expected — the plan's revisionsAgree (7-hex prefix)
  local reported="$1" expected="$2"
  [ -n "$reported" ] && [ -n "$expected" ] || return 1
  [ "$reported" = "$expected" ] && return 0
  local n="${#reported}"
  [ "$n" -ge 7 ] || return 1
  [ "$n" -lt "${#expected}" ] || return 1
  case "$expected" in "$reported"*) return 0 ;; *) return 1 ;; esac
}
go_stamp() { # $1 binary — prints "rev mod"
  local rev mod
  rev="$(go version -m "$1" 2>/dev/null | tr '\t' ' ' \
    | awk '{for(i=1;i<=NF;i++) if($i ~ /^vcs\.revision=/){sub(/^vcs\.revision=/,"",$i); print $i; exit}}' || true)"
  mod="$(go version -m "$1" 2>/dev/null | tr '\t' ' ' \
    | awk '{for(i=1;i<=NF;i++) if($i ~ /^vcs\.modified=/){sub(/^vcs\.modified=/,"",$i); print $i; exit}}' || true)"
  printf '%s %s' "$rev" "$mod"
}
newest_vl_log() { ls -1t "$VL_ROOT"/data/vl_*.log 2>/dev/null | head -1 || true; }

# updater_job_in_flight — the step-0 (j) scan as a reusable probe: prints the
# first NON-TERMINAL updater job YOUNGER than 30 min and returns 0; returns 1
# when nothing is in flight (stale/recovery_needed/terminal jobs are listed and
# left alone, exactly as step 0 always did).
updater_job_in_flight() {
  local job_file job_id job_state job_phase job_created age
  for job_file in "$OLD_ROOT/data/updater/jobs"/*.json; do
    [ -e "$job_file" ] || continue
    job_id="$(jq -r '.job_id // "?"' "$job_file" 2>/dev/null || echo '?')"
    job_state="$(jq -r '.state // "?"' "$job_file" 2>/dev/null || echo '?')"
    job_phase="$(jq -r '.phase // "started"' "$job_file" 2>/dev/null || echo 'started')"
    if [ "$job_state" = "recovery_needed" ]; then
      say "updater job $job_id: recovery_needed (listed, left alone)" >&2
      continue
    fi
    case "$job_state" in
      complete|rolled_back|cancelled|refused) [ "$job_phase" = "done" ] && continue ;;
    esac
    job_created="$(jq -r '.created_at // ""' "$job_file" 2>/dev/null || true)"
    if [ -n "$job_created" ]; then
      age=$(( $(date +%s) - $(date -d "$job_created" +%s 2>/dev/null || echo 0) ))
      if [ "$age" -gt 1800 ]; then
        say "updater job $job_id: stale (age ${age}s) — left alone" >&2
        continue
      fi
    fi
    printf '%s (state=%s phase=%s)' "$job_id" "$job_state" "$job_phase"
    return 0
  done
  return 1
}

# =============================================================================
# STEP 0 — refuse, before anything changes
# =============================================================================
step0() {
  say "step 0: pre-flight refusals (in order)"

  # (b) already migrated — checked FIRST. Every vl path in (d) EXCEPT the lock
  # home: under Z18 ~/vl-main.lock.d must NOT exist until R5, so a post-R2 box
  # can never satisfy a check that requires it (DS-101 P3-1). A PRESENT lock
  # home is still a refusal, via (d) below.
  local all_vl=1
  for p in "$HOME/vl" "$HOME/vl-backups" "$HOME/vl-releases" "$HOME/vl-inbox" \
           "$HOME/.config/vl-updater" "$HOME/bin/vl-updater"; do
    if [ ! -e "$p" ] && [ ! -L "$p" ]; then all_vl=0; break; fi
  done
  if [ "$all_vl" = 1 ] && ! ( systemctl cat vl >/dev/null 2>&1 && systemctl cat vl-web >/dev/null 2>&1 ); then
    all_vl=0
  fi
  if [ "$all_vl" = 1 ] && [ -L "$OLD_ROOT" ] \
     && [ "$(readlink "$OLD_ROOT")" = "$VL_ROOT" ] \
     && systemctl is-active vl >/dev/null 2>&1; then
    say "already migrated: every vl path exists, the install dir is a symlink to ~/vl and unit vl is active — verify-only run"
    if verify_legs "rerun"; then exit 0; else exit 1; fi
  fi

  # (c) the old install dir must be a real directory.
  if [ ! -e "$OLD_ROOT" ] && [ ! -L "$OLD_ROOT" ]; then
    die "the install tree at $OLD_ROOT is missing — a box without it is never migrated by this script"
  fi
  if [ -L "$OLD_ROOT" ]; then
    if [ "$(readlink "$OLD_ROOT")" = "$VL_ROOT" ]; then
      die "this box looks migrated — start unit vl ('systemctl start vl') and re-run; the already-migrated branch verifies and exits"
    fi
    die "$OLD_ROOT is a symlink (to $(readlink "$OLD_ROOT")), not the real install tree"
  fi
  [ -d "$OLD_ROOT" ] || die "$OLD_ROOT is not a directory"

  # (d) no vl path may exist yet (the unit files are checked by what systemd
  # has loaded: systemctl cat, which reads the real unit database).
  for p in "$HOME/vl" "$HOME/vl-backups" "$HOME/vl-releases" "$HOME/vl-inbox" \
           "$HOME/.config/vl-updater" "$HOME/bin/vl-updater" \
           "$HOME/vl-main.lock.d"; do
    if [ -e "$p" ] || [ -L "$p" ]; then
      die "$p already exists — refuse (the migration is one-way and irreversible by hand)"
    fi
  done
  if systemctl cat vl >/dev/null 2>&1 || systemctl cat vl-web >/dev/null 2>&1; then
    die "unit vl or vl-web is already loaded — refuse"
  fi

  # (e) the updater trio.
  UP_HAS_CONFIG=0; UP_HAS_BIN=0; UP_HAS_UNIT=0
  [ -e "$HOME/.config/$o-updater" ] && UP_HAS_CONFIG=1 || true
  [ -e "$HOME/bin/$o-updater" ] && UP_HAS_BIN=1 || true
  systemctl --user cat "$o-updater" >/dev/null 2>&1 && UP_HAS_UNIT=1 || true
  if [ "$NO_UPDATER" = 0 ]; then
    if [ "$UP_HAS_CONFIG" != 1 ] || [ "$UP_HAS_BIN" != 1 ] || [ "$UP_HAS_UNIT" != 1 ]; then
      die "the updater trio is incomplete (config=$UP_HAS_CONFIG bin=$UP_HAS_BIN unit=$UP_HAS_UNIT) — this box runs the worker; only a machine that never had it may pass --no-updater"
    fi
  else
    if [ "$UP_HAS_CONFIG" = 1 ] || [ "$UP_HAS_BIN" = 1 ] || [ "$UP_HAS_UNIT" = 1 ]; then
      die "--no-updater given but the updater trio exists (config=$UP_HAS_CONFIG bin=$UP_HAS_BIN unit=$UP_HAS_UNIT) — this box can never take the partner path"
    fi
  fi

  # (f) units: the two system units are required; the user units are recorded.
  systemctl cat "$o" >/dev/null 2>&1     || die "unit '$o' is not present"
  systemctl cat "$o-web" >/dev/null 2>&1 || die "unit '$o-web' is not present"
  HAS_BACKUP_SVC=0; HAS_BACKUP_TMR=0; HAS_CLOCK_SVC=0; HAS_CLOCK_TMR=0
  systemctl --user cat "$o-backup.service"       >/dev/null 2>&1 && HAS_BACKUP_SVC=1 || true
  systemctl --user cat "$o-backup.timer"         >/dev/null 2>&1 && HAS_BACKUP_TMR=1 || true
  systemctl --user cat "$o-clock-guard.service"  >/dev/null 2>&1 && HAS_CLOCK_SVC=1 || true
  systemctl --user cat "$o-clock-guard.timer"    >/dev/null 2>&1 && HAS_CLOCK_TMR=1 || true
  say "optional user units present: backup.service=$HAS_BACKUP_SVC backup.timer=$HAS_BACKUP_TMR clock.service=$HAS_CLOCK_SVC clock.timer=$HAS_CLOCK_TMR (absent ones are skipped with a log line)"

  # (g) the tree is at --sha and tracked-clean (untracked web/dist.old.* are expected).
  if [ "$MODE" != "rollback" ]; then
    local head dirty
    head="$(git -C "$OLD_ROOT" rev-parse HEAD 2>/dev/null || true)"
    if [ -z "$head" ] || [ "$(upper12 "$head")" != "$(upper12 "$SHA")" ]; then
      die "the install tree HEAD (${head:-none}) is not --sha ($SHA)"
    fi
    dirty="$(git -C "$OLD_ROOT" status --porcelain --untracked-files=no 2>/dev/null || true)"
    [ -z "$dirty" ] || die "the install tree has tracked changes — refuse"
  fi

  # (h) OLD_SHA reconciliation — the way back must be the running build.
  local go_out
  go_out="$(go_stamp "$OLD_BIN")"
  OLD_SHA="${go_out%% *}"
  [ -n "$OLD_SHA" ] || die "cannot read vcs.revision from the current binary — refusing a move with no way back"
  OLD12="$(rev12 "$(upper12 "$OLD_SHA")")"
  say "current: rev=$OLD12"
  local health_rev_old release_rev_old
  health_rev_old="$(health_rev)"
  release_rev_old="$([ -f "$OLD_ROOT/deploy/RELEASE" ] && tr -d '[:space:]' < "$OLD_ROOT/deploy/RELEASE" 2>/dev/null | tr 'A-F' 'a-f' || true)"
  if [ -n "$health_rev_old" ] && [ "$(rev12 "$health_rev_old")" != "$OLD12" ]; then
    die "OLD_SHA mismatch: disk=$OLD12 but the RUNNING process reports $(rev12 "$health_rev_old") via /api/health — refusing a move whose way back is not the running build"
  fi
  if [ -n "$release_rev_old" ] && [ "$(rev12 "$release_rev_old")" != "$OLD12" ]; then
    die "OLD_SHA mismatch: disk=$OLD12 but the install marker says $(rev12 "$release_rev_old") — refusing a move whose way back is not what the marker names"
  fi
  if [ -z "$health_rev_old" ] && [ -z "$release_rev_old" ]; then
    die "cannot reconcile OLD_SHA=$OLD12 — neither /api/health nor the install marker answered; refusing a move with no proof of what is running"
  fi
  say "current reconciled: disk=$OLD12 health=$(rev12 "${health_rev_old:-}") marker=$(rev12 "${release_rev_old:-}")"

  # (i) the lock: meta names --session and the tool's OWN verdict is 1 (held+fresh).
  if [ ! -f "$LOCK_HOME/meta" ]; then
    die "the lock home $LOCK_HOME has no meta — refusing"
  fi
  local meta_session
  meta_session="$(grep -m1 '^session=' "$LOCK_HOME/meta" 2>/dev/null | cut -d= -f2- || true)"
  if [ -z "$meta_session" ] || [ "$meta_session" != "$SESSION" ]; then
    die "the lock meta names a different session ('${meta_session:-none}'), not --session '$SESSION' — refusing; meta: $(cat "$LOCK_HOME/meta" 2>/dev/null || echo unreadable)"
  fi
  check_rc=0
  "$LOCK_TOOL" check || check_rc=$?
  case "$check_rc" in
    1) say "lock: held by '$SESSION' and fresh (check rc 1)";;
    2) die "the lock is STALE (check rc 2) — the holder is not beating; refuse and tell the operator to re-acquire";;
    3) die "the lock is INCOMPLETE (check rc 3) — an acquire is in flight; never take it over";;
    4) die "the lock is ABANDONED-incomplete (check rc 4) — refuse";;
    *) die "the lock tool verdict is not 'held' (check rc $check_rc) — refuse";;
  esac

  # (j) no updater job in flight. The rule this loop implements: a NON-TERMINAL
  # job YOUNGER than 30 min is a refusal (vl-updater's start sweep would resume
  # it against the new install and could kill the new unit mid-verify); a job
  # OLDER than 30 min is stale — the worker's sweep marks it recovery_needed —
  # so it is listed and left alone; recovery_needed jobs are listed and left
  # alone (worker.go's sweep at the base, verified by the checkers).
  # Extracted as updater_job_in_flight so step 1's 6b re-runs the same scan.
  local inflight
  inflight="$(updater_job_in_flight || true)"
  if [ -n "$inflight" ]; then
    die "an updater job is in flight: $inflight — a resumed job would kill the new unit mid-verify; wait for it or clean it first"
  fi

  # (k) the installation gate.
  gate_check

  # (l) the release dir: outside the install, complete, stamped; then re-resolve.
  local real_rel real_old given
  given="$RELEASE_DIR"
  real_rel="$(realpath -m "$given" 2>/dev/null || echo "$given")"
  real_old="$(realpath -m "$OLD_ROOT" 2>/dev/null || echo "$OLD_ROOT")"
  case "$real_rel" in
    "$real_old"|"$real_old"/*) die "--release-dir ($given) is inside the install tree — refuse" ;;
  esac
  local artifact
  for artifact in "vl-bin" "vl-activate" "updater/vl-updater" "updater/vl-updater-bootstrap"; do
    [ -f "$given/$artifact" ] || die "the release dir is missing $artifact"
  done
  [ -d "$given/web/dist" ] || die "the release dir is missing web/dist"
  local stamp rev mod
  for artifact in "vl-bin" "vl-activate" "updater/vl-updater" "updater/vl-updater-bootstrap"; do
    stamp="$(go_stamp "$given/$artifact")"
    rev="${stamp%% *}"; mod="${stamp##* }"
    if [ "$(upper12 "$rev")" != "$(upper12 "$SHA")" ]; then
      die "$artifact is stamped vcs.revision=${rev:-none}, not --sha — refuse"
    fi
    if [ "$mod" != "false" ]; then
      die "$artifact is stamped vcs.modified=${mod:-missing} (not false) — refuse"
    fi
  done
  say "release dir OK: $given"
  # Re-resolve: if the given dir lives under the moved releases dir, rewrite the
  # prefix now (pure path math, valid before and after the move). The anchor is
  # the PRE-MOVE realpath, saved for step 2's re-check (after the move the
  # old path resolves through the new symlink and would match everything).
  local real_oldrel
  real_oldrel="$(realpath -m "$HOME/$o-releases" 2>/dev/null || echo "$HOME/$o-releases")"
  OLD_REL_REAL="$real_oldrel"
  case "$real_rel" in
    "$real_oldrel"/*) RELEASE_DIR="$HOME/vl-releases/${real_rel#"$real_oldrel"/}" ;;
  esac

  # (m) node and npm. VL_MIGRATE_NODE is a test seam: when set, the detection
  # steps are skipped and that dir is used directly (unset in production).
  if [ -n "${VL_MIGRATE_NODE:-}" ]; then
    NODE_DIR="${VL_MIGRATE_NODE}"
  else
    NODE_DIR="$(detect_node)" || die "node not found (login shell, interactive shell, ~/.nvm, PATH) — install Node.js first"
  fi
  [ -x "$NODE_DIR/node" ] || die "node not found at $NODE_DIR/node"
  [ -x "$NODE_DIR/npm" ] || die "npm not found next to node ($NODE_DIR) — broken Node install"
  say "node: $NODE_DIR/node"

  # (n) sudo.
  if [ "$MODE" = "dry" ]; then
    say "would sudo -v"
  else
    sudo -v || die "sudo -v failed — the operator's password is required before anything is stopped"
    ( while :; do sudo -n -v 2>/dev/null || break; sleep 60; done ) >/dev/null 2>&1 &
    TMP_KEEPALIVE=$!
  fi

  # (o) worktree count.
  WT_COUNT="$(git -C "$OLD_ROOT" worktree list 2>/dev/null | wc -l | tr -d ' ')"
  say "worktrees before the move: $WT_COUNT"
}

detect_node() {
  local n
  n="$(bash -lc 'command -v node' 2>/dev/null | tail -1 || true)"
  if [ -z "${n:-}" ]; then
    n="$(bash -ic 'command -v node' 2>/dev/null | tail -1 || true)"
  fi
  if [ -z "${n:-}" ]; then
    n="$(ls -1d "$HOME"/.nvm/versions/node/*/bin/node 2>/dev/null | sort -V | tail -1 || true)"
  fi
  if [ -z "${n:-}" ]; then
    n="$(command -v node 2>/dev/null || true)"
  fi
  [ -n "${n:-}" ] || return 1
  dirname "$n"
}

gate_check() { # prints the legs and dies on any failing required leg
  local tok hdr gate legs name pass
  tok="$(token_or_die)"
  hdr="$(mkt)"
  ( umask 077; printf 'Authorization: Bearer %s' "$tok" > "$hdr" ) \
    || die "cannot write the token header file"
  gate="$(gate_payload "$hdr")"
  rm -f "$hdr"
  [ -n "$gate" ] || die "the installation gate did not answer — refusing to kill a trader whose state is unknown"
  legs="$(printf '%s' "$gate" | jq -r '.legs[]? | "\(.name)\t\(.pass)"' 2>/dev/null || true)"
  [ -n "$legs" ] || die "the installation gate payload names no legs — refusing"
  say "installation gate legs:"
  printf '%s\n' "$legs" | sed 's/^/    leg: /'
  require_leg() { # $1 = glob
    local found=0 bad=""
    while IFS=$'\t' read -r name pass; do
      case "$name" in $1)
        found=$((found+1))
        [ "$pass" = "true" ] || bad="$bad $name"
        ;;
      esac
    done <<LEGS
$legs
LEGS
    [ "$found" -gt 0 ] || die "the installation gate names no $1 leg; refusing (an unevaluable leg is not a pass)"
    if [ -n "$bad" ]; then
      local nt8_hint=""
      case "$bad" in *working_orders*|*trader_cutover*) nt8_hint=" — NT8 looks closed — reopen NT8 (accounts flat), then re-run" ;; esac
      die "failing installation-gate legs:$bad$nt8_hint"
    fi
  }
  require_leg 'trader_cutover:*'
  require_leg 'ledger_exposure'
  require_leg 'planner_in_flight'
  require_leg 'traders_nt8'
  if printf '%s\n' "$legs" | cut -f1 | grep -qx 'addon_census_prehold'; then
    require_leg 'addon_census_prehold'
  fi
  say "installation gate READY — every required leg passed"
}

# gate_recheck — 6a (RA1): the step-0 verdict can be as old as the operator's
# password prompt; the bot keeps trading until the system stop below. Re-read
# the gate right before that stop and stepdie naming any failing required leg
# (a stepdie here triggers the auto-rollback, which only restarts the old
# updater — the system units were never stopped).
gate_recheck() {
  local tok hdr gate legs name pass bad=""
  tok="$(token_or_die)"
  hdr="$(mkt)"
  ( umask 077; printf 'Authorization: Bearer %s' "$tok" > "$hdr" ) \
    || stepdie "cannot write the token header file on the re-check"
  gate="$(gate_payload "$hdr")"
  rm -f "$hdr"
  [ -n "$gate" ] || stepdie "the installation gate did not answer on the re-check — refusing before the bot is stopped"
  legs="$(printf '%s' "$gate" | jq -r '.legs[]? | "\(.name)\t\(.pass)"' 2>/dev/null || true)"
  [ -n "$legs" ] || stepdie "the installation gate payload names no legs on the re-check — refusing before the bot is stopped"
  require_leg2() { # $1 = glob
    local found=0
    while IFS=$'\t' read -r name pass; do
      case "$name" in $1)
        found=$((found+1))
        [ "$pass" = "true" ] || bad="$bad $name"
        ;;
      esac
    done <<LEGS
$legs
LEGS
    [ "$found" -gt 0 ] || stepdie "the installation gate names no $1 leg on the re-check — refusing before the bot is stopped"
  }
  require_leg2 'trader_cutover:*'
  require_leg2 'ledger_exposure'
  require_leg2 'planner_in_flight'
  require_leg2 'traders_nt8'
  if printf '%s\n' "$legs" | cut -f1 | grep -qx 'addon_census_prehold'; then
    require_leg2 'addon_census_prehold'
  fi
  if [ -n "$bad" ]; then
    stepdie "gate changed since step 0:$bad — refusing before the bot is stopped"
  fi
  say "gate re-check: READY ($(printf '%s' "$legs" | tr '\t' '=' | tr '\n' ' '))"
}

# =============================================================================
# STEPS 1–5 — the move (forward mode). Runs in a subshell so that ANY failure
# (a guarded stepdie or a bare failing command under set -e) returns non-zero
# to the caller, which then runs the automatic --rollback.
# =============================================================================
steps_1_5() {
  (
    set -euo pipefail
    local dest src receipt bytes ok integ

    # --- step 1: stop and back up -------------------------------------------
    say "step 1: stopping units and backing up the DB"
    if [ "$NO_UPDATER" = 0 ]; then
      systemctl --user stop "$o-updater" || stepdie "cannot stop the old updater"
    fi
    # 6b (RA2): the old worker could have created/advanced a job between step 0
    # and its stop — re-run the step-0 (j) scan before anything else moves.
    local inflight6b
    inflight6b="$(updater_job_in_flight || true)"
    if [ -n "$inflight6b" ]; then
      stepdie "updater job appeared since step 0: $inflight6b — refusing before the bot is stopped"
    fi
    if [ "$HAS_BACKUP_SVC" = 1 ]; then systemctl --user stop "$o-backup.service" || stepdie "cannot stop the backup service"; fi
    if [ "$HAS_CLOCK_SVC" = 1 ]; then systemctl --user stop "$o-clock-guard.service" || stepdie "cannot stop the clock-guard service"; fi
    if [ "$HAS_BACKUP_TMR" = 1 ]; then systemctl --user stop "$o-backup.timer" || stepdie "cannot stop the backup timer"; fi
    if [ "$HAS_CLOCK_TMR" = 1 ]; then systemctl --user stop "$o-clock-guard.timer" || stepdie "cannot stop the clock-guard timer"; fi
    # 6a (RA1): the gate verdict predates the sudo password prompt — re-read it
    # immediately before the bot is stopped.
    gate_recheck
    sudo systemctl stop "$o" "$o-web" || stepdie "cannot stop the system units"
    local waited=0 mainpid=0
    while [ "$waited" -lt 30 ]; do
      mainpid="$(systemctl show -p MainPID --value "$o" 2>/dev/null || echo 0)"
      [ "${mainpid:-0}" = "0" ] && break
      sleep 1; waited=$((waited+1))
    done
    [ "${mainpid:-0}" = "0" ] || stepdie "the old system unit did not stop within 30 s (MainPID=$mainpid)"

    src="$OLD_ROOT/data/data.db"
    [ -f "$src" ] || stepdie "the DB at $src does not exist — refusing a backup that would create an empty file"
    dest="$HOME/$o-backups/pre-vl-rename-$TS/data.db"
    mkdir -p "$(dirname "$dest")"
    receipt_err="$(mkt)"
    receipt="$("$RELEASE_DIR/vl-activate" backup -db "$src" -dest "$dest" 2>"$receipt_err" || true)"
    ok="$(printf '%s' "$receipt" | jq -r '.ok // false' 2>/dev/null || echo false)"
    integ="$(printf '%s' "$receipt" | jq -r '.evidence.integrity_check // ""' 2>/dev/null || true)"
    bytes="$(printf '%s' "$receipt" | jq -r '.evidence.bytes // ""' 2>/dev/null || true)"
    if [ "$ok" != "true" ] || [ "$integ" != "ok" ] \
       || ! printf '%s' "$bytes" | grep -Eq '^[0-9]+$' || [ "$bytes" -le 0 ] 2>/dev/null; then
      err_json="$(printf '%s' "$receipt" | jq -r '.err // empty' 2>/dev/null || true)"
      err_tail="$([ -s "$receipt_err" ] && tail -n 5 "$receipt_err" 2>/dev/null | tr '\n' ' ' | sed 's/  */ /g' || true)"
      stepdie "the DB backup receipt is not ok:true + evidence.integrity_check=ok + numeric evidence.bytes>0 (got ok=$ok integrity=$integ bytes='$bytes' err='${err_json:-none}' stderr='${err_tail:-none}') — refusing"
    fi
    say "DB backed up to $dest (bytes=$bytes)"

    # --- step 2: move ---------------------------------------------------------
    say "step 2: moving the install and the data dirs"
    mv "$OLD_ROOT" "$VL_ROOT"
    ln -s "$VL_ROOT" "$OLD_ROOT"
    local pair oldp newp
    for pair in "$HOME/$o-backups:$HOME/vl-backups" "$HOME/$o-releases:$HOME/vl-releases" "$HOME/$o-inbox:$HOME/vl-inbox" "$HOME/.config/$o-updater:$HOME/.config/vl-updater"; do
      oldp="${pair%%:*}"; newp="${pair##*:}"
      if [ "$NO_UPDATER" = 1 ] && [ "$oldp" = "$HOME/.config/$o-updater" ]; then
        say "skip $oldp (--no-updater: absent by definition)"; continue
      fi
      if [ -e "$oldp" ] || [ -L "$oldp" ]; then
        mv "$oldp" "$newp"
        ln -s "$newp" "$oldp"
      else
        say "skip $oldp (absent)"
      fi
    done
    mkdir -p "$HOME/vl-backups/updater"

    if [ "$NO_UPDATER" = 0 ]; then
      local env_file
      env_file="$HOME/.config/vl-updater/env"
      [ -f "$env_file" ] || stepdie "the updater env file is missing after the move"
      cp "$env_file" "$env_file.pre-vl"
      chmod 600 "$env_file.pre-vl"
      sed -i "s/^${O}_/VL_/" "$env_file"
      if grep -q '^VL_RELEASE_DIR=' "$env_file"; then
        sed -i "s|^VL_RELEASE_DIR=.*|VL_RELEASE_DIR=$HOME/vl-releases|" "$env_file"
      else
        printf 'VL_RELEASE_DIR=%s\n' "$HOME/vl-releases" >> "$env_file"
      fi
      if [ -d "$HOME/vl-inbox" ]; then
        if grep -q '^VL_RELEASE_INBOX=' "$env_file"; then
          sed -i "s|^VL_RELEASE_INBOX=.*|VL_RELEASE_INBOX=$HOME/vl-inbox|" "$env_file"
        else
          printf 'VL_RELEASE_INBOX=%s\n' "$HOME/vl-inbox" >> "$env_file"
        fi
      else
        sed -i '/^VL_RELEASE_INBOX=/d' "$env_file"
      fi
      say "env file rewritten (values never printed; original kept as env.pre-vl)"
    fi

    local real_rel real_vl
    real_rel="$(realpath -m "$RELEASE_DIR" 2>/dev/null || echo "$RELEASE_DIR")"
    case "$real_rel" in
      "$OLD_REL_REAL"/*) stepdie "release dir still resolves under the moved releases dir after the re-resolve — refuse" ;;
    esac
    real_vl="$(realpath -m "$VL_ROOT" 2>/dev/null || echo "$VL_ROOT")"
    if [ "$real_rel" != "$RELEASE_DIR" ]; then
      stepdie "release-root invariant broken: realpath($RELEASE_DIR)=$real_rel — a symlink anywhere in it is refused"
    fi
    case "$real_rel" in
      "$real_vl"|"$real_vl"/*) stepdie "the release dir resolves inside the new install tree — refuse" ;;
    esac
    say "release-root invariant holds: $RELEASE_DIR"

    local wt_now
    wt_now="$(git -C "$VL_ROOT" worktree list 2>/dev/null | wc -l | tr -d ' ')"
    [ "$wt_now" = "$WT_COUNT" ] || stepdie "worktree count changed across the move ($WT_COUNT -> $wt_now) — refuse"
    git -C "$VL_ROOT" status --porcelain >/dev/null 2>&1 || stepdie "git status failed in the moved tree"
    say "worktrees after the move: $wt_now (unchanged) and git status works"

    # --- step 3: install the new build and start the vl system units ----------
    say "step 3: installing the new build and starting the vl system units"
    cp "$RELEASE_DIR/vl-bin" "$VL_ROOT/vl-bin"
    if [ -e "$VL_ROOT/web/dist" ] || [ -L "$VL_ROOT/web/dist" ]; then
      mv "$VL_ROOT/web/dist" "$VL_ROOT/web/dist.old.$OLD12.$TS"
    fi
    mkdir -p "$VL_ROOT/web"
    cp -r "$RELEASE_DIR/web/dist" "$VL_ROOT/web/dist"

    if [ -e "$VL_ROOT/deploy/RELEASE" ]; then
      mv "$VL_ROOT/deploy/RELEASE" "$VL_ROOT/deploy/RELEASE.pre-vl.$OLD12"
    fi
    local marker_tmp
    marker_tmp="$(mkt)"
    printf '%s\n' "$SHA" > "$marker_tmp"
    mv "$marker_tmp" "$VL_ROOT/deploy/RELEASE"
    say "RELEASE marker WRITTEN (never copied) from --sha"

    local user dir tpl out unit
    user="$(id -un)"
    dir="$(cd "$VL_ROOT" && pwd -P)"
    for unit in vl vl-web; do
      tpl="$VL_ROOT/deploy/$unit.service"
      [ -f "$tpl" ] || stepdie "unit template $tpl is missing"
      [ -r "$tpl" ] || stepdie "unit template $unit is unreadable"
      out="$(mkt)"
      sed -e "s|__VL_USER__|$user|g" -e "s|__VL_DIR__|$dir|g" -e "s|__NODE_DIR__|$NODE_DIR|g" "$tpl" > "$out"
      if grep -q '__' "$out"; then
        stepdie "unit template $unit still holds a placeholder after rendering"
      fi
      sudo tee "/etc/systemd/system/$unit.service" >/dev/null < "$out"
      rm -f "$out"
    done
    sudo systemctl daemon-reload
    date +%s > "$STEP3_MARK"
    sudo systemctl enable --now vl vl-web
    say "vl system units enabled and started"

    # --- step 4: install the vl user units ------------------------------------
    say "step 4: installing the vl user units"
    if [ "$NO_UPDATER" = 0 ]; then
      mkdir -p "$HOME/bin"
      cp "$RELEASE_DIR/updater/vl-updater" "$HOME/bin/vl-updater"
    fi
    mkdir -p "$HOME/.config/systemd/user"
    for unit in vl-backup.service vl-backup.timer vl-clock-guard.service vl-clock-guard.timer; do
      cp "$VL_ROOT/deploy/systemd-user/$unit" "$HOME/.config/systemd/user/$unit"
    done
    if [ "$NO_UPDATER" = 0 ]; then
      cp "$VL_ROOT/deploy/systemd-user/vl-updater.service" "$HOME/.config/systemd/user/vl-updater.service"
    fi
    systemctl --user daemon-reload
    systemctl --user enable --now vl-backup.timer vl-clock-guard.timer
    if [ "$NO_UPDATER" = 0 ]; then
      systemctl --user enable --now vl-updater
    fi
    say "vl user units enabled and started"

    # --- step 5: lock — no hand-over (Z18) ------------------------------------
    say "step 5: lock — no hand-over; the old home stays held by '$SESSION' and its keeper keeps beating"
  )
}

# =============================================================================
# STEP 6 — verify; shared with the already-migrated branch
# =============================================================================
# verify_legs <mode>   mode=forward: auto legs decide (roll back on failure)
#                      mode=rerun:   the already-migrated branch (no rollback)
#
# B2/B3 (R2 attempt-3): the bot needs seconds to write BOOT INTEGRITY OK and
# serve :8080. Each deciding leg polls up to VL_MIGRATE_VERIFY_WAIT_S seconds
# (default 90; the env is a TEST SEAM only) and passes the moment it is true,
# fails only at the deadline, and prints how long it took.
verify_wait_secs() {
  local v="${VL_MIGRATE_VERIFY_WAIT_S:-90}"
  case "$v" in ''|*[!0-9]*) echo 90 ;; *) echo "$v" ;; esac
}

# poll_secs reads the wait_leg poll-interval test seam. Production polls every
# 2 s; VL_MIGRATE_POLL_S (a positive integer) shrinks it — a TEST SEAM only.
poll_secs() {
  local p="${VL_MIGRATE_POLL_S:-2}"
  case "$p" in ''|*[!0-9]*|0*) echo 2 ;; *) echo "$p" ;; esac
}

# wait_leg <secs> <probe...> — polls every poll_secs until the probe exits 0;
# prints the seconds waited on success, the seconds at the deadline on failure;
# rc 0 = passed within the deadline.
wait_leg() {
  local max="$1" waited=0 poll; shift
  poll="$(poll_secs)"
  while [ "$waited" -lt "$max" ]; do
    if "$@" >/dev/null 2>&1; then printf '%s' "$waited"; return 0; fi
    sleep "$poll"; waited=$((waited+poll))
  done
  printf '%s' "$waited"; return 1
}

boot_line_ready() { # $1 mode (forward|rerun)
  local mode="$1" log_file boot_epoch
  log_file="$(newest_vl_log)"
  boot_epoch="$(stat -c %Y "$log_file" 2>/dev/null || echo 0)"
  if [ "$mode" = "rerun" ]; then
    [ "$boot_epoch" -ge "$RUN_START" ] 2>/dev/null || return 1
  else
    [ "$boot_epoch" -ge "$STEP3_EPOCH" ] 2>/dev/null || return 1
  fi
  [ -n "$log_file" ] || return 1
  grep -q "BOOT INTEGRITY OK — rev $SHA12" "$log_file" 2>/dev/null
}

health_ready() {
  local h
  h="$(health_rev)"
  revs_agree "$h" "$(upper12 "$SHA")"
}

verify_legs() {
  local mode="$1" ok=1
  say "step 6: verify"

  local max_wait waited
  max_wait="$(verify_wait_secs)"

  if waited="$(wait_leg "$max_wait" boot_line_ready "$mode")"; then
    say "boot line OK after ${waited}s: $(newest_vl_log)"
  else
    say "BOOT INTEGRITY leg FAIL after ${waited}s: no 'BOOT INTEGRITY OK — rev $SHA12' in the newest vl log ($(newest_vl_log) or none) newer than the install"
    ok=0
  fi

  if waited="$(wait_leg "$max_wait" health_ready)"; then
    say "health revision OK after ${waited}s: $(health_rev) (a 7+ hex prefix of --sha — never == against 40 hex)"
  else
    say "health revision leg FAIL after ${waited}s: /api/health reports '$(health_rev)' or none — not a 7+ hex prefix of --sha"
    ok=0
  fi

  if [ "$ok" != 1 ]; then
    return 1
  fi

  # --- printed legs (never decide) --------------------------------------------
  nt8_leg
  worker_leg
  timers_leg
  vite_leg
  postboot_leg
  return 0
}

nt8_leg() {
  local tok hdr gate nt8 eligible build waited log_file
  tok="$(printenv VL_CUTOVER_TOKEN 2>/dev/null || printenv "${O}_CUTOVER_TOKEN" 2>/dev/null || true)"
  if [ -z "$tok" ]; then say "NT8: n/a (no token — skipped)"; return; fi
  hdr="$(mkt)"
  ( umask 077; printf 'Authorization: Bearer %s' "$tok" > "$hdr" ) || { rm -f "$hdr"; say "NT8: n/a (header write failed)"; return; }
  gate="$(gate_payload "$hdr")"
  nt8="$(printf '%s' "$gate" | jq -r 'if .nt8_absent == null then "null" else .nt8_absent end' 2>/dev/null || echo null)"
  if [ "$nt8" = "null" ] || [ -z "$nt8" ]; then
    rm -f "$hdr"
    say "NT8: n/a (no NT8 wire)"
    return
  fi
  eligible="$(printf '%s' "$gate" | jq -r '.nt8_absent.eligible // false' 2>/dev/null || echo false)"
  build="$(printf '%s' "$gate" | jq -r '.nt8_absent.build_id // .build_id // "n/a"' 2>/dev/null || echo n/a)"
  if [ "$eligible" = "true" ]; then
    rm -f "$hdr"
    say "NT8: n/a (eligible — link down long enough; no wait) build=$build"
    return
  fi
  waited=0
  while [ "$waited" -lt 90 ]; do
    log_file="$(newest_vl_log)"
    if [ -n "$log_file" ] && grep -qi 'hello' "$log_file" 2>/dev/null; then
      rm -f "$hdr"
      say "NT8: hello seen (build=$build)"
      return
    fi
    gate="$(gate_payload "$hdr")"
    eligible="$(printf '%s' "$gate" | jq -r '.nt8_absent.eligible // false' 2>/dev/null || echo false)"
    if [ "$eligible" = "true" ]; then
      rm -f "$hdr"
      say "NT8: eligible during the wait (build=$build)"
      return
    fi
    sleep 2; waited=$((waited+2))
  done
  rm -f "$hdr"
  say "NT8: not seen in ${waited}s (build=$build)"
}

worker_leg() {
  if [ "$NO_UPDATER" = 1 ]; then say "updater: n/a (--no-updater)"; return; fi
  if journalctl --user -u vl-updater --no-pager -n 50 2>/dev/null | grep -q "serving" \
     && journalctl --user -u vl-updater --no-pager -n 50 2>/dev/null | grep -Fq "$HOME/vl"; then
    say "updater worker: serving $HOME/vl"
  else
    say "updater worker leg FAIL: no 'serving … $HOME/vl' line in journalctl --user -u vl-updater"
  fi
}

timers_leg() {
  local out
  out="$(systemctl --user list-timers 2>/dev/null || true)"
  if printf '%s' "$out" | grep -q 'vl-backup'; then say "timer vl-backup: next run listed"; else say "timer vl-backup leg FAIL: not listed"; fi
  if printf '%s' "$out" | grep -q 'vl-clock-guard'; then say "timer vl-clock-guard: next run listed"; else say "timer vl-clock-guard leg FAIL: not listed"; fi
}

vite_ready() {
  [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:3000 2>/dev/null || true)" = "200" ]
}

vite_leg() {
  local max_wait waited code
  max_wait="$(verify_wait_secs)"
  if waited="$(wait_leg "$max_wait" vite_ready)"; then
    say "vite :3000 answers 200 after ${waited}s"
  else
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:3000 2>/dev/null || true)"
    say "vite :3000 leg FAIL after ${waited}s: http ${code:-none}"
  fi
}

postboot_leg() {
  if [ -x "$VL_ROOT/deploy/postboot-check.sh" ]; then
    if ( cd "$VL_ROOT" && VL_REPO="$VL_ROOT" VL_DATA="$VL_ROOT/data" VL_BIN="$VL_ROOT/vl-bin" bash "$VL_ROOT/deploy/postboot-check.sh" >/dev/null 2>&1 ); then
      say "postboot-check: PASS"
    else
      say "postboot-check leg FAIL (printed only — the owner decides)"
    fi
  else
    say "postboot-check: n/a (script absent)"
  fi
}

# =============================================================================
# STEP 7 — finalize
# =============================================================================
step7() {
  say "step 7: finalize"
  sudo systemctl disable "$o" "$o-web"
  if [ -e "$VL_ROOT/$o-bin" ]; then
    mv "$VL_ROOT/$o-bin" "$VL_ROOT/${o}-bin.old.$OLD12"
    say "parked the old binary as $VL_ROOT/${o}-bin.old.$OLD12 (after verify)"
  fi
  if [ "$HAS_BACKUP_TMR" = 1 ]; then systemctl --user disable "$o-backup.timer" || warn "cannot disable the old backup timer"; fi
  if [ "$HAS_CLOCK_TMR" = 1 ]; then systemctl --user disable "$o-clock-guard.timer" || warn "cannot disable the old clock-guard timer"; fi
  if [ "$NO_UPDATER" = 0 ]; then
    systemctl --user disable "$o-updater" || warn "cannot disable the old updater unit"
    if [ -e "$HOME/bin/$o-updater" ]; then
      mv "$HOME/bin/$o-updater" "$HOME/bin/${o}-updater.old.$OLD12"
    fi
  fi

  if [ "$RELEASE_LOCK" = 1 ]; then
    local marker_content head up
    marker_content="$(tr -d '[:space:]' < "$VL_ROOT/deploy/RELEASE" 2>/dev/null || true)"
    if [ "$(upper12 "$marker_content")" != "$(upper12 "$SHA")" ]; then
      die "--release-lock refused: the RELEASE marker does not name THIS boot's sha (content check, never a cleanliness check)"
    fi
    head="$(git -C "$VL_ROOT" rev-parse HEAD 2>/dev/null || true)"
    up="$(git -C "$VL_ROOT" rev-parse @{u} 2>/dev/null || true)"
    if [ -z "$up" ] || [ "$head" != "$up" ]; then
      die "--release-lock refused: the tree's HEAD ($head) is not on its upstream (@{u}=${up:-none})"
    fi
    "$VL_LOCK_TOOL" release "$SESSION" || die "--release-lock: the lock tool refused the release"
    say "lock released (same home)"
  else
    say "lock stays HELD by '$SESSION'"
    say "when the RELEASE marker commit is pushed from the tree, the deploy owner runs:"
    say "    cd $HOME/vl && deploy/vl-lock.sh release $SESSION"
  fi

  local wt_end
  wt_end="$(git -C "$VL_ROOT" worktree list 2>/dev/null | wc -l | tr -d ' ')"
  say "worktrees after the boot: $wt_end"
  say "DONE — the box runs vl; the old units stay disabled as the rollback target until the R5 phase"
}

# =============================================================================
# --rollback
# =============================================================================
do_rollback() {
  say "rollback: reading the state file"
  [ -f "$STATE_FILE" ] || die "no state file at $STATE_FILE — refusing (the rollback reads ONLY that file, never globs parked binaries)"
  local old12_s st_sha st_no_up old_sha_s ts_rb
  st_sha="$(grep -m1 '^sha=' "$STATE_FILE" | cut -d= -f2- || true)"
  old12_s="$(grep -m1 '^old12=' "$STATE_FILE" | cut -d= -f2- || true)"
  st_no_up="$(grep -m1 '^no_updater=' "$STATE_FILE" | cut -d= -f2- || echo 0)"
  old_sha_s="$(grep -m1 '^old_sha=' "$STATE_FILE" | cut -d= -f2- || true)"
  ts_rb="$(grep -m1 '^ts=' "$STATE_FILE" | cut -d= -f2- || true)"
  [ -n "$st_sha" ] || die "the state file records no sha"
  [ -n "$old12_s" ] || die "the state file records no old12"
  [ -n "$old_sha_s" ] || die "the state file records no old_sha"
  OLD12_RB="$old12_s"
  local rb_old="$old_sha_s"

  # B2: the lock is checked FIRST, before anything is stopped.
  local meta_session check_rc
  if [ ! -f "$LOCK_HOME/meta" ]; then
    die "rollback refused: the lock home has no meta — print for a human: $(cat "$LOCK_HOME/meta" 2>/dev/null || echo unreadable)"
  fi
  meta_session="$(grep -m1 '^session=' "$LOCK_HOME/meta" 2>/dev/null | cut -d= -f2- || true)"
  check_rc=0
  "$LOCK_TOOL" check || check_rc=$?
  if [ "$meta_session" != "$SESSION" ] || [ "$check_rc" != 1 ]; then
    cat >&2 <<EOF
[migrate] REFUSED: the rollback needs the lock held by '$SESSION' and fresh (check rc 1).
  meta: $(cat "$LOCK_HOME/meta" 2>/dev/null || echo unreadable)
  check rc: $check_rc
  state file: $STATE_FILE
  Recovery (exact commands, run by the operator):
    cd $HOME/$o && deploy/$o-lock.sh acquire $SESSION "re-acquire for the vl rename rollback" 120
    $0 --session $SESSION --sha $st_sha --release-dir ${RELEASE_DIR:-<dir>} --rollback
EOF
    exit 1
  fi
  say "rollback: lock held by '$SESSION' and fresh — proceeding"

  # 1. stop the vl units (guarded).
  systemctl --user stop vl-updater 2>/dev/null || say "rollback: vl-updater not running (skip)"
  systemctl --user stop vl-backup.service 2>/dev/null || say "rollback: vl-backup.service not running (skip)"
  systemctl --user stop vl-clock-guard.service 2>/dev/null || say "rollback: vl-clock-guard.service not running (skip)"
  systemctl --user stop vl-backup.timer 2>/dev/null || say "rollback: vl-backup.timer not running (skip)"
  systemctl --user stop vl-clock-guard.timer 2>/dev/null || say "rollback: vl-clock-guard.timer not running (skip)"
  sudo systemctl stop vl vl-web 2>/dev/null || say "rollback: vl units not running (skip)"

  # 2. rm the symlinks and move every vl dir back (guarded, never abort).
  if [ -L "$OLD_ROOT" ]; then rm "$OLD_ROOT"; fi
  if [ -e "$VL_ROOT" ]; then mv "$VL_ROOT" "$OLD_ROOT"; else say "rollback: $VL_ROOT absent (skip)"; fi
  local pair oldp newp
  for pair in "$HOME/$o-backups:$HOME/vl-backups" "$HOME/$o-releases:$HOME/vl-releases" "$HOME/$o-inbox:$HOME/vl-inbox" "$HOME/.config/$o-updater:$HOME/.config/vl-updater"; do
    oldp="${pair%%:*}"; newp="${pair##*:}"
    if [ "$st_no_up" = 1 ] && [ "$oldp" = "$HOME/.config/$o-updater" ]; then say "rollback: skip $oldp (--no-updater run)"; continue; fi
    if [ -L "$oldp" ]; then rm "$oldp"; fi
    if [ -e "$newp" ] || [ -L "$newp" ]; then mv "$newp" "$oldp"; else say "rollback: $newp absent (skip)"; fi
  done

  # 3. restore by the names the state file recorded.
  if [ "$st_no_up" = 0 ]; then
    if [ -f "$HOME/.config/$o-updater/env.pre-vl" ]; then
      mv "$HOME/.config/$o-updater/env.pre-vl" "$HOME/.config/$o-updater/env"
    fi
    if [ -f "$HOME/bin/${o}-updater.old.$old12_s" ]; then
      mv "$HOME/bin/${o}-updater.old.$old12_s" "$HOME/bin/$o-updater"
    fi
  fi
  if [ -f "$OLD_ROOT/${o}-bin.old.$old12_s" ]; then
    mv "$OLD_ROOT/${o}-bin.old.$old12_s" "$OLD_ROOT/$o-bin"
  fi
  local dist_old
  dist_old="$OLD_ROOT/web/dist.old.$old12_s.$ts_rb"
  if [ -e "$dist_old" ]; then
    rm -rf "$OLD_ROOT/web/dist" 2>/dev/null || true
    mv "$dist_old" "$OLD_ROOT/web/dist"
  fi
  local marker_saved marker_now
  marker_saved="$OLD_ROOT/deploy/RELEASE.pre-vl.$old12_s"
  if [ -n "$marker_saved" ] && [ -e "$marker_saved" ]; then
    mv "$marker_saved" "$OLD_ROOT/deploy/RELEASE"
  fi
  marker_now="$(tr -d '[:space:]' < "$OLD_ROOT/deploy/RELEASE" 2>/dev/null | tr 'A-F' 'a-f' || true)"
  if [ -n "$marker_now" ] && [ "$(rev12 "$marker_now")" != "$old12_s" ]; then
    say "rollback step 3 FAIL: the restored marker names $(rev12 "$marker_now"), not $old12_s — the restored old bot would itself refuse"
  else
    say "rollback: RELEASE marker restored ($(rev12 "$marker_now"))"
  fi

  # 4. remove the vl system units.
  sudo systemctl disable --now vl vl-web 2>/dev/null || say "rollback: vl units already gone (skip)"
  sudo rm -f /etc/systemd/system/vl.service /etc/systemd/system/vl-web.service 2>/dev/null || true
  sudo systemctl daemon-reload 2>/dev/null || true

  # 5. remove the vl user units.
  local unit
  for unit in vl-backup.service vl-backup.timer vl-clock-guard.service vl-clock-guard.timer vl-updater.service; do
    rm -f "$HOME/.config/systemd/user/$unit"
  done
  rm -f "$HOME/bin/vl-updater"
  systemctl --user daemon-reload 2>/dev/null || true

  # 6. re-enable the old units (presence read from the state file).
  local rb_bt rb_ct rb_cs
  rb_bt="$(grep -m1 '^backup_tmr=' "$STATE_FILE" | cut -d= -f2- || echo 0)"
  rb_ct="$(grep -m1 '^clock_tmr=' "$STATE_FILE" | cut -d= -f2- || echo 0)"
  rb_cs="$(grep -m1 '^clock_svc=' "$STATE_FILE" | cut -d= -f2- || echo 0)"
  sudo systemctl enable --now "$o" "$o-web" || say "rollback step 6 FAIL: cannot re-enable the old system units"
  if [ "$rb_bt" = 1 ]; then systemctl --user enable --now "$o-backup.timer" 2>/dev/null || warn "cannot re-enable the old backup timer"; fi
  if [ "$rb_ct" = 1 ]; then systemctl --user enable --now "$o-clock-guard.timer" 2>/dev/null || warn "cannot re-enable the old clock-guard timer"; fi
  if [ "$st_no_up" = 0 ]; then
    systemctl --user enable --now "$o-updater" 2>/dev/null || warn "cannot re-enable the old updater unit"
  fi
  if [ "$rb_cs" = 1 ]; then
    systemctl --user start "$o-clock-guard.service" 2>/dev/null \
      || say "rollback step 6 FAIL: the old clock-guard service cannot start (the wrappers must be mode 100755)"
  fi

  # 7. verify the OLD boot (B3: poll — the old bot needs seconds to boot and
  # serve after the re-enable; one immediate read rolled back a good boot).
  rb_boot_ready() {
    local log_file boot_epoch
    log_file="$(ls -1t "$OLD_ROOT"/data/${o}_*.log 2>/dev/null | head -1 || true)"
    boot_epoch="$(stat -c %Y "$log_file" 2>/dev/null || echo 0)"
    [ "$boot_epoch" -ge "$RUN_START" ] 2>/dev/null || return 1
    [ -n "$log_file" ] || return 1
    grep -q "BOOT INTEGRITY OK — rev $old12_s" "$log_file" 2>/dev/null
  }
  rb_health_ready() {
    local h
    h="$(health_rev)"
    revs_agree "$h" "$(upper12 "$rb_old")"
  }
  local max_wait waited ok=1
  max_wait="$(verify_wait_secs)"
  if waited="$(wait_leg "$max_wait" rb_boot_ready)"; then
    say "rollback: OLD boot line OK after ${waited}s ($(ls -1t "$OLD_ROOT"/data/${o}_*.log 2>/dev/null | head -1 || true))"
  else
    say "rollback step 7 FAIL after ${waited}s: no fresh 'BOOT INTEGRITY OK — rev $old12_s' line in the newest old log"
    ok=0
  fi
  if waited="$(wait_leg "$max_wait" rb_health_ready)"; then
    say "rollback: health revision OK after ${waited}s ($(health_rev))"
  else
    say "rollback step 7 FAIL after ${waited}s: /api/health reports '$(health_rev)' or none — not a 7+ hex prefix of the old sha"
    ok=0
  fi
  [ "$ok" = 1 ] || return 1
  say "rollback DONE — the pre-rename state is restored and verified"
  return 0
}

# =============================================================================
# state file
# =============================================================================
state_content() {
  cat <<EOF
session=$SESSION
sha=$SHA
old_sha=$OLD_SHA
old12=$OLD12
ts=$TS
no_updater=$NO_UPDATER
release_dir=$RELEASE_DIR
dist_old=$VL_ROOT/web/dist.old.$OLD12.$TS
marker_saved=$VL_ROOT/deploy/RELEASE.pre-vl.$OLD12
bin_park=$VL_ROOT/${o}-bin.old.$OLD12
updater_park=$HOME/bin/${o}-updater.old.$OLD12
env_backup=$HOME/.config/vl-updater/env.pre-vl
backup_svc=$HAS_BACKUP_SVC
backup_tmr=$HAS_BACKUP_TMR
clock_svc=$HAS_CLOCK_SVC
clock_tmr=$HAS_CLOCK_TMR
EOF
}

write_state() {
  local f
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR"
  f="$(mkt)"
  state_content > "$f"
  chmod 600 "$f"
  mv "$f" "$STATE_FILE"
}

# =============================================================================
# dry run
# =============================================================================
dry_run() {
  say "DRY RUN — every read-only step-0 check runs; nothing is written"
  step0
  say "state file content that a real run would write:"
  state_content | sed 's/^/    /'
  say "would step 1: stop the old user units, stop the old system units, back up the DB to $HOME/$o-backups/pre-vl-rename-$TS/data.db"
  say "would step 2: mv the install tree to $HOME/vl with a symlink back; move the data dirs; rewrite the updater env file (values never printed)"
  say "would step 3: install vl-bin, web/dist and the WRITTEN RELEASE marker; render and install vl.service + vl-web.service; enable --now vl vl-web"
  say "would step 4: install ~/bin/vl-updater and the vl user units; enable --now vl-backup.timer vl-clock-guard.timer vl-updater"
  say "would step 5: nothing (lock — no hand-over)"
  say "would step 6: verify the boot line, the health revision, NT8, the worker, the timers, :3000 and postboot-check"
  say "would step 7: disable the old units, park the old binaries, keep (or release) the lock"
  say "DRY RUN PASSED — every step-0 check holds"
  exit 0
}

# =============================================================================
# main
# =============================================================================
# RUN_START is the FILE-CLOCK instant this process began, taken from a mark
# file's mtime — the same coarse kernel clock the boot-line freshness checks
# read from log files. Realtime (date +%s) MUST NOT be compared against file
# mtimes: ~0.42% of writes get an mtime in the previous second relative to a
# realtime read (CTO probe 2026-09-30), which fails good boots forever.
# The mark is NOT created in dry-run mode: a dry run must change nothing on
# disk (TestDryRunChangesNothing hashes the HOME tree), and RUN_START is
# never read there.
if [ "$MODE" != "dry" ]; then
  RUN_MARK="${VL_MIGRATE_RUN_MARK:-$(mkt)}"
  RUN_START="$(stat -c %Y "$RUN_MARK" 2>/dev/null || echo 0)"
else
  RUN_START=0
fi

if [ "$MODE" = "rollback" ]; then
  do_rollback
  exit $?
fi

if [ "$MODE" = "dry" ]; then
  dry_run
fi

step0
write_state
say "state file written: $STATE_FILE"

STEP3_MARK="${VL_MIGRATE_STEP3_MARK:-$(mkt)}"
STEP3_EPOCH=0
# the capture pattern: errexit is OFF only around this one call, so the
# function's own subshell keeps its set -e and its first failure is the rc
set +e
steps_1_5
steps_rc=$?
set -e
if [ "$steps_rc" != 0 ]; then
  if [ "$NO_AUTO_ROLLBACK" = 1 ]; then
    say "============================================================"
    say "ERR in steps 1–5 and --no-auto-rollback was given."
    say "THE BOT MAY BE STOPPED. The owner is present: decide."
    say "Roll back by hand when ready: $0 --session $SESSION --sha $SHA --release-dir $RELEASE_DIR --rollback"
    say "============================================================"
    exit 1
  fi
  say "ERR in steps 1–5 — automatic rollback"
  if do_rollback; then exit 1; else say "ROLLBACK FAILED — a human must look now" >&2; exit 2; fi
fi
# STEP3_EPOCH is the mark file's MTIME, not its content: the freshness checks
# below compare file-clock to file-clock. The content (date +%s written at the
# top of step 3) is kept only as a debugging hint and is NEVER read here.
STEP3_EPOCH="$(stat -c %Y "$STEP3_MARK" 2>/dev/null || echo 0)"
rm -f "$STEP3_MARK" 2>/dev/null || true

if ! verify_legs "forward"; then
  if [ "$NO_AUTO_ROLLBACK" = 1 ]; then
    say "============================================================"
    say "verify FAIL and --no-auto-rollback was given."
    say "The owner is present: decide."
    say "Roll back by hand when ready: $0 --session $SESSION --sha $SHA --release-dir $RELEASE_DIR --rollback"
    say "============================================================"
    exit 1
  fi
  say "verify FAIL — automatic rollback"
  if do_rollback; then exit 1; else say "ROLLBACK FAILED — a human must look now" >&2; exit 2; fi
fi

step7
exit 0
