package agent

import (
	"testing"
	"time"

	"vl/logger"
)

// TestSchedulerStepMissedMinuteCatchesUp pins P2-16 at the production call
// site (Scheduler.Start consumes schedulerStep): the daily report must run on
// the FIRST tick after 21:00 even when the process missed the exact minute,
// and the hourly cleanup must fire once per hour-change, never skipped.
func TestSchedulerStepMissedMinuteCatchesUp(t *testing.T) {
	// The schedule is CT (R22 P3-3, schedulerStep reads logger.CTLocation()),
	// so the test builds its instants in CT too — time.Local made it pass only
	// on a box whose machine zone is Chicago and fail in UTC CI.
	ct := logger.CTLocation()
	run := func(now time.Time, st *schedulerStamps) (daily, cleanup bool) {
		d, c, _ := schedulerStep(now, st)
		return d, c
	}

	// (a) normal 21:00 tick runs; a same-day 21:05 tick does not re-run.
	st := &schedulerStamps{}
	day := time.Date(2026, 9, 26, 21, 0, 0, 0, ct)
	if d, _ := run(day, st); !d {
		t.Fatalf("21:00 tick must run the daily report")
	}
	if d, _ := run(day.Add(5*time.Minute), st); d {
		t.Fatalf("same-day 21:05 tick must not re-run the daily report")
	}

	// (b) the missed-minute case: the process's first tick lands at 22:15.
	st2 := &schedulerStamps{}
	late := time.Date(2026, 9, 26, 22, 15, 0, 0, ct)
	if d, _ := run(late, st2); !d {
		t.Fatalf("22:15 tick must catch up the missed 21:00 report")
	}
	if d, _ := run(late.Add(1*time.Minute), st2); d {
		t.Fatalf("catch-up must run once per day")
	}

	// (c) hourly cleanup: once per hour-change, including a minute that is
	// not :00 (the old now.Minute()==0 check would skip 10:01).
	st3 := &schedulerStamps{lastCleanupHour: -1}
	base := time.Date(2026, 9, 26, 9, 15, 0, 0, ct)
	if _, c := run(base, st3); !c {
		t.Fatalf("first tick must run the cleanup")
	}
	if _, c := run(base.Add(1*time.Minute), st3); c {
		t.Fatalf("same-hour tick must not re-run the cleanup")
	}
	afterHour := time.Date(2026, 9, 26, 10, 1, 0, 0, ct)
	if _, c := run(afterHour, st3); !c {
		t.Fatalf("10:01 tick must run the hourly cleanup (missed the :00 minute)")
	}

	// (d) the 4h risk check keeps its elapsed cadence.
	st4 := &schedulerStamps{lastCheckAt: base, lastCleanupHour: -1}
	if _, _, chk := schedulerStep(base, st4); chk {
		t.Fatalf("risk check must not fire at the first tick")
	}
	if _, _, chk := schedulerStep(base.Add(5*time.Hour), st4); !chk {
		t.Fatalf("risk check must fire after 4h elapsed")
	}
}
