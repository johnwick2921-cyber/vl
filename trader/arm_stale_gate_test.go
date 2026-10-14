package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// ── OPS NEW-2 (10-14) — planner arms honour the stale-data gate ────────────
//
// Before this fix a planner arm's send point (runArmedPlacementAt →
// armAdmitted → admitEntry(arm)) ran no feed-staleness check at all: with
// NT8's feed dead and the TCP link up, an in-zone row placed a limit off the
// frozen close. The 03:44 shape: the feed has been dead 200 s of REAL time and
// the wall clock reads 125 s behind, so the wall bar-age rule alone passes it;
// the monotonic receipt leg must refuse it.

func armStaleRow(t *testing.T, at *AutoTrader, st *store.Store) (store.ArmedOrderDB, armAdmission) {
	t.Helper()
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "stale-arm", Version: 1, Scenario: "S1", State: store.StateArmed,
		Side: "long", Kind: "limit", EntryPx: 29600, StopPx: 29590, TargetPx: 29630}
	if err := st.ArmedOrders().UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	return row, armAdmission{armAdmitKey(row.PlanID, "S1", 0): true}
}

func armStaleRowState(t *testing.T, st *store.Store, row store.ArmedOrderDB) store.ArmedOrderDB {
	t.Helper()
	if err := st.ArmedOrders().DB().First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPlannerArmRefusedOnDeadFeedUnderBackwardWallStep(t *testing.T) {
	real := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC) // Mon 10:00 CT, CME open
	t.Cleanup(kernel.ClockSampleForTest(kernel.NewSteppedClock(real).Sample))
	wall := real.Add(-125 * time.Second)
	at, st, signals, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, wall)
	mentorStaleBlockFeed(t, real, 200*time.Second) // newest 1m opened 200 s before the TRUE time
	staleStepReceipt(t, 200*time.Second)           // last live 1m frame 200 s ago, monotonic
	if stale, _ := kernel.FeedStaleAt(at.futuresSymbol(), real.Add(-200*time.Second).UnixMilli(), wall); !stale {
		t.Fatal("fixture: the receipt leg must call this feed stale")
	}
	if kernel.StaleEntryGateFeed(real.Add(-200*time.Second).UnixMilli(), wall) {
		t.Fatal("fixture: the wall rule alone must PASS this feed (that is the hole)")
	}
	row, admitted := armStaleRow(t, at, st)
	before := gateBlocks(at.id, "stale_data")

	for i := 0; i < 3; i++ { // three passes in one episode
		at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, wall.Add(-time.Hour).UnixMilli(), wall.Add(time.Duration(i)*time.Second), admitted)
	}
	if got := armStaleRowState(t, st, row); got.State != store.StateArmed || got.SignalID != "" {
		t.Fatalf("a 200 s-dead feed under a −125 s wall step must NOT place the planner arm (0 placements); row = %s signal %q", got.State, got.SignalID)
	}
	select {
	case s := <-signals:
		t.Fatalf("a planner arm reached the wire on a dead feed: %+v", s)
	case <-time.After(300 * time.Millisecond):
	}
	if got := gateBlocks(at.id, "stale_data"); got != before+1 {
		t.Fatalf("the refusal must be counted ONCE per episode as stale_data: %d → %d", before, got)
	}

	// Positive control: the feed comes back (a 1m bar 30 s old on the wall
	// clock, a live frame 1 s ago) → the SAME row places on the next pass.
	mentorStaleBlockFeed(t, wall, 30*time.Second)
	staleStepReceipt(t, time.Second)
	at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, wall.Add(-time.Hour).UnixMilli(), wall.Add(5*time.Second), admitted)
	if got := armStaleRowState(t, st, row); got.SignalID == "" {
		t.Fatalf("with the feed back the planner arm must place; row = %s", got.State)
	}
}

// TestPlannerArmHeldByAClockStepOnALiveFeed — a wall step at or above the
// freeze threshold holds planner arms too, on a live feed, until the hold
// window passes on MONOTONIC time.
func TestPlannerArmHeldByAClockStepOnALiveFeed(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	clk := kernel.NewSteppedClock(now)
	t.Cleanup(kernel.ClockSampleForTest(clk.Sample))
	at, st, _, _ := shadowWireHarnessAt(t, store.StrategyConfig{}, now)
	mentorStaleBlockFeed(t, now, 30*time.Second)
	staleStepReceipt(t, time.Second)
	row, admitted := armStaleRow(t, at, st)
	before := gateBlocks(at.id, "clock_step")

	kernel.ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-125 * time.Second)
	at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, now.Add(-time.Hour).UnixMilli(), now, admitted)
	if got := armStaleRowState(t, st, row); got.SignalID != "" {
		t.Fatalf("a −125 s wall step must hold planner arms; the row placed (%s)", got.State)
	}
	if got := gateBlocks(at.id, "clock_step"); got != before+1 {
		t.Fatalf("the hold refusal must be counted once as clock_step: %d → %d", before, got)
	}
	clk.Advance(time.Duration(kernel.LoadClockStepConfig().HoldS+1) * time.Second)
	at.runArmedPlacementAt([]market.Kline{{Close: 29600}}, now.Add(-time.Hour).UnixMilli(), now.Add(time.Second), admitted)
	if got := armStaleRowState(t, st, row); got.SignalID == "" {
		t.Fatalf("after the hold window the live feed must place the arm; row = %s", got.State)
	}
}
