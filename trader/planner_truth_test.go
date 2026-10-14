package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── FIX-PLANNER-TRUTH (1) — a WORKING planner arm whose scenario id is ABSENT
// from the new plan version is cancelled through the normal cancel path
// (cancel_pending → broker proof). Re-spec only re-prices a scenario that is
// STILL authored; until this wave a dropped scenario rested until a fill, the
// 15-min stale reaper, a band cancel, or plan death.

// A dropped scenario's working order is cancelled exactly once and its row is
// cancel_pending naming "scenario dropped by plan v2".
func TestDroppedScenarioWorkingArmCancelledByNewVersion(t *testing.T) {
	r := newZoneRig(t, "fp-dropped", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	before, _ := store.SystemCounter(r.st, "arm:dropped_scenario_cancel")

	// v2 authors ONLY S2 (S1 is dropped) and S2 is not armable, so the pass's
	// only action is the dropped-scenario cancel.
	sc2 := zoneScenario("S2", kernel.EntryPolicyMarketInZone, zone, false)
	sc2.Arm.Enabled = false
	r.newVersion(sc2)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	sigs, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid {
		t.Fatalf("a dropped scenario must cancel its working order %s exactly once: sigs=%+v cancels=%+v", sid, sigs, cancels)
	}
	if len(sigs) != 0 {
		t.Fatalf("v2 authors no armable scenario — nothing may place: %+v", sigs)
	}
	row := r.row("S1")
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "scenario dropped by plan v2") {
		t.Fatalf("the dropped scenario's row must be cancel_pending naming 'scenario dropped by plan v2': %+v", row)
	}
	if n, _ := store.SystemCounter(r.st, "arm:dropped_scenario_cancel"); n != before+1 {
		t.Fatalf("the dropped-scenario cancel must be counted once (%d → %d)", before, n)
	}
}

// A scenario that STAYS in the new version keeps resting — the dropped-scenario
// sweep must not touch it.
func TestDroppedScenarioSweepLeavesPresentScenarioWorking(t *testing.T) {
	r := newZoneRig(t, "fp-present", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	r.newVersion(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false))
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("a still-present scenario must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	if row := r.row("S1"); row.State != store.StateWorking {
		t.Fatalf("the working row must keep resting: %+v", row)
	}
}

// A FILLED row is never touched even when its scenario is dropped.
func TestDroppedScenarioFilledRowUntouched(t *testing.T) {
	r := newZoneRig(t, "fp-filled", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	sid := r.placeWorking(100.5).SignalID
	if err := r.st.ArmedOrders().SetState(r.row("S1").ID, store.StateFilled, "test fill"); err != nil {
		t.Fatal(err)
	}
	sc2 := zoneScenario("S2", kernel.EntryPolicyMarketInZone, zone, false)
	sc2.Arm.Enabled = false
	r.newVersion(sc2)
	t1 := r.now.Add(time.Minute)
	r.restingBook(t1, sid, 100.5)
	r.setTape(zoneTape(101.95, t1, 0))
	r.at.maybeManageArmedOrdersAt(nil, t1)
	_, cancels := r.drain()
	if len(cancels) != 0 {
		t.Fatalf("a FILLED row must never be cancelled even when its scenario is dropped: %+v", cancels)
	}
	if row := r.row("S1"); row.State != store.StateFilled {
		t.Fatalf("the filled row must stay filled: %+v", row)
	}
}

// ── FIX-PLANNER-TRUTH (3) — the placement log names the REAL order type: a
// mentor row routes PlaceStopEntryWithLimit (stop-limit), and the success line
// must say so instead of the old hardcoded "stop-market".

func TestStopEntryLogNamesRealOrderTypeForMentorRow(t *testing.T) {
	at := &AutoTrader{id: "hoang"}
	pl := &fakePlacer{}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{
		ID: 1, PlanID: "p1", Version: 1, Scenario: "S1", LegIndex: 0,
		Side: "SHORT", EntryPx: 29591.02, StopPx: 29650, TargetPx: 0,
		Origin: store.ArmOriginMentor, ExpiryMs: time.Now().Add(time.Hour).UnixMilli(),
		Contracts: store.IntPtr(1),
	}
	d := decideStopEntry("SHORT", r.EntryPx, testOffset(), testTick, 29650.00)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be an otherwise-placeable mentor arm, got action %q", d.Action)
	}
	logBuf := captureTraderLog(t)
	at.placeOneStopEntry(pl, led, r, d, 29650.00, time.Now(), freeSlot())
	if pl.stopLimitCalls != 1 {
		t.Fatalf("a mentor row must route through the stop-limit variant, got %d stop-limit call(s)", pl.stopLimitCalls)
	}
	if !strings.Contains(logBuf.String(), "stop-limit") {
		t.Fatalf("the placement log must name stop-limit for a mentor row:\n%s", logBuf.String())
	}
	if strings.Contains(logBuf.String(), "stop-market") {
		t.Fatalf("the placement log must not say stop-market for a mentor row:\n%s", logBuf.String())
	}
}
