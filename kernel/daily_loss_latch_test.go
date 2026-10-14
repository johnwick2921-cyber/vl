package kernel

import (
	"testing"
	"time"
)

// r22 RISK-GATES — THE TRIP IS A LATCH STAMPED WITH ITS CME SESSION-DAY. It
// holds through 16:59 CT and reads lifted from 17:00 CT, whoever asks — the
// roll (MaybeResetDaily) runs only inside an AI decision cycle, which a mentor
// trader never runs, so before r22 a mentor trader's trip lifted only on a
// restart. MUTANT: return trip.reason without the day comparison → RED.
func TestDailyForceFlatLatchLiftsAt1700CT(t *testing.T) {
	const id = "latch-1700"
	ct := CTLocation()
	t.Cleanup(func() { ClearDailyForceFlat(id) })
	SetDailyForceFlatAt(id, "daily loss limit hit", time.Date(2026, 10, 6, 10, 0, 0, 0, ct))
	if DailyForceFlatReasonAt(id, time.Date(2026, 10, 6, 16, 59, 59, 0, ct)) == "" {
		t.Fatal("the latch must hold through 16:59:59 CT on its session-day")
	}
	if r := DailyForceFlatReasonAt(id, time.Date(2026, 10, 6, 17, 0, 0, 0, ct)); r != "" {
		t.Fatalf("the latch must lift at 17:00 CT (next CME session-day), still %q", r)
	}
}

// The roll clears earlier-day trips only: a trip set after 17:00 CT (today's)
// survives a roll that runs later. MUTANT: clear every trip on the roll → RED.
func TestRollKeepsATripSetAfterTheBoundary(t *testing.T) {
	const old, fresh = "roll-old", "roll-fresh"
	ct := CTLocation()
	t.Cleanup(func() { ClearDailyForceFlat(old); ClearDailyForceFlat(fresh) })
	ResetDailyPnLAt(time.Date(2026, 10, 6, 9, 0, 0, 0, ct))
	SetDailyForceFlatAt(old, "yesterday", time.Date(2026, 10, 6, 11, 0, 0, 0, ct))
	SetDailyForceFlatAt(fresh, "after the roll", time.Date(2026, 10, 6, 17, 5, 0, 0, ct))
	if !MaybeResetDaily(time.Date(2026, 10, 6, 17, 10, 0, 0, ct)) {
		t.Fatal("fixture: no roll seen")
	}
	at := time.Date(2026, 10, 6, 17, 11, 0, 0, ct)
	if DailyForceFlatReasonAt(fresh, at) == "" {
		t.Fatal("a trip set after the 17:00 CT boundary must survive the roll that runs later")
	}
	if DailyForceFlatReasonAt(old, at) != "" {
		t.Fatal("the earlier session-day's trip must be lifted")
	}
}
