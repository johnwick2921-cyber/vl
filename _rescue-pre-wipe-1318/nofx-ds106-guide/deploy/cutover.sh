#!/usr/bin/env bash
# W-ONE-BUTTON M4 — the manual boot, v7.
#
#   usage: cutover.sh [--dry-run] <new-sha40> <new-bin> [new-dist]
#
# v7 is a THIN WRAPPER over cmd/vl-activate. v6 reimplemented the activation
# steps in bash; two implementations of a safety procedure drift, and the one
# that drifts is the one nobody runs until an incident. Everything that can be
# expressed as a step now runs the same Go code the unattended worker (3b-B)
# will run. What stays here is what only the attended boot does: parse the
# operator's arguments, hold the token gate, and refuse.
#
# v6 SEMANTICS PRESERVED, deliberately and testably:
#   --dry-run performs every preflight and kills/swaps/writes NOTHING
#   the flat gate (class 33) must answer READY before anything is touched
#   NOFX_CUTOVER_TOKEN is read from the environment, NEVER a command-line arg
#   the new binary is PROVEN before anything moves (two distinct refusals,
#     class 248: no vcs stamps at all vs stamped with a different revision)
#   the dist must carry the sha being installed
#   rollback restores ALL THREE halves and proves the OLD rev
#
# ONE REAL DEFECT v6 CARRIED, fixed by the delegation rather than patched:
#   start_ticks() read /proc/<pid>/stat with `split($0,a," "); print a[22]`.
#   The comm field is in parentheses and MAY CONTAIN SPACES AND PARENTHESES,
#   which shifts every later field — so the identity check could compare the
#   wrong number and either refuse a valid restart or, worse, accept a
#   recycled pid. internal/activation parses from the LAST ')' and is pinned
#   by a test whose comm is literally "(vl bin (x))".
#
# NO SECRETS ARE WRITTEN OR ECHOED ANYWHERE IN THIS FILE.
set -uo pipefail

DRY=0
[ "${1:-}" = "--dry-run" ] && { DRY=1; shift; }
NEW_SHA="${1:-}"; NEW_BIN="${2:-}"; NEW_DIST="${3:-}"
# Shell twins VL_ → NOFX_ → default; the install default is $HOME/vl and
# the unit default is vl (plan v7 FINAL D2 item 7). R5 removes the NOFX twins.
UNIT="${VL_UNIT:-${NOFX_UNIT:-vl}}"
INSTALL="${VL_INSTALL:-${NOFX_INSTALL:-$HOME/vl}}"
ACTIVATE="${VL_ACTIVATE_BIN:-${NOFX_ACTIVATE_BIN:-}}"

say()  { printf 'cutover: %s\n' "$*"; }
plan() { printf 'cutover: WOULD %s\n' "$*"; }
die()  { printf 'cutover: REFUSED — %s\n' "$*" >&2; exit 1; }

[ -n "$NEW_SHA" ] && [ -n "$NEW_BIN" ] || die "usage: cutover.sh [--dry-run] <new-sha40> <new-bin> [new-dist]"
case "$NEW_SHA" in
  *[!0-9a-f]*|"") die "<new-sha40> must be 40 lowercase hex characters, got '$NEW_SHA'" ;;
esac
[ "${#NEW_SHA}" -eq 40 ] || die "<new-sha40> must be 40 characters, got ${#NEW_SHA}"
SHORT="${NEW_SHA:0:12}"

# --- the activation binary ----------------------------------------------------
# Built from THIS tree when not supplied, so the boot never runs a stale helper
# left over from an earlier release.
if [ -z "$ACTIVATE" ]; then
  ACTIVATE="$(mktemp -t vl-activate.XXXXXX)"
  trap 'rm -f "$ACTIVATE"' EXIT
  go build -o "$ACTIVATE" ./cmd/vl-activate 2>/dev/null \
    || die "cannot build cmd/vl-activate from this tree; pass VL_ACTIVATE_BIN=<path> (or NOFX_ACTIVATE_BIN until R5) if you have one"
fi
[ -x "$ACTIVATE" ] || die "$ACTIVATE is not executable"

# --- prove the new binary BEFORE anything is touched --------------------------
# A release dir is assembled beside the inputs so the same `verify` the worker
# runs is the one that answers here. The manifest records what the operator
# asserted; verify decides whether the binary agrees.
STAGE_DIR="$(mktemp -d -t vl-cutover.XXXXXX)"
trap 'rm -rf "$STAGE_DIR"; [ -n "${ACTIVATE:-}" ] && [ -z "${VL_ACTIVATE_BIN:-}${NOFX_ACTIVATE_BIN:-}" ] && rm -f "$ACTIVATE"; rm -f "${TOKEN_HDR:-}"' EXIT
[ -f "$NEW_BIN" ] || die "new binary $NEW_BIN not found"
cp "$NEW_BIN" "$STAGE_DIR/vl-bin" || die "cannot stage $NEW_BIN"
NEW_MD5="$(md5sum "$STAGE_DIR/vl-bin" | cut -d' ' -f1)"
printf '{"source_sha":"%s","binary_md5":"%s","signature_verdict":"attended-boot"}\n' \
  "$NEW_SHA" "$NEW_MD5" > "$STAGE_DIR/manifest.json"

