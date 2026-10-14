package trader

import (
	"errors"
	"testing"
	"time"

	"vl/store"
	"vl/telemetry"
)

// liveLoop reports whether at still has a live armed event loop — the loop
// that drives the mentor evaluator (mentorEventPassAt) and the armed pass.
func liveLoop(at *AutoTrader) bool { return at.armedEvent.Load() != nil }

// isRunningNow reads the run flag under its own mutex (GetStatus also reads
// cycle fields runCycle writes unlocked — a pre-existing race the -race run
// flags when a test polls it mid-cycle).
func isRunningNow(at *AutoTrader) bool {
	at.isRunningMutex.RLock()
	defer at.isRunningMutex.RUnlock()
	return at.isRunning
}

// TestReloadRaceOneLiveRunPerTraderID — MENTOR-NY item 3, the 10-13 16:35
// reload sequence at the production call site (AutoTrader.Run over a real TCP
// wire): AT1 (request A's instance) is running and owns the ordered slot; the
// reload (request B) dropped AT1 from the roster WITHOUT a Stop and starts a
// fresh AT2 for the SAME trader_id. Before the fix AT2's Run was refused by the
// ordered-owner check after it had already started its armed event loop, so
// TWO live event loops (= two mentor evaluators) ran for one trader_id. The
// rule: exactly ONE live run per trader_id, the old one stopped (and exited)
// before the new one starts.
// MUTANT: skip the claim (claimRunOwner returns ok without stopping the old
// run) → RED.
func TestReloadRaceOneLiveRunPerTraderID(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	h := newRunHarness(t)
	if err := h.st.Trader().Create(&store.Trader{ID: "t1", UserID: "u", Name: "hoang", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true}); err != nil {
		t.Fatal(err)
	}
	at1 := h.mk("t1")
	run1 := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }()
		run1 <- at1.Run()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !installed(at1) && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !installed(at1) || !liveLoop(at1) {
		t.Fatal("AT1 never reached its loop")
	}

	at2 := h.mk("t1") // the reload's fresh instance, same trader_id
	run2 := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }()
		run2 <- at2.Run()
	}()
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if installed(at2) && !liveLoop(at1) && !isRunningNow(at1) {
			break
		}
		select {
		case err := <-run2:
			run2 <- err // AT2's Run returned early (the pre-fix refusal)
			deadline = time.Now()
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	var run2Err error
	select {
	case run2Err = <-run2:
		run2 <- run2Err
	default:
	}
	loops := 0
	for _, at := range []*AutoTrader{at1, at2} {
		if liveLoop(at) {
			loops++
		}
	}
	if loops != 1 || !liveLoop(at2) || liveLoop(at1) {
		t.Fatalf("one trader_id must have exactly ONE live event loop (the new instance's): AT1 loop=%v running=%v · AT2 loop=%v running=%v installed=%v · AT2 Run returned %v",
			liveLoop(at1), isRunningNow(at1), liveLoop(at2), isRunningNow(at2), installed(at2), run2Err)
	}
	if isRunningNow(at1) || !isRunningNow(at2) {
		t.Fatalf("the OLD run must be stopped and the NEW one running: AT1=%v AT2=%v", isRunningNow(at1), isRunningNow(at2))
	}
	select {
	case <-run1:
	case <-time.After(5 * time.Second):
		t.Fatal("AT1's Run must have EXITED before AT2 started")
	}
	if got := liveRunOwner("t1"); got != at2 {
		t.Fatalf("the run owner of t1 must be AT2, got %p (AT1 %p, AT2 %p)", got, at1, at2)
	}
	if _, tbl := telemetry.GateBlockSnapshot(); tbl["t1"]["trader_run_replaced_old_instance"] != 1 {
		t.Fatalf("the replacement must be counted once, got %v", tbl["t1"])
	}

	at2.Stop()
	select {
	case <-run2:
	case <-time.After(5 * time.Second):
		t.Fatal("AT2's Run did not exit after Stop")
	}
	if liveRunOwner("t1") != nil {
		t.Fatal("the claim must be released when the run exits")
	}
}

