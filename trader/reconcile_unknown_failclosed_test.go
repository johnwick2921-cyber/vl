// CTO addendum 2 (2026-09-25, CI job Backend Tests at 83f9fe1b): the reconcile
// consumer logged 'positions read failed — reconcile treats as flat' — the
// exact unknown-as-flat reading F4 exists to kill. ntHeldPosition returned ""
// on a read error and reconcileBeforeOpenNTReport read "" as "NT8 flat →
// proceed": the open went ahead on an UNREADABLE book. Fail-closed contract:
// unknown refuses the open (and never submits a flatten); only a POSITIVE
// empty book is flat.
package trader

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReconcileBeforeOpenNTUnknownPositionsRefuses(t *testing.T) {
	at := &AutoTrader{
		id:       "recon-unknown",
		exchange: "ninjatrader",
		trader:   &stubTrader{err: errors.New("NT8 account positions unknown: no account snapshot or confirmed entry fill")},
	}
	flattenSent, err := at.reconcileBeforeOpenNTReport("MNQ", "long")
	if err == nil {
		t.Fatal("an UNKNOWN positions read must refuse the open — reconcile treated it as flat and proceeded")
	}
	if flattenSent {
		t.Fatal("unknown must refuse BEFORE any flatten is submitted (do nothing destructive on an unreadable book)")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("the refusal must say unknown, got: %v", err)
	}
}

func TestReconcileBeforeOpenNTKnownFlatProceeds(t *testing.T) {
	at := &AutoTrader{
		id:       "recon-flat",
		exchange: "ninjatrader",
		trader:   &stubTrader{positions: []map[string]interface{}{}}, // known-flat
	}
	flattenSent, err := at.reconcileBeforeOpenNTReport("MNQ", "long")
	if err != nil {
		t.Fatalf("a POSITIVE empty book is flat and must proceed, got err=%v", err)
	}
	if flattenSent {
		t.Fatal("a flat book must not submit a flatten")
	}
}

// flattenWaitStub holds an opposite-side position on the FIRST read (the
// flatten trigger) and errors on every read after — the shape of the CTO's
// m10 mutant: an UNKNOWN read mid-flatten must never green-light the open.
type flattenWaitStub struct {
	stubTrader
	mu    sync.Mutex
	calls int
}

func (s *flattenWaitStub) GetPositions() ([]map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return []map[string]interface{}{{"symbol": "MNQ", "side": "short", "positionAmt": -1.0}}, nil
	}
	return nil, errors.New("NT8 account positions unknown: no account snapshot or confirmed entry fill")
}

// TestReconcileFlattenWaitNeverDeclaresFlatOnUnknown (CTO 2026-09-25 09:43Z,
// m10 survivor): the wait-for-flat loop promised "keep waiting, never declare
// flat on an error" and nothing pinned it. A GetPositions error mid-flatten
// must NOT green-light the open — the call must refuse at the deadline.
// RED = the m10 mutant (drop the hErr guard): the loop returns (true, nil)
// "flattened + confirmed flat" on the FIRST unknown read.
func TestReconcileFlattenWaitNeverDeclaresFlatOnUnknown(t *testing.T) {
	// Shrink the real-clock wait: the production 35 s deadline × 500 ms poll is a
	// fixed sleep; the fail-closed contract this pins is the SAME at any timeout.
	prevTimeout, prevPoll := reconcileFlattenTimeout, reconcileFlattenPollInterval
	reconcileFlattenTimeout, reconcileFlattenPollInterval = 100*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { reconcileFlattenTimeout, reconcileFlattenPollInterval = prevTimeout, prevPoll })

	stub := &flattenWaitStub{}
	at := &AutoTrader{id: "recon-wait-unknown", exchange: "ninjatrader", trader: stub}
	flattenSent, err := at.reconcileBeforeOpenNTReport("MNQ", "long")
	if !flattenSent {
		t.Fatal("fixture: the held opposite side must trigger the flatten")
	}
	if err == nil {
		t.Fatal("an UNKNOWN positions read during the flatten wait must refuse at the deadline — the call returned (true, nil) 'flattened + confirmed flat'")
	}
	if !strings.Contains(err.Error(), "flatten not confirmed flat") {
		t.Fatalf("the refusal must be the deadline refusal, got %v", err)
	}
}
