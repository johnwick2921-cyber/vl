package trader

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// newRunningFlagFixture builds the same harness ordered_exec_owner_test.go uses:
// a store with the given traders and a wire whose ordered owner is pre-registered
// to ownerID on (MNQ, Sim101).
func newRunningFlagFixture(t *testing.T, ownerID string, traders ...*store.Trader) (*store.Store, *ntwire.TCPServer) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "running-flag.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, tr := range traders {
		if err := st.Trader().Create(tr); err != nil {
			t.Fatal(err)
		}
	}
	srv := ntwire.NewTCPServer(nil)
	if ownerID != "" {
		if _, err := srv.RegisterOrderedExecutionsFor("MNQ", "Sim101", ntwire.OrderedExecutionHandlers{TraderID: ownerID}); err != nil {
			t.Fatalf("pre-register owner: %v", err)
		}
	}
	return st, srv
}

func runningFlagTrader(st *store.Store, srv *ntwire.TCPServer, id string) *AutoTrader {
	nt := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
	return &AutoTrader{
		id: id, name: "trader-" + id, exchange: "ninjatrader", exchangeID: "ex",
		trader: nt, store: st, userID: "u",
		config: AutoTraderConfig{
			NinjaTraderSymbol: "MNQ",
			StrategyConfig:    &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}},
		},
	}
}

// TestDuplicateSelfStartDoesNotClearRunningFlag pins FIX-RUNNING-FLAG: when the
// ordered-execution slot is already held by THIS SAME trader (a second start of
// a running trader), the install must return ErrTraderAlreadyRunning and must
// NOT write is_running=0 nor raise a P0. RED at base: the refusal wrote
// is_running=0 for the live trader and raised a P0.
func TestDuplicateSelfStartDoesNotClearRunningFlag(t *testing.T) {
	st, srv := newRunningFlagFixture(t, "t2",
		&store.Trader{ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
	)
	at := runningFlagTrader(st, srv, "t2")

	err := at.installNTOrderedExecutions(at.trader.(*ntTrader.TCPTrader))
	if err == nil || !errors.Is(err, ErrTraderAlreadyRunning) {
		t.Fatalf("a self-duplicate install must return ErrTraderAlreadyRunning, got %v", err)
	}
	row, _ := st.Trader().GetByID("t2")
	if row == nil || !row.IsRunning {
		t.Fatalf("the live trader's is_running must stay true after a duplicate self-start, got %+v", row)
	}
	if alerts, _ := st.Alert().List("t2", 10); len(alerts) != 0 {
		t.Fatalf("a self-duplicate must NOT raise a P0, got %d alerts", len(alerts))
	}
}

// TestConcurrentSelfStartsOneInstanceFlagStaysRunning pins the race: two
// concurrent installs for the SAME trader yield exactly one winner and one
// ErrTraderAlreadyRunning loser, and is_running stays true. RED at base: the
// loser wrote is_running=0.
func TestConcurrentSelfStartsOneInstanceFlagStaysRunning(t *testing.T) {
	st, srv := newRunningFlagFixture(t, "",
		&store.Trader{ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
	)
	at := runningFlagTrader(st, srv, "t2")
	nt := at.trader.(*ntTrader.TCPTrader)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var nils, dups int
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := at.installNTOrderedExecutions(nt); err == nil {
				mu.Lock()
				nils++
				mu.Unlock()
			} else if errors.Is(err, ErrTraderAlreadyRunning) {
				mu.Lock()
				dups++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if nils != 1 || dups != 1 {
		t.Fatalf("two concurrent self-starts must yield exactly one winner and one ErrTraderAlreadyRunning loser, got %d winners %d duplicates", nils, dups)
	}
	row, _ := st.Trader().GetByID("t2")
	if row == nil || !row.IsRunning {
		t.Fatalf("is_running must stay true across concurrent self-starts, got %+v", row)
	}
}

// TestOrderedOwnerConflictLeavesOwnerFlagUntouched pins the REL18-C O2 side: a
// start refused for a duplicate (symbol, account) clears the REFUSED trader's
// flag but leaves the OWNING trader's flag untouched.
func TestOrderedOwnerConflictLeavesOwnerFlagUntouched(t *testing.T) {
	st, srv := newRunningFlagFixture(t, "t1",
		&store.Trader{ID: "t1", UserID: "u", Name: "trader-1", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
		&store.Trader{ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
	)
	at2 := runningFlagTrader(st, srv, "t2")

	err := at2.installNTOrderedExecutions(at2.trader.(*ntTrader.TCPTrader))
	if err == nil || !errors.Is(err, ntwire.ErrOrderedOwnerExists) {
		t.Fatalf("install must fail with ErrOrderedOwnerExists, got %v", err)
	}
	refused, _ := st.Trader().GetByID("t2")
	owner, _ := st.Trader().GetByID("t1")
	if refused == nil || refused.IsRunning {
		t.Fatalf("the refused trader t2 must be marked not-running, got %+v", refused)
	}
	if owner == nil || !owner.IsRunning {
		t.Fatalf("the owning trader t1's flag must be untouched, got %+v", owner)
	}
}

// TestStopClearsRunningFlag pins "stop → 0": after Stop the in-memory flag is
// false, and the Run CAS must allow a restart (a stopped trader is not
// "already running"). Guards the FIX-RUNNING-FLAG idempotency against blocking
// a legitimate restart.
func TestStopClearsRunningFlag(t *testing.T) {
	at := &AutoTrader{id: "t", name: "trader", exchange: "ninjatrader"}
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()

	at.Stop()
	if running, _ := at.GetStatus()["is_running"].(bool); running {
		t.Fatalf("Stop must clear the in-memory running flag")
	}
	// The CAS must now allow a fresh Run — the flag is false again.
	at.isRunningMutex.RLock()
	stillRunning := at.isRunning
	at.isRunningMutex.RUnlock()
	if stillRunning {
		t.Fatalf("after Stop, isRunning must be false so Run can restart")
	}
}

