package store

import (
	"path/filepath"
	"testing"
)

// look-twice C — the arithmetic of a reverse: Σ receipts − entry_quantity, and
// the average price of exactly the contracts that over-closed.
func TestOverCloseFor(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "over.db"))
	if err != nil {
		t.Fatal(err)
	}
	row := openNT8Row(t, st, 3) // LONG 3
	mk := func(id string, qty, px float64, atMs int64) NT8ExitReceipt {
		r := receiptFor(row, qty, px, 2)
		r.ID, r.SignalID, r.Reason, r.ExitMs = id, "Close", "manual", atMs
		if e := st.Position().SavePendingExit(r); e != nil {
			t.Fatal(e)
		}
		return r
	}
	t0 := row.EntryTime + 300_000
	get := func(r NT8ExitReceipt) OverClose {
		t.Helper()
		oc, e := st.Position().OverCloseFor("t1", r, 10_000)
		if e != nil {
			t.Fatal(e)
		}
		return oc
	}

	applied := receiptFor(row, 3, 105, 2)
	applied.ExitMs = t0
	if out, e := st.Position().ApplyNT8Exit(applied); e != nil || !out.Closed {
		t.Fatalf("fixture: %+v %v", out, e)
	}
	// An old unapplied receipt (its reverse is long over) must not be counted.
	mk("stale", 1, 90, t0+1_000)
	a := mk("a", 1, 100, t0+60_000)
	if oc := get(a); oc.Row == nil || oc.Row.ID != row.ID || oc.Entry != 3 || oc.Applied != 3 || oc.Pending != 1 || oc.Excess != 1 || oc.AvgPx != 100 || oc.FirstID != "a" {
		t.Fatalf("one over-close: %+v", oc)
	}
	b := mk("b", 3, 101, t0+60_006)
	for _, r := range []NT8ExitReceipt{a, b} { // either receipt of the group reads the SAME reverse
		if oc := get(r); oc.Excess != 4 || oc.AvgPx != (1*100+3*101)/4.0 || oc.FirstID != "a" {
			t.Fatalf("a group of two (fractional average): %+v", oc)
		}
	}

	// A partial over-close: only the LAST excess contracts price the reverse.
	open := openNT8RowNamed(t, st, "pos-2", 3, t0+200_000)
	part := receiptFor(open, 2, 110, 2)
	part.ID, part.ExitMs = "part", open.EntryTime+1
	if out, e := st.Position().ApplyNT8Exit(part); e != nil || out.Closed || !out.Applied {
		t.Fatalf("fixture: %+v %v", out, e)
	}
	over := receiptFor(open, 3, 108, 2)
	over.ID, over.SignalID, over.Reason, over.ExitMs = "over", "Close", "manual", open.EntryTime+2
	if e := st.Position().SavePendingExit(over); e != nil {
		t.Fatal(e)
	}
	if oc := get(over); oc.Row.ID != open.ID || oc.Excess != 2 || oc.AvgPx != 108 {
		t.Fatalf("partial over-close: %+v", oc)
	}

	// No row of this trader: nothing can be counted.
	if oc, e := st.Position().OverCloseFor("other", over, 10_000); e != nil || oc.Row != nil {
		t.Fatalf("another trader has no row here: %+v %v", oc, e)
	}
	// A late receipt of a row closed WITHOUT receipts is not an over-close.
	recon := openNT8RowNamed(t, st, "pos-3", 1, t0+400_000)
	if ok, e := st.Position().ClosePosition(recon.ID, 100, "reconcile_flat", 0, 0, "sync"); e != nil || !ok {
		t.Fatalf("fixture: %v %v", ok, e)
	}
	late := receiptFor(recon, 1, 100, 2)
	late.ID, late.ExitMs = "late", recon.EntryTime+1
	if e := st.Position().SavePendingExit(late); e != nil {
		t.Fatal(e)
	}
	if oc := get(late); oc.Excess > 0 {
		t.Fatalf("a late receipt is not an over-close: %+v", oc)
	}
}

func openNT8RowNamed(t *testing.T, st *Store, name string, qty float64, entryMs int64) *TraderPosition {
	t.Helper()
	row := &TraderPosition{TraderID: "t1", ExchangeType: "ninjatrader", ExchangePositionID: name, Symbol: "MNQ", Side: "LONG",
		Quantity: qty, EntryQuantity: qty, EntryPrice: 100, EntryTime: entryMs, EntryOrderID: "entry-" + name, Leverage: 1,
		Status: "OPEN", Source: "armed_entry", Account: "Sim101", CreatedAt: entryMs, UpdatedAt: entryMs}
	if err := st.Position().CreateOpenPosition(row); err != nil {
		t.Fatal(err)
	}
	return row
}

// Only an OPEN, manual reconcile adoption of THIS trader is corrected, once.
func TestMarkOwnReverseBot(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "own.db"))
	if err != nil {
		t.Fatal(err)
	}
	adopted := func(name, source string) *TraderPosition {
		r := openNT8RowNamed(t, st, name, 1, 1_700_000_000_000)
		st.GormDB().Model(&TraderPosition{}).Where("id = ?", r.ID).Updates(map[string]any{
			"source": source, "plan_id": PlanUnresolvable, "entry_order_id": "", "origin": OriginManual})
		return r
	}
	r := adopted("a", "reconcile")
	for name, c := range map[string]struct {
		id     int64
		trader string
	}{"another trader": {r.ID, "t2"}, "not a reconcile adoption": {adopted("b", "armed_entry").ID, "t1"}} {
		if ok, e := st.Position().MarkOwnReverseBot(c.id, c.trader); e != nil || ok {
			t.Fatalf("%s: %v %v", name, ok, e)
		}
	}
	if ok, e := st.Position().MarkOwnReverseBot(r.ID, "t1"); e != nil || !ok {
		t.Fatalf("the adoption: %v %v", ok, e)
	}
	if ok, e := st.Position().MarkOwnReverseBot(r.ID, "t1"); e != nil || ok {
		t.Fatalf("twice: %v %v", ok, e)
	}
	c := adopted("c", "reconcile")
	st.GormDB().Model(&TraderPosition{}).Where("id = ?", c.ID).Update("status", "CLOSED")
	if ok, e := st.Position().MarkOwnReverseBot(c.ID, "t1"); e != nil || ok {
		t.Fatalf("a CLOSED row is history: %v %v", ok, e)
	}
}
