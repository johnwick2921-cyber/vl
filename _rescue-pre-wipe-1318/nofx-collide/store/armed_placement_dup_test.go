// D5 regression — the re-authorization must not spawn a row per cycle.
//
// Live defect from boot 8: UpsertArm's lookup used First() with NO ordering, so
// SQLite returned the LOWEST rowid — which after the first cancel is the
// terminal row. The "terminal row that reached the broker keeps its record"
// branch then fired on EVERY re-authorization, minting placement_seq 1,2,3…
// once per cycle. One NY scenario reached 24 rows in 100 minutes and drove the
// cutover gate's leg 4 to "broker 1 vs ledger 23 — MISMATCH".
//
// The record-keeping law is right; the lookup was wrong. A LIVE row must win.

package store

import (
	"testing"
	"time"
)

func TestReauthorizationDoesNotSpawnARowPerCycle(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	now := time.Now()

	mk := func() *ArmedOrderDB {
		return &ArmedOrderDB{
			TraderID: "t1", PlanID: "P1", Scenario: "S1", Side: "long",
			EntryPx: 29657.38, StopPx: 29680, TargetPx: 29600,
			State: "armed", CreatedAt: now, UpdatedAt: now,
		}
	}
	// A first placement that reached the broker and was then cancelled — the
	// shape that poisoned the lookup.
	seed := mk()
	if err := st.UpsertArm(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.SetSignal(seed.ID, "sig-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(seed.ID, "cancelled", "stale window"); err != nil {
		t.Fatal(err)
	}

	// The re-authorization cycle runs many times, as it does live (~2 min).
	for i := 0; i < 10; i++ {
		if err := st.UpsertArm(mk()); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}

	var rows []ArmedOrderDB
	if err := st.db.Where("plan_id = ? AND scenario = ?", "P1", "S1").Find(&rows).Error; err != nil {
		t.Fatalf("list: %v", err)
	}
	// The cancelled placement plus ONE live row. Not one row per cycle.
	if len(rows) != 2 {
		t.Fatalf("10 re-authorizations produced %d rows, want 2 (the cancelled placement + one live row)", len(rows))
	}
	live := 0
	for _, r := range rows {
		if !IsTerminalArmState(r.State) {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("want exactly 1 live row, got %d", live)
	}
	// And the cancelled placement still keeps its record (D5 unchanged).
	var prior ArmedOrderDB
	if err := st.db.First(&prior, seed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if prior.State != "cancelled" || prior.SignalID != "sig-1" {
		t.Fatalf("the placed row must keep its record: %+v", prior)
	}
}
