package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// P3-1 (ENGINE-AUDIT-R2 DS-104) — call-site pin: the boot depth seam
// (SeedDepths["today session"]) and the fail-closed gate (seedDepthOf.today)
// must AGREE on what "today session" means. Before the fix SeedDepths used the
// CT calendar day (dayStartCT) while the gate used the 17:00 CT Globex session
// key (sessionKeyCT) — the two diverged for a bar from 17:00+ on the prior
// calendar day once now rolled past midnight.
func TestSeedDepthsTodaySessionAgreesWithGate(t *testing.T) {
	// 17:05 CT on 2026-10-06: belongs to the Globex session keyed "2026-10-07"
	// (at/after 17:00 the session is keyed by TOMORROW's date).
	barB := time.Date(2026, 10, 6, 17, 5, 0, 0, ctime()).UnixMilli()
	bars1m := []market.Kline{{
		OpenTime:  barB,
		CloseTime: barB + 60_000 - 1,
		High:      101,
		Low:       99,
		Close:     100,
	}}
	bars1h := barsTF(bars1m, 60)

	// Sweep across the 17:00 CT boundary and the midnight rollover. The 00:30
	// CT instant is the one that fails under the calendar-day definition: the
	// only bar is from 17:05 the PRIOR calendar day, so the calendar-day scan
	// finds nothing while the gate (session key) finds the bar.
	instants := []time.Time{
		time.Date(2026, 10, 6, 18, 0, 0, 0, ctime()), // the dispatch's 18:00 CT pin
		time.Date(2026, 10, 6, 17, 30, 0, 0, ctime()),
		time.Date(2026, 10, 7, 0, 30, 0, 0, ctime()), // midnight rollover — catches the old bug
		time.Date(2026, 10, 7, 16, 30, 0, 0, ctime()),
	}
	for _, inst := range instants {
		now := inst.UnixMilli()
		gate := seedDepthOf(bars1m, now).today
		seam := SeedDepths(bars1m, bars1h, now)["today session"]
		if seam != gate {
			t.Fatalf("now=%s CT: SeedDepths[\"today session\"]=%d but gate seedDepthOf.today=%d — the seam and the gate must use the same 17:00 CT session definition",
				inst.In(ctime()).Format("2006-01-02 15:04"), seam, gate)
		}
		if gate != 1 {
			t.Fatalf("now=%s CT: gate seedDepthOf.today=%d, want 1 (the 17:05 CT bar is in the current session)", inst.In(ctime()).Format("2006-01-02 15:04"), gate)
		}
	}
}