// TestRefusedRunLeavesNoEventLoop — MENTOR-NY item 3, the 10-13 16:35:48 leak
// at Run's ordered-owner refusal path: a start refused because ANOTHER trader
// owns the (symbol, account) must tear down everything it started — before the
// fix only the stop monitor was cancelled and the refused instance's armed
// event loop (its mentor evaluator) kept running.
// MUTANT: drop teardownRunStarted from the ordered-install refusal path → RED.
func TestRefusedRunLeavesNoEventLoop(t *testing.T) {
	h := newRunHarness(t)
	for _, tr := range []*store.Trader{
		{ID: "owner", UserID: "u", Name: "owner", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
		{ID: "dup", UserID: "u", Name: "dup", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true},
	} {
		if err := h.st.Trader().Create(tr); err != nil {
			t.Fatal(err)
		}
	}
	owner := h.mk("owner")
	ownerDone := make(chan struct{})
	go func() {
		defer func() { _ = recover() }()
		defer close(ownerDone)
		_ = owner.Run()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !installed(owner) && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !installed(owner) {
		t.Fatal("the owner never installed")
	}
	dup := h.mk("dup")
	err := dup.Run()
	if err == nil {
		t.Fatal("the duplicate (symbol, account) start must be refused")
	}
	if liveLoop(dup) {
		t.Fatalf("a refused start left its armed event loop running (a second live mentor evaluator): %v", err)
	}
	if isRunningNow(dup) || liveRunOwner("dup") != nil {
		t.Fatalf("a refused start must leave nothing running or claimed: running=%v owner=%p", isRunningNow(dup), liveRunOwner("dup"))
	}
	if !liveLoop(owner) || !isRunningNow(owner) {
		t.Fatal("the owner must be untouched by the refused duplicate")
	}
	owner.Stop()
	<-ownerDone
}

// TestRetiredInstanceNeverRuns — a Retire()d instance's Run is refused before
// any side effect, with the no-op sentinel (never is_running=0 over the
// roster's current instance), and counted.
func TestRetiredInstanceNeverRuns(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	h := newRunHarness(t)
	at := h.mk("t9")
	at.Retire()
	runErr := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }()
		runErr <- at.Run()
	}()
	var err error
	select {
	case err = <-runErr:
	case <-time.After(3 * time.Second):
		at.Stop()
		t.Fatal("a retired instance RAN (its Run did not return — it entered its loop)")
	}
	if !errors.Is(err, ErrTraderInstanceRetired) || !errors.Is(err, ErrTraderAlreadyRunning) {
		t.Fatalf("a retired instance must be refused with ErrTraderInstanceRetired (wrapping ErrTraderAlreadyRunning), got %v", err)
	}
	if liveLoop(at) || isRunningNow(at) || installed(at) || liveRunOwner("t9") != nil {
		t.Fatal("a retired instance must start nothing")
	}
	if _, tbl := telemetry.GateBlockSnapshot(); tbl["t9"]["trader_run_refused_retired"] != 1 {
		t.Fatalf("the refusal must be counted, got %v", tbl["t9"])
	}
}

// TestRunPanicLeavesNoLiveLoop — MENTOR-NY item 3: a Run that PANICS out (here
// time.NewTicker(0) right after the ordered install, the old harness's
// accident) must not leave its armed event loop, its ordered install or its
// run claim behind — otherwise the next instance of the trader_id claims while
// a zombie evaluator still runs. The panic still propagates to the caller.
// MUTANT: drop the recover-teardown defer in Run → RED.
func TestRunPanicLeavesNoLiveLoop(t *testing.T) {
	h := newRunHarness(t)
	at := h.mk("tp")
	at.config.ScanInterval = 0 // Run panics at NewTicker after the install
	panicked := make(chan interface{}, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_ = at.Run()
	}()
	select {
	case r := <-panicked:
		if r == nil {
			t.Fatal("precondition: Run must panic with a zero scan interval")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run neither panicked nor returned")
	}
	if liveLoop(at) || installed(at) || isRunningNow(at) || liveRunOwner("tp") != nil {
		t.Fatalf("a panicked Run left state behind: loop=%v installed=%v running=%v owner=%p",
			liveLoop(at), installed(at), isRunningNow(at), liveRunOwner("tp"))
	}
}
