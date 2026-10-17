#!/usr/bin/env bash
# W-ONE-BUTTON M4 — prove a rollback pair before advertising it.
#
# A manifest that says rollback_pairs:[{to_release:X, tested:true}] is a promise
# that the owner can go back to X. The only way to keep that promise is to have
# actually done it, in this order:
#
#   1. the OLD binary creates a FRESH database   (its own schema)
#   2. the NEW binary boots against that database (forward migration)
#   3. the OLD binary boots against the migrated database (the rollback itself)
#
# Step 3 is the one that matters and the one nobody runs: a forward migration
# that drops a column the old binary still SELECTs makes the rollback fail at
# the moment it is needed most.
#
# THE NETWORK IS ACTUALLY DISABLED, not merely asserted: each boot runs under
# `unshare -rn` (its own empty network namespace), so the binary cannot reach a
# broker, an AI endpoint or NT8 even if something tried. A comment claiming
# "network disabled" while the process can still dial out is a claim nobody
# checked; this one is enforced, and when unshare is unavailable the script
# SAYS the claim does not hold rather than pretending it does.
#
# Success is judged on THE DATABASE, not on the process: the file exists,
# carries its core tables, and no migration error was logged. With no NT8 the
# binary is not expected to stay up, so its exit code says nothing about the
# schema.
#
# KNOWN LIMIT — BOOT-TIME ONLY. This proves the SCHEMA survives the round trip:
# old creates, new migrates, old reads it back. It does NOT prove runtime
# behaviour on that data — a query that only runs mid-session, a migration that
# rewrites rows lazily, or a code path reached after the first trade are all
# outside what a boot can observe. A pair marked tested:true means "the rollback
# boots and reads its schema", never "the rollback is safe in every respect".
#
# Usage: db-compat.sh <old-ref> <new-ref> [work-dir]
# Exit 0 = the pair is proven. Non-zero = NOT proven; the caller must advertise
# tested:false (or omit the pair) rather than guess.
set -uo pipefail
OLD_REF="${1:-}"; NEW_REF="${2:-}"; WORK="${3:-$(mktemp -d)}"
[ -n "$OLD_REF" ] && [ -n "$NEW_REF" ] || { echo "db-compat: usage: db-compat.sh <old-ref> <new-ref> [work-dir]" >&2; exit 2; }
REPO="$(git rev-parse --show-toplevel)"
BOOT_SECS="${DB_COMPAT_BOOT_SECS:-45}"
mkdir -p "$WORK"

build_at() { # <ref> <out>
  local ref="$1" out="$2" wt="$WORK/src-$1"
  git -C "$REPO" worktree add --detach "$wt" "$ref" >/dev/null 2>&1 || { echo "db-compat: cannot check out $ref"; return 1; }
  ( cd "$wt" && go build -trimpath -o "$out" ./ ) || { echo "db-compat: build failed at $ref"; return 1; }
  git -C "$REPO" worktree remove --force "$wt" >/dev/null 2>&1 || true
  return 0
}

# Run a binary long enough to open and migrate the database, then stop it. The
# exit code is deliberately NOT the verdict: with no NT8 the process may exit on
# its own, and that says nothing about the schema.
# NETNS: the real isolation, with an honest fallback.
NETNS=""
if unshare -rn true >/dev/null 2>&1; then
  NETNS="unshare -rn"
  echo "db-compat: network DISABLED for every boot (unshare -rn)"
else
  echo "db-compat: WARNING — unshare -rn unavailable; boots are NOT network-isolated here"
fi

