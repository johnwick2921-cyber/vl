package agent

// R22 TIME-WEEKLY P3-3 — the 21:00 daily report is a CT time. schedulerStep
// read now.Hour() in whatever zone the caller's clock carried (time.Now() is
// the MACHINE zone), so it was 21:00 CT only because the box happens to run
// in Chicago. Driven at the production decision core with UTC instants.

import (
	"testing"
	"time"
)

func TestR22SchedulerDailyReportIs21CT(t *testing.T) {
	st := &schedulerStamps{lastCleanupHour: -1}
	// 21:30 UTC = 16:30 CDT — not yet 21:00 CT.
	if d, _, _ := schedulerStep(time.Date(2026, 10, 14, 21, 30, 0, 0, time.UTC), st); d {
		t.Fatal("the daily report fired at 16:30 CT (21:30 UTC) — the 21:00 gate read the clock's own zone, not CT")
	}
	// 02:05 UTC 10-15 = 21:05 CDT 10-14 — due, and keyed to the CT date 10-14.
	if d, _, _ := schedulerStep(time.Date(2026, 10, 15, 2, 5, 0, 0, time.UTC), st); !d {
		t.Fatal("the daily report did not fire at 21:05 CT")
	}
	if st.lastReportDay != "2026-10-14" {
		t.Fatalf("report day stamped %q, want the CT date 2026-10-14", st.lastReportDay)
	}
}
