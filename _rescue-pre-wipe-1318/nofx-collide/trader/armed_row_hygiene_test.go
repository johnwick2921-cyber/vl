package trader

import (
	"path/filepath"
	"testing"

	"nofx/store"
)

// T5 — armed row 35 (2026-09-03, NY v2 S1) read state=filled with
// fill_quantity=0 beside a trader_positions row of quantity 1. Nothing wrote
// the column, and 0 is also a legal "nothing filled", so the ledger could not
// be read either way.
func TestArmedFillQuantityStamped(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ao := st.ArmedOrders()

	row := store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-03:NY:t1", Version: 2, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29285, StopPx: 29362.5, TargetPx: 29130,
		State: "armed", EntryClass: "armed_fill",
	}
	if err := ao.UpsertArm(&row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("no id after upsert")
	}
	if err := ao.SetFillQuantity(row.ID, 1); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	got, err := ao.ListForPlan(row.PlanID)
	if err != nil || len(got) != 1 {
		t.Fatalf("read back: %v n=%d", err, len(got))
	}
	if got[0].FillQuantity != 1 {
		t.Errorf("fill_quantity = %d, want 1 (row 35 read 0 beside a position of 1)", got[0].FillQuantity)
	}
	// A zero is never written over a real quantity — 0 means "not stamped".
	if err := ao.SetFillQuantity(row.ID, 0); err != nil {
		t.Fatalf("zero stamp: %v", err)
	}
	got, _ = ao.ListForPlan(row.PlanID)
	if got[0].FillQuantity != 1 {
		t.Errorf("a zero overwrote a measured quantity: %d", got[0].FillQuantity)
	}
	// ATTRIBUTION: the version the arm BELONGS to, stamped once by UpsertArm.
	if got[0].ArmedUnderVersion != 2 {
		t.Errorf("armed_under_version = %d, want 2", got[0].ArmedUnderVersion)
	}
}

// T5b — a filled or cancelled row never re-logs "⚔️ armed". On 2026-09-03 that
// line fired 5× after row 35 had already filled.
//
// SUPERSEDED SPEC: this asserted armAuthoredLoggable(state), a guard that read
// the DESIRED state and therefore always said "armed". The persisted-state
// question is answered by the id — see TestAuthoredLogOnlyWhenSomethingWasArmed
// and armedActually.
func TestArmedAuthoredNeverRelogsATerminalRow(t *testing.T) {
	for _, state := range store.ArmStateNames() {
		want := !store.IsTerminalArmState(state)
		if got := armedActually(7, state); got != want {
			t.Errorf("armedActually(7, %q) = %v, want %v", state, got, want)
		}
	}
}

// The fill line and the card must name the version the arm BELONGS to. Rows
// created before the attribution column was written carry 0, and 0 must fall
// back to the mutable Version rather than rendering "armed under v0".
func TestArmedUnderVersionFallsBackHonestly(t *testing.T) {
	if got := armedUnderVersionOf(store.ArmedOrderDB{Version: 3, ArmedUnderVersion: 2}); got != 2 {
		t.Errorf("with attribution stamped, want 2, got %d", got)
	}
	// armed rows 35 and 36 are exactly this shape: created 09:02 and 09:20,
	// before the attribution wave booted at 10:28.
	if got := armedUnderVersionOf(store.ArmedOrderDB{Version: 2, ArmedUnderVersion: 0}); got != 2 {
		t.Errorf("unstamped row must fall back to Version, want 2, got %d", got)
	}
}

// ── RE-ARM: STORE-DERIVED, AND THE AUTHORED LOG THAT LIED ───────────────────
//
// Read-only finding (2026-09-03): what stops a filled scenario re-arming in the
// same version is store/armed_orders.go:206 — MANUAL-CANCEL-WINS:
//
//	if existing.Version == row.Version && !IsBootSweepReason(existing.StateReason) {
//	    return nil
//	}
//
// That is a DB read, so it survives a restart. But it returns nil having done
// NOTHING, leaving row.ID at 0, and the caller then logged "⚔️ armed …" as if
// it had succeeded — five times after the 09:03:53 fill, each with a different
// ATR-drifted stop, which is also why the dedup never suppressed them.
//
// So the guard was real and the LOG was the lie.

