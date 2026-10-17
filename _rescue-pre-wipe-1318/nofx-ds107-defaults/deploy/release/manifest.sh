#!/usr/bin/env bash
# W-ONE-BUTTON M4 — the manifest the updater reads before it touches anything.
#
# Every field is READ from the staged tree or passed in explicitly. Nothing is
# inferred and nothing is defaulted to a plausible value: a pair that has not
# been proven by the DB-compat job is advertised tested:false, because an
# untested rollback that claims to be tested is worse than no rollback at all.
#
# And an UNCOMPUTED list is null, never []. [] means "a job computed this and
# found nothing"; null means "nobody computed it". A reader that cannot tell
# those apart will treat an unrun job as a proven-empty result.
set -uo pipefail
STAGE="${1:-}"; SRC_SHA="${2:-}"; REL_ID="${3:-}"
[ -d "${STAGE:-}" ] && [ -n "${SRC_SHA:-}" ] && [ -n "${REL_ID:-}" ] || {
  echo "manifest: usage: manifest.sh <stage-dir> <source-sha40> <release-id>" >&2; exit 2; }
case "$SRC_SHA" in *[!0-9a-f]*|"") echo "manifest: REFUSED — source sha must be 40 hex: $SRC_SHA" >&2; exit 1;; esac
[ "${#SRC_SHA}" -eq 40 ] || { echo "manifest: REFUSED — source sha must be 40 hex (got ${#SRC_SHA})" >&2; exit 1; }

# P3: the packaged marker and the manifest must agree. package.sh WRITES
# deploy/RELEASE from the source sha; if the two ever diverge the archive would
# claim one revision and carry another, and the updater trusts the manifest.
STAGED_REL="$STAGE/deploy/RELEASE"
[ -f "$STAGED_REL" ] || { echo "manifest: REFUSED — $STAGED_REL missing (package.sh writes it)" >&2; exit 1; }
STAGED_SHA="$(tr -d '[:space:]' < "$STAGED_REL")"
[ "$STAGED_SHA" = "$SRC_SHA" ] || {
  echo "manifest: REFUSED — packaged deploy/RELEASE ($STAGED_SHA) != source sha ($SRC_SHA)" >&2; exit 1; }

# A manifest that lists ITSELF at 0 bytes is describing a placeholder, not an
# artifact: the entry carries the sha256 of the empty string, which matches
# nothing that will ever be shipped, and a verifier checking it would either
# fail on a good archive or — worse — pass it by treating the empty hash as a
# special case. Refuse, rather than emit an entry that cannot be true.
arts="$(cd "$STAGE" && find . -type f -printf '%P\n' | LC_ALL=C sort | while read -r p; do
  bytes="$(stat -c%s "$p")"
  case "$p" in
    manifest.json)
      if [ "$bytes" -eq 0 ]; then
        echo "manifest: REFUSED — the staged manifest.json is 0 bytes, so the manifest would list itself as an empty artifact whose sha256 matches nothing that ships" >&2
        exit 1
      fi ;;
  esac
  printf '{"path":"%s","sha256":"%s","bytes":%s}\n' "$p" "$(sha256sum "$p" | cut -d' ' -f1)" "$bytes"
done | paste -sd, -)" || exit 1

