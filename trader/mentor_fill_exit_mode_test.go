package trader

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── P2-2 (rel22 mentor audit) — an empty exit mode never silently means B ───
//
// The fill path read the exit branch ONLY from the in-memory staging map
// (mentorBuildLivePos: Mode = mentorExitMode(r.Scenario)). When the staging was
// missing — a restart, or a second instance of the trader, between authoring
// and fill — the position registered with Mode "" and the exit drive, which has
// no case for "", ran the B logic. Row 347 (10-13 17:50:54 CT, position 648)
// logged "mentor exit driver … mode= armedBE=false" while its armed_orders row
// says exit_mode='B'; harmless there, but a C (confluence) trade would get B's
// stop moves, against "do NOT move the stop up at all" [D4.2 p1 @ 14:57].
//
// The resolution is explicit and ordered: the staged branch → the branch
// persisted on the row at authoring → SWING4H's own rule → the course default
// B ("normal": trail every closed candle, 00-METHOD §6 [D2.4 p1 @ 07:01]).
// There is no strategy-level exit-mode default to read (no such knob exists).
// The mode used and WHY is logged and counted once per arm.
//
// Production call site: onArmedOrderUpdate (the full-fill handler).
// MUTANT: drop the persisted step (go straight from staged to the derived
// default) → the C row registers as B → RED.
func TestMentorFillExitModeNeverSilentlyB(t *testing.T) {
	cases := []struct {
		name      string
		staged    string
		column    *string
		condition string
		wantMode  string
		wantCount string
	}{
		{"staging lost, row says C → C (never B)", "", strPtr("C"), "ISB", "C", "exit_mode_persisted"},
		{"staging lost, row says B → B from the row", "", strPtr("B"), "ISB", "B", "exit_mode_persisted"},
		{"staging lost, no column, SWING4H → swing", "", nil, "SWING4H", "swing", "exit_mode_derived_swing"},
		{"staging lost, no column, ISB → course default B", "", nil, "ISB", "B", "exit_mode_course_default"},
		{"staged C wins over a row B (B20 upgrade)", "C", strPtr("B"), "ISB", "C", "exit_mode_staged"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
			resetMentorCounters(at)
			sig := "sig-mode-" + string(rune('a'+i))
			row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-mode-" + string(rune('a'+i)),
				Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: c.condition,
				State: store.StateWorking, SignalID: sig, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1), ExitMode: c.column}
			if err := ledger.DB().Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if c.staged != "" {
				at.setMentorExitMode(row.Scenario, c.staged)
			}
			at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sig, State: "filled", Quantity: 1, FillPrice: 29600, Account: "Sim101"}, ledger)
			got := at.mentorLivePosList()
			if len(got) != 1 {
				t.Fatalf("a full mentor fill must register ONE live position, got %d", len(got))
			}
			if got[0].Pos.Mode != c.wantMode {
				col := "NULL"
				if c.column != nil {
					col = *c.column
				}
				t.Fatalf("registered mode %q, want %q (row exit_mode %s, staged %q) [P2-2; D4.2 p1 @ 14:57]", got[0].Pos.Mode, c.wantMode, col, c.staged)
			}
			if n := at.mentorCountSnapshot()[c.wantCount]; n != 1 {
				t.Fatalf("%s = %d, want 1 — the mode used and why must be counted once per arm (counters %v)", c.wantCount, n, at.mentorCountSnapshot())
			}
		})
	}
}

// TestMentorFillExitModeLoggedOncePerArm — the same arm registered TWICE on a
// real path: a partial fill registered at expiry (B1, mentorRegisterPartial-
// FillIfAbsent — the expiry sweep's call) and then the completing FULL fill
// through onArmedOrderUpdate (the authoritative overwrite). The mode line and
// its counter fire once, not twice.
//
// MUTANT: drop the prior-mode dedup in mentorNoteFillExitMode → 2 → RED.
func TestMentorFillExitModeLoggedOncePerArm(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	resetMentorCounters(at)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-once-1",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: "sig-once", Origin: store.ArmOriginMentor, Contracts: store.IntPtr(3),
		FillQuantity: 1, FillPrice: 29600, ExitMode: strPtr("B")}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !at.mentorRegisterPartialFillIfAbsent(row) {
		t.Fatal("fixture: the partial fill must register")
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-once", State: "filled", Quantity: 3, FillPrice: 29600, Account: "Sim101"}, ledger)
	got := at.mentorLivePosList()
	if len(got) != 1 || got[0].Pos.Mode != "B" || got[0].Legs[0].Qty+got[0].Legs[1].Qty != 3 {
		t.Fatalf("fixture: the full fill must re-register the arm as B ×3, got %d positions %+v", len(got), got)
	}
	if n := at.mentorCountSnapshot()["exit_mode_persisted"]; n != 1 {
		t.Fatalf("exit_mode_persisted = %d across partial + full registration, want 1 (once per arm)", n)
	}
}
