package trader

import (
	"sync"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"

	ntwire "vl/provider/ninjatrader"
	nttrader "vl/trader/ninjatrader"
)

// ── MENTOR STALE-DATA BLOCK (release #11) — call-site pins ──────────────────
//
// The mentor path (the armed-entry authoring in mentorPlaceIntent and the armed
// pass placement in mentorOnlyPlacementPass) refuses NEW work while the live 1m
// feed is stale — B4's formula (~75 s), CME-open/halt aware. These pins drive
// the production call sites; the named RED (drop the gate call in
// mentorPlaceIntent) fails TestMentorStaleDataBlockRefusesAuthoring.

// mentorStaleBlockClock returns a fixed CME-open RTH instant: Wednesday
// 2026-09-23 09:00 CT — inside the default 08:30–09:30 trading window, no news.
func mentorStaleBlockClock(h, m int) time.Time {
	ct := kernel.CTLocation()
	return time.Date(2026, 9, 23, h, m, 0, 0, ct)
}

// mentorStaleBlockFeed overrides the live bar provider with a single 1m bar
// `age` old at `now` (age=0 → the forming bar, fresh).
func mentorStaleBlockFeed(t *testing.T, now time.Time, age time.Duration) {
	t.Helper()
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{
			Open: 21000, High: 21010, Low: 20990, Close: 21005,
			OpenTime: now.UnixMilli() - age.Milliseconds(), CloseTime: now.UnixMilli() - age.Milliseconds() + 59_999, Final: true,
		}}
	}
	t.Cleanup(func() { market.FuturesBarsProvider = nil })
}

func mentorStaleBlockIntent() mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-stale", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12, ExpiryMs: 0}
}

// TestMentorStaleDataBlockRefusesAuthoring — the named RED: a 1m bar 3 min old
// while CME is open must refuse to AUTHOR a new arm (the placement recorder
// never fires) and name the refusal ("stale_data") in the funnel counter.
func TestMentorStaleDataBlockRefusesAuthoring(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	now := mentorStaleBlockClock(9, 0)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t)
	mentorStaleBlockFeed(t, now, 3*time.Minute) // stale: ~3 min old (AFTER the seams, which own their own feed)

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)

	if len(placed) != 0 {
		t.Fatalf("a stale feed must NOT author a new arm, but %d placement(s) reached the recorder", len(placed))
	}
	if got := MentorCountSnapshot()["stale_data"]; got != 1 {
		t.Fatalf("the refusal must be counted once as stale_data, got %d", got)
	}
}

// TestMentorStaleDataBlockAllowsFresh — a 1m bar 30 s old is normal: the gate
// does NOT fire and the placement proceeds (the recorder fires once).
func TestMentorStaleDataBlockAllowsFresh(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	now := mentorStaleBlockClock(9, 0)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t)
	mentorStaleBlockFeed(t, now, 30*time.Second) // fresh: 30 s old (AFTER the seams, which own their own feed)

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)

	if len(placed) != 1 {
		t.Fatalf("a fresh feed must reach the placement path, placed=%v", placed)
	}
	if got := MentorCountSnapshot()["stale_data"]; got != 0 {
		t.Fatalf("a fresh feed must NOT count a stale_data refusal, got %d", got)
	}
}

// TestMentorStaleDataBlockHaltNoFalseRefusal — the daily 16:00–17:00 break must
// never false-refuse: CME is closed, so the gate (B4's own halt awareness) does
// not fire and no stale_data refusal is counted.
func TestMentorStaleDataBlockHaltNoFalseRefusal(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	now := mentorStaleBlockClock(16, 30) // inside the 16:00–17:00 daily break
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorStaleBlockFeed(t, now, 3*time.Minute) // bars are old, but CME is closed

	if at.mentorStaleDataBlocked(now) {
		t.Fatalf("the daily halt must not trip the stale-data block (B4's halt rule)")
	}
	if got := MentorCountSnapshot()["stale_data"]; got != 0 {
		t.Fatalf("the halt must not count a stale_data refusal, got %d", got)
	}
}

// TestMentorStaleDataBlockDoesNotBlockExitDrive — the stale block NEVER gates
// exits / protection: with a stale feed and a filled position, the exit-drive
// stop move (mentorMoveStopBE → moveStopWire) still runs.
func TestMentorStaleDataBlockDoesNotBlockExitDrive(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	var mu sync.Mutex
	var moved []float64
	oldWire := moveStopWire
	moveStopWire = func(nt *nttrader.TCPTrader, side string, newStop float64) error {
		mu.Lock()
		moved = append(moved, newStop)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { moveStopWire = oldWire })

	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-stale",
		Side: "long", EntryPx: 29600, StopPx: 29595, TargetPx: 29660, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateFilled, SignalID: "swing-stale-sig", FillPrice: 29600, FillQuantity: 1}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("swing-stale", at.id, row.ID, "long", 29600)

	now := mentorStaleBlockClock(10, 0) // CME open, stale feed
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorStaleBlockFeed(t, now, 3*time.Minute) // stale: ~3 min old

	ResetMentorCountersForTest()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "swing-stale", Reason: "swing +1R"}, mentorTierInputs{}, 1000, 1100)

	mu.Lock()
	defer mu.Unlock()
	if len(moved) != 1 || moved[0] != 29600 {
		t.Fatalf("the exit-drive stop move must still run while the feed is stale, got %v", moved)
	}
}