# Read from the AddOn source rather than restated here (L7: READ, never literal).
ADDON_BUILD="$(grep -hoE 'VL_BUILD_ID[^"]*"[^"]+"' "$STAGE"/ninjascript/*.cs 2>/dev/null | head -1 | sed 's/.*"\(.*\)"/\1/')"
[ -n "$ADDON_BUILD" ] || ADDON_BUILD="n/a"
# THE PROTOCOL VERSION COMES FROM THE CODE, NOT THE DOCUMENTATION.
# This used to grep the first `protocol_version` out of
# ninjascript/vltrader_tcp_PROTOCOL.md — and the first match in that file is a
# JSON EXAMPLE showing 2, while the shipped wire has been 3 since the
# symbol-tagged-fills generation. The manifest therefore reported a protocol
# version the release does not speak, to an updater that trusts the manifest
# for compatibility. The line directly above reads the AddOn build id "from the
# AddOn source rather than restated here (L7: READ, never literal)"; this line
# read prose instead.
#
# The value is a constant in two places that must agree — PROTOCOL_VERSION in
# the C# client and ProtocolVersion in provider/ninjatrader/tcp_framing.go —
# so both are read and a DISAGREEMENT is a refusal, not a preference.
PROTO_CS="$(grep -hoE 'PROTOCOL_VERSION[^=]*=[[:space:]]*([0-9]+)' "$STAGE"/ninjascript/*.cs 2>/dev/null | head -1 | grep -oE '[0-9]+$')"
PROTO_GO="$(grep -hoE '^const ProtocolVersion[[:space:]]*=[[:space:]]*([0-9]+)' "$STAGE"/provider/ninjatrader/tcp_framing.go 2>/dev/null | head -1 | grep -oE '[0-9]+$')"
if [ -n "$PROTO_CS" ] && [ -n "$PROTO_GO" ] && [ "$PROTO_CS" != "$PROTO_GO" ]; then
  echo "manifest: REFUSED — the C# PROTOCOL_VERSION ($PROTO_CS) and Go ProtocolVersion ($PROTO_GO) disagree; they ship in lockstep or not at all" >&2
  exit 1
fi
PROTO="${PROTO_GO:-$PROTO_CS}"
# Absent is NOT zero and NOT a guess: an unread value is null (A24).
[ -n "$PROTO" ] || PROTO="null"

# OWNER DATA vs PROGRAM ARTIFACTS — a distinction the archive has to carry,
# because activation treats them differently and nothing else records which is
# which. calendar_static_t1.json is the owner-EDITABLE static T1 blackout file
# (trader/auto_trader_calendar.go:113-125): the binary reads it from the
# working directory, and an owner is expected to edit it.
#
# Activate installs the binary, the bundle and the marker — and NOT this. That
# is correct: overwriting it would silently discard the owner's edits on every
# update. But it has a consequence worth naming rather than leaving implied: an
# installation that has never had the file gets nil from the loader and loses
# the static blackout fallback, with only a warning (FetchWeek reports
# SourceNone). The file ships as a TEMPLATE so a fresh install has one; it is
# never installed over an existing one.
#
# Listing it under "artifacts" alone would tell an updater it is a program file
# to put in place. Listing it here says what it actually is.
owner_data="$(cd "$STAGE" && for p in calendar_static_t1.json; do
  [ -f "$p" ] || continue
  printf '{"path":"%s","sha256":"%s","bytes":%s,"install":"template-only","reason":"owner-editable; activation never overwrites it"}\n' \
    "$p" "$(sha256sum "$p" | cut -d' ' -f1)" "$(stat -c%s "$p")"
done | paste -sd, -)"

cat <<JSON
{
  "release_id": "$REL_ID",
  "source_sha": "$SRC_SHA",
  "artifacts": [${arts}],
  "owner_data": [${owner_data}],
  "platform": { "os": "linux", "arch": "amd64", "wsl": true },
  "nt8": { "min_version": "${NT8_MIN:-8.1.2.1}", "max_tested_version": "${NT8_MAX_TESTED:-n/a}" },
  "addon": { "build_id": "$ADDON_BUILD", "protocol_version": $PROTO, "transition_order": "${ADDON_TRANSITION_ORDER:-go-then-addon}" },
  "updater_min_version": "${UPDATER_MIN:-0.0.0}",
  "upgrade_pairs": ${UPGRADE_PAIRS:-null},
  "rollback_pairs": ${ROLLBACK_PAIRS:-null},
  "capabilities_required": ${CAPABILITIES_REQUIRED:-null},
  "data_readiness": [ { "mode": "picture_htf", "tf": "4h", "bars": "pivot_window+4" } ]
}
JSON
