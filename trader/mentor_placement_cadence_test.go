package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// N7 part 3 — while an unexpired mentor arm sits UNPLACED, the event loop's
// cadence tick re-runs the mentor-only placement every ~5s (the 2-min scan
// stays the fallback), so a 1-candle arm authored between scans is placed
// within ~10s. The pin drives the REAL event-loop goroutine (canon 53): the
// cadence is shrunk to keep the test fast, and the wait is bounded.
func TestMentorPlacementCadencePlacesAnArmAuthoredBetweenScans(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	oldCadence := mentorPlaceCadence
	mentorPlaceCadence = 20 * time.Millisecond
	t.Cleanup(func() { mentorPlaceCadence = oldCadence })

	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	resetMentorCounters()

	// Pin the mentor clock to an RTH instant (class 110): the admission chain
	// asks the calendar on the clock it is GIVEN.
	now := rthInstant()
	oldNow := mentorNowSource
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = oldNow })

	// A fresh flat book + a tape below the long trigger, so the placement's
	// stop-side guard rests and the one-contract guard reads an empty account.
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{Open: 29599, High: 29601, Low: 29598, Close: 29599,
			OpenTime: now.Add(-time.Minute).UnixMilli(), CloseTime: now.UnixMilli(), Final: true}}
	}

	// Author a 1-candle mentor arm but run NO placement pass: the arm sits
	// "armed" — the exact "authored between scans" state. The injector must
	// not place it directly.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-cad", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29610, StopPts: 5, TargetPts: 10, ExpiryMs: now.UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, now.Add(-time.Minute).UnixMilli(), now.UnixMilli())
	if sawMentorFrame(t, frames, ntwire.FrameSignal, 300*time.Millisecond) {
		t.Fatal("the injector must not place directly — the cadence pass places")
	}

	// Start the event loop: its cadence tick must place the unplaced arm.
	at.startArmedEventLoop()
	t.Cleanup(at.stopArmedEventLoop)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sawMentorFrame(t, frames, ntwire.FrameSignal, 50*time.Millisecond) {
			return // placed by the cadence tick
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the event-loop cadence must place an unexpired mentor arm authored between scans")
}

// The cadence predicate is true ONLY for an UNPLACED, UNEXPIRED mentor arm: a
// working row needs no retry, an expired row is cancelled (not placed), and a
// non-mentor or absent row must never wake the 5s ticker into a full pass.
func TestMentorPlacementDue(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, "")
	mentorWireSeams(t, at, ledger)
	now := rthInstant()

	if at.mentorPlacementDue(now) {
		t.Fatal("no mentor arm → not due")
	}
	arm := &store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "cad-due", Side: "long", State: store.StateArmed, EntryPx: 29600, StopPx: 29595, TargetPx: 29610,
		Kind: "stop_entry", Origin: store.ArmOriginMentor, ExpiryMs: now.UnixMilli() + 60_000}
	if err := ledger.UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if !at.mentorPlacementDue(now) {
		t.Fatal("an unplaced unexpired mentor arm must be due")
	}
	// Expired → cancelled by the pass, not placed.
	if err := ledger.SetArmExpiry(arm.ID, now.UnixMilli()-1); err != nil {
		t.Fatal(err)
	}
	if at.mentorPlacementDue(now) {
		t.Fatal("an expired mentor arm must NOT be due")
	}
	// Working (already placed) unexpired → no retry needed.
	if err := ledger.SetArmExpiry(arm.ID, now.UnixMilli()+60_000); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetState(arm.ID, store.StateWorking, ""); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetSignal(arm.ID, "sig-x"); err != nil {
		t.Fatal(err)
	}
	if at.mentorPlacementDue(now) {
		t.Fatal("a working mentor arm must NOT be due (it already reached the pass)")
	}
	// A non-mentor armed row must never wake the cadence.
	planner := &store.ArmedOrderDB{TraderID: at.id, PlanID: "planner", Version: 1, Session: "NY",
		Scenario: "S1", Side: "long", State: store.StateArmed, EntryPx: 29600, StopPx: 29595, TargetPx: 29610}
	if err := ledger.UpsertArm(planner); err != nil {
		t.Fatal(err)
	}
	if at.mentorPlacementDue(now) {
		t.Fatal("a non-mentor armed row must NOT be due")
	}
}
