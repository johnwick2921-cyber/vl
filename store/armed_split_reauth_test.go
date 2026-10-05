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
