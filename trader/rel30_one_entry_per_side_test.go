package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── release #30: ONE working mentor entry per side (every kind) ─────────────
//
// Two same-side stop entries resting at once can both fill in one bar (10-02
// 07:11 ISB-rev long 5 + ISB long 3 = 8 MNQ). The fix is two layers, both
// tested at the production call sites:
//   1. placement-time REFUSE — a new same-side entry while another same-side
//      entry is working is refused (the reference is the FIRST; a new arm forms
//      only after the previous one is filled/cancelled/expired).
//   2. fill-time OCO — when one fills, every other working SAME-SIDE entry is
//      cancelled before it can fill (the never-add sweep only runs next tick).

// rel30WorkingRow writes a WORKING mentor-origin armed row for one side.
func rel30WorkingRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario, side, signalID, condition string) store.ArmedOrderDB {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: mentorPlanID(at.id), Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: side, EntryPx: 29600, StopPx: 29595, TargetPx: 29620,
		Kind: "stop_entry", Condition: condition, State: store.StateWorking, SignalID: signalID,
		Origin: store.ArmOriginMentor, ExpiryMs: mentorClockNow().UnixMilli() + 60_000,
	}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

// rel30SeedBook puts every signal id on the live book as a WORKING entry, so
// cancelSafetyFor lets the OCO cancel through (an unseen entry refuses).
func rel30SeedBook(t *testing.T, at *AutoTrader, signals ...string) {
	t.Helper()
	orders := make([]ntwire.NT8Order, 0, len(signals))
	for _, s := range signals {
		orders = append(orders, ntwire.NT8Order{Name: s, State: "Working", Type: "stop_limit"})
	}
	at.armedTrader().GetServer().OrderSnapshots().PutAt(
		ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: orders}, mentorClockNow())
}

// TestMentorOneEntryPerSideRefusesSameSidePlacement — the placement layer: a
// new same-side entry while another same-side entry works is REFUSED (one
// working entry per side). An OPPOSITE-side entry proceeds (flip is allowed —
// never-add bars only the same side). MUTANT: remove the same-side check in
// mentorPlaceIntent → the same-side reverse-ISB places → RED.
func TestMentorOneEntryPerSideRefusesSameSidePlacement(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	mentorPinClockToRTH(t)
	resetMentorCounters(at)

	// An ISB long already rests (working) at the broker.
	rel30WorkingRow(t, at, ledger, "isb-1-123", "long", "sig-isb-rest", "ISB")

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}
	rev := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 29601, Stop: 29596, Target: 29620, Setup: "ISBREV", StopPts: 5, ArmID: "isbrev-1"}
	at.mentorPlaceIntent(rev, choice, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("a same-side entry while another same-side entry works must be refused, placed=%d", len(placed))
	}
	if c := at.mentorCountSnapshot()["refused_same_side_working"]; c != 1 {
		t.Fatalf("refused_same_side_working = %d, want 1", c)
	}

	// An opposite-side entry proceeds while a same-side entry merely WORKS (no
	// open position) — the no-flip gate bars only a POSITION-open flip, not a
	// resting same-side arm (that is the same-side refuse above).
	short := rev
	short.Side = mentor.SideShort
	short.Price = 29500
	short.Stop = 29505
	short.Target = 29480
	short.ArmID = "isb-short-1"
	at.mentorPlaceIntent(short, choice, 1000, 1100)
	if len(placed) != 1 {
		t.Fatalf("an opposite-side entry must proceed while no position is open, placed=%d", len(placed))
	}
}

