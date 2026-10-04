package trader

import (
	"errors"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── C1 — AN ACK TIMEOUT IS NOT A CANCELLATION ───────────────────────────────
//
// cancelArmedOrdersSyncWith wrote SetState(id, "cancelled", "… (ack timeout —
// flatten proceeds)"). A TIMEOUT promoted the row straight to the word that
// frees the arm slot (UpsertArm) and that cutover leg 4 counts, skipping the
// whole RequestCancel → cancel_pending → ConfirmCancel lifecycle the 2026-09-06
// wave exists to enforce. A row promoted on a timeout can be replaced while its
// order still rests at the broker.
//
// TestUnconfirmableCancelIsNeverPromoted describes THIS defect in its header and
// then exercises confirmPendingCancels — a different function — so it passed
// throughout. This pins the site the header names.
func TestAckTimeoutHoldsCancelPendingNotCancelled(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	// A stream that never delivers: the cancel is sent and no ack comes back.
	silent := make(chan ntwire.OrderUpdatePayload)
	at, rt := flatFixture(t, now, false, store.StateWorking, "sig-timeout", silent)

	n, unacked := at.cancelArmedOrdersSyncWith("session close — EOD flat",
		10*time.Millisecond, at.armedSyncSeam.Cancel, at.armedSyncSeam.Stream, nil)
	if len(rt.events) == 0 {
		t.Fatal("the cancel must still be SENT — this fix is about what we RECORD, not about skipping the wire")
	}
	if n != 0 || unacked != 1 {
		t.Fatalf("an unacked cancel is not a cancellation: want n=0 unacked=1, got n=%d unacked=%d", n, unacked)
	}
	rows, err := at.store.ArmedOrders().ListForPlan("2026-08-18:NY:trader-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("read back: err=%v rows=%d", err, len(rows))
	}
	if rows[0].State != store.StateCancelPending {
		t.Fatalf("an ack timeout must HOLD the row %q so the settlement pass owns it; got %q — that is the 'ack timeout — flatten proceeds' defect",
			store.StateCancelPending, rows[0].State)
	}
	if rows[0].CancelSettledSnapshotID != 0 {
		t.Fatalf("nothing confirmed it, so no settling snapshot may be recorded; got %d", rows[0].CancelSettledSnapshotID)
	}
}

// ── C4 — A FILL IS NOT A CANCEL ─────────────────────────────────────────────
//
// `acked` meant only "the row left the non-terminal set", and EVERY terminal
// state satisfies that — including FILLED. A limit that filled two seconds
// before the close was counted by n++ and logged as an order we cancelled. The
// two facts are opposite, and the second understates the book at exactly the
// moment the flatten is about to claim it is empty.
func TestFillDuringDrainIsNotCountedAsACancel(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	acks := make(chan ntwire.OrderUpdatePayload, 1)
	at, _ := flatFixture(t, now, false, store.StateWorking, "sig-fill", acks)

	// The order FILLS during the drain window instead of being cancelled.
	acks <- ntwire.OrderUpdatePayload{SignalID: "sig-fill", State: "filled", FillPrice: 29950, Quantity: 1}

	n, unacked := at.cancelArmedOrdersSyncWith("session close — EOD flat",
		300*time.Millisecond, at.armedSyncSeam.Cancel, at.armedSyncSeam.Stream, nil)
	rows, err := at.store.ArmedOrders().ListForPlan("2026-08-18:NY:trader-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("read back: err=%v rows=%d", err, len(rows))
	}
	if rows[0].State != store.StateFilled {
		t.Skipf("fixture did not reach state filled (got %q) — this pin needs the fill to land", rows[0].State)
	}
	if n != 0 {
		t.Fatalf("a FILLED row must not be counted as a cancelled order: got n=%d. It became a POSITION; counting it as a cancel understates the book at the close", n)
	}
	_ = unacked
}

// ── C3 — NO BROKER LINK MEANS NO BROKER OUTCOME ─────────────────────────────
//
// cancelArmedOrders is reached exactly when at.armedTrader() is nil — the NT8
// bridge absent, which is precisely when resting orders are most likely to
// outlive us. It walked every non-terminal row and wrote SetState(id,
// "cancelled") with no wire, no book and no state test, and its count then fed
// the operator-facing flat claim. A flat claim with zero broker contact behind it.
func TestNoLinkFallbackHoldsPendingAndReportsUnsettled(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	at, _ := flatFixture(t, now, false, store.StateWorking, "sig-nolink", nil)
	at.armedSyncSeam = nil // force the no-link fallback
	at.trader = nil

	retired, unsettled := at.cancelArmedOrders("session close — EOD flat", nil)

	rows, err := at.store.ArmedOrders().ListForPlan("2026-08-18:NY:trader-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("read back: err=%v rows=%d", err, len(rows))
	}
	if rows[0].State != store.StateCancelPending {
		t.Fatalf("with NO broker contact a placed row may not be called %q; want %q, got %q",
			store.StateCancelled, store.StateCancelPending, rows[0].State)
	}
	if retired != 0 || unsettled != 1 {
		t.Fatalf("the counts must separate retired from unsettled so the flat line cannot print 'cancelled': want 0/1, got %d/%d", retired, unsettled)
	}
}

// A row that was NEVER PLACED has no broker outcome to be wrong about, so it may
// be retired truthfully without a book — and holding it pending would strand an
// arm slot on an order that never existed.
func TestNoLinkFallbackStillRetiresNeverPlacedRows(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, chicagoLoc())
	at, _ := flatFixture(t, now, false, store.StateArmed, "", nil)
	at.armedSyncSeam = nil
	at.trader = nil

	retired, unsettled := at.cancelArmedOrders("session close — EOD flat", nil)
	if retired != 1 || unsettled != 0 {
		t.Fatalf("a never-placed row is retired, not held: want 1/0, got %d/%d", retired, unsettled)
	}
}

