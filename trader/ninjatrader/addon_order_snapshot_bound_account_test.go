package ninjatrader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// TestOrderSnapshotBookResolvesBoundAccountNotAnother locks the Go half of
// REL19-ADDON: the cache is per-account and OrderSnapshotBook() — the seam every
// broker-book reader (F1 stop-move verify, cancel-confirm, slot guard, F4
// protection, cutover leg 4) reads through — resolves the trader's BOUND
// account, never another account's. Two accounts stream periodic snapshots on
// one link; each trader must see only its own orders. The C# fix (stream every
// account) is what feeds both books; this pin proves the Go side was already
// account-correct and needed no change.
func TestOrderSnapshotBookResolvesBoundAccountNotAnother(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	now := time.Now()

	sim101, err := ntwire.ParseOrderSnapshot([]byte(
		`{"account":"Sim101","build_id":"2026-10-13-a","emitted_at_ms":1,"orders":[` +
			`{"order_id":"o1","name":"sig1-sl","action":"sell","type":"stop","stop_price":100.0,"quantity":1,"state":"Working","symbol":"MNQ"}]}`))
	if err != nil {
		t.Fatalf("Sim101 fixture: %v", err)
	}
	simAcct1, err := ntwire.ParseOrderSnapshot([]byte(
		`{"account":"SimAccount1","build_id":"2026-10-13-a","emitted_at_ms":2,"orders":[` +
			`{"order_id":"o2","name":"sig2-sl","action":"sell","type":"stop","stop_price":200.0,"quantity":1,"state":"Working","symbol":"MNQ"}]}`))
	if err != nil {
		t.Fatalf("SimAccount1 fixture: %v", err)
	}
	s.OrderSnapshots().PutAt(sim101, now)
	s.OrderSnapshots().PutAt(simAcct1, now)

	hoang := NewTCPTrader(s, "MNQ", "Sim101")
	book, _, ok := hoang.OrderSnapshotBook()
	if !ok || len(book.Orders) != 1 || book.Orders[0].Name != "sig1-sl" {
		t.Fatalf("Sim101 trader must resolve ITS OWN book, got orders=%+v ok=%v", book.Orders, ok)
	}

	acct1 := NewTCPTrader(s, "MNQ", "SimAccount1")
	book2, _, ok2 := acct1.OrderSnapshotBook()
	if !ok2 || len(book2.Orders) != 1 || book2.Orders[0].Name != "sig2-sl" {
		t.Fatalf("SimAccount1 trader must resolve ITS OWN book, got orders=%+v ok=%v", book2.Orders, ok2)
	}
}
