package ninjatrader

import (
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// F3 (REL17) — the leg-1 TP receipt latch must fire on the ORDERED-OWNED close
// path too, not only on the legacy recordClose. Before this change the ordered
// frames (which carry Leg) skipped OnPositionLegClosed, so the mentor marked
// leg 1 scaled only on the I7 snapshot fallback (#641 latched 24 s after its
// TP). These pins sit at the production call site: recordCloseOrdered must fire
// the hook on an APPLIED leg-1 tp frame, and never on a PENDING or refused
// receipt. The downstream latch (mentorMarkLeg1Scaled → mentorLatchLeg1Scaled →
// "mentor leg 1 scaled on the broker TP receipt" + leg1_at_target) is pinned in
// trader/mentor_b1_b2_build_all_test.go:58.

func newLeg1HookFixture(t *testing.T) (*TCPTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "f3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	tr := NewTCPTrader(ntwire.NewTCPServer(nil), "MNQ", "Sim101")
	return tr, st
}

// (a) an APPLIED leg-1 TP receipt on the ordered path fires the leg-closed hook
// with the full payload (Leg preserved), so the mentor latches on the broker's
// TP receipt. MUTANT: remove the OnPositionLegClosed call in recordCloseOrdered
// → this test goes RED (the hook never fires).
func TestOrderedLeg1TPReceiptFiresLegClosedHook(t *testing.T) {
	tr, st := newLeg1HookFixture(t)
	now := time.Now().UTC().UnixMilli()
	row := &store.TraderPosition{
		TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: "p1",
		Symbol: "MNQ", Side: "LONG", Quantity: 2, EntryQuantity: 2,
		EntryPrice: 31023.50, EntryTime: now - 60_000, EntryOrderID: "sig-leg1",
		Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101",
		CreatedAt: now - 60_000, UpdatedAt: now - 60_000,
	}
	if err := st.Position().CreateOpenPosition(row); err != nil {
		t.Fatal(err)
	}

	saved := OnPositionLegClosed
	t.Cleanup(func() { OnPositionLegClosed = saved })
	var gotTrader string
	var gotP ntwire.PositionClosePayload
	called := 0
	OnPositionLegClosed = func(traderID string, p ntwire.PositionClosePayload) {
		gotTrader, gotP = traderID, p
		called++
	}

	tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, ntwire.PositionClosePayload{
		SignalID: "sig-leg1", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 1, ExitPrice: 31037.25, ExitReason: "tp", Leg: 1,
		ExitTime: time.Now().UTC().Format(time.RFC3339),
	})

	if called != 1 {
		t.Fatalf("applied leg-1 tp receipt must fire OnPositionLegClosed once, fired %d", called)
	}
	if gotTrader != "t1" || gotP.SignalID != "sig-leg1" || gotP.Leg != 1 || gotP.ExitReason != "tp" {
		t.Fatalf("hook got (%q, %+v), want (t1, sig-leg1 leg=1 reason=tp)", gotTrader, gotP)
	}
}

// (b) a PENDING receipt (no open row) returns before the hook — never a latch on
// a receipt that has not applied.
func TestOrderedPendingReceiptDoesNotFireLegClosedHook(t *testing.T) {
	tr, st := newLeg1HookFixture(t)
	saved := OnPositionLegClosed
	t.Cleanup(func() { OnPositionLegClosed = saved })
	called := 0
	OnPositionLegClosed = func(string, ntwire.PositionClosePayload) { called++ }

	tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, ntwire.PositionClosePayload{
		SignalID: "sig-ghost", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 1, ExitPrice: 31037.25, ExitReason: "tp", Leg: 1,
		ExitTime: time.Now().UTC().Format(time.RFC3339),
	})

	if called != 0 {
		t.Fatalf("a PENDING receipt must not fire OnPositionLegClosed, fired %d", called)
	}
}

// (c) a REFUSED receipt (ambiguous ownership: two OPEN rows) returns before the
// hook — never a latch on evidence the store refused.
func TestOrderedRefusedReceiptDoesNotFireLegClosedHook(t *testing.T) {
	tr, st := newLeg1HookFixture(t)
	now := time.Now().UTC().UnixMilli()
	for i, id := range []string{"p1", "p2"} {
		row := &store.TraderPosition{
			TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: id,
			Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
			EntryPrice: 31023.50, EntryTime: now - 60_000, EntryOrderID: "sig-a",
			Leverage: 1, Status: "OPEN", Source: "armed_entry", Account: "Sim101",
			CreatedAt: now - 60_000, UpdatedAt: now - 60_000,
		}
		row.ID = int64(1000 + i) // distinct rows, same account+symbol+side
		if err := st.Position().CreateOpenPosition(row); err != nil {
			t.Fatal(err)
		}
	}
	saved := OnPositionLegClosed
	t.Cleanup(func() { OnPositionLegClosed = saved })
	called := 0
	OnPositionLegClosed = func(string, ntwire.PositionClosePayload) { called++ }

	tr.recordCloseOrdered("t1", "ex", "ninjatrader", st, ntwire.PositionClosePayload{
		SignalID: "sig-a", Symbol: "MNQ", PositionSide: "long", Account: "Sim101",
		Quantity: 1, ExitPrice: 31037.25, ExitReason: "tp", Leg: 1,
		ExitTime: time.Now().UTC().Format(time.RFC3339),
	})

	if called != 0 {
		t.Fatalf("a REFUSED receipt must not fire OnPositionLegClosed, fired %d", called)
	}
}
