// DS-103 split legs (part 1): the entry-group predicates + the group-aware
// one-contract exemption + the sibling-cancel exemption. Every pin is against
// the production call sites the CTO named (one-contract adjudication,
// cancelOtherArmsInPlan) plus the pure group predicates both consume.
package trader

import (
	"path/filepath"
	"testing"
	"time"

	nt "vl/provider/ninjatrader"
	"vl/store"
)

// ── pure predicates ─────────────────────────────────────────────────────────

func TestSameEntryGroup(t *testing.T) {
	a := store.ArmedOrderDB{ID: 1, EntryGroup: "mentor-1"}
	b := store.ArmedOrderDB{ID: 2, EntryGroup: "mentor-1"}
	c := store.ArmedOrderDB{ID: 3, EntryGroup: "mentor-2"}
	d := store.ArmedOrderDB{ID: 4} // no group
	if !sameEntryGroup(a, b) {
		t.Fatalf("same group must be siblings")
	}
	if sameEntryGroup(a, c) {
		t.Fatalf("different groups must NOT be siblings")
	}
	if sameEntryGroup(a, d) || sameEntryGroup(d, a) {
		t.Fatalf("an empty group must never match")
	}
}

func TestEntryGroupSiblings(t *testing.T) {
	leg1 := store.ArmedOrderDB{ID: 1, EntryGroup: "mentor-1", LegIndex: 0}
	leg2 := store.ArmedOrderDB{ID: 2, EntryGroup: "mentor-1", LegIndex: 1}
	other := store.ArmedOrderDB{ID: 3, EntryGroup: "mentor-2"}
	plain := store.ArmedOrderDB{ID: 4}
	rows := []store.ArmedOrderDB{leg1, leg2, other, plain}

	sibs := entryGroupSiblings(rows, leg1)
	if len(sibs) != 1 || sibs[0].ID != leg2.ID {
		t.Fatalf("leg1's siblings = %+v, want just leg2", sibs)
	}
	if got := entryGroupSiblings(rows, plain); got != nil {
		t.Fatalf("a no-group row has no siblings, got %+v", got)
	}
}

func TestEntryGroupSiblingSignalIDs(t *testing.T) {
	leg1 := store.ArmedOrderDB{ID: 1, EntryGroup: "mentor-1", SignalID: "sig-1"}
	leg2 := store.ArmedOrderDB{ID: 2, EntryGroup: "mentor-1", SignalID: ""} // not yet placed
	rows := []store.ArmedOrderDB{leg1, leg2}
	ids := entryGroupSiblingSignalIDs(rows, leg2)
	if len(ids) != 1 || !ids["sig-1"] {
		t.Fatalf("leg2's sibling signal ids = %+v, want {sig-1}", ids)
	}
	if got := entryGroupSiblingSignalIDs(rows, leg1); got != nil {
		t.Fatalf("leg1's sibling (leg2) has no signal id yet, want nil, got %+v", got)
	}
}

func TestEntryGroupFilledSiblingCount(t *testing.T) {
	leg1 := store.ArmedOrderDB{ID: 1, EntryGroup: "mentor-1", State: store.StateFilled, FillQuantity: 3}
	leg2 := store.ArmedOrderDB{ID: 2, EntryGroup: "mentor-1", State: store.StateArmed}
	rows := []store.ArmedOrderDB{leg1, leg2}
	if n := entryGroupFilledSiblingCount(rows, leg2); n != 1 {
		t.Fatalf("leg2 has 1 filled sibling, got %d", n)
	}
	if n := entryGroupFilledSiblingCount(rows, leg1); n != 0 {
		t.Fatalf("leg1 has no filled sibling, got %d", n)
	}
}

// ── one-contract group exemption ────────────────────────────────────────────

