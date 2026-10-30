package trader

import (
	"testing"
	"time"
)

// STALE EVALUATION ON A DAY WITH NO PASS (re-review 6, P3): the card merged the
// evaluator's newest result (box, refusals, signal word) with a fresh Day,
// TradesToday and flat countdown for the CURRENT clock. On a Saturday after a
// Thursday trade it showed Thursday's box, "Refusal: pep_position_open" and
// "waiting for a break" under Saturday's date.
//
// MUTANT: remove the `pepDayNow(t.at) != day` reset in PepTruthSnapshot
// -> the Saturday card carries Thursday's box and refusals (RED).
func TestPepTruthDoesNotCarryYesterdaysEvaluationOver(t *testing.T) {
	w := newPepWire(t, pepCfgOf("A", 6))
	pepFeed(t, pepBarsA(t, pepDay, "08:39", pepBreakUp))
	pepLive(t, 25041)
	now := pepNow(t)
	w.at.pepEventPassAt(now)
	waitSignal(t, w)
	w.at.pepEventPassAt(now.Add(time.Minute))

	// same CT day: the result is today's and is shown (control)
	same := w.at.PepTruthSnapshot(now.Add(2 * time.Minute))
	if !same.Box.Drawn || len(same.Refusals) == 0 {
		t.Fatalf("control: the same-day card lost its evaluation: %+v", same)
	}

	// a later day with no pass at all
	later := w.at.PepTruthSnapshot(ctTimeAt(t, "2026-10-31", "10:00"))
	if later.Day != "2026-10-31" {
		t.Fatalf("day %q", later.Day)
	}
	if later.Box.Drawn || later.Box.High != 0 || later.Box.Low != 0 {
		t.Errorf("a day with no pass shows an older day's box: %+v", later.Box)
	}
	if len(later.Refusals) != 0 {
		t.Errorf("a day with no pass shows an older day's refusals: %+v", later.Refusals)
	}
	if later.Signal != "no_data" {
		t.Errorf("signal %q, want no_data", later.Signal)
	}
	if later.TradesToday != 0 {
		t.Errorf("trades today %d on a new day", later.TradesToday)
	}
	// when it last ran stays an honest fact
	if later.LastEvaluatedMs == 0 {
		t.Errorf("last_evaluated_ms dropped")
	}
}
