#!/usr/bin/env bash
# W-ONE-BUTTON M4 — the manual boot, v7.
#
#   usage: cutover.sh [--dry-run] <new-sha40> <new-bin> [new-dist]
#
# v7 is a THIN WRAPPER over cmd/nofx-activate. v6 reimplemented the activation
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
#   by a test whose comm is literally "(nofx bin (x))".
#
# NO SECRETS ARE WRITTEN OR ECHOED ANYWHERE IN THIS FILE.
set -uo pipefail

DRY=0
[ "${1:-}" = "--dry-run" ] && { DRY=1; shift; }
NEW_SHA="${1:-}"; NEW_BIN="${2:-}"; NEW_DIST="${3:-}"
UNIT="${NOFX_UNIT:-nofx}"
INSTALL="${NOFX_INSTALL:-$HOME/nofx}"
ACTIVATE="${NOFX_ACTIVATE_BIN:-}"

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
  ACTIVATE="$(mktemp -t nofx-activate.XXXXXX)"
  trap 'rm -f "$ACTIVATE"' EXIT
  go build -o "$ACTIVATE" ./cmd/nofx-activate 2>/dev/null \
    || die "cannot build cmd/nofx-activate from this tree; pass NOFX_ACTIVATE_BIN=<path> if you have one"
fi
[ -x "$ACTIVATE" ] || die "$ACTIVATE is not executable"

# --- prove the new binary BEFORE anything is touched --------------------------
# A release dir is assembled beside the inputs so the same `verify` the worker
# runs is the one that answers here. The manifest records what the operator
# asserted; verify decides whether the binary agrees.
STAGE_DIR="$(mktemp -d -t nofx-cutover.XXXXXX)"
trap 'rm -rf "$STAGE_DIR"; [ -n "${ACTIVATE:-}" ] && [ -z "${NOFX_ACTIVATE_BIN:-}" ] && rm -f "$ACTIVATE"' EXIT
[ -f "$NEW_BIN" ] || die "new binary $NEW_BIN not found"
cp "$NEW_BIN" "$STAGE_DIR/nofx-bin" || die "cannot stage $NEW_BIN"
NEW_MD5="$(md5sum "$STAGE_DIR/nofx-bin" | cut -d' ' -f1)"
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
OLD_SHA="$(go version -m "$INSTALL/nofx-bin" 2>/dev/null | tr '\t' ' ' \
  | awk '{for(i=1;i<=NF;i++) if($i ~ /^vcs\.revision=/){sub(/^vcs\.revision=/,"",$i); print $i; exit}}')"
[ -n "$OLD_SHA" ] || die "cannot read vcs.revision from the CURRENT binary; refusing a cutover with no way back"
OLD_SHORT="${OLD_SHA:0:12}"
RELEASES="${NOFX_RELEASE_DIR:-$INSTALL/releases}"
say "current: rev=$OLD_SHORT  releases → $RELEASES"

# --- THE FLAT GATE (class 33) -------------------------------------------------
# Nothing is touched until every leg passes. An unevaluable leg is a FAILURE.
# The token comes from the environment and is never echoed, never logged, and
# never accepted as an argument.
[ -n "${NOFX_CUTOVER_TOKEN:-}" ] || die "cutover gate needs a token — set NOFX_CUTOVER_TOKEN (never pass it on the command line)"
GATE="$(curl -s --max-time 10 -H "Authorization: Bearer ${NOFX_CUTOVER_TOKEN}" \
         http://127.0.0.1:8080/api/cutover-gate 2>/dev/null || true)"
[ -n "$GATE" ] || die "the cutover gate did not answer; refusing to kill a trader whose state is unknown"
printf '%s\n' "$GATE" | sed 's/^/    gate: /'
printf '%s' "$GATE" | grep -qi '"ready"[[:space:]]*:[[:space:]]*true' \
  || die "the cutover gate is NOT ready — a leg failed above. An unevaluable leg counts as a failure (A5)"
say "flat gate READY — every leg passed"

if [ "$DRY" -eq 1 ]; then
  plan "back up $INSTALL/data/data.db with nofx-activate backup (online copy + integrity_check)"
  plan "install the release as $RELEASES/$NEW_SHA and repoint current"
  plan "run: nofx-activate activate -release $RELEASES/$NEW_SHA -prev $RELEASES/$OLD_SHA"
  plan "  which installs binary + dist + RELEASE atomically, THEN kills the unit's"
  plan "  MainPID only if /proc/<pid>/stat field 22 still matches (a recycled pid is refused)"
  plan "run: nofx-activate watch -release $RELEASES/$NEW_SHA -log <the NEWEST data/nofx_*.log>"
  plan "  GREEN needs BOTH a boot line newer than the kill AND /api/health reporting $SHORT"
  plan "on ANY failure: nofx-activate rollback -prev $RELEASES/$OLD_SHA, restoring all three"
  plan "  halves, and prove $OLD_SHORT came back"
  say "dry run complete — nothing was killed, swapped or written"
  exit 0
fi

die "unattended activation is not enabled in v7 from this script.
    Run the steps explicitly with the owner present, each printing its receipt:
      nofx-activate backup   -db $INSTALL/data/data.db
      nofx-activate activate -release $RELEASES/$NEW_SHA -prev $RELEASES/$OLD_SHA
      nofx-activate watch    -release $RELEASES/$NEW_SHA    # -log defaults to the NEWEST data/nofx_*.log; do NOT build it from today's date
      nofx-activate rollback -prev $RELEASES/$OLD_SHA        # if watch refuses
    NO UNATTENDED DEPLOYS is canon: a cutover needs the owner reachable and
    acking the boot line, or a tested auto-rollback. The worker (3b-B) is the
    supported unattended path, and it is not built yet."
