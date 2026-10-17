#!/usr/bin/env bash
# planner_ab_report_fixture_test.sh — fixture pin for deploy/planner-ab-report.sh.
# Builds a canned log + a tiny sqlite fixture in a temp dir, runs the report,
# and asserts the exact expected numbers. Read-only with respect to anything
# outside the temp dir.
set -u

DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPORT=$DIR/planner-ab-report.sh
T=$(mktemp -d /tmp/planner-ab-fixture.XXXXXX)
trap 'rm -rf "$T"' EXIT

fail=0
assert_md() { # <file> <needle>
  if ! grep -qF "$2" "$1"; then
    echo "FAIL md lacks: $2"
    fail=$((fail+1))
  else
    echo "PASS md has: $2"
  fi
}

# Canned log, CT timestamps. Boot = 2026-09-26.
# baseline window: 2 max calls (100s, 300s), 3 reads (attempts=1 active,
# attempts=2 active, attempts=3 no_trade), 1 💀 drop.
# test window: 1 high call (150s), 1 read attempts=1 active, no drops.
cat > "$T/nofx_fixture.log" <<'LOG'
09-24 10:00:00 [INFO] trader [id=t1] 🧠 planner call (reasoning=max wire=max/100 cap=64000 stream idle=60s total=1200s) completed in 100.0s
09-24 10:02:00 [INFO] trader [id=t1] 🧭 planner read: session=NY attempts=1 reject_classes=none read→publish=1200ms lifecycle=active
09-24 12:00:00 [INFO] trader [id=t1] 🧠 planner call (reasoning=max wire=max/100 cap=64000 stream idle=60s total=1200s) completed in 300.0s
09-24 12:05:00 [INFO] trader [id=t1] 🧭 planner read: session=NY attempts=2 reject_classes=born_dead read→publish=1800ms lifecycle=active
09-24 14:00:00 [INFO] trader [id=t1] 🧠 planner call (reasoning=max wire=max/100 cap=64000 stream idle=60s total=1200s) completed in 200.0s
09-24 14:04:00 [INFO] trader [id=t1] 💀 born-dead S2 dropped at publish: authored condition breached
09-24 14:04:01 [INFO] trader [id=t1] 🧭 planner read: session=NY attempts=3 reject_classes=born_dead read→publish=900ms lifecycle=no_trade
09-26 11:00:00 [INFO] trader [id=t1] 🧠 planner call (reasoning=high wire=high/100 cap=64000 stream idle=60s total=1200s) completed in 150.0s
09-26 11:03:00 [INFO] trader [id=t1] 🧭 planner read: session=NY attempts=1 reject_classes=none read→publish=800ms lifecycle=active
LOG

# Tiny sqlite fixture: one active plan in the baseline window + 3 positions
# (pnl 10 win, -5 loss, NULL unresolved).
sqlite3 "$T/fixture.db" <<'SQL'
CREATE TABLE plans (plan_id TEXT, version INTEGER, lifecycle TEXT, created_at DATETIME);
INSERT INTO plans VALUES ('PLAN-X', 1, 'active', '2026-09-24 10:00:00');
CREATE TABLE trader_positions (plan_id TEXT, plan_version INTEGER, pnl_corrected REAL);
INSERT INTO trader_positions VALUES ('PLAN-X', 1, 10.0);
INSERT INTO trader_positions VALUES ('PLAN-X', 1, -5.0);
INSERT INTO trader_positions VALUES ('PLAN-X', 1, NULL);
SQL

bash "$REPORT" --boot 2026-09-26 --log "$T/nofx_fixture.log" --db "$T/fixture.db" --out-dir "$T" >/dev/null

md=$T/planner_ab_report.md
csv=$T/planner_ab_report.csv

# baseline max row — exact numbers
assert_md "$md" "| baseline | max | 3 | 200.0 | 200.0 | 300.0 | 33.3% | 66.7% | 1 | 1 | 3 | 50.0% | 5.00 | 1 |"
# test high row
assert_md "$md" "| test | high | 1 | 150.0 | 150.0 | 150.0 | 100.0% | 100.0% | 0 | 0 | 0 | 0.0% | 0.00 | 0 |"
# CSV mirrors it
grep -qF "baseline,max,3,200.000,200.000,300.000,33.3,66.7,1,1,3,50.0,5.00,1" "$csv" \
  && echo "PASS csv baseline row" || { echo "FAIL csv baseline row"; fail=$((fail+1)); }
grep -qF "test,high,1,150.000,150.000,150.000,100.0,100.0,0,0,0,0.0,0.00,0" "$csv" \
  && echo "PASS csv test row" || { echo "FAIL csv test row"; fail=$((fail+1)); }

echo "== fixture test: $fail failure(s)"
exit $fail
