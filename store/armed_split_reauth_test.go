package store

import (
	"path/filepath"
	"testing"
)

// REVIEW-SPLIT-2 P2: the mentor size and split follow every authorization —
// the in-place refresh of an ARMED row and the re-authorization of a terminal
// row under a new version. Mutant: drop "leg1_tp" (or contracts / leg1_qty)
// from either update map → the re-armed row keeps the stale split → RED.
func TestUpsertArmCarriesTheSplitThroughReauthorization(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "split.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ao := st.ArmedOrders()
	arm := func(version, contracts, leg1 int, leg1TP float64) *ArmedOrderDB {
		return &ArmedOrderDB{TraderID: "t1", PlanID: "mentor", Scenario: "lvl-split", Session: "MENTOR",
			Side: "long", State: StateArmed, EntryPx: 100, StopPx: 95, TargetPx: 110, Kind: "stop_entry",
			Version: version, Origin: ArmOriginMentor, Contracts: IntPtr(contracts), Leg1Qty: IntPtr(leg1), Leg1TP: leg1TP}
	}
	read := func() ArmedOrderDB {
		var r ArmedOrderDB
		if err := ao.DB().Where("scenario = ?", "lvl-split").Order("id DESC").First(&r).Error; err != nil {
			t.Fatal(err)
		}
		return r
	}
	check := func(stage string, contracts, leg1 int, leg1TP float64) {
		t.Helper()
		r := read()
		if r.Contracts == nil || *r.Contracts != contracts || r.Leg1Qty == nil || *r.Leg1Qty != leg1 || r.Leg1TP != leg1TP {
			t.Fatalf("%s: row split = (%v, %v, %.2f), want (%d, %d, %.2f)", stage, r.Contracts, r.Leg1Qty, r.Leg1TP, contracts, leg1, leg1TP)
		}
	}
	if err := ao.UpsertArm(arm(1, 5, 3, 105)); err != nil {
		t.Fatal(err)
	}
	check("first authorization", 5, 3, 105)
	// ARMED row refreshed in place (the stop moved → leg 1's TP moved).
	if err := ao.UpsertArm(arm(2, 4, 2, 104)); err != nil {
		t.Fatal(err)
	}
	check("armed in-place refresh", 4, 2, 104)
	// Terminal (never placed) → re-authorized under a NEW version.
	if err := ao.DB().Model(&ArmedOrderDB{}).Where("scenario = ?", "lvl-split").Update("state", "expired").Error; err != nil {
		t.Fatal(err)
	}
	if err := ao.UpsertArm(arm(3, 6, 4, 103)); err != nil {
		t.Fatal(err)
	}
	check("terminal re-authorization", 6, 4, 103)
}

// TestUpsertArmUnstampedDoesNotNullASize (dispatch-31 D7): a PLANNER arm sized
// at its FIRST authorization keeps that size through an in-place refresh that
// carries no stamp — planner_contracts dropped 4→1 mid-arm must not NULL the
// row's contracts/leg1 (the wire already sized from the first authorization; an
// unplaced refresh re-prices, never un-sizes). Mutant: write the size columns
// unconditionally (a nil stamp NULLs the size) → RED.
func TestUpsertArmUnstampedDoesNotNullASize(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "split-nonull.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ao := st.ArmedOrders()
	planner := func(contracts, leg1 int, leg1TP float64) *ArmedOrderDB {
		return &ArmedOrderDB{TraderID: "t1", PlanID: "planner", Scenario: "lvl-4", Session: "NY",
			Side: "long", State: StateArmed, EntryPx: 100, StopPx: 95, TargetPx: 110, Kind: "limit",
			Version: 1, Contracts: IntPtr(contracts), Leg1Qty: IntPtr(leg1), Leg1TP: leg1TP}
	}
	read := func() ArmedOrderDB {
		var r ArmedOrderDB
		if err := ao.DB().Where("scenario = ?", "lvl-4").First(&r).Error; err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := ao.UpsertArm(planner(4, 2, 105)); err != nil {
		t.Fatal(err)
	}
	// The knob drops 4 → 1: the re-authorization carries NO stamp (nil size).
	unstamped := planner(0, 0, 0)
	unstamped.Contracts, unstamped.Leg1Qty, unstamped.Leg1TP = nil, nil, 0
	if err := ao.UpsertArm(unstamped); err != nil {
		t.Fatal(err)
	}
	r := read()
	if r.Contracts == nil || *r.Contracts != 4 || r.Leg1Qty == nil || *r.Leg1Qty != 2 || r.Leg1TP != 105 {
		t.Fatalf("an unstamped refresh must not NULL the first authorization's size: %+v", r)
	}
}
