// D5 (arms-follow-bias, 2026-09-04) — a WORKING row is a live broker order, and
// its prices are a record of what is resting at the exchange.
//
// UpsertArm rewrote entry/stop/target in place on a `working` row with no
// cancel: the slot was overwritten and the brackets lost (rows 582, 585). What
// the ledger then said, and what the broker actually held, were two different
// orders wearing one id.
//
// A re-authorization under a working row is REFUSED here. Replacing a live
// order requires a cancel first, and the store is not the place that can issue
// one — so it declines rather than quietly diverging from the broker.

package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func workingRow(t *testing.T, st *ArmedOrderStore, entry, stop, target float64) *ArmedOrderDB {
	t.Helper()
	now := time.Now()
	row := &ArmedOrderDB{
		TraderID: "T1", PlanID: "P1", Scenario: "S1", Side: "long",
		EntryPx: entry, StopPx: stop, TargetPx: target,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(row); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	if err := st.SetState(row.ID, "working", ""); err != nil {
		t.Fatalf("to working: %v", err)
	}
	if err := st.SetSignal(row.ID, "sig-1"); err != nil {
		t.Fatalf("set signal: %v", err)
	}
	return row
}

// The defect, stated directly.
func TestUpsertArmRefusesToRewriteAWorkingRow(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	seed := workingRow(t, st, 29500, 29470, 29560)

	now := time.Now()
	err := st.UpsertArm(&ArmedOrderDB{
		TraderID: "T1", PlanID: "P1", Scenario: "S1", Side: "long",
		EntryPx: 29610, StopPx: 29580, TargetPx: 29670, // a DIFFERENT order
		State: "armed", CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("a re-authorization under a WORKING row must be refused — the broker order is still live")
	}
	if !strings.Contains(err.Error(), "working") {
		t.Errorf("the refusal must name the state it protected: %v", err)
	}

	// And nothing moved: the row still describes the order the broker holds.
	var after ArmedOrderDB
	if e := st.db.First(&after, seed.ID).Error; e != nil {
		t.Fatalf("reload: %v", e)
	}
	if after.EntryPx != 29500 || after.StopPx != 29470 || after.TargetPx != 29560 {
		t.Fatalf("prices were rewritten under a live order: %.2f/%.2f/%.2f", after.EntryPx, after.StopPx, after.TargetPx)
	}
	if after.SignalID != "sig-1" {
		t.Errorf("the live signal id must survive: %q", after.SignalID)
	}
}

// An ARMED row has no broker order behind it — refreshing its prices is the
// normal re-authorization path and must still work.
func TestUpsertArmStillRefreshesAnArmedRow(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	now := time.Now()
	seed := &ArmedOrderDB{
		TraderID: "T1", PlanID: "P2", Scenario: "S1", Side: "long",
		EntryPx: 29500, StopPx: 29470, TargetPx: 29560,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.UpsertArm(&ArmedOrderDB{
		TraderID: "T1", PlanID: "P2", Scenario: "S1", Side: "long",
		EntryPx: 29510, StopPx: 29480, TargetPx: 29570,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("an armed row must still be refreshable: %v", err)
	}
	var after ArmedOrderDB
	if e := st.db.First(&after, seed.ID).Error; e != nil {
		t.Fatalf("reload: %v", e)
	}
	if after.EntryPx != 29510 {
		t.Errorf("armed row must refresh, entry = %.2f", after.EntryPx)
	}
}

// 582/585's shape: the replacement is a SECOND row, not a rewrite of the first.
// Once the live order is cancelled, the new authorization lands as its own row
// and the cancelled one stays on the record forever.
func TestReplacementAfterCancelIsASecondRow(t *testing.T) {
	st := NewArmedOrderStore(newArmedTestDB(t))
	seed := workingRow(t, st, 29500, 29470, 29560)

	if err := st.SetState(seed.ID, "cancelled", "replaced"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	now := time.Now()
	// CTO fold (PR #526): the SAME version never re-places a slot that reached
	// the broker — the replacement arrives under a NEW version.
	if err := st.UpsertArm(&ArmedOrderDB{
		TraderID: "T1", PlanID: "P1", Scenario: "S1", Side: "long", Version: seed.Version,
		EntryPx: 29610, StopPx: 29580, TargetPx: 29670,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}); !errors.Is(err, ErrArmSlotSpent) {
		t.Fatalf("a same-version replacement of a placed slot must be refused as spent, got %v", err)
	}
	if err := st.UpsertArm(&ArmedOrderDB{
		TraderID: "T1", PlanID: "P1", Scenario: "S1", Side: "long", Version: seed.Version + 1,
		EntryPx: 29610, StopPx: 29580, TargetPx: 29670,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("after a cancel the new-version replacement must be accepted: %v", err)
	}

	var rows []ArmedOrderDB
	if e := st.db.Where("plan_id = ? AND scenario = ?", "P1", "S1").Find(&rows).Error; e != nil {
		t.Fatalf("list: %v", e)
	}
	if len(rows) != 2 {
		t.Fatalf("every broker placement is one row forever — want 2 rows, got %d", len(rows))
	}
}