// T-rearm-1 — a filled row in the same version is not re-armable, and the
// upsert reports that by leaving the id unset.
func TestFilledRowIsNotReArmableInTheSameVersion(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ao := st.ArmedOrders()

	first := store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-03:NY:t1", Version: 2, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29285, StopPx: 29362.5, TargetPx: 29130,
		State: "armed",
	}
	if err := ao.UpsertArm(&first); err != nil {
		t.Fatalf("first arm: %v", err)
	}
	if err := ao.SetState(first.ID, "filled", "fill"); err != nil {
		t.Fatalf("fill: %v", err)
	}

	// the next cycle's arm intent, same version, stop drifted by live ATR
	again := store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-03:NY:t1", Version: 2, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29285, StopPx: 29354.91, TargetPx: 29130,
		State: "armed",
	}
	if err := ao.UpsertArm(&again); err != nil {
		t.Fatalf("re-arm attempt must not error, it must decline: %v", err)
	}
	if again.ID != 0 {
		t.Errorf("a declined re-arm must leave the id unset (that is how the caller knows nothing happened), got %d", again.ID)
	}
	rows, _ := ao.ListForPlan(first.PlanID)
	if len(rows) != 1 || rows[0].State != "filled" {
		t.Fatalf("the filled row must stay filled and alone: %d rows, state %q", len(rows), rows[0].State)
	}
	if rows[0].StopPx != 29362.5 {
		t.Errorf("a declined re-arm must not rewrite the filled row's prices, stop = %v", rows[0].StopPx)
	}
}

// T-rearm-2 — the guard is STORE-derived, so it holds across a restart. A fresh
// store handle over the same file is the restart: no in-memory dedup survives.
func TestReArmGuardSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")

	st1, err := store.New(path)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	row := store.ArmedOrderDB{
		TraderID: "t1", PlanID: "2026-09-03:NY:t1", Version: 2, Session: "NY",
		Scenario: "S1", Side: "short", EntryPx: 29285, StopPx: 29362.5, TargetPx: 29130,
		State: "armed",
	}
	if err := st1.ArmedOrders().UpsertArm(&row); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := st1.ArmedOrders().SetState(row.ID, "filled", "fill"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	_ = st1.Close()

	st2, err := store.New(path) // the restart
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	after := row
	after.ID, after.State = 0, "armed"
	if err := st2.ArmedOrders().UpsertArm(&after); err != nil {
		t.Fatalf("post-restart re-arm: %v", err)
	}
	if after.ID != 0 {
		t.Error("the guard must still decline after a restart — it is a DB read, not memory")
	}
	rows, _ := st2.ArmedOrders().ListForPlan(row.PlanID)
	if len(rows) != 1 || rows[0].State != "filled" {
		t.Fatalf("after restart: %d rows, state %q — want the filled row untouched", len(rows), rows[0].State)
	}
}

// T-rearm-3 — the authored log fires ONLY when the upsert actually armed
// something. This is the defect in the first cut of F4: it guarded on
// row.State, which is the DESIRED state ("armed") and never the persisted one,
// so it passed every time and the five post-fill lines would still have fired.
func TestAuthoredLogOnlyWhenSomethingWasArmed(t *testing.T) {
	if armedActually(0, "armed") {
		t.Error("id 0 means the upsert declined — nothing was armed, so nothing may be logged")
	}
	if !armedActually(7, "armed") {
		t.Error("a real id with a live state is a real arm")
	}
	for _, terminal := range store.ArmStateNames() {
		if !store.IsTerminalArmState(terminal) {
			continue
		}
		if armedActually(7, terminal) {
			t.Errorf("state %q is not an arm", terminal)
		}
	}
}