// TestMentorOneEntryPerSideOCOCancelsSameSideOnFill — the fill layer: when one
// mentor entry fills, every other working SAME-SIDE entry is cancelled at the
// wire (cancel_order) and moved to cancel_pending before it can fill. Runs for
// the reverse-ISB + ISB pair, the BOX + ISB pair and the SWING4H + ISB pair —
// one working entry per side across EVERY kind. MUTANT: drop the
// mentorCancelOtherSameSideWorking call in the fill handler → the other row
// stays working and no cancel_order fires → RED.
func TestMentorOneEntryPerSideOCOCancelsSameSideOnFill(t *testing.T) {
	cases := []struct{ name, fillCond, otherCond string }{
		{"reverse-ISB + ISB", "ISBREV", "ISB"},
		{"BOX + ISB", "BOX", "ISB"},
		{"SWING4H + ISB", "SWING4H", "ISB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
			resetMentorCounters(at)

			fillRow := rel30WorkingRow(t, at, ledger, c.fillCond+"-1-123", "long", "sig-fill", c.fillCond)
			other := rel30WorkingRow(t, at, ledger, c.otherCond+"-1-123", "long", "sig-other", c.otherCond)
			rel30SeedBook(t, at, "sig-fill", "sig-other")

			at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{
				SignalID: "sig-fill", State: "filled", Quantity: 5, FillPrice: 29600, Account: "Sim101",
			}, ledger)

			if !sawMentorFrame(t, frames, ntwire.FrameCancelOrder, 1*time.Second) {
				t.Fatal("the fill must cancel the other same-side entry on the wire (cancel_order)")
			}
			var got store.ArmedOrderDB
			if err := ledger.DB().First(&got, other.ID).Error; err != nil {
				t.Fatal(err)
			}
			if got.State != store.StateCancelPending {
				t.Fatalf("the other same-side row must be cancel_pending, got %q", got.State)
			}
			if got.ID == fillRow.ID {
				t.Fatal("the filled row must never be cancelled")
			}
			if c := at.mentorCountSnapshot()["cancel_requested"]; c != 1 {
				t.Fatalf("cancel_requested = %d, want 1", c)
			}
		})
	}
}

// TestMentorOneEntryPerSideOCOCancelsOppositeSideOnFill — the release #30 fold
// (CTO ruling, no flip): when a position opens, an OPPOSITE-side working entry
// is cancelled too (its fill would net the position and leave the open trade's
// stop/TP working against the wrong side). MUTANT: keep the OCO same-side-only
// → the opposite-side row stays working → RED.
func TestMentorOneEntryPerSideOCOCancelsOppositeSideOnFill(t *testing.T) {
	at, _, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	resetMentorCounters(at)

	rel30WorkingRow(t, at, ledger, "isb-1-123", "long", "sig-long", "ISB")
	other := rel30WorkingRow(t, at, ledger, "isb-short-123", "short", "sig-short", "ISB")
	rel30SeedBook(t, at, "sig-long", "sig-short")

	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{
		SignalID: "sig-long", State: "filled", Quantity: 5, FillPrice: 29600, Account: "Sim101",
	}, ledger)

	// The opposite-side (short) entry must be cancelled before it can fill.
	if !sawMentorFrame(t, frames, ntwire.FrameCancelOrder, 1*time.Second) {
		t.Fatal("a fill must cancel the opposite-side working entry on the wire (cancel_order)")
	}
	var got store.ArmedOrderDB
	if err := ledger.DB().First(&got, other.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateCancelPending {
		t.Fatalf("the opposite-side row must be cancel_pending, got %q", got.State)
	}
}

// TestMentorOneEntryPerSideNoFlipRefusesWhileOpenThenPlacesAfterFlat — the
// placement half of the fold: ANY entry (same or opposite side) is refused
// while a mentor position is open, and once FLAT the next tick may place the
// other side (the box "Uno reverse" trades after the stop, never before).
// MUTANT: drop the opposite-side refusal in mentorAddGate → the short places
// while the long is open → RED.
func TestMentorOneEntryPerSideNoFlipRefusesWhileOpenThenPlacesAfterFlat(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	mentorPinClockToRTH(t)
	resetMentorCounters(at)

	// A long position is open.
	at.mentorOpenSideSource = func() string { return "long" }
	t.Cleanup(func() { at.mentorOpenSideSource = nil })

	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })

	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}
	short := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideShort,
		Price: 29500, Stop: 29505, Target: 29480, Setup: "ISB", StopPts: 5, ArmID: "isb-short-1"}
	at.mentorPlaceIntent(short, choice, 1000, 1100)
	if len(placed) != 0 {
		t.Fatalf("an opposite-side entry while a position is open must be refused (no flip), placed=%d", len(placed))
	}
	if c := at.mentorCountSnapshot()["mentor_no_flip"]; c != 1 {
		t.Fatalf("mentor_no_flip = %d, want 1", c)
	}

	// Flat again → the next tick may place the other side.
	at.mentorOpenSideSource = func() string { return "" }
	at.mentorPlaceIntent(short, choice, 1000, 1100)
	if len(placed) != 1 {
		t.Fatalf("once flat, the next tick must place the other side, placed=%d", len(placed))
	}
}
