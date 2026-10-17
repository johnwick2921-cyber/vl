package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
)

// W-FLIP-DIRECTION (2026-09-17) — plans ALREADY in the store were written
// before the validator learned direction. They never pass the write site
// again, so the only place an inverted flip can be named is where a stored
// flip is EVALUATED: describeActivePlanDeath (the function that logs
// flip_eval_skipped). These tests run at that production call site through
// the flip-hold harness (store row + frozen clock + installed tape). WARN,
// never a reject, and the evaluation itself is unchanged.

func invertedShortPlanDoc(t *testing.T) string {
	t.Helper()
	// The LONDON v3 shape: short bias, flip BELOW → long (can never fire on a rally).
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short", FlipCondition: "flips long on 2x5m below 100"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m", FlipTo: "long"}}
	blob, _ := json.Marshal(doc)
	return string(blob)
}

func TestDescribeActivePlanDeath_InvertedStoredFlipWarns(t *testing.T) {
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	row := appendVersion(t, st, at, td, "NY_scheduled_read", invertedShortPlanDoc(t), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 10)) // rally to 104: the move the flip was MEANT to catch
	buf := captureTraderLog(t)

	_, dead := at.describeActivePlanDeath(row)

	if dead {
		t.Fatalf("an inverted flip must not fire on the rally it cannot see (behaviour unchanged):\n%s", buf.String())
	}
	log := buf.String()
	if !strings.Contains(log, "flip_direction_inverted plan="+row.PlanID+" v1 (active)") || !strings.Contains(log, "contradicts bias short") {
		t.Fatalf("a stored inverted flip must be named in the journal at the evaluation site; got:\n%s", log)
	}
	// Once per plan version, not per tick: a second evaluation adds no line.
	at.describeActivePlanDeath(row)
	if n := strings.Count(buf.String(), "flip_direction_inverted"); n != 1 {
		t.Fatalf("the line must print once per plan version, got %d:\n%s", n, buf.String())
	}
}

// A DORMANT plan is evaluated by describeDormantCleared, not
// describeActivePlanDeath; the inverted shape must be named there too.
func TestDescribeDormantCleared_InvertedStoredFlipWarns(t *testing.T) {
	at, st, now := flipHoldTrader(t)
	td := "2026-08-19" // distinct plan id from the active-plan test (once-per-version memory)
	row := appendVersion(t, st, at, td, "dormant:flip:flip-condition", invertedShortPlanDoc(t), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 10))
	buf := captureTraderLog(t)

	at.describeDormantCleared(row)

	log := buf.String()
	if !strings.Contains(log, "flip_direction_inverted plan="+row.PlanID+" v1 (dormant)") || !strings.Contains(log, "contradicts bias short") {
		t.Fatalf("a dormant inverted flip must be named at its evaluation site; got:\n%s", log)
	}
}

func TestDescribeActivePlanDeath_CorrectStoredFlipSilent(t *testing.T) {
	at, st, now := flipHoldTrader(t)
	td := "2026-08-18"
	row := appendVersion(t, st, at, td, "NY_scheduled_read", shortPlanDoc(t), now.Add(-45*time.Minute))
	barsAt(flipHoldTape(now, 40, 10))
	buf := captureTraderLog(t)

	detail, dead := at.describeActivePlanDeath(row)

	if !dead || !strings.Contains(detail.Killer, "flip-condition") {
		t.Fatalf("the correctly-pointed flip must still fire on the rally: dead=%v killer=%q log=%s", dead, detail.Killer, buf.String())
	}
	if strings.Contains(buf.String(), "flip_direction_inverted") {
		t.Fatalf("a correctly-pointed flip must not be named as inverted:\n%s", buf.String())
	}
}
