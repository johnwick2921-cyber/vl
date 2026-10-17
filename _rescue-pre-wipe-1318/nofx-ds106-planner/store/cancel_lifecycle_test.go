package store

import (
	"strings"
	"testing"
	"time"
)

func cancelStore(t *testing.T) (*ArmedOrderStore, int64) {
	t.Helper()
	st := NewArmedOrderStore(newArmedTestDB(t))
	row := &ArmedOrderDB{
		TraderID: "hoang", PlanID: "2026-09-06:NY", Version: 1, Session: "NY",
		Scenario: "S2", Side: "short", EntryPx: 29720, StopPx: 29755, TargetPx: 29635,
		State: StateArmed,
	}
	if err := st.UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(row.ID, StateWorking, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSignal(row.ID, "sig-1"); err != nil {
		t.Fatal(err)
	}
	return st, row.ID
}

// E2 — REQUEST IS NOT CONFIRMATION. A cancel request moves the row to
// cancel_pending and NEVER to cancelled; only a snapshot may say cancelled.
func TestCancelRequestNeverWritesCancelled(t *testing.T) {
	st, id := cancelStore(t)
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC).UnixMilli()

	if err := st.RequestCancel(id, "gate changed", now); err != nil {
		t.Fatal(err)
	}
	var row ArmedOrderDB
	if err := st.db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != StateCancelPending {
		t.Fatalf("a cancel REQUEST must leave the row %q, got %q — writing 'cancelled' on a send is the whole defect", StateCancelPending, row.State)
	}
	if row.CancelRequestedAtMs != now {
		t.Fatalf("the request time must be recorded, got %d", row.CancelRequestedAtMs)
	}
	if row.CancelAttempts != 1 {
		t.Fatalf("attempts must count sends, got %d", row.CancelAttempts)
	}
	if row.CancelSettledSnapshotID != 0 {
		t.Fatalf("nothing has settled it yet, got snapshot %d", row.CancelSettledSnapshotID)
	}

	// THE SLOT IS STILL LOCKED. A pending cancel is NON-TERMINAL.
	rows, err := st.ListNonTerminal("hoang")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("a cancel_pending row must remain NON-TERMINAL — otherwise a replacement races the cancel it is waiting on; got %d row(s)", len(rows))
	}

	// Only the snapshot may finish it.
	if err := st.ConfirmCancel(id, 1664, "absent from a fresh book"); err != nil {
		t.Fatal(err)
	}
	if err := st.db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != StateCancelled || row.CancelSettledSnapshotID != 1664 {
		t.Fatalf("confirmation must record WHICH snapshot settled it: state=%q snapshot=%d", row.State, row.CancelSettledSnapshotID)
	}
	if rows, _ := st.ListNonTerminal("hoang"); len(rows) != 0 {
		t.Fatalf("a confirmed cancel frees the slot, got %d non-terminal row(s)", len(rows))
	}
}

// E3 — A RE-REQUEST BUMPS ATTEMPTS AND KEEPS THE ORIGINAL AGE. The WARN must
// report how long the FIRST attempt has gone unconfirmed, not the latest.
func TestReRequestKeepsTheOriginalAgeAndCountsAttempts(t *testing.T) {
	st, id := cancelStore(t)
	first := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC).UnixMilli()
	later := first + 90_000

	if err := st.RequestCancel(id, "gate changed", first); err != nil {
		t.Fatal(err)
	}
	if err := st.RequestCancel(id, "re-request after timeout", later); err != nil {
		t.Fatal(err)
	}
	var row ArmedOrderDB
	if err := st.db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.CancelAttempts != 2 {
		t.Fatalf("two sends must count 2 attempts, got %d", row.CancelAttempts)
	}
	if row.CancelRequestedAtMs != first {
		t.Fatalf("the age must be measured from the FIRST request (%d), got %d", first, row.CancelRequestedAtMs)
	}
	if row.State != StateCancelPending {
		t.Fatalf("a re-request must NOT promote the row, got %q", row.State)
	}

	// The unconfirmed counter sees it once it passes the timeout.
	if n := st.CountCancelUnconfirmed(first+91_000, 90_000); n != 1 {
		t.Fatalf("a request older than the timeout must count as unconfirmed, got %d", n)
	}
	if n := st.CountCancelUnconfirmed(first+1_000, 90_000); n != 0 {
		t.Fatalf("a fresh request is pending, not unconfirmed, got %d", n)
	}
	if n := st.CountCancelPending(); n != 1 {
		t.Fatalf("pending count must be read from the table, got %d", n)
	}
}

// A CANCEL_PENDING ROW IS A LIVE BROKER ORDER AND MUST NOT MINT A REPLACEMENT.
//
// Found by adversarial review of THIS wave's own in-flight code. UpsertArm's
// three decision points all predate cancel_pending: the sort key at :236 ranks
// it with the terminal rows, the working-row refusal at :243 does not cover it,
// and the mint branch at :251 fires on "not armed AND has a signal id" — which
// a cancel_pending row satisfies exactly. The result would have been a NEW
// placement, in state 'armed' with the signal id cleared, while the old order
// may still be resting at the broker: a fresh path to the very stacking this
// wave exists to stop.
func TestCancelPendingRowNeverMintsAReplacement(t *testing.T) {
	st, id := cancelStore(t)
	if err := st.RequestCancel(id, "gate changed", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var before int64
	st.db.Model(&ArmedOrderDB{}).Count(&before)

	// The next authorization cycle for the SAME slot.
	again := &ArmedOrderDB{
		TraderID: "hoang", PlanID: "2026-09-06:NY", Version: 2, Session: "NY",
		Scenario: "S2", Side: "short", EntryPx: 29725, StopPx: 29760, TargetPx: 29640,
		State: StateArmed,
	}
	err := st.UpsertArm(again)

	var after int64
	st.db.Model(&ArmedOrderDB{}).Count(&after)
	if after != before {
		t.Fatalf("a cancel_pending row minted a replacement: %d rows before, %d after — a cancel in flight is a LIVE broker order and must be refused like a working one", before, after)
	}
	if err == nil {
		t.Fatal("UpsertArm must REFUSE to rewrite a row whose cancel is still in flight, and say so")
	}
	if !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("the refusal must name the reason, got %q", err.Error())
	}

	// And the original row is untouched — still pending, still holding the slot.
	var row ArmedOrderDB
	if err := st.db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != StateCancelPending || row.EntryPx != 29720 {
		t.Fatalf("the pending row must be left exactly as it was: state=%q entry=%.2f", row.State, row.EntryPx)
	}
}
