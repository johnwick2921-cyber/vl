package trader

import (
	"path/filepath"
	"testing"
	"time"

	"vl/store"
)

// ── B4 (L7) — a mentor placement must NOT cancel every other mentor arm ──────
//
// cancelOtherArmsInPlan is the "one live entry per plan" owner ruling: when one
// arm places, every other non-terminal arm in the SAME plan is cancelled. Every
// mentor arm shares PlanID="mentor", so before B4 a single mentor fill swept a
// resting level order and a swing order off the book. The course keeps every
// level alive independently ("he saves all levels"; a level order dies only on
// a close-through or the window end [D2.3 p1 @18:01–19:12]) and cancels every
// arm ONLY on the 15m/5m conflict [D4.2 p1 @13:59–14:53] — which the evaluator
// already emits as per-ArmID cancels (conflictArmCancels). So a mentor
// placement must cancel only its SAME-SLOT sibling (same scenario + leg), never
// a different level or the swing.

func seedMentorScopeArm(t *testing.T, ledger *store.ArmedOrderStore, scen string, leg, seq int, entry float64, signal string) store.ArmedOrderDB {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: "hoang", PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scen, Side: "long", EntryPx: entry, StopPx: entry - 10, TargetPx: entry + 20,
		State: store.StateArmed, LegIndex: leg, Kind: "stop_entry",
		Origin: store.ArmOriginMentor, PlacementSeq: seq,
	}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if signal != "" {
		_ = ledger.SetState(row.ID, store.StateWorking, "")
		_ = ledger.SetSignal(row.ID, signal)
	}
	return row
}

func TestB4MentorPlacementKeepsOtherMentorSlots(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "b4scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ledger := st.ArmedOrders()

	placed := seedMentorScopeArm(t, ledger, "isb-1", 0, 0, 29600, "sig-isb")
	level := seedMentorScopeArm(t, ledger, "lvl-2", 0, 0, 29500, "")    // resting level order, never placed
	swing := seedMentorScopeArm(t, ledger, "swing-1", 0, 0, 29400, "")  // resting swing order, never placed
	sameSlot := seedMentorScopeArm(t, ledger, "isb-1", 0, 1, 29600, "") // a re-authorization of the SAME slot

	rows, err := ledger.ListNonTerminal("hoang")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("harness: want 4 non-terminal mentor arms, got %d", len(rows))
	}

	at := &AutoTrader{id: "hoang", store: st}
	at.cancelOtherArmsInPlan(ledger, rows, placed, time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC))

	stateByID := map[int64]string{}
	after, _ := ledger.ListNonTerminal("hoang")
	for _, r := range after {
		stateByID[r.ID] = r.State
	}
	stateOf := func(row store.ArmedOrderDB) string {
		if s, ok := stateByID[row.ID]; ok {
			return s
		}
		return store.StateCancelled // not non-terminal → it was cancelled
	}

	if got := stateOf(level); got != store.StateArmed {
		t.Fatalf("a resting level order at another level must survive a mentor placement, got %q", got)
	}
	if got := stateOf(swing); got != store.StateArmed {
		t.Fatalf("a resting swing order must survive a mentor placement, got %q", got)
	}
	if got := stateOf(sameSlot); got != store.StateCancelled {
		t.Fatalf("a same-slot sibling (same scenario + leg) must still be cancelled, got %q", got)
	}
	if got := stateOf(placed); got != store.StateWorking {
		t.Fatalf("the placed arm must keep its state, got %q", got)
	}
}
