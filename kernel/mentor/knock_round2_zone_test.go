package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D2-G10 — Knock Knock counts tests per price ZONE, pre-market
// included [Oct D2.1 @ 21:18-21:36, 22:24-22:39, 23:24-23:43] (OctD2.1-R22..R38):
// the line 28,816.25 is drawn "from the far left" and the pre-market tests of
// Fri 30 Oct count (frame s_01351). Real stored bars: testdata/
// r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv.

func round2ZoneAt(e *Evaluator, lo, hi float64) (ZoneVisit, bool) {
	best := ZoneVisit{}
	for _, z := range e.State.ZoneVisits {
		if z.Price >= lo && z.Price <= hi && z.N > best.N {
			best = z
		}
	}
	return best, best.N > 0
}

func TestRound2D2G10ZoneCountsPreMarketTests(t *testing.T) {
	withFlatNear(t)
	bars := m413Load(t, "testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv")
	e := New(m473Cfg())
	var preN, postN int
	for i := range bars {
		now := bars[i].CloseTime + 1
		if now > time.Date(2026, 10, 30, 8, 50, 0, 0, ctime()).UnixMilli() {
			break
		}
		e.Tick(append([]market.Kline(nil), bars[:i+1]...), now)
		z, ok := round2ZoneAt(e, 28790, 28845)
		if now == time.Date(2026, 10, 30, 8, 30, 0, 0, ctime()).UnixMilli() && ok {
			preN = z.N
		}
		postN = z.N
	}
	if preN < 2 {
		t.Fatalf("pre-market tests of the 28,8xx zone at 08:30 = %d, want >= 2 (07:37-08:00 tests count) — zones: %+v", preN, e.State.ZoneVisits)
	}
	if preN == 0 || postN < preN {
		t.Fatalf("zone count fell across the 08:30 gate latch: %d -> %d (the pre-market tests were wiped)", preN, postN)
	}
}

func TestRound2D2G10KnobOffCountsNoZones(t *testing.T) {
	withFlatNear(t)
	cfg := m473Cfg()
	cfg.KnockKnockZone = false
	bars := m413Load(t, "testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv")
	e := New(cfg)
	for i := range bars {
		now := bars[i].CloseTime + 1
		if now > time.Date(2026, 10, 30, 8, 50, 0, 0, ctime()).UnixMilli() {
			break
		}
		e.Tick(append([]market.Kline(nil), bars[:i+1]...), now)
	}
	if len(e.State.ZoneVisits) != 0 {
		t.Fatalf("knob off must keep no zone table: %+v", e.State.ZoneVisits)
	}
}

// The zone count reaches the cap: over the same Thu 17:00 - Fri 12:00 tape the
// 4th test of a zone is refused level_visit_cap more often than the per-key
// count ever reaches (the per-key count was 12 on this tape, 28 with zones).
func TestRound2D2G10ZoneCountRefusesTheFourthTest(t *testing.T) {
	withFlatNear(t)
	bars := m413Load(t, "testdata/r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv")
	run := func(zone bool) int {
		cfg := m473Cfg()
		cfg.KnockKnockZone = zone
		e := New(cfg)
		for i := range bars {
			e.Tick(append([]market.Kline(nil), bars[:i+1]...), bars[i].CloseTime+1)
		}
		return e.State.Refusals["level_visit_cap"]
	}
	off, on := run(false), run(true)
	if on <= off {
		t.Fatalf("level_visit_cap with zones = %d, per-key = %d: the zone count must refuse the 4th test of a re-keyed level too", on, off)
	}
}
