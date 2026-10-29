package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
)

// m445 round 4 (D1/D2): the moment a hole leaves a window by itself is a READ
// value, and it is exact — blocked one minute before it, clear at it. The owner
// message prints it, so it must be the real instant.
func TestClearsAtIsTheExactMomentTheHoleLeavesTheWindow(t *testing.T) {
	armIntegrity(t)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, kernel.CTLocation()) }
	t.Cleanup(SetBarHolesWithHolesForTest(at(1, 17, 0).UnixMilli(), at(28, 10, 0).UnixMilli(), []TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(14, 3, 0).UnixMilli(), ToMs: at(14, 3, 30).UnixMilli()},
	}))
	for _, win := range []int{IntegrityIntradayOpenMin, IntegrityDeepOpenMin, IntegritySwingOpenMin} {
		now := at(14, 8, 0)
		v := IntegrityVerdictFor("MNQ", win, now)
		if !v.Blocked || len(v.Holes) != 1 {
			t.Fatalf("window %d: the hole must block right after it: %+v", win, v)
		}
		clears := v.Holes[0].ClearsAtMs
		if clears <= now.UnixMilli() {
			t.Fatalf("window %d: ClearsAtMs must lie ahead: %d", win, clears)
		}
		c := time.UnixMilli(clears)
		if b := IntegrityVerdictFor("MNQ", win, c.Add(-time.Minute)); !b.Blocked {
			t.Fatalf("window %d: still inside the window one minute before ClearsAt (%s): %+v", win, kernel.TableTimeCT(c), b)
		}
		if a := IntegrityVerdictFor("MNQ", win, c); a.Blocked {
			t.Fatalf("window %d: ClearsAt (%s) must be the first clear minute: %+v", win, kernel.TableTimeCT(c), a)
		}
		// the MENTOR verdict on an OVERNIGHT hole (no RTH minute) clears at the
		// same instant: the key-level extension (round 5) applies to RTH holes only
		if mv := IntegrityVerdictForMentor("MNQ", win, now); !mv.Blocked || len(mv.Holes) != 1 || mv.Holes[0].ClearsAtMs != clears {
			t.Fatalf("window %d: an overnight hole clears at the window exit for the mentor verdict too: %+v", win, mv)
		}
	}
}

// A tracker hole outside NT8's replay (retired) must carry the remedy that
// works, in the verdict AND on the health surface: no reload.
func TestRetiredHoleCarriesTheRemedyThatWorksOnTheHealthSurface(t *testing.T) {
	armIntegrity(t)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, kernel.CTLocation()) }
	t.Cleanup(SetBarHolesWithHolesForTest(at(1, 17, 0).UnixMilli(), at(28, 10, 0).UnixMilli(), []TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(26, 17, 0).UnixMilli(), ToMs: at(26, 21, 57).UnixMilli()},
	}))
	now := at(28, 10, 0)
	views, armed := TradingPausedSnapshot(now)
	if !armed || len(views) != 1 || !views[0].Paused {
		t.Fatalf("the retired hole pauses: %+v", views)
	}
	a := views[0].Action
	if strings.Contains(a, "Reload All Historical Data") || !strings.Contains(a, "no NT8 reload reaches") || !strings.Contains(a, "/api/admin/bars/import") {
		t.Fatalf("the health action for a retired hole must not send the owner to a reload: %q", a)
	}
	if len(views[0].Reasons) != 1 || len(views[0].Reasons[0].ClearsAt) != 1 || !strings.Contains(views[0].Reasons[0].ClearsAt[0], "clears by itself") {
		t.Fatalf("the health reason must say WHEN it clears by itself: %+v", views[0].Reasons)
	}
}

// The key-level leg and the seeded-series leg agree on the reach: a tracker hole
// older than IntegritySeedReachDays no longer pauses the mentor on the key-level
// leg either (round 3 kept it to 100 days — a pause past the reach the seed leg
// and the guide state).
func TestKeyLevelLegStopsAtTheSameReachAsTheSeededSeriesLeg(t *testing.T) {
	armIntegrity(t)
	at := func(mo time.Month, d, h, m int) time.Time {
		return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation())
	}
	holeTo := at(7, 22, 10, 40)
	t.Cleanup(SetBarHolesWithHolesForTest(at(7, 1, 17, 0).UnixMilli(), at(10, 28, 10, 0).UnixMilli(), []TestHole{
		{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: at(7, 22, 10, 0).UnixMilli(), ToMs: holeTo.UnixMilli()},
	}))
	inReach := holeTo.Add(IntegritySeedReachDays*24*time.Hour - time.Hour)
	if v := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, inReach); !v.Blocked {
		t.Fatalf("inside the %d-day reach an RTH hole pauses the mentor: %+v", IntegritySeedReachDays, v)
	}
	past := holeTo.Add(IntegritySeedReachDays*24*time.Hour + time.Hour)
	if v := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, past); v.Blocked && v.Class == IntegrityClassHole {
		t.Fatalf("past the reach the key-level leg lets go, as the seeded-series leg does: %+v", v)
	}
}
