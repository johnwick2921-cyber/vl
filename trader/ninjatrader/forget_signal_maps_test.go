package ninjatrader

import (
	"testing"
)

// I9 / U3: ForgetSignalMaps drops the split record (the U3 unregister gap — the
// record was written at placement but never forgotten). MUTANT: no delete → the
// stale record survives → RED.
func TestForgetSignalMapsClearsSplitRecord(t *testing.T) {
	tr := &TCPTrader{
		splitBySignal: map[string]SentSplit{"sig-9": {Leg1Qty: 3, Leg1TP: 12}},
	}
	if _, ok := tr.SplitSentFor("sig-9"); !ok {
		t.Fatal("fixture: the split record must be present")
	}
	tr.ForgetSignalMaps("sig-9")
	if _, ok := tr.SplitSentFor("sig-9"); ok {
		t.Fatal("ForgetSignalMaps must drop the split record")
	}
	// A no-op for an unknown signal (fail-closed, no panic).
	tr.ForgetSignalMaps("never-seen")
}

// P3 (rel9): positionMap must not dereference a nil server (the GetPositions
// fillDerived path guards t.server==nil, but positionMap's bar-cache fallback
// did not). With no server the mark falls back to entry. MUTANT: unguard the
// BarCache call → nil deref → RED (panic).
func TestPositionMapNilServerFallsBackToEntry(t *testing.T) {
	tr := &TCPTrader{} // nil server
	m := tr.positionMap("MNQ", "short", 2, 100, nil)
	if got := m["mark_price"].(float64); got != 100 {
		t.Fatalf("nil-server mark = %.2f, want entry 100", got)
	}
	if got := m["positionAmt"].(float64); got != -2 {
		t.Fatalf("short positionAmt = %.2f, want -2 (signed)", got)
	}
}
