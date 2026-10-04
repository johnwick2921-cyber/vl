package store

import (
	"path/filepath"
	"testing"
)

// PARTIAL-CLOSE (2026-10-03) — the store halves at the production methods.

func newPartialCloseTestStore(t *testing.T) *partialCloseStore {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.PartialClose()
}

func TestRecordReduceAndApplyFillUpsertLatestWins(t *testing.T) {
	pc := newPartialCloseTestStore(t)
	r := &PositionReduction{TraderID: "t1", Symbol: "MNQ", Side: "long", ClientID: "rx-1", Quantity: 3, Remaining: -1, BracketQty: -1, Who: "mentor"}
	if err := pc.RecordReduce(r); err != nil {
		t.Fatalf("record: %v", err)
	}
	// The fill is applied by client_id — a part-fill then full-fill pair
	// upserts, never duplicates.
	if err := pc.ApplyReduceFill("rx-1", 100.25, 2, 2); err != nil {
		t.Fatalf("apply fill: %v", err)
	}
	if err := pc.ApplyReduceFill("rx-1", 100.25, 2, 2); err != nil {
		t.Fatalf("apply fill again (idempotent): %v", err)
	}
	rows, err := pc.ListReductions("t1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %d", err, len(rows))
	}
	if rows[0].FillPrice != 100.25 || rows[0].Remaining != 2 || rows[0].Quantity != 3 || rows[0].BracketQty != 2 {
		t.Fatalf("want fill=100.25 remaining=2 qty=3 bracket_qty=2, got %+v", rows[0])
	}
	if rows[0].Who != "mentor" {
		t.Fatalf("the ledger row must carry who asked, got %q", rows[0].Who)
	}
}

func TestRecordReduceRejectsGarbage(t *testing.T) {
	pc := newPartialCloseTestStore(t)
	if err := pc.RecordReduce(&PositionReduction{TraderID: "t1", Quantity: 1}); err == nil {
		t.Fatalf("a reduce without a client id must be refused")
	}
	if err := pc.RecordReduce(&PositionReduction{TraderID: "t1", ClientID: "rx-1"}); err == nil {
		t.Fatalf("a reduce without a positive quantity must be refused")
	}
}
