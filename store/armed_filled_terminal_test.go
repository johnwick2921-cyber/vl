package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// W117 F2 — R2 terminal fill guard at the STORE call sites: a late fill can
// never be unwound. The pass's RequestCancel and any SetState must refuse to
// move a filled row; the CAS lives in the WHERE clause of the store writes
// themselves (canon 53: the guard is exercised at the production call sites,
// not on rebuilt inputs).
func TestFilledRowIsTerminalAgainstSetStateAndRequestCancel(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	now := time.Now()

	arm := &ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-25:R2", Version: 1, Session: "RTH",
		Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: "armed", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.UpsertArm(arm); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.SetSignal(arm.ID, "sig-r2"); err != nil {
		t.Fatalf("signal: %v", err)
	}

	// The fill lands (the armed executor's own write).
	if err := st.SetState(arm.ID, StateFilled, "fill@29347.25"); err != nil {
		t.Fatalf("fill: %v", err)
	}

	// The armed pass races in with a cancel request — the CAS must refuse it,
	// TYPED (release #22 F-1: a refused request is never a silent nil a caller
	// could read as "requested").
	if err := st.RequestCancel(arm.ID, "pass invalidation", now.UnixMilli()); !errors.Is(err, ErrArmCancelTerminal) {
		t.Fatalf("request cancel on a filled row: want ErrArmCancelTerminal, got %v", err)
	}
	// And a direct SetState (the pass's invalidation path) must refuse too.
	if err := st.SetState(arm.ID, StateCancelled, "trader stopped"); err != nil {
		t.Fatalf("setstate: %v", err)
	}

	var row ArmedOrderDB
	if err := db.First(&row, arm.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.State != StateFilled {
		t.Fatalf("filled row was moved out of 'filled': state=%q reason=%q", row.State, row.StateReason)
	}
	if !strings.Contains(row.StateReason, "fill@") {
		t.Fatalf("the fill's reason must survive: %q", row.StateReason)
	}
	if row.CancelRequestedAtMs != 0 {
		t.Fatalf("a filled row must not carry a cancel request stamp: %d", row.CancelRequestedAtMs)
	}
}
