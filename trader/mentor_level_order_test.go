package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// LEVEL-ORDER-DEATH — call-site tests. A sized PHL Place must reach the ledger
// (the "never silent" rule), and the stale-feed refusal must be LOUD.

// eveningInstant is a FIXED weekday evening inside the CME Globex session —
// Wednesday 2026-09-23 19:49:00 America/Chicago (after the 16:00–17:00 daily
// break, CME open).
func eveningInstant() time.Time {
	chicago, _ := time.LoadLocation("America/Chicago")
	return time.Date(2026, 9, 23, 19, 49, 0, 0, chicago)
}

func pinMentorClock(t *testing.T, tm time.Time) {
	t.Helper()
	prev := mentorNowSource
	mentorNowSource = func() time.Time { return tm }
	t.Cleanup(func() { mentorNowSource = prev })
}

func pinBars(t *testing.T, openMs int64) {
	t.Helper()
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{Open: 1, High: 2, Low: 0.5, Close: 1.5, OpenTime: openMs}}
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
}

func levelIntent() mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "lvl-1", Setup: "PHL", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10}
}

// TestMentorLevelOrderReachesLedger — a sized PHL Place at 10:30 CT (live bars)
// authors one armed row (condition PHL) and registers the arm. This is the path
// that produced ZERO level rows ever: if it drops anywhere, this is RED.
func TestMentorLevelOrderReachesLedger(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	mentorPinClockToRTH(t) // 10:30 CT Wednesday; mentorWireSeams pins fresh bars

	at.mentorDispatchIntent(levelIntent(), mentorTierInputs{}, rthInstant().UnixMilli(), 1100)

	var rows []store.ArmedOrderDB
	if err := ledger.DB().Where("scenario LIKE ?", "lvl-1-%").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("one sized level order must yield exactly one armed row, got %d", len(rows))
	}
	if rows[0].Condition != "PHL" || rows[0].State != store.StateArmed {
		t.Fatalf("row must be an armed PHL, got condition=%q state=%q", rows[0].Condition, rows[0].State)
	}
	if _, ok := at.mentorLiveArmFor("lvl-1"); !ok {
		t.Fatal("lvl-1 must be registered after the row is authored")
	}
}

// TestMentorLevelOrderEveningRestsUntilNextRTH — at 19:49 CT (Globex open, live
// bars) a level order rests until 15:00 the NEXT trading day (D2-44 item 11).
// The expiry is the lvl- prefix default, stamped on the ledger row.
func TestMentorLevelOrderEveningRestsUntilNextRTH(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli()) // fresh bar at the pinned clock

	at.mentorDispatchIntent(levelIntent(), mentorTierInputs{}, evening.UnixMilli(), 1100)

	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario LIKE ?", "lvl-1-%").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	want := mentor.LevelArmExpiry(evening.UnixMilli())
	if row.ExpiryMs != want {
		t.Fatalf("level order expiry = %d, want %d (15:00 next trading day)", row.ExpiryMs, want)
	}
}

// TestMentorLevelOrderStaleFeedRefusedLoud — the dropping condition: a sized
// PHL Place while the 1m feed is stale is REFUSED with a per-intent counter
// (stale_data_placement_refused) and a log line — never silent. The mutant
// (restore the silent return) leaves the counter at 0 and this turns RED.
func TestMentorLevelOrderStaleFeedRefusedLoud(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli()-10*60_000) // 10-minute-old bar → B4 stale

	resetMentorCounters(at)
	at.mentorDispatchIntent(levelIntent(), mentorTierInputs{}, evening.UnixMilli(), 1100)

	if c := at.mentorCountSnapshot()["stale_data_placement_refused"]; c != 1 {
		t.Fatalf("stale_data_placement_refused = %d, want 1 (the refusal must be counted per intent)", c)
	}
	var n int64
	if err := ledger.DB().Model(&store.ArmedOrderDB{}).Where("scenario LIKE ?", "lvl-1-%").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a stale-feed level order must NOT reach the ledger, got %d rows", n)
	}
	if _, ok := at.mentorLiveArmFor("lvl-1"); ok {
		t.Fatal("a stale-feed level order must NOT register")
	}
}
