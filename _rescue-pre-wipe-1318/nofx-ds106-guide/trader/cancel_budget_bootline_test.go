package trader

import (
	"strings"
	"testing"

	"vl/store"
)

// B2's boot line. The owner's instruction was explicit: the change is INERT
// until a cancel is re-requested after a restart, and the boot line must say so.
//
// Why it earns a clause rather than a report footnote: a reader who sees
// "rerequest-cap=5" has no way to tell whether that budget is per row forever
// (the defect) or per process (the fix). Both render identically. The line has
// to name WHICH, and it has to name the carry — how many rows in this ledger
// were counted by a process that is gone, i.e. how many will reset on their next
// request. Zero of those is the normal, quiet case and must read as a measured
// zero, not as an uncomputed one (A24).
func TestCancelBootLineStatesThePerProcessBudget(t *testing.T) {
	line := CancelBootLine(nil, ReconcileCounts{}, 0)

	for _, want := range []string{
		"rerequest-cap=",
		"budget=per-process",
		"inert until a cancel is re-requested after a restart",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("cancel boot line must contain %q so a reader can tell a per-process budget from a per-row one; got:\n  %s", want, line)
		}
	}

	// The carry must be reported, and with a nil store it is UNKNOWN — never 0.
	// A fabricated zero here would read as "no rows carry a foreign boot", which
	// is a claim this call cannot make.
	if !strings.Contains(line, "carry=UNKNOWN") {
		t.Fatalf("with no store the foreign-boot carry is UNKNOWN, never a plausible zero; got:\n  %s", line)
	}
}

// With a real ledger the carry is a MEASURED number, and a row stamped by
// another process must be counted in it — that row is exactly the one whose
// budget resets on its next request.
func TestCancelBootLineCountsTheForeignBootCarry(t *testing.T) {
	st := newCancelBootLineStore(t)

	line := CancelBootLine(st, ReconcileCounts{}, 0)
	if !strings.Contains(line, "carry=0") {
		t.Fatalf("an empty ledger carries nothing and must say carry=0 (measured); got:\n  %s", line)
	}

	ao := st.ArmedOrders()
	row := &store.ArmedOrderDB{
		TraderID: "t1", PlanID: "p1", Version: 1, Session: "NY",
		Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 99, TargetPx: 102,
		State: store.StateCancelPending,
	}
	if err := ao.UpsertArm(row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ao.DB().Exec(
		"UPDATE armed_orders SET cancel_attempts = 5, cancel_attempts_boot = ? WHERE id = ?",
		"1-1700000000000-a-process-that-is-gone", row.ID).Error; err != nil {
		t.Fatalf("stamp a foreign boot: %v", err)
	}

	line = CancelBootLine(st, ReconcileCounts{}, 0)
	if !strings.Contains(line, "carry=1") {
		t.Fatalf("one row counted by a departed process must appear as carry=1 — it is the row whose budget resets on its next request; got:\n  %s", line)
	}
}

func newCancelBootLineStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(t.TempDir() + "/boot.db")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
