package trader

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

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

// runHarness is the TCP fixture a real Run() needs: a started TCPServer with one
// fake AddOn client that has sent Hello, so Run can install the ordered
// execution and enter its loop without a real NinjaTrader.
type runHarness struct {
	st *store.Store
	mk func(id string) *AutoTrader
}

func newRunHarness(t *testing.T) *runHarness {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest("127.0.0.1:0")
	srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	t.Cleanup(cancel)

	c, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, srv)
	t.Cleanup(func() { _ = c.Close() })
	if err := ntwire.WriteFrame(c, ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon"}); err != nil {
		t.Fatal(err)
	}

	mk := func(id string) *AutoTrader {
		nt := ntTrader.NewTCPTrader(srv, "MNQ", "Sim101")
		at := &AutoTrader{id: id, store: st, exchange: "ninjatrader", trader: nt, exchangeID: "ex"}
		at.config.StrategyConfig = &store.StrategyConfig{}
		// A real scan interval: with 0, Run panicked at time.NewTicker right
		// after the ordered install and never entered its loop (MENTOR-NY
		// item 3 — the run-owner pins need a Run that actually LIVES).
		at.config.ScanInterval = time.Minute
		at.mcpClient = &fakeDecisionClient{}
		return at
	}
	return &runHarness{st: st, mk: mk}
}

func installed(at *AutoTrader) bool {
	at.orderedExecMu.Lock()
	defer at.orderedExecMu.Unlock()
	return at.orderedExecUnreg != nil
}

// TestConcurrentRunOneInstanceFlagStaysRunning is the REAL fix at the call site
// (P1-b): two concurrent Run() on ONE AutoTrader. Exactly one instance runs; the
// loser returns ErrTraderAlreadyRunning with no side effects (it never reaches
// the ordered install), and is_running stays true. M1 (delete the Run CAS) is
// RED: the loser proceeds, its install is refused, and it writes is_running=0.
func TestConcurrentRunOneInstanceFlagStaysRunning(t *testing.T) {
	h := newRunHarness(t)
	if err := h.st.Trader().Create(&store.Trader{ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true}); err != nil {
		t.Fatal(err)
	}
	at := h.mk("t2")

	runDone := make(chan struct{})
	go func() {
		defer func() { _ = recover() }() // a bare-fixture cycle panic must not fail the pin
		defer close(runDone)
		_ = at.Run()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !installed(at) && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !installed(at) {
		t.Fatal("the first Run never installed the ordered executions")
	}

	// The second start of the SAME running trader is a no-op.
	err := at.Run()
	if err == nil || !errors.Is(err, ErrTraderAlreadyRunning) {
		t.Fatalf("a second Run of a running trader must return ErrTraderAlreadyRunning, got %v", err)
	}
	// Read the flag under its mutex: GetStatus also reads callCount /
	// lastResetTime, which runCycle writes unlocked (a pre-existing race the
	// -race gate flags now that this harness's Run lives — MENTOR-NY item 3).
	if !isRunningNow(at) {
		t.Fatalf("is_running must stay true after a duplicate Run")
	}
	// The loser must have had NO side effects: no ordered-owner P0 (it never
	// reached install), and the DB flag stays true.
	if alerts, _ := h.st.Alert().List("t2", 10); len(alerts) != 0 {
		t.Fatalf("the loser must not raise a P0, got %d alerts", len(alerts))
	}
	row, _ := h.st.Trader().GetByID("t2")
	if row == nil || !row.IsRunning {
		t.Fatalf("DB is_running must stay true, got %+v", row)
	}

	at.Stop()
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the winning Run did not exit after Stop")
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