VERIFY_OUT="$("$ACTIVATE" verify -release "$STAGE_DIR" 2>&1)" || {
  printf '%s\n' "$VERIFY_OUT" | sed 's/^/    /' >&2
  die "the new binary did not verify — see the receipt above"
}
say "new binary proven: vcs.revision=$SHORT vcs.modified=false md5=$NEW_MD5"

# --- the dist must carry the sha ---------------------------------------------
DIST="$INSTALL/web/dist"
SRC_DIST="${NEW_DIST:-$DIST}"
[ -d "$SRC_DIST" ] || die "$SRC_DIST missing — build it with VITE_GUIDE_BUILT_REV=$NEW_SHA npm run build"
grep -rql "$NEW_SHA" "$SRC_DIST" 2>/dev/null >/dev/null \
  || die "the bundle at $SRC_DIST does not carry $NEW_SHA.
    Build it with:  cd web && VITE_GUIDE_BUILT_REV=$NEW_SHA npm run build"
say "dist carries $SHORT"

# --- what is running now ------------------------------------------------------
# Install side: vl-bin wins when both exist. R5 removes the nofx branch.
INSTALL_BIN="$INSTALL/vl-bin"; [ -f "$INSTALL_BIN" ] || INSTALL_BIN="$INSTALL/nofx-bin"
OLD_SHA="$(go version -m "$INSTALL_BIN" 2>/dev/null | tr '\t' ' ' \
  | awk '{for(i=1;i<=NF;i++) if($i ~ /^vcs\.revision=/){sub(/^vcs\.revision=/,"",$i); print $i; exit}}')"
[ -n "$OLD_SHA" ] || die "cannot read vcs.revision from the CURRENT binary; refusing a cutover with no way back"
OLD_SHORT="${OLD_SHA:0:12}"
RELEASES="${VL_RELEASE_DIR:-${NOFX_RELEASE_DIR:-$INSTALL/releases}}" # R5 removes the NOFX twin
say "current: rev=$OLD_SHORT  releases → $RELEASES"

# --- reconcile OLD_SHA with what is ACTUALLY running -------------------------
# The disk binary alone is not the running build: a crashed staging leaves a
# never-proven file on disk while the old process keeps serving. Reconcile the
# way back against BOTH /api/health (the running process's own revision) and
# the RELEASE marker. A mismatch, or neither consultable, refuses the cutover.
HEALTH_URL="${VL_HEALTH_URL:-${NOFX_HEALTH_URL:-http://127.0.0.1:8080/api/health}}" # R5 removes the NOFX twin
HEALTH_REV="$(curl -s --max-time 5 "$HEALTH_URL" 2>/dev/null \
  | sed -n 's/.*"revision"[[:space:]]*:[[:space:]]*"\([0-9a-fA-F]*\)".*/\1/p' | tr 'A-F' 'a-f')"
