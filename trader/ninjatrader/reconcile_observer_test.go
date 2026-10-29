package ninjatrader

import (
	"path/filepath"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// m447: the data-truth broker_agreement check samples on the reconcile loop, so
// the observer must fire after EVERY pass - including the pass that returns
// early because NT8 has not reported positions (that silence is itself a
// finding for the check) - and HeldBook must be the map the reconcile judges by.
func TestReconcileObserverFiresOnEveryPassIncludingTheEarlyReturn(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "obs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	n := 0
	tr.SetReconcileObserver(func() { n++ })

	tr.ReconcilePassForTest("t-obs", "nt", "ninjatrader", st) // no positions frame yet: early return
	if n != 1 {
		t.Fatalf("early-return pass must notify the observer, got %d", n)
	}
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	tr.ReconcilePassForTest("t-obs", "nt", "ninjatrader", st)
	if n != 2 {
		t.Fatalf("a full pass must notify the observer, got %d", n)
	}
	tr.SetReconcileObserver(nil)
	tr.ReconcilePassForTest("t-obs", "nt", "ninjatrader", st)
	if n != 2 {
		t.Fatalf("a removed observer must not fire, got %d", n)
	}
}

func TestHeldBookKeysAreTheReconcileKeys(t *testing.T) {
	avg, qty := HeldBook([]ntwire.OpenPosition{
		{Symbol: "MNQ", Side: "short", Quantity: 2, AvgPrice: 29310.5},
		{Symbol: "MNQ", Side: "LONG", Quantity: 0, AvgPrice: 1}, // flat: absent
	})
	if len(avg) != 1 || avg["MNQ|SHORT"] != 29310.5 || qty["MNQ|SHORT"] != 2 {
		t.Fatalf("HeldBook: %v %v", avg, qty)
	}
}

// m447 review defect 3. The observer is data-truth SAMPLING that rides the
// protective reconcile goroutine (safe.GoNet freezes the trader on a panic and
// the loop exits). A panic in a check must not escape the pass.
func TestReconcileObserverPanicDoesNotEscapeTheReconcilePass(t *testing.T) {
	tr, st := newLeg1HookFixture(t)
	ran := 0
	tr.SetReconcileObserver(func() { ran++; panic("observer bug") })
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a panic in the data-truth observer escaped reconcilePositions (in production: safe.GoNet freezes the trader and the reconcile loop exits): %v", r)
		}
	}()
	tr.ReconcilePassForTest("t1", "ex", "ninjatrader", st)
	tr.ReconcilePassForTest("t1", "ex", "ninjatrader", st) // the next pass still runs and still notifies
	if ran != 2 {
		t.Fatalf("the observer must run on every pass, ran %d", ran)
	}
}
