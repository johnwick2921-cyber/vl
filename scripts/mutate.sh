#!/usr/bin/env bash
# mutate — a mutation test that cannot report a verdict it has not earned.
#
# WHY THIS EXISTS. A mutation test answers one question: "if I break this line,
# does a pin notice?" It is only evidence when the break actually happened. Four
# times in one week this repo recorded a SURVIVOR that was nothing of the kind:
#
#   · three seds never matched (class 89 — a search string with three spaces
#     where gofmt had left one). The file was untouched, the suite passed
#     because nothing was broken, and "SURVIVED" went into a report as proof
#     that a pin was weak. It was proof of a typo.
#   · one mutant did not compile. `go test` reported a build failure, the
#     harness read the absence of "--- FAIL" as a pass, and called it SURVIVED.
#
# Both failures share a shape: the harness inferred a verdict from the ABSENCE
# of a failure line, without first establishing that the experiment ran. So this
# script refuses to read the suite until it has proven two things happened —
# the edit landed, and the mutant builds. Any other outcome is named, never
# folded into SURVIVED.
#
#   usage: scripts/mutate.sh <file> <sed-expression> <go-test-pattern> [package]
#
#   scripts/mutate.sh store/armed_orders.go \
#       's|if row.CancelAttemptsBoot != ProcessBootID() {|if false {|' \
#       'CancelBudget' ./store/
#
# Exit codes are the verdict, so a caller can branch on them:
#   0 KILLED         the mutant broke a pin — the pin guards this line
#   1 SURVIVED       the mutant built, ran, and NOTHING failed — the pin is weak
#   2 NOT-APPLIED    the sed matched nothing — the experiment never ran
#   3 BUILD-FAILED   the mutant does not compile — the experiment never ran
#   4 USAGE/ERROR
set -uo pipefail

die() { printf 'mutate: %s\n' "$*" >&2; exit 4; }

[ $# -ge 3 ] || die "usage: mutate.sh <file> <sed-expression> <go-test-pattern> [package]"
FILE="$1"; EXPR="$2"; PATTERN="$3"; PKG="${4:-./...}"

[ -f "$FILE" ] || die "no such file: $FILE"
command -v go >/dev/null 2>&1 || die "go is not on PATH"

BACKUP="$(mktemp "${TMPDIR:-/tmp}/mutate.XXXXXX")"
cp "$FILE" "$BACKUP" || die "could not back up $FILE"

# The restore runs on EVERY exit path, including a signal. A mutation harness
# that can leave a mutant in the tree is worse than no harness: the next command
# anyone runs is against code nobody chose.
restore() { cp "$BACKUP" "$FILE"; rm -f "${BACKUP:?}"; }
trap restore EXIT INT TERM

printf '── mutating %s\n' "$FILE"

if ! sed -i "$EXPR" "$FILE" 2>/dev/null; then
  printf '\nNOT-APPLIED — sed itself errored on the expression.\n  %s\n' "$EXPR"
  exit 2
fi

# CHECK 1 — DID THE EDIT LAND? Compare against the backup, not against git:
# the file may already be dirty for unrelated reasons, and `git diff` would then
# report a change this mutation did not make.
if cmp -s "$BACKUP" "$FILE"; then
  printf '\nNOT-APPLIED — the sed matched nothing and the file is byte-identical.\n'
  printf '  expression: %s\n' "$EXPR"
  printf '  THIS IS NOT A SURVIVOR. The experiment never ran. Check the search\n'
  printf '  string against the file as it is NOW (gofmt may have re-spaced it).\n'
  exit 2
fi
printf '   edit landed:\n'
diff -u "$BACKUP" "$FILE" | grep -E '^[-+][^-+]' | sed 's/^/     /' | head -20

# CHECK 2 — DOES THE MUTANT COMPILE? A mutant that does not build cannot be
# tested, and "no --- FAIL line" from a build failure is not a passing suite.
if ! go build ./... >/tmp/mutate-build.$$ 2>&1; then
  printf '\nBUILD-FAILED — the mutant does not compile, so no pin could have run.\n'
  sed 's/^/     /' /tmp/mutate-build.$$ | head -10
  rm -f /tmp/mutate-build.$$
  printf '  THIS IS NOT A SURVIVOR. Choose a mutation that still type-checks.\n'
  exit 3
fi
rm -f /tmp/mutate-build.$$
printf '   mutant builds\n'

# CHECK 3 — ONLY NOW may the suite be read.
OUT="$(go test "$PKG" -run "$PATTERN" 2>&1)"
RC=$?
if printf '%s' "$OUT" | grep -qE '^(--- FAIL|FAIL)'; then
  printf '\nKILLED — a pin caught it.\n'
  printf '%s\n' "$OUT" | grep -E '^(--- FAIL|FAIL|ok)' | sed 's/^/     /' | head -8
  exit 0
fi

# A test binary that ran zero tests is not a pin passing; it is a pattern typo.
if printf '%s' "$OUT" | grep -q 'no tests to run\|warning: no tests'; then
  printf '\nNOT-APPLIED — the pattern %q matched no test in %s.\n' "$PATTERN" "$PKG"
  printf '  THIS IS NOT A SURVIVOR: nothing was asked to catch the mutant.\n'
  exit 2
fi

printf '\nSURVIVED — the edit landed, the mutant built, the suite ran, and nothing failed.\n'
printf '  The pin does not guard this line. (go test rc=%d)\n' "$RC"
printf '%s\n' "$OUT" | grep -E '^(ok|---)' | sed 's/^/     /' | head -8
exit 1