RELEASE_REV="$([ -f "$INSTALL/RELEASE" ] && tr -d '[:space:]' < "$INSTALL/RELEASE" 2>/dev/null | tr 'A-F' 'a-f')"
rev12() { v="$1"; if [ ${#v} -ge 12 ]; then printf '%s' "${v:0:12}"; else printf '%s' "$v"; fi; }
OLD12="$(rev12 "$OLD_SHA")"
if [ -n "$HEALTH_REV" ] && [ "$(rev12 "$HEALTH_REV")" != "$OLD12" ]; then
  die "OLD_SHA mismatch: disk=$OLD_SHORT but the RUNNING process reports $(rev12 "$HEALTH_REV") via /api/health — refusing a cutover whose way back is not the running build"
fi
if [ -n "$RELEASE_REV" ] && [ "$(rev12 "$RELEASE_REV")" != "$OLD12" ]; then
  die "OLD_SHA mismatch: disk=$OLD_SHORT but $INSTALL/RELEASE says $(rev12 "$RELEASE_REV") — refusing a cutover whose way back is not what the marker names"
fi
if [ -z "$HEALTH_REV" ] && [ -z "$RELEASE_REV" ]; then
  die "cannot reconcile OLD_SHA=$OLD_SHORT — neither /api/health nor $INSTALL/RELEASE answered; refusing a cutover with no proof of what is running"
fi
say "current reconciled: disk=$OLD_SHORT health=$(rev12 "${HEALTH_REV:-}") release=$(rev12 "${RELEASE_REV:-}")"

# --- THE INSTALLATION GATE (W-ONE-BUTTON M2) -------------------------------
# Nothing is touched until every REQUIRED leg passes. The payload's overall
# "ready" verdict is NEVER trusted: it folds in legs like addon_census that can
# never pass on a bot that has not been held, so a green gate could still hide
# a trader holding a position (finding [1]). Every leg is printed; a required
# leg that is absent, unevaluable or failing refuses the cutover. The token
# comes from the environment and is never echoed, never logged, and never
# accepted as an argument.
[ -n "${NOFX_CUTOVER_TOKEN:-}" ] || die "cutover gate needs a token — set NOFX_CUTOVER_TOKEN (never pass it on the command line)"
GATE_URL="${VL_GATE_URL:-${NOFX_GATE_URL:-http://127.0.0.1:8080/api/installation-gate}}" # R5 removes the NOFX twin
# The token never rides ANY process's argv ([25]/[29]): it is written to a 0600
# header file and handed to curl as -H @file, so ps and /proc/<pid>/cmdline show
# only the file path for the call's lifetime, and the file is removed on every
# exit path.
TOKEN_HDR="$(mktemp -t vl-cutover-hdr.XXXXXX)" || die "cannot create the token header file; refusing"
( umask 077; printf 'Authorization: Bearer %s' "$NOFX_CUTOVER_TOKEN" > "$TOKEN_HDR" ) \
  || { rm -f "$TOKEN_HDR"; die "cannot write the token header file; refusing"; }
GATE="$(curl -s --max-time 10 -H "@$TOKEN_HDR" "$GATE_URL" 2>/dev/null || true)"
rm -f "$TOKEN_HDR"
[ -n "$GATE" ] || die "the installation gate did not answer; refusing to kill a trader whose state is unknown"
LEGS="$(printf '%s' "$GATE" | jq -r '.legs[]? | "\(.name)\t\(.pass)"' 2>/dev/null)" \
  || die "the installation gate answered something that is not a gate payload; refusing a cutover over unreadable state"
[ -n "$LEGS" ] || die "the installation gate payload names no legs; refusing a cutover over unreadable state"
printf '%s\n' "$LEGS" | sed 's/^/    leg: /'

require_legs() { # $1 = name or glob; every matching leg must pass, one must exist
  found=0; bad=""
  while IFS=$'\t' read -r name pass; do
    case "$name" in
      $1)
        found=$((found+1))
        [ "$pass" = "true" ] || bad="$bad $name"
        ;;
    esac
  done <<LEGS_EOF
$LEGS
LEGS_EOF
  [ "$found" -gt 0 ] || die "the installation gate names no $1 leg; refusing (an unevaluable leg is a failure)"
  [ -z "$bad" ] || die "failing installation-gate legs:$bad — refusing the cutover"
}

require_legs 'trader_cutover:*'
require_legs 'ledger_exposure'
require_legs 'planner_in_flight'
require_legs 'traders_nt8'
# addon_census_prehold lands with #206; require it the moment the payload has it.
if printf '%s\n' "$LEGS" | cut -f1 | grep -qx 'addon_census_prehold'; then
  require_legs 'addon_census_prehold'
fi
say "installation gate READY — every required leg passed"

if [ "$DRY" -eq 1 ]; then
  plan "back up $INSTALL/data/data.db with vl-activate backup (online copy + integrity_check)"
  plan "install the release as $RELEASES/$NEW_SHA and repoint current"
  plan "run: vl-activate activate -release $RELEASES/$NEW_SHA -prev $RELEASES/$OLD_SHA"
  plan "  which installs binary + dist + RELEASE atomically, THEN kills the unit's"
  plan "  MainPID only if /proc/<pid>/stat field 22 still matches (a recycled pid is refused)"
  plan "run: vl-activate watch -release $RELEASES/$NEW_SHA -log <the NEWEST data/{vl,nofx}_*.log>"
  plan "  GREEN needs BOTH a boot line newer than the kill AND /api/health reporting $SHORT"
  plan "on ANY failure BEFORE anything moved (verify, gate, staging, backup):"
  plan "  REFUSE and stop — the running bot is NOT touched and NO rollback runs"
  plan "  (a healthy bot must never be restarted for a cutover that never started)"
  plan "on a failure AFTER vl-activate began installing: vl-activate rollback"
  plan "  -prev $RELEASES/$OLD_SHA, restoring all three halves, and prove $OLD_SHORT"
  plan "  came back"
  say "dry run complete — nothing was killed, swapped or written"
  exit 0
fi

die "unattended activation is not enabled in v7 from this script.
    Run the steps explicitly with the owner present, each printing its receipt:
      vl-activate backup   -db $INSTALL/data/data.db
      vl-activate activate -release $RELEASES/$NEW_SHA -prev $RELEASES/$OLD_SHA
      vl-activate watch    -release $RELEASES/$NEW_SHA    # -log defaults to the NEWEST data/{vl,nofx}_*.log; do NOT build it from today's date
      vl-activate rollback -prev $RELEASES/$OLD_SHA        # if watch refuses
    NO UNATTENDED DEPLOYS is canon: a cutover needs the owner reachable and
    acking the boot line, or a tested auto-rollback. The worker (3b-B) is the
    supported unattended path, and it is not built yet."
