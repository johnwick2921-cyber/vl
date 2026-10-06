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
