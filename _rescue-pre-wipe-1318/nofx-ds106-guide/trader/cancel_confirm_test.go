package trader

import (
	"strings"
	"testing"
	"time"

	nt "vl/provider/ninjatrader"
)

// THE FIXTURE IS THE INCIDENT. These are the nine orders NT8 actually held in
// nt8_order_snapshots id 1664 (2026-09-04 15:52:00 UTC, reason=state_change,
// order_count 9, working_count 9) — one arm slot, 2026-09-04:NY v3 S2 leg 0
// SHORT, while all nine ledger rows read state 'cancelled'. Names, states and
// the null stop price are copied from the stored frame, not invented.
func snapshot1664() []nt.NT8Order {
	ids := []struct{ order, name, state string }{
		{"4c4cea3b76", "7dd07a19-701e-4afa-9b15-655f65810e9e", "Accepted"},
		{"1143428170", "6a5da3fa-2992-426f-b56e-9d1eb805c350", "Accepted"},
		{"d260f9bd68", "183d16ad-75df-4fcf-bd9c-685900a8aa25", "Accepted"},
		{"55cf8b4ad4", "993322b9-6c27-42af-8afb-2a3620b797eb", "Accepted"},
		{"83a76c0dc5", "b42631c8-4737-4e59-aa42-e51b5a7237db", "Accepted"},
		{"63fe23a6e0", "fa195d44-705e-46c9-be53-5d4a2198e3c8", "Accepted"},
		{"a591d6a126", "60adff7a-afe7-4b11-be91-1c6e8a413c8f", "Accepted"},
		{"c58c25527c", "3bea0045-32b4-469d-ae5c-ca24ba366509", "Accepted"},
		{"b8c2169dd0", "04b3e307-eb7f-4bc8-88fe-7088dff9f6a5", "Initialized"},
	}
	out := make([]nt.NT8Order, 0, len(ids))
	for _, o := range ids {
		out = append(out, nt.NT8Order{
			OrderID: o.order, Name: o.name, State: o.state,
			Type: "stop", LimitPrice: 29590.5, Quantity: 1,
		})
	}
	return out
}

func slot1664Signals() []string {
	s := make([]string, 0, 9)
	for _, o := range snapshot1664() {
		s = append(s, o.Name)
	}
	return s
}

const bookBound = 60 * time.Second

// E1 (adjudication half) — NINE LIVE ORDERS ON ONE SLOT, LEDGER SAYS CANCELLED.
// The book is the authority: the slot is LIVE and a placement must be refused.
func TestSlotIsLiveWhenTheBookStillHoldsTheOrders(t *testing.T) {
	v := adjudicateSlot(snapshot1664(), true, 5*time.Second, bookBound, 1664, slot1664Signals())
	if v.Allowed() {
		t.Fatalf("nine working orders for this slot and the adjudicator allowed a placement: %+v", v)
	}
	if v.Action != slotLive {
		t.Fatalf("want %q, got %q", slotLive, v.Action)
	}
	if v.SnapshotID != 1664 {
		t.Fatalf("the settling snapshot id must be recorded, got %d", v.SnapshotID)
	}
	if v.State != "Accepted" {
		t.Fatalf("the broker's own word for the order must be carried, got %q", v.State)
	}
	t.Logf("%s", v.Refusal())
}

// E4 — A STALE OR ABSENT BOOK REFUSES. An unverifiable slot is not an empty slot.
func TestUnverifiableBookRefusesRatherThanAllows(t *testing.T) {
	t.Run("no book ever received", func(t *testing.T) {
		v := adjudicateSlot(nil, false, 0, bookBound, 0, slot1664Signals())
		if v.Allowed() || v.Action != slotUnverifiable {
			t.Fatalf("absent book must be unverifiable, got %+v", v)
		}
	})
	t.Run("book older than the bound", func(t *testing.T) {
		v := adjudicateSlot(nil, true, 10*time.Minute, bookBound, 99, slot1664Signals())
		if v.Allowed() || v.Action != slotUnverifiable {
			t.Fatalf("stale book must be unverifiable, got %+v", v)
		}
	})
	t.Run("an EMPTY fresh book is FREE, not unverifiable", func(t *testing.T) {
		// An account with no orders legitimately emits orders: []. Confusing
		// that with "cannot see" would refuse every placement forever.
		v := adjudicateSlot([]nt.NT8Order{}, true, time.Second, bookBound, 100, slot1664Signals())
		if !v.Allowed() {
			t.Fatalf("an empty FRESH book means the slot is free, got %+v", v)
		}
	})
}