// ── C2 — "FLAT" IS A CLAIM ABOUT THE BROKER, NOT ABOUT OUR TABLE ────────────
//
// Every flatten path asked ONE question — GetOpenPositions on our own table —
// and read len()==0 as flat. position_desync.go:18 documents that table lagging
// the broker by up to ~80s after a real exit, and :78 documents the reverse.
// The expensive direction is OUR TABLE EMPTY WHILE THE BROKER STILL HOLDS ONE:
// the flatten declares "book flat", returns, and the position rides the close
// with no stop attached to it.
//
// The cutover gate already refuses one number here — leg 1 sqlite, legs 2/3
// broker, quoted separately "so a per-account routing fault cannot hide behind
// one number". The flatten had only the sqlite leg.
func TestFlatIsNotClaimedFromTheLocalTableAlone(t *testing.T) {
	one := []map[string]interface{}{{"symbol": "MNQ", "side": "LONG", "quantity": 1}}

	t.Run("store empty, broker holds one — NOT flat, and the disagreement is named", func(t *testing.T) {
		v := flatTruthFrom(0, func() ([]map[string]interface{}, error) { return one, nil })
		if v.ProvenFlat() {
			t.Fatal("our table being empty is ONE reader; the broker reports a position and this must not read as flat")
		}
		if !v.Disagrees() {
			t.Fatal("the expensive direction must be reported as a disagreement, not as an unverified read")
		}
	})

	t.Run("broker unreadable — UNVERIFIED, never flat", func(t *testing.T) {
		v := flatTruthFrom(0, func() ([]map[string]interface{}, error) {
			return nil, errBrokerDown
		})
		if v.ProvenFlat() {
			t.Fatal("a failed broker read is UNKNOWN and must never be folded into flat (A24)")
		}
		if v.Disagrees() {
			t.Fatal("an unreadable broker is not a disagreement — we do not know what it holds")
		}
	})

	t.Run("no broker link at all — UNVERIFIED, never flat", func(t *testing.T) {
		if flatTruthFrom(0, nil).ProvenFlat() {
			t.Fatal("with nothing to ask, flatness is unproven — one reader agreeing with itself is not corroboration")
		}
	})

	t.Run("both readers say zero — proven flat", func(t *testing.T) {
		v := flatTruthFrom(0, func() ([]map[string]interface{}, error) { return nil, nil })
		if !v.ProvenFlat() {
			t.Fatal("two independent readers both at zero is the only thing that proves flat")
		}
	})

	t.Run("the verdict always names both sources", func(t *testing.T) {
		got := flatTruthFrom(0, func() ([]map[string]interface{}, error) { return one, nil }).Why()
		if got != "store 0 · broker 1" {
			t.Fatalf("a flat claim must be auditable without re-deriving it; got %q", got)
		}
	})
}

var errBrokerDown = errors.New("broker link down")
