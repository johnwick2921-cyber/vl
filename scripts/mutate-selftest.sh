#!/usr/bin/env bash
# mutate-selftest — the harness's own pins.
#
# The whole point of scripts/mutate.sh is that it refuses to say SURVIVED unless
# the experiment demonstrably ran. So the cases that MUST be distinguishable are
# the ones this tests: a sed that matches nothing, a mutant that will not
# compile, a pattern that selects no test, and a real survivor. Each has its own
# exit code, and none of them is 1 except the real survivor.
#
#   scripts/mutate-selftest.sh        # rc 0 = all pins pass
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

MUTATE=scripts/mutate.sh
[ -x "$MUTATE" ] || { echo "SELFTEST FAIL: $MUTATE is not executable"; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/mutate-selftest.XXXXXX")"
trap 'rm -rf "${WORK:?}"' EXIT

pass=0; fail=0
check() { # check <name> <want-rc> <got-rc> <output>
  if [ "$2" = "$3" ]; then printf '  PASS  %s (rc %s)\n' "$1" "$3"; pass=$((pass+1))
  else printf '  FAIL  %s — want rc %s, got %s\n' "$1" "$2" "$3"; printf '%s\n' "$4" | sed 's/^/        /' | head -6; fail=$((fail+1)); fi
}

# A throwaway Go module, so the harness is exercised end to end without touching
# the repo's own source.
mkdir -p "$WORK/pkg"
cat > "$WORK/go.mod" <<'GOMOD'
module mutatetest

go 1.21
GOMOD
cat > "$WORK/pkg/calc.go" <<'GO'
package pkg

func Double(n int) int {
	return n * 2
}
GO
cat > "$WORK/pkg/calc_test.go" <<'GO'
package pkg

import "testing"

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fatalf("Double(3) = %d, want 6", Double(3))
	}
}

func TestUnguarded(t *testing.T) {
	// deliberately asserts nothing about Double's arithmetic
	_ = Double(1)
}
GO
cp "$MUTATE" "$WORK/mutate.sh"

echo "── mutate.sh self-test"

# PIN 1 — a sed that matches NOTHING reports NOT-APPLIED (rc 2), never SURVIVED.
# This is the class-89 case: three "survivors" in one week were this.
out="$(cd "$WORK" && ./mutate.sh pkg/calc.go 's|return n \* 999999|return n * 3|' 'TestDouble' ./pkg/ 2>&1)"; rc=$?
check "a non-matching sed is NOT-APPLIED, not SURVIVED" 2 "$rc" "$out"
printf '%s' "$out" | grep -q 'NOT-APPLIED' || { echo "  FAIL  output does not say NOT-APPLIED"; fail=$((fail+1)); }

# PIN 2 — a mutant that does not COMPILE reports BUILD-FAILED (rc 3).
out="$(cd "$WORK" && ./mutate.sh pkg/calc.go 's|return n \* 2|return n * "two"|' 'TestDouble' ./pkg/ 2>&1)"; rc=$?
check "a non-compiling mutant is BUILD-FAILED, not SURVIVED" 3 "$rc" "$out"

# PIN 3 — a real mutation that a pin catches is KILLED (rc 0).
out="$(cd "$WORK" && ./mutate.sh pkg/calc.go 's|return n \* 2|return n * 3|' 'TestDouble' ./pkg/ 2>&1)"; rc=$?
check "a caught mutant is KILLED" 0 "$rc" "$out"

# PIN 4 — a real mutation that NO pin catches is SURVIVED (rc 1). This is the
# only case allowed to report a weak pin, and it must still be reachable.
out="$(cd "$WORK" && ./mutate.sh pkg/calc.go 's|return n \* 2|return n * 3|' 'TestUnguarded' ./pkg/ 2>&1)"; rc=$?
check "an uncaught mutant is SURVIVED" 1 "$rc" "$out"

# PIN 5 — a test PATTERN that selects nothing is NOT-APPLIED, not SURVIVED: a
# suite asked to run zero tests cannot have failed to catch anything.
out="$(cd "$WORK" && ./mutate.sh pkg/calc.go 's|return n \* 2|return n * 3|' 'TestNoSuchName' ./pkg/ 2>&1)"; rc=$?
check "a pattern matching no test is NOT-APPLIED, not SURVIVED" 2 "$rc" "$out"

# PIN 6 — the file is RESTORED on every path, including the failing ones above.
if grep -q 'return n \* 2' "$WORK/pkg/calc.go"; then
  printf '  PASS  the file is restored after every run\n'; pass=$((pass+1))
else
  printf '  FAIL  the harness left a mutant in the tree:\n'; sed 's/^/        /' "$WORK/pkg/calc.go"; fail=$((fail+1))
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