// E5 — CANCEL-IN-FLIGHT IS NOT GONE. CancelSubmitted (130 occurrences across 360
// live frames) and CancelPending (22) are non-terminal, so the slot stays locked
// while a cancel is still travelling. This is the state that would otherwise let
// a replacement race the cancel it is waiting on.
func TestCancelInFlightStatesKeepTheSlotLocked(t *testing.T) {
	for _, state := range []string{"CancelSubmitted", "CancelPending", "Working", "Submitted", "Initialized", "Accepted"} {
		t.Run(state, func(t *testing.T) {
			book := []nt.NT8Order{{OrderID: "o1", Name: "sig-a", State: state, Type: "stop"}}
			v := adjudicateSlot(book, true, time.Second, bookBound, 7, []string{"sig-a"})
			if v.Allowed() {
				t.Fatalf("state %q must keep the slot locked, adjudicator allowed placement", state)
			}
		})
	}
	// And a genuinely terminal state DOES free it.
	for _, state := range []string{"Filled", "Cancelled", "Rejected"} {
		t.Run("terminal/"+state, func(t *testing.T) {
			book := []nt.NT8Order{{OrderID: "o1", Name: "sig-a", State: state, Type: "stop"}}
			v := adjudicateSlot(book, true, time.Second, bookBound, 7, []string{"sig-a"})
			if !v.Allowed() {
				t.Fatalf("terminal state %q must free the slot, got %+v", state, v)
			}
		})
	}
}

// The bracket children carry the signal as a prefix, so they hold the slot too.
func TestBracketChildrenBelongToTheSlot(t *testing.T) {
	book := []nt.NT8Order{{OrderID: "o-sl", Name: "sig-a-sl", State: "Accepted"}}
	if v := adjudicateSlot(book, true, time.Second, bookBound, 8, []string{"sig-a"}); v.Allowed() {
		t.Fatalf("a live protective child still belongs to the slot: %+v", v)
	}
	// A DIFFERENT signal must not be matched by accident.
	other := []nt.NT8Order{{OrderID: "o2", Name: "sig-abcdef", State: "Accepted"}}
	if v := adjudicateSlot(other, true, time.Second, bookBound, 8, []string{"sig-a"}); !v.Allowed() {
		t.Fatalf("sig-abcdef is not sig-a and must not lock the slot: %+v", v)
	}
}

// E2 (adjudication half) — a cancel settles ONLY on a fresh book that no longer
// lists it; a stale or absent book settles nothing.
func TestCancelSettlesOnlyOnAFreshBookWithoutIt(t *testing.T) {
	sig := "7dd07a19-701e-4afa-9b15-655f65810e9e"
	if ok, why := cancelSettled(snapshot1664(), true, time.Second, bookBound, sig); ok {
		t.Fatalf("the order is still in the book — a cancel must not settle: %s", why)
	}
	if ok, _ := cancelSettled([]nt.NT8Order{}, true, time.Second, bookBound, sig); !ok {
		t.Fatal("absent from a fresh book means settled")
	}
	if ok, why := cancelSettled([]nt.NT8Order{}, true, 10*time.Minute, bookBound, sig); ok {
		t.Fatalf("a STALE book must settle nothing — 'cancelled' is the destructive branch: %s", why)
	}
	if ok, why := cancelSettled(nil, false, 0, bookBound, sig); ok {
		t.Fatalf("an absent book must settle nothing: %s", why)
	}
	// A cancel still in flight does NOT settle.
	inflight := []nt.NT8Order{{OrderID: "o1", Name: sig, State: "CancelSubmitted"}}
	if ok, why := cancelSettled(inflight, true, time.Second, bookBound, sig); ok {
		t.Fatalf("CancelSubmitted is not gone: %s", why)
	}
}

// D6 — THE BOOT LINE, EVERY FIELD READ. The reconciliation half must say n/a
// before a book exists rather than print a zero it did not measure (A24: an
// uncomputed value is not 0).
func TestCancelBootLineReadsItsFieldsAndSaysNaBeforeAnyBook(t *testing.T) {
	line := CancelBootLine(nil, ReconcileCounts{}, 0)
	for _, want := range []string{
		"cancels:", "confirm=broker-snapshot", "pending=0", "unconfirmed=0",
		"slot-guard=on(refuse-on-live|stale)", "timeout=", "stale-bound=",
		"rerequest-cap=", "reconciled=n/a (no broker book yet)",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line missing %q:\n%s", want, line)
		}
	}
	// Once a pass has run, the real counts appear — and n/a must be gone.
	ran := CancelBootLine(nil, ReconcileCounts{Ran: true, ConfirmedGone: 3, LiveAtBroker: 9, Unconfirmed: 1, SnapshotID: 1664}, 0)
	if !strings.Contains(ran, "reconciled(confirmed=3 live=9 unconfirmed=1 snapshot=1664)") {
		t.Fatalf("measured counts must replace n/a:\n%s", ran)
	}
	if strings.Contains(ran, "n/a") {
		t.Fatalf("a measured line must not still say n/a:\n%s", ran)
	}
	t.Logf("%s", ran)
}