migrate_with() { # <binary> <datadir> <label>
  # NOTE: a single `local a=$1 b="$WORK/$a.log"` expands $a BEFORE it is
  # assigned, which `set -u` rejects with "unbound variable". Found by the
  # first REAL run — the script had only ever been syntax-checked, and
  # `bash -n` cannot see this.
  local bin="$1" dir="$2" label="$3"
  local log="$WORK/$label.log"
  mkdir -p "$dir"
  ( cd "$dir" && $NETNS env RSA_PRIVATE_KEY="$RSA_PRIVATE_KEY" \
      DATA_ENCRYPTION_KEY="$DATA_ENCRYPTION_KEY" JWT_SECRET="$JWT_SECRET" \
      timeout "${BOOT_SECS}s" "$bin" >"$log" 2>&1 ) || true
  if [ ! -f "$dir/data/data.db" ]; then
    echo "db-compat: $label — no database was created at $dir/data/data.db"
    tail -20 "$log" | sed 's/^/    /'
    return 1
  fi
  # A migration error must fail the step even if the file exists.
  if grep -qiE 'migrat(e|ion).*(fail|error)|AutoMigrate.*error|no such column|table .* has no column' "$log"; then
    echo "db-compat: $label — migration error in the log:"
    grep -iE 'migrat(e|ion).*(fail|error)|AutoMigrate.*error|no such column|table .* has no column' "$log" | head -5 | sed 's/^/    /'
    return 1
  fi
  local n
  n="$(sqlite3 "$dir/data/data.db" "select count(*) from sqlite_master where type='table';" 2>/dev/null || echo 0)"
  if [ "${n:-0}" -lt 5 ]; then
    echo "db-compat: $label — only $n table(s); the schema did not migrate"
    return 1
  fi
  echo "db-compat: $label ok ($n tables)"
  return 0
}

# EPHEMERAL BOOT SECRETS — found by the first real run, which reported
# NOT PROVEN for a reason that had nothing to do with the schema:
#
#   [FATA] main.go:60 Failed to initialize encryption service:
#          environment variable RSA_PRIVATE_KEY not set
#
# The binary FATALs on a missing RSA key BEFORE it reaches store init, so
# without these it can never create the database this job exists to compare.
# They are generated fresh per run into the work dir, are throwaway by
# construction, and are never printed. A real key must never appear here: this
# job proves a SCHEMA round trip, and needs a key only because the boot path
# demands one.
gen_boot_secrets() {
  [ -f "$WORK/rsa.pem" ] && return 0
  openssl genrsa -out "$WORK/rsa.pem" 2048 >/dev/null 2>&1 || {
    echo "db-compat: REFUSED — cannot generate an ephemeral RSA key (openssl missing?)"; return 1; }
  chmod 600 "$WORK/rsa.pem"
  export RSA_PRIVATE_KEY; RSA_PRIVATE_KEY="$(cat "$WORK/rsa.pem")"
  # crypto/crypto.go names exactly two: RSA_PRIVATE_KEY (PEM) and
  # DATA_ENCRYPTION_KEY (AES, base64). Both are demanded before store init, so
  # both must exist for the binary to reach the migration this job measures.
  export DATA_ENCRYPTION_KEY; DATA_ENCRYPTION_KEY="$(openssl rand -base64 32)"
  # A random secret, never a literal: this repo is public, and the /updates
  # gate refuses every JWT secret the tree publishes (api/handler_updates_secret_test.go).
  export JWT_SECRET; JWT_SECRET="$(openssl rand -base64 48)"
  return 0
}

# P3 (CTO): a throwaway key is still a key. It is shredded and unset when the
# run ends by ANY path — success, failure, or interrupt — so it cannot outlive
# the job in a work dir someone later tars up.
cleanup_boot_secrets() {
  [ -f "$WORK/rsa.pem" ] && { shred -u "$WORK/rsa.pem" 2>/dev/null || rm -f "$WORK/rsa.pem"; }
  unset RSA_PRIVATE_KEY DATA_ENCRYPTION_KEY JWT_SECRET
}
trap cleanup_boot_secrets EXIT INT TERM

OLD_BIN="$WORK/nofx-old"; NEW_BIN="$WORK/nofx-new"
gen_boot_secrets || exit 1
build_at "$OLD_REF" "$OLD_BIN" || exit 1
build_at "$NEW_REF" "$NEW_BIN" || exit 1

INST="$WORK/install"
echo "db-compat: pair $OLD_REF -> $NEW_REF"
migrate_with "$OLD_BIN" "$INST" "1-old-creates-fresh"      || { echo "db-compat: NOT PROVEN"; exit 1; }
migrate_with "$NEW_BIN" "$INST" "2-new-migrates-forward"   || { echo "db-compat: NOT PROVEN"; exit 1; }
# THE ROLLBACK ITSELF — the step that is normally skipped.
migrate_with "$OLD_BIN" "$INST" "3-old-boots-migrated-db"  || { echo "db-compat: NOT PROVEN — the rollback is the failing step"; exit 1; }
echo "db-compat: PROVEN $OLD_REF <-> $NEW_REF"