// PIN: a sibling leg's WORKING entry does not count against the group — the
// two rows are ONE entry. Without the exemption this is exactly the
// snapshot-7812 refusal, wrongly applied to the group's own second leg.
func TestOneContractExempt_SiblingWorkingEntry_DoesNotCount(t *testing.T) {
	book := []nt.NT8Order{
		entryOrder("leg1-oid", "sig-leg1", 29541.25, "Working"),
	}
	exempt := map[string]bool{"sig-leg1": true}
	v := adjudicateAccountContractExempt(book, true, time.Second, testMaxAge, 7812, 0, exempt)
	if !v.Allowed() {
		t.Fatalf("a sibling leg's working entry must be exempted; got %+v (%s)", v, v.Refusal())
	}
	if v.WorkingEntries != 0 {
		t.Fatalf("want WorkingEntries 0 after exempting the sibling, got %d", v.WorkingEntries)
	}
}

// An UNRELATED working entry is still refused exactly as today — the group
// exemption must not widen the hole.
func TestOneContractExempt_UnrelatedWorkingEntry_StillRefuses(t *testing.T) {
	book := []nt.NT8Order{
		entryOrder("other-oid", "sig-other", 29541.25, "Working"),
	}
	v := adjudicateAccountContractExempt(book, true, time.Second, testMaxAge, 7812, 0, map[string]bool{"sig-leg1": true})
	if v.Allowed() {
		t.Fatalf("an unrelated working entry must still refuse; got %+v", v)
	}
	if v.WorkingEntries != 1 {
		t.Fatalf("want WorkingEntries 1, got %d", v.WorkingEntries)
	}
}

// The no-group form is byte-identical to the legacy call: nil exempt changes
// nothing.
func TestOneContractExempt_NilExemptMatchesLegacy(t *testing.T) {
	book := []nt.NT8Order{
		entryOrder("oid", "sig", 29541.25, "Working"),
	}
	legacy := adjudicateAccountContract(book, true, time.Second, testMaxAge, 7812, 0)
	grouped := adjudicateAccountContractExempt(book, true, time.Second, testMaxAge, 7812, 0, nil)
	if legacy.WorkingEntries != grouped.WorkingEntries || legacy.Action != grouped.Action {
		t.Fatalf("nil-exempt form diverged: legacy=%+v grouped=%+v", legacy, grouped)
	}
}

// ── sibling-cancel exemption ────────────────────────────────────────────────

func seedGroupArm(t *testing.T, ledger *store.ArmedOrderStore, scen, group string, leg int, signal string) store.ArmedOrderDB {
	t.Helper()
	row := &store.ArmedOrderDB{
		TraderID: "hoang", PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scen, Side: "long", EntryPx: 29530.25, StopPx: 29504.25, TargetPx: 29607.25,
		State: store.StateArmed, LegIndex: leg, EntryGroup: group,
	}
	if err := ledger.UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	if signal != "" {
		_ = ledger.SetState(row.ID, store.StateWorking, "")
		_ = ledger.SetSignal(row.ID, signal)
	}
	return *row
}

// PIN: leg 1 places → its SIBLING leg (same EntryGroup) is NOT cancelled, and
// an unrelated arm in the same plan IS. The two rows are ONE entry.
func TestSiblingCancel_Leg2NotCancelledByLeg1(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "sibling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ledger := st.ArmedOrders()

	leg1 := seedGroupArm(t, ledger, "leg1", "mentor-1", 0, "sig-leg1")
	leg2 := seedGroupArm(t, ledger, "leg2", "mentor-1", 1, "") // armed, never placed
	other := seedGroupArm(t, ledger, "other", "", 0, "")       // unrelated, no group
	_ = other

	rows, err := ledger.ListNonTerminal("hoang")
	if err != nil {
		t.Fatal(err)
	}
	at := &AutoTrader{id: "hoang", store: st}
	at.cancelOtherArmsInPlan(ledger, rows, leg1, time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC))

	after, err := ledger.ListNonTerminal("hoang")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]store.ArmedOrderDB{}
	for _, r := range after {
		byID[r.ID] = r
	}

	// The sibling leg 2 must survive, still armed.
	if got, ok := byID[leg2.ID]; !ok || got.State != store.StateArmed {
		t.Fatalf("sibling leg 2 must NOT be cancelled by leg 1's placement; present=%v state=%+v", ok, got)
	}
	// The unrelated arm must be cancelled (never placed → terminal).
	if got, ok := byID[other.ID]; ok {
		t.Fatalf("the unrelated arm must be cancelled; still live: %+v", got)
	}
}
