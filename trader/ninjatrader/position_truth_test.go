package ninjatrader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// T27 (ui-studio-truth): positionMap must carry the CME futures truth — the
// per-contract point value and the notional position value (qty × mark × $/pt).
// The dashboard reads these instead of price×qty (wrong for $2/pt MNQ) or a
// UI-side constant. MUTANT: drop point_value from the map → RED.
func TestPositionMapCarriesFuturesPointValueAndValue(t *testing.T) {
	tr := &TCPTrader{} // nil server — point value does not need a bar cache

	m := tr.positionMap("MNQ", "short", 2, 30000, nil)
	if got, ok := m["point_value"].(float64); !ok || got != 2.0 {
		t.Fatalf("MNQ point_value = %v, want 2 ($2/pt)", m["point_value"])
	}
	// notional: 2 contracts × $30,000 × $2/pt = $120,000 (side is unsigned).
	if got, ok := m["position_value"].(float64); !ok || got != 120000.0 {
		t.Fatalf("MNQ position_value = %v, want 120000", m["position_value"])
	}

	m2 := tr.positionMap("ES", "long", 1, 7000, nil)
	if got, ok := m2["point_value"].(float64); !ok || got != 50.0 {
		t.Fatalf("ES point_value = %v, want 50 ($50/pt)", m2["point_value"])
	}
	if got, ok := m2["position_value"].(float64); !ok || got != 350000.0 {
		t.Fatalf("ES position_value = %v, want 350000", m2["position_value"])
	}
}

// T32 (ui-studio-truth): the open position's working stop/target is read from
// the broker's own order book (protective legs carry -sl / -tp suffixes).
// MUTANT: drop the bracket from positionMap → RED.
func TestPositionMapCarriesWorkingBracket(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	now := time.Now().UTC()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{
		Account: "Sim101",
		Orders: []ntwire.NT8Order{
			{OrderID: "o1", Symbol: "MNQ", Name: "sig-7-sl", Action: "sell", Type: "stop_market", StopPrice: 30500, Quantity: 1, State: "Working"},
			{OrderID: "o2", Symbol: "MNQ", Name: "sig-7-tp", Action: "sell", Type: "limit", LimitPrice: 30750, Quantity: 1, State: "Working"},
		},
	}, now)

	m := tr.positionMap("MNQ", "long", 1, 30600, nil)
	b, ok := m["bracket"]
	if !ok {
		t.Fatal("a working protective bracket must be present")
	}
	pb, ok := b.(*positionBracket)
	if !ok {
		t.Fatalf("bracket type %T, want *positionBracket", b)
	}
	if pb.Stop != 30500 || pb.Target != 30750 {
		t.Fatalf("bracket = %+v, want stop 30500 target 30750", pb)
	}
	if pb.Source != "nt8_order_snapshot" {
		t.Fatalf("bracket source = %q, want nt8_order_snapshot", pb.Source)
	}
}

// T32: the bracket is ACCOUNT-scoped — one trader's bound account never reads
// another account's protective legs (the cache is keyed by account).
func TestPositionMapBracketIsAccountScoped(t *testing.T) {
	s := ntwire.NewTCPServer(nil)
	now := time.Now().UTC()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{
		Account: "SimAccount1",
		Orders: []ntwire.NT8Order{
			{OrderID: "o1", Symbol: "MNQ", Name: "sig-8-sl", Action: "sell", Type: "stop_market", StopPrice: 29999, Quantity: 1, State: "Working"},
		},
	}, now)

	tr := NewTCPTrader(s, "MNQ", "Sim101")
	if _, ok := tr.positionMap("MNQ", "long", 1, 30600, nil)["bracket"]; ok {
		t.Fatal("Sim101 must not read SimAccount1's protective legs")
	}
}

// T32: absent when unknown — never a zero bracket. A nil server and an empty
// book both leave the key OUT of the map.
func TestPositionMapBracketAbsentWhenUnknown(t *testing.T) {
	tr := &TCPTrader{}
	if _, ok := tr.positionMap("MNQ", "long", 1, 30600, nil)["bracket"]; ok {
		t.Fatal("nil server must not fabricate a bracket")
	}

	s := ntwire.NewTCPServer(nil)
	tr2 := NewTCPTrader(s, "MNQ", "Sim101")
	// No working protective orders (only a resting entry) → no bracket.
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{
		Account: "Sim101",
		Orders: []ntwire.NT8Order{
			{OrderID: "o1", Symbol: "MNQ", Name: "sig-entry", Action: "buy", Type: "limit", LimitPrice: 30500, Quantity: 1, State: "Working"},
		},
	}, time.Now().UTC())
	if _, ok := tr2.positionMap("MNQ", "long", 1, 30600, nil)["bracket"]; ok {
		t.Fatal("no -sl/-tp working orders must leave the bracket absent")
	}
}
